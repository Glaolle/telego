package middleend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// The default 100s generation budget leaves three existing 10s windows after
// construction: bootstrap, admission probes, and publication/cleanup margin.
// A caller deadline can shorten this construction ceiling further.
const initialGenerationConstructionTimeout = 70 * time.Second

// Each phase leaves time for later phases. For short caller deadlines, reserve
// at most half the remaining time so construction and bootstrap can still run.
// Only the returned context owns this cutoff: cancellation of parent remains
// fatal, even if an earlier phase already constructed a usable link.
func generationPreparationPhaseContext(
	parent context.Context,
	maximum time.Duration,
	remainingWindows int,
) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		reserve := min(time.Duration(remainingWindows)*slotReplacementPreparationTimeout, remaining/2)
		maximum = min(maximum, remaining-reserve)
	}
	return context.WithTimeout(parent, maximum)
}

// Root cancellation can precede its AfterFunc propagation to the preparation
// context. Neither lifecycle boundary can retain a candidate in that interval.
func generationPreparationCause(prepare, root context.Context) error {
	if cause := context.Cause(prepare); cause != nil {
		return cause
	}
	return context.Cause(root)
}

// A failed dial still owns one stable physical position. This immutable closed
// link gives the manager normal consumer, diagnostic, and cleanup ownership
// until the existing repair worker publishes a verified replacement.
type failedGenerationLink struct {
	cause    error
	events   <-chan LinkEvent
	capacity <-chan struct{}
	done     <-chan struct{}
}

func newFailedGenerationLink(cause error) *failedGenerationLink {
	events := make(chan LinkEvent)
	done := make(chan struct{})
	close(events)
	close(done)
	return &failedGenerationLink{
		cause: cause, events: events, capacity: make(chan struct{}, 1), done: done,
	}
}

func (*failedGenerationLink) String() string                     { return "middleend.failedGenerationLink{redacted}" }
func (l *failedGenerationLink) GoString() string                 { return l.String() }
func (l *failedGenerationLink) Start(context.Context) error      { return l.cause }
func (*failedGenerationLink) TrySubmit(LinkSubmission) error     { return ErrLinkClosed }
func (l *failedGenerationLink) SubmissionReady() <-chan struct{} { return l.capacity }
func (l *failedGenerationLink) Events() <-chan LinkEvent         { return l.events }
func (*failedGenerationLink) Snapshot() LinkSnapshot             { return LinkSnapshot{State: LinkStateClosed} }
func (l *failedGenerationLink) Done() <-chan struct{}            { return l.done }
func (l *failedGenerationLink) Err() error                       { return l.cause }
func (*failedGenerationLink) Close() error                       { return nil }

// Partial startup bounds each bootstrap independently, leaving time for other
// surviving slots and their admission probes within the generation deadline.
func (m *fixedBindingManager) startInitialSlot(ctx context.Context, slot *fixedBindingSlot) error {
	if !m.partialStartup {
		return slot.link.Start(ctx)
	}
	ctx, cancel := generationPreparationPhaseContext(ctx, slotReplacementPreparationTimeout, 2)
	defer cancel()
	return slot.link.Start(ctx)
}

// The caller holds manager.mu. Admission probes must finish for every surviving
// candidate slot before publication. A closed engine never supplies coverage,
// even when its Events consumer has not yet recorded the terminal state.
func (m *fixedBindingManager) readyCoverageLocked() map[DCID]struct{} {
	coverage := make(map[DCID]struct{}, len(m.slotGroups))
	for _, slot := range m.order {
		if !slot.failed && !slot.retired && !channelClosed(slot.link.Done()) {
			coverage[slot.dcID] = struct{}{}
		}
	}
	return coverage
}

func (*generationSupervisorState) probePartialGeneration(
	parent context.Context,
	generation *supervisedGeneration,
	timeout time.Duration,
) error {
	ctx, cancel := generationPreparationPhaseContext(parent, min(timeout, slotReplacementPreparationTimeout), 1)
	defer cancel()
	m := generation.manager.state
	m.mu.Lock()
	slots := slices.Clone(m.order)
	m.mu.Unlock()
	results := make([]error, len(slots))
	var probes sync.WaitGroup
	for index, slot := range slots {
		probes.Go(func() {
			err := m.probeSlot(ctx, slot.dcID, slot)
			results[index] = err
			if err != nil && context.Cause(parent) == nil {
				reason := FixedBindingSlotFailureControlSubmission
				if errors.Is(err, context.DeadlineExceeded) {
					reason = FixedBindingSlotFailureProbeTimeout
				}
				m.failSlot(slot, err, reason)
			}
		})
	}
	probes.Wait()
	if cause := context.Cause(parent); cause != nil {
		return fmt.Errorf("%w: %w", ErrGenerationProbe, cause)
	}
	m.mu.Lock()
	ready := m.state == fixedBindingManagerReady && m.accepting && len(m.readyCoverageLocked()) != 0
	m.mu.Unlock()
	if ready {
		return nil
	}
	for index, err := range results {
		if err != nil {
			return fmt.Errorf("%w: DC %d: %w", ErrGenerationProbe, slots[index].dcID, err)
		}
	}
	return ErrGenerationProbe
}

var _ ClientLink = (*failedGenerationLink)(nil)
