package middleend

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// These values match the production configuration in pkg/config/middleend.go.
// The larger valid snapshot needs more dial waves than the preparation budget
// permits, so the test observes the phase cutoff rather than endpoint exhaustion.
func deadlineGenerationTestFactory(
	t *testing.T,
	healthyDC DCID,
) (*GnetGenerationFactory, *generationFactoryTestLinkBuilder, map[DCID]time.Time) {
	t.Helper()
	const dcCount = 24
	endpoints := make(map[DCID][]netip.AddrPort, dcCount)
	for dcID := DCID(1); dcID <= dcCount; dcID++ {
		for port := range 4 {
			endpoints[dcID] = append(endpoints[dcID], netip.AddrPortFrom(
				netip.AddrFrom4([4]byte{192, 0, 2, byte(dcID)}), uint16(8888+port)))
		}
	}
	firstAttempts := make(map[DCID]time.Time, dcCount)
	var attemptsMu sync.Mutex
	base := &generationFactoryTestDialer{}
	dialer := partialGenerationTestDialer(func(ctx context.Context, endpoint netip.AddrPort, timeout time.Duration) (*net.TCPConn, netip.AddrPort, netip.AddrPort, error) {
		if cause := context.Cause(ctx); cause != nil {
			return nil, netip.AddrPort{}, netip.AddrPort{}, cause
		}
		dcID := DCID(endpoint.Addr().As4()[3])
		attemptsMu.Lock()
		if firstAttempts[dcID].IsZero() {
			firstAttempts[dcID] = time.Now()
		}
		attemptsMu.Unlock()
		if dcID == healthyDC {
			return base.Dial(ctx, endpoint, timeout)
		}
		dialContext, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		<-dialContext.Done()
		return nil, netip.AddrPort{}, netip.AddrPort{}, context.Cause(dialContext)
	})
	builder := &generationFactoryTestLinkBuilder{}
	factory := newGenerationFactoryForTest(t, generationFactoryTestSnapshot(endpoints), dialer, builder, func(config *GnetGenerationFactoryConfig) {
		config.EndpointDialTimeout = 3 * time.Second
		config.LinksPerDC = 4
		config.DialConcurrency = 8
		config.BindingLimits.MaxPendingControlItems = dcCount*4 + 1
	})
	return factory, builder, firstAttempts
}

func TestPartialGenerationConstructionCutoffPreservesEarlyAndLateDCs(t *testing.T) {
	for _, healthyDC := range []DCID{1, 24} {
		name := "healthy first"
		if healthyDC == 24 {
			name = "healthy last"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				factory, builder, firstAttempts := deadlineGenerationTestFactory(t, healthyDC)
				config := partialGenerationTestConfig()
				config.PreparationTimeout = 100 * time.Second
				config.ProbeInterval = 5 * time.Second
				config.ProbeFailureTimeout = 100 * time.Second
				supervisor := newGenerationTestSupervisor(t, config)
				started := time.Now()
				generation, err := supervisor.state.prepareGeneration(t.Context(), factory.Build)
				if err != nil {
					t.Fatalf("healthy signed DC %d lost partial admission: %v", healthyDC, err)
				}
				defer supervisor.state.closeGeneration(generation)
				if elapsed := time.Since(started); elapsed != initialGenerationConstructionTimeout {
					t.Fatalf("construction/admission took %v, want cutoff %v", elapsed, initialGenerationConstructionTimeout)
				}
				if len(firstAttempts) != 24 || firstAttempts[healthyDC].Sub(started) > 20*time.Second {
					t.Fatalf("DC-fair first replicas were starved: healthy first at %v, visited %d DCs",
						firstAttempts[healthyDC].Sub(started), len(firstAttempts))
				}
				if len(builder.snapshotLinks()) == 0 || len(generation.manager.Snapshot().Slots) != 96 {
					t.Fatal("phase cutoff lost a reachable link or configured failed-slot identities")
				}
				binding, err := generation.manager.Bind(healthyDC)
				if err != nil {
					t.Fatal(err)
				}
				if err := binding.Close(); err != nil {
					t.Fatal(err)
				}
				assertNoStartupCoverageLoss(t, supervisor)
			})
		})
	}
}

func TestPartialGenerationConstructionCutoffBoundsTotalFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		factory, builder, _ := deadlineGenerationTestFactory(t, 0)
		started := time.Now()
		manager, err := factory.Build(t.Context())
		if manager != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrGnetGenerationDial) {
			t.Fatalf("all-unreachable construction = (%v, %v)", manager, err)
		}
		if elapsed := time.Since(started); elapsed != initialGenerationConstructionTimeout {
			t.Fatalf("unbounded total failure took %v", elapsed)
		}
		if len(builder.snapshotLinks()) != 0 {
			t.Fatal("unreachable snapshot constructed a link")
		}
	})
}

