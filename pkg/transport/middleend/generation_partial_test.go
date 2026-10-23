package middleend

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func partialGenerationTestConfig() GenerationSupervisorConfig {
	config := generationTestConfig()
	config.PreparationTimeout = time.Minute
	config.ProbeInterval = 5 * time.Millisecond
	config.ProbeFailureTimeout = 20 * time.Millisecond
	return config
}

func partialGenerationTestManager(t *testing.T, slots []FixedBindingSlot, repair fixedBindingSlotRepair) *FixedBindingManager {
	t.Helper()
	manager, err := newFixedBindingManager(slots, fixedBindingTestLimits(), repair)
	if err != nil {
		t.Fatal(err)
	}
	manager.state.partialStartup = true
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	return manager
}

func assertNoStartupCoverageLoss(t *testing.T, supervisor *FixedBindingGenerationSupervisor) {
	t.Helper()
	for _, dc := range supervisor.Snapshot().DCs {
		if dc.ZeroReadyTransitions != 0 {
			t.Fatalf("startup counted lost coverage: %+v", dc)
		}
	}
}

func TestPartialGenerationDialFailureRecoversExactSignedDCInPlace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		healthy := netip.MustParseAddrPort("192.0.2.1:8888")
		unreachable := netip.MustParseAddrPort("192.0.2.2:8888")
		dialer := &generationFactoryTestDialer{failures: map[netip.AddrPort]error{unreachable: errors.New("refused")}}
		builder := &generationFactoryTestLinkBuilder{}
		budget := responseBudgetForTest(t, ResponseBudgetConfig{LimitBytes: 4096})
		factory := newGenerationFactoryForTest(t,
			generationFactoryTestSnapshot(map[DCID][]netip.AddrPort{2: {healthy}, -203: {unreachable}}),
			dialer, builder, func(config *GnetGenerationFactoryConfig) {
				config.LinksPerDC = 2
				config.ResponseBudget = budget
			})
		supervisor := newGenerationTestSupervisor(t, partialGenerationTestConfig())
		var builds atomic.Int32
		if err := supervisor.Start(t.Context(), func(ctx context.Context) (*FixedBindingManager, error) {
			builds.Add(1)
			return factory.Build(ctx)
		}); err != nil {
			t.Fatal(err)
		}
		snapshot := supervisor.Snapshot()
		if !snapshot.Admitting || len(snapshot.Active.Slots) != 4 || !slices.Equal(snapshot.ActiveDCIDs, []DCID{-203, 2}) {
			t.Fatalf("partial admission lost configured topology: %+v", snapshot)
		}
		active := supervisor.state.active.manager
		if active.state.responseBudget != budget {
			t.Fatal("partial manager replaced the shared response budget")
		}
		binding, err := supervisor.Bind(2)
		if err != nil {
			t.Fatal(err)
		}
		originalLink := binding.state.slot.link
		originalLink.(*fixedBindingFakeLink).emit(LinkEvent{
			Kind: LinkEventProxyAnswer, ConnectionID: binding.ConnectionID(), Packet: []byte{1, 2, 3, 4},
		})
		synctest.Wait()
		if used := budget.Snapshot().UsedBytes; used == 0 || used > 4096 {
			t.Fatalf("partial generation response ownership = %d", used)
		}
		if _, err := supervisor.Bind(-203); !errors.Is(err, ErrFixedBindingSlotFailed) {
			t.Fatalf("unreachable signed DC admitted: %v", err)
		}
		if _, err := supervisor.Bind(203); !errors.Is(err, ErrFixedBindingUnknownDC) {
			t.Fatalf("negative DC aliased its positive identity: %v", err)
		}
		assertNoStartupCoverageLoss(t, supervisor)
		dialer.mu.Lock()
		delete(dialer.failures, unreachable)
		dialer.mu.Unlock()
		waitGenerationCondition(t, supervisor, time.Second, func(snapshot GenerationSupervisorSnapshot) bool {
			return snapshot.SlotRepairSuccesses == 2 && snapshot.Active != nil && snapshot.Active.RepairingSlots == 0
		})
		if builds.Load() != 1 || supervisor.state.active.manager != active || binding.state.slot.link != originalLink {
			t.Fatal("missing slot recovery replaced the generation or moved a healthy binding")
		}
		recovered, err := supervisor.Bind(-203)
		if err != nil || recovered.DCID() != -203 {
			t.Fatalf("recovered negative DC bind: %v", err)
		}
		if err := binding.Close(); err != nil {
			t.Fatal(err)
		}
		if err := recovered.Close(); err != nil {
			t.Fatal(err)
		}
		if err := supervisor.Close(); err != nil {
			t.Fatal(err)
		}
		if used := budget.Snapshot().UsedBytes; used != 0 {
			t.Fatalf("partial generation retained response ownership after shutdown: %d", used)
		}
	})
}

