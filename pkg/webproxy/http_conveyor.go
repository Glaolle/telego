package webproxy

import (
	"context"
	"crypto/sha256"
	"math"
	"sync"
)

const httpUpWindow = 4

type upCommit struct {
	sequence uint64
	digest   [sha256.Size]byte
}

type httpUpBatch struct {
	digest  [sha256.Size]byte
	known   bool
	readers int
}

type httpUpLane struct {
	pending  map[uint64]*httpUpBatch
	notify   chan struct{}
	readers  int
	applying bool
}

// httpUpReservation owns the complete request body and prepaid application
// capacity. HTTP acquires it after authentication and before allocating a body.
// Application transfers the exact backend charge without releasing capacity
// that another session could consume between reservation and application.
type httpUpReservation struct {
	session               *Session
	lane                  *httpUpLane
	laneID                uint32
	sequence              uint64
	bodySize              int
	cost, items           int
	applyCost, applyItems int
	releaseOnce           func()
}

func (s *Session) reserveHTTPUp(laneID uint32, sequence uint64, size int) (*httpUpReservation, error) {
	if sequence == 0 || size < FrameHeaderSize || size > min(s.limits.MaxBodyBytes, maxCarrierBatchBytes) ||
		(s.carrier != CarrierHTTPS && s.carrier != CarrierHTTPSLanes) || laneID > MaxStreamID ||
		(s.carrier == CarrierHTTPS && laneID != 0) {
		return nil, ErrProtocol
	}
	applyItems := min(size/FrameHeaderSize, MaxBatchFrames)
	applyCost := size + applyItems*queueItemCost
	cost, items := size+queueItemCost+applyCost, 1+applyItems
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	committed := uint64(0)
	if lane := s.carrierLanes[laneID]; lane != nil {
		committed = lane.lastUpSequence
	}
	retired := s.retiredHTTPUpLaneLocked(laneID)
	if !retired && sequence > committed && sequence-committed > httpUpWindow {
		return nil, ErrProtocol
	}
	if !retired && sequence <= committed && (committed-sequence >= httpUpWindow || committed == 0) {
		return nil, ErrProtocol
	}
	lane := s.httpUpLanes[laneID]
	if lane == nil {
		if len(s.httpUpLanes) >= s.limits.MaxStreamsPerSession+1 {
			return nil, ErrBackpressure
		}
		lane = &httpUpLane{pending: make(map[uint64]*httpUpBatch), notify: make(chan struct{})}
	}
	if lane.readers >= 2*httpUpWindow || !s.reservePendingLocked(cost, items, pendingUplink) {
		return nil, ErrBackpressure
	}
	if s.httpUpLanes == nil {
		s.httpUpLanes = make(map[uint32]*httpUpLane)
	}
	s.httpUpLanes[laneID] = lane
	lane.readers++
	if sequence > committed {
		batch := lane.pending[sequence]
		if batch == nil {
			batch = &httpUpBatch{}
			lane.pending[sequence] = batch
		}
		batch.readers++
	}
	s.httpInputCost += cost
	s.httpInputItems += items
	reservation := &httpUpReservation{session: s, lane: lane, laneID: laneID, sequence: sequence, bodySize: size,
		cost: cost, items: items, applyCost: applyCost, applyItems: applyItems}
	reservation.releaseOnce = sync.OnceFunc(reservation.releaseOwned)
	return reservation, nil
}

func (r *httpUpReservation) Release() {
	if r == nil {
		return
	}
	r.releaseOnce()
}

func (r *httpUpReservation) releaseOwned() {
	s := r.session
	s.mu.Lock()
	defer s.mu.Unlock()
	s.httpInputCost -= r.cost
	s.httpInputItems -= r.items
	s.releasePendingLocked(r.cost, r.items)
	r.cost = 0
	r.items = 0
	r.applyCost = 0
	r.applyItems = 0
	r.lane.readers--
	if batch := r.lane.pending[r.sequence]; batch != nil {
		batch.readers--
		if batch.readers == 0 {
			delete(r.lane.pending, r.sequence)
		}
	}
	if r.lane.readers == 0 && s.httpUpLanes[r.laneID] == r.lane {
		delete(s.httpUpLanes, r.laneID)
	}
	r.lane.signal()
}

func (l *httpUpLane) signal() { close(l.notify); l.notify = make(chan struct{}) }

func (r *httpUpReservation) Process(ctx context.Context, body []byte) (uint64, error) {
	s := r.session
	if len(body) != r.bodySize {
		s.laneProtocolFailure(r.laneID, nil)
		return 0, ErrProtocol
	}
	digest := sha256.Sum256(body)
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return 0, ErrClosed
		}
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return 0, err
		}
		// Tombstone eviction retires replay history. The existing lane path
		// accepts only harmless late stream frames after that retirement.
		if s.retiredHTTPUpLaneLocked(r.laneID) {
			s.mu.Unlock()
			frames, err := parseUplinkBatch(body, r.laneID, true)
			if err != nil || !onlyLateLaneFrames(frames) {
				s.laneProtocolFailure(r.laneID, nil)
				return 0, ErrProtocol
			}
			s.recordCarrierRetry(carrierOperationUplink)
			return r.sequence, nil
		}
		lane := s.carrierLanes[r.laneID]
		committed := uint64(0)
		if lane != nil {
			committed = lane.lastUpSequence
		}
		if r.sequence <= committed {
			previous := lane.upHistory[r.sequence%httpUpWindow]
			matches := previous.sequence == r.sequence && previous.digest == digest
			s.mu.Unlock()
			if !matches {
				s.laneProtocolFailure(r.laneID, nil)
				return 0, ErrProtocol
			}
			s.recordCarrierRetry(carrierOperationUplink)
			return r.sequence, nil
		}
		batch := r.lane.pending[r.sequence]
		if batch == nil || batch.known && batch.digest != digest {
			s.mu.Unlock()
			s.laneProtocolFailure(r.laneID, nil)
			return 0, ErrProtocol
		}
		batch.digest = digest
		batch.known = true
		if committed != math.MaxUint64 && r.sequence == committed+1 && !r.lane.applying {
			r.lane.applying = true
			s.mu.Unlock()
			ack, err := s.processUp(r.laneID, r.sequence, body, s.carrier == CarrierHTTPSLanes, nil, r)
			s.mu.Lock()
			r.lane.applying = false
			if err == nil {
				delete(r.lane.pending, r.sequence)
			}
			r.lane.signal()
			s.mu.Unlock()
			return ack, err
		}
		changed := r.lane.notify
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-s.done:
			return 0, ErrClosed
		}
	}
}

func (s *Session) retiredHTTPUpLaneLocked(laneID uint32) bool {
	return s.carrier == CarrierHTTPSLanes && laneID != 0 &&
		s.carrierLanes[laneID] == nil && s.usedStreams.Contains(laneID)
}

// transferApplicationLocked moves prepaid capacity to backend queue ownership.
// The caller holds Session.mu. Unused application capacity becomes available
// only after the exact accepted backend charge has changed owners.
func (r *httpUpReservation) transferApplicationLocked(cost, items int) bool {
	if cost > r.applyCost || items > r.applyItems {
		return false
	}
	s := r.session
	s.httpInputCost -= r.applyCost
	s.httpInputItems -= r.applyItems
	s.releasePendingQuietLocked(r.applyCost-cost, r.applyItems-items)
	r.cost -= r.applyCost
	r.items -= r.applyItems
	r.applyCost = 0
	r.applyItems = 0
	return true
}