func TestPartialGenerationShortBudgetReservesBootstrapAndAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		healthy := netip.MustParseAddrPort("192.0.2.1:8888")
		blocked := netip.MustParseAddrPort("192.0.2.2:8888")
		base := &generationFactoryTestDialer{}
		dialer := partialGenerationTestDialer(func(ctx context.Context, endpoint netip.AddrPort, timeout time.Duration) (*net.TCPConn, netip.AddrPort, netip.AddrPort, error) {
			if endpoint == healthy {
				return base.Dial(ctx, endpoint, timeout)
			}
			<-ctx.Done()
			return nil, netip.AddrPort{}, netip.AddrPort{}, context.Cause(ctx)
		})
		builder := &generationFactoryTestLinkBuilder{}
		factory := newGenerationFactoryForTest(t,
			generationFactoryTestSnapshot(map[DCID][]netip.AddrPort{1: {healthy}, 2: {blocked}}),
			dialer, builder, func(config *GnetGenerationFactoryConfig) {
				config.LinksPerDC = 3
				config.DialConcurrency = 8
			})
		config := partialGenerationTestConfig()
		config.PreparationTimeout = 4 * time.Second
		config.ProbeInterval = 5 * time.Second
		config.ProbeFailureTimeout = 100 * time.Second
		supervisor := newGenerationTestSupervisor(t, config)
		started := time.Now()
		generation, err := supervisor.state.prepareGeneration(t.Context(), func(ctx context.Context) (*FixedBindingManager, error) {
			manager, err := factory.Build(ctx)
			if err == nil {
				links := builder.snapshotLinks()
				links[1].startGate = make(chan struct{})
				links[2].afterTry = nil
			}
			return manager, err
		})
		if err != nil {
			t.Fatalf("one silent bootstrap and one silent probe discarded a verified sibling: %v", err)
		}
		defer supervisor.state.closeGeneration(generation)
		if elapsed := time.Since(started); elapsed != 3500*time.Millisecond {
			t.Fatalf("short preparation elapsed %v, want 3.5s with publication time remaining", elapsed)
		}
		if snapshot := generation.manager.Snapshot(); snapshot.DCs[0].ReadySlots != 1 {
			t.Fatalf("unverified short-budget links admitted: %+v", snapshot)
		}
	})
}

func TestPartialGenerationCallerAndRootCancellationRemainFatalAfterCutoff(t *testing.T) {
	for _, cancellation := range []string{"caller", "deadline", "root"} {
		t.Run(cancellation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				factory, builder, _ := deadlineGenerationTestFactory(t, 1)
				config := partialGenerationTestConfig()
				config.PreparationTimeout = 100 * time.Second
				supervisor := newGenerationTestSupervisor(t, config)
				parent, cancelParent := context.WithCancel(t.Context())
				defer cancelParent()
				wantError := context.Canceled
				if cancellation == "deadline" {
					var cancelDeadline context.CancelFunc
					parent, cancelDeadline = context.WithTimeout(parent, 90*time.Second)
					defer cancelDeadline()
					wantError = context.DeadlineExceeded
				}
				started := time.Now()
				generation, err := supervisor.state.prepareGeneration(parent, func(ctx context.Context) (*FixedBindingManager, error) {
					manager, err := factory.Build(ctx)
					if err != nil {
						return manager, err
					}
					switch cancellation {
					case "root":
						supervisor.state.cancelRoot()
					case "caller":
						cancelParent()
					case "deadline":
						// A factory can return a manager after caller expiry.
						// Retaining constructed links must not weaken that deadline.
						<-parent.Done()
					}
					return manager, nil
				})
				if generation != nil || !errors.Is(err, wantError) {
					t.Fatalf("%s cancellation escaped partial preparation: (%v, %v)", cancellation, generation, err)
				}
				if len(builder.snapshotLinks()) == 0 {
					t.Fatal("cancellation test never retained a healthy constructed sibling")
				}
				for _, link := range builder.snapshotLinks() {
					if !channelClosed(link.Done()) {
						t.Fatal("cancellation leaked an earlier constructed link")
					}
				}
				if elapsed := time.Since(started); elapsed > 100*time.Second {
					t.Fatalf("cancellation cleanup exceeded preparation lifetime: %v", elapsed)
				}
			})
		})
	}
}