func TestPartialGenerationInitialFailuresPreserveHealthyLinks(t *testing.T) {
	for _, stage := range []string{"start", "event", "probe", "sink"} {
		for _, sibling := range []bool{false, true} {
			name := stage + "/other DC"
			if sibling {
				name = stage + "/same DC"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					good, bad := newFixedBindingFakeLink(), newFixedBindingFakeLink()
					respondToFixedBindingPings(good)
					const healthyDC DCID = -203
					badDC := DCID(2)
					if sibling {
						badDC = healthyDC
					}
					var badLink ClientLink = bad
					if stage == "start" {
						bad.startErr = errors.New("bootstrap failed")
					}
					if stage == "sink" {
						sink := newResponseSinkFakeLink()
						sink.installErr = errLinkResponseSink
						bad, badLink = sink.fixedBindingFakeLink, sink
					}
					if stage == "probe" {
						bad.afterTry = func(accepted bool) {
							if accepted {
								attempts := bad.attemptedSubmissions()
								ping, err := ParsePing(attempts[len(attempts)-1].Payload)
								if err == nil {
									bad.emit(LinkEvent{Kind: LinkEventPong, KeepaliveID: ping.ID + 1})
								}
							}
						}
					}
					replacement := newFixedBindingFakeLink()
					respondToFixedBindingPings(replacement)
					manager := partialGenerationTestManager(t,
						[]FixedBindingSlot{{DCID: healthyDC, Link: good}, {DCID: badDC, Link: badLink}},
						refreshCandidateFactory(replacement))
					if stage == "event" {
						bad.onStart = func() {
							bad.peerClose(io.EOF)
							// Keep Start in progress until the Events consumer records
							// the failure, exercising the asynchronous startup path.
							<-manager.state.order[1].consumerDone
						}
					}
					supervisor := newGenerationTestSupervisor(t, partialGenerationTestConfig())
					if err := supervisor.Start(t.Context(), func(context.Context) (*FixedBindingManager, error) { return manager, nil }); err != nil {
						t.Fatal(err)
					}
					if snapshot := supervisor.Snapshot(); !snapshot.Admitting || snapshot.SlotFailures != 1 || !snapshot.Active.Slots[1].Failed {
						t.Fatalf("initial failure removed healthy admission or counted twice: %+v", snapshot)
					}
					assertNoStartupCoverageLoss(t, supervisor)
					binding, err := supervisor.Bind(healthyDC)
					if err != nil || binding.state.slot.link != good {
						t.Fatalf("healthy initial link unavailable: %v", err)
					}
					if !sibling {
						if _, err := supervisor.Bind(badDC); !errors.Is(err, ErrFixedBindingSlotFailed) {
							t.Fatalf("unverified initial link admitted: %v", err)
						}
					}
					if err := manager.state.repairFailedSlots(t.Context()); err != nil {
						t.Fatal(err)
					}
					if binding.state.slot.link != good || supervisor.state.active.manager != manager {
						t.Fatal("initial failure recovery moved a healthy binding")
					}
					if err := binding.Close(); err != nil {
						t.Fatal(err)
					}
				})
			})
		}
	}
}

func TestPartialGenerationSilentStartupHasIndependentDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		good, silent := newFixedBindingFakeLink(), newFixedBindingFakeLink()
		respondToFixedBindingPings(good)
		silent.startGate = make(chan struct{})
		manager := partialGenerationTestManager(t, []FixedBindingSlot{{DCID: 2, Link: good}, {DCID: 3, Link: silent}}, nil)
		config := generationTestConfig()
		config.PreparationTimeout = time.Minute
		supervisor := newGenerationTestSupervisor(t, config)
		started := time.Now()
		if err := supervisor.Start(t.Context(), func(context.Context) (*FixedBindingManager, error) { return manager, nil }); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed != slotReplacementPreparationTimeout {
			t.Fatalf("silent startup elapsed %v, want %v", elapsed, slotReplacementPreparationTimeout)
		}
		if snapshot := supervisor.Snapshot(); !snapshot.Admitting || !snapshot.Active.Slots[1].Failed {
			t.Fatalf("silent bootstrap blocked healthy admission: %+v", snapshot)
		}
	})
}

func TestPartialGenerationTotalFailureCleansAllLinks(t *testing.T) {
	for _, stage := range []string{"dial", "start", "probe"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				supervisor := newGenerationTestSupervisor(t, partialGenerationTestConfig())
				if stage == "dial" {
					endpoint := netip.MustParseAddrPort("192.0.2.1:8888")
					dialer := &generationFactoryTestDialer{failures: map[netip.AddrPort]error{endpoint: errors.New("refused")}}
					builder := &generationFactoryTestLinkBuilder{}
					factory := newGenerationFactoryForTest(t, generationFactoryTestSnapshot(map[DCID][]netip.AddrPort{-203: {endpoint}}), dialer, builder, nil)
					if manager, err := factory.Build(t.Context()); manager != nil || !errors.Is(err, ErrGnetGenerationDial) {
						t.Fatalf("all failed dials = (%v, %v)", manager, err)
					}
					return
				}
				first, second := newFixedBindingFakeLink(), newFixedBindingFakeLink()
				if stage == "start" {
					first.startErr, second.startErr = io.EOF, io.EOF
				}
				manager := partialGenerationTestManager(t, []FixedBindingSlot{{DCID: 2, Link: first}, {DCID: -203, Link: second}}, nil)
				generation, err := supervisor.state.prepareGeneration(t.Context(), func(context.Context) (*FixedBindingManager, error) { return manager, nil })
				if generation != nil || err == nil || !channelClosed(manager.Done()) || !channelClosed(first.Done()) || !channelClosed(second.Done()) {
					t.Fatalf("total %s failure escaped cleanup: generation %v, error %v", stage, generation, err)
				}
				if supervisor.Snapshot().Admitting {
					t.Fatal("total failure advertised admission")
				}
				assertNoStartupCoverageLoss(t, supervisor)
			})
		})
	}
}

type partialGenerationTestDialer func(context.Context, netip.AddrPort, time.Duration) (*net.TCPConn, netip.AddrPort, netip.AddrPort, error)

func (f partialGenerationTestDialer) Dial(ctx context.Context, endpoint netip.AddrPort, timeout time.Duration) (*net.TCPConn, netip.AddrPort, netip.AddrPort, error) {
	return f(ctx, endpoint, timeout)
}

func TestPartialGenerationCancellationCleansHealthySiblings(t *testing.T) {
	for _, stage := range []string{"dial", "start", "probe"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if stage == "dial" {
					good, blocked := netip.MustParseAddrPort("192.0.2.1:8888"), netip.MustParseAddrPort("192.0.2.2:8888")
					base := &generationFactoryTestDialer{}
					dialer := partialGenerationTestDialer(func(ctx context.Context, endpoint netip.AddrPort, timeout time.Duration) (*net.TCPConn, netip.AddrPort, netip.AddrPort, error) {
						if endpoint == blocked {
							cancel()
							return nil, netip.AddrPort{}, netip.AddrPort{}, context.Cause(ctx)
						}
						return base.Dial(ctx, endpoint, timeout)
					})
					builder := &generationFactoryTestLinkBuilder{}
					factory := newGenerationFactoryForTest(t, generationFactoryTestSnapshot(map[DCID][]netip.AddrPort{1: {good}, 2: {blocked}}), dialer, builder,
						func(config *GnetGenerationFactoryConfig) { config.DialConcurrency = 1 })
					if manager, err := factory.Build(ctx); manager != nil || !errors.Is(err, context.Canceled) {
						t.Fatalf("canceled partial Build = (%v, %v)", manager, err)
					}
					for _, link := range builder.snapshotLinks() {
						if !channelClosed(link.Done()) {
							t.Fatal("construction cancellation leaked a healthy sibling")
						}
					}
					return
				}
				good, bad := newFixedBindingFakeLink(), newFixedBindingFakeLink()
				respondToFixedBindingPings(good)
				if stage == "start" {
					good.onStart = cancel
					bad.startGate = make(chan struct{})
				} else {
					respond := good.afterTry
					good.afterTry = func(accepted bool) { respond(accepted); cancel() }
				}
				manager := partialGenerationTestManager(t, []FixedBindingSlot{{DCID: 2, Link: good}, {DCID: -203, Link: bad}}, nil)
				supervisor := newGenerationTestSupervisor(t, partialGenerationTestConfig())
				generation, err := supervisor.state.prepareGeneration(ctx, func(context.Context) (*FixedBindingManager, error) { return manager, nil })
				if generation != nil || !errors.Is(err, context.Canceled) || !channelClosed(manager.Done()) {
					t.Fatalf("canceled %s preparation = (%v, %v)", stage, generation, err)
				}
				if !channelClosed(good.Done()) || !channelClosed(bad.Done()) {
					t.Fatal("cancellation leaked initial links")
				}
			})
		})
	}
}

func TestPartialGenerationRotationPreservesCoverageAndBindings(t *testing.T) {
	for _, losesCoverage := range []bool{false, true} {
		name := "new unavailable DC is allowed"
		if losesCoverage {
			name = "existing ready DC is required"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				oldDCs := []DCID{2}
				if losesCoverage {
					oldDCs = append(oldDCs, -203)
				}
				old := newGenerationTestManager(t, oldDCs, true)
				supervisor := newGenerationTestSupervisor(t, partialGenerationTestConfig())
				if err := supervisor.Start(t.Context(), func(context.Context) (*FixedBindingManager, error) { return old.manager, nil }); err != nil {
					t.Fatal(err)
				}
				binding, err := supervisor.Bind(2)
				if err != nil {
					t.Fatal(err)
				}
				good, bad := newFixedBindingFakeLink(), newFixedBindingFakeLink()
				respondToFixedBindingPings(good)
				bad.startErr = io.EOF
				candidate := partialGenerationTestManager(t, []FixedBindingSlot{{DCID: 2, Link: good}, {DCID: -203, Link: bad}}, nil)
				err = supervisor.Rotate(t.Context(), func(context.Context) (*FixedBindingManager, error) { return candidate, nil })
				if losesCoverage {
					if !errors.Is(err, ErrGenerationChanged) || supervisor.state.active.manager != old.manager || !old.manager.Snapshot().Accepting || !channelClosed(candidate.Done()) {
						t.Fatalf("coverage-reducing rotation escaped rejection: %v", err)
					}
				} else if err != nil || supervisor.state.active.manager != candidate || old.manager.Snapshot().Accepting {
					t.Fatalf("partial coverage-preserving rotation failed: %v", err)
				}
				if binding.state.slot.link != old.links[2] || channelClosed(old.links[2].Done()) {
					t.Fatal("partial rotation moved or closed an existing healthy binding")
				}
				if len(supervisor.state.sources) > 2 {
					t.Fatal("partial rotation exceeded two live managers")
				}
				if err := binding.Close(); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestPartialGenerationRotationSeesRepairBeforeObserver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		good, failed := newFixedBindingFakeLink(), newFixedBindingFakeLink()
		respondToFixedBindingPings(good)
		failed.startErr = io.EOF
		replacement := newFixedBindingFakeLink()
		respondToFixedBindingPings(replacement)
		old := partialGenerationTestManager(t, []FixedBindingSlot{{DCID: 2, Link: good}, {DCID: -203, Link: failed}}, refreshCandidateFactory(replacement))
		supervisor := newGenerationTestSupervisor(t, partialGenerationTestConfig())
		if err := supervisor.Start(t.Context(), func(context.Context) (*FixedBindingManager, error) { return old, nil }); err != nil {
			t.Fatal(err)
		}
		candidateGood, candidateBad := newFixedBindingFakeLink(), newFixedBindingFakeLink()
		respondToFixedBindingPings(candidateGood)
		candidateBad.startErr = io.EOF
		candidateManager := partialGenerationTestManager(t, []FixedBindingSlot{{DCID: 2, Link: candidateGood}, {DCID: -203, Link: candidateBad}}, nil)
		candidate, err := supervisor.state.prepareGeneration(t.Context(), func(context.Context) (*FixedBindingManager, error) { return candidateManager, nil })
		if err != nil {
			t.Fatal(err)
		}
		defer supervisor.state.closeGeneration(candidate)
		repairPublished, releaseObserver := make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(releaseObserver) })
		defer release()
		old.state.mu.Lock()
		observer := old.state.slotRepairObserver
		old.state.slotRepairObserver = func(success bool, record GenerationDiagnosticRecord) {
			if success {
				close(repairPublished)
				<-releaseObserver
			}
			observer(success, record)
		}
		old.state.mu.Unlock()
		repairResult := make(chan error, 1)
		go func() { repairResult <- old.state.repairFailedSlots(t.Context()) }()
		<-repairPublished
		// The manager has restored -203, while its supervisor observer still
		// waits. Publication must inspect the manager within its lock boundary.
		supervisor.state.mu.Lock()
		published := supervisor.state.publishGenerationLocked(candidate, supervisor.state.active)
		supervisor.state.mu.Unlock()
		if published || !old.Snapshot().Accepting {
			t.Fatal("rotation overlooked concurrently restored signed-DC coverage")
		}
		release()
		if err := <-repairResult; err != nil {
			t.Fatal(err)
		}
	})
}
