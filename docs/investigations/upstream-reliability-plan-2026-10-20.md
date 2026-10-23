# Upstream reliability plan for v0.8.0

Date: 2026-10-20. Branch: `feat/upstream-reliability-v0.8.0`.

## Objective and scope

Improve transport reliability and prepare a VPS candidate for real network tests before release.
The user reports slow mobile messages and images after the proxy connects.
WebSocket throttling is a hypothesis. No measurement yet establishes its cause on the user network.

The approved changes are GREASE filtering, fragmented ClientHello support, partial Middle-End availability, an HTTPS conveyor, a TLS certificate diagnostic, and optional ServerHello segmentation.
The segmentation feature remains disabled by default.
Every release must update Go and all package dependencies to compatible stable versions.

Calls through WEB remain deferred until the upstream implementation merges.
Call padding and jitter remain deferred with that transport.
An indefinitely open upload with a single browser `fetch` is outside this plan.
Do not copy telemt implementation code.
Use upstream behavior as evidence and implement through existing telego ownership rules.

## Phase 0: Evidence and allowed APIs

### Sources

- [mtg GREASE change](https://github.com/9seconds/mtg/pull/446).
- [mtg fragmented TLS records](https://github.com/9seconds/mtg/pull/431).
- [mtg fragmentation follow-up](https://github.com/9seconds/mtg/pull/433/files).
- [telemt partial availability proposal](https://github.com/telemt/telemt/pull/959).
- [telemt HTTPS conveyor](https://github.com/telemt/telemt/pull/946).
- [mtg TLS diagnostic](https://github.com/9seconds/mtg/pull/549).
- [mtg ServerHello segmentation proposal](https://github.com/9seconds/mtg/pull/580).
- [tproxy carrier observations](https://github.com/telegramdesktop/tproxy-server/pull/19).
- [official Go downloads](https://go.dev/dl/?mode=json). The current stable toolchain is `go1.27.3`.

The local references below describe the source before implementation.
Line numbers can move after each phase.
Verify upstream state again before release notes describe an upstream change as merged.
The upstream TLS diagnostic diff was unavailable during initial discovery.
Its interface is not an implementation contract.

### Local contracts

| Area | Existing API or pattern | Source |
| --- | --- | --- |
| FakeTLS authentication | `ParseClientHello(secret []byte, payload []byte) (*ClientHello, error)` | `pkg/transport/faketls/handshake.go:54` |
| Cipher choice | `extractCipherSuite(payload []byte, sessionIDLen int) uint16` | `pkg/transport/faketls/handshake.go:136` |
| Synthetic cipher output | `buildServerHelloMessage(*ClientHello)` uses `ClientHello.CipherSuite` | `pkg/transport/faketls/handshake.go:649` |
| Native and logical input | `clientEndpoint.Peek`, `Discard`, `InboundBuffered`, and `Write` | `pkg/gproxy/client_endpoint.go:11` |
| TLS admission | `handleTLSHeader` and `handleTLSPayload` retain input until authentication | `pkg/gproxy/handshake.go:157`, `pkg/gproxy/handshake.go:198` |
| Splice ownership | `startSplice` retains original inbound bytes for the relay | `pkg/gproxy/splice.go:49`, `pkg/gproxy/splice.go:189` |
| CLI | Kong command fields and command `Run() error` methods | `cmd/telego/main.go:37`, `cmd/telego/main.go:437` |
| Fronting targets | `config.Load` and `ToGProxyConfig` resolve target defaults | `cmd/telego/main.go:52`, `pkg/config/config.go:325` |
| ME construction | `GnetGenerationFactory.Build(context.Context) (*FixedBindingManager, error)` | `pkg/transport/middleend/generation_factory.go:233` |
| ME initial failure | Factory and manager startup currently reject individual failed slots | `pkg/transport/middleend/generation_factory.go:274`, `pkg/transport/middleend/binding_manager.go:922` |
| ME startup event | A slot failure during startup currently terminalizes the manager | `pkg/transport/middleend/binding_manager.go:3697` |
| ME admission | `prepareGeneration`, `probeGeneration`, and `publishGenerationLocked` own publication | `pkg/transport/middleend/generation_supervisor.go:413`, `:737`, `:545` |
| ME recovery | `watchGenerationSlot` owns independent slot maintenance | `pkg/transport/middleend/generation_supervisor.go:644` |
| WEB uplink | `Session.ProcessUp` and `ProcessUpLane` | `pkg/webproxy/session.go:398`, `:406` |
| WEB downlink | `Session.PollCarrier` and `PollCarrierLane` return a `PollLease` | `pkg/webproxy/session.go:632`, `:641` |
| WEB budgets | Existing pending reservations and refunds | `pkg/webproxy/session.go:1280`, `:1321` |
| HTTP ownership | Request workers call session APIs and retain the downlink lease | `pkg/webproxy/http_server.go:1281`, `:1303` |
| Browser responses | `readResponse` handles finite response sizes and `Content-Length` | `pkg/webproxy/bridge.go:494` |
| Browser uplink | `runUp` and `runLaneUp` currently serialize requests | `pkg/webproxy/bridge.go:632`, `:686` |

### Allowed standard APIs

Use the installed Go source and official package documentation to verify standard APIs before edits.
The installed source establishes these TLS APIs:

- `tls.Dialer.DialContext(context.Context, string, string) (net.Conn, error)`.
- `(*tls.Conn).HandshakeContext(context.Context) error`.
- `(*tls.Conn).ConnectionState() tls.ConnectionState`.
- `(*x509.Certificate).Verify(x509.VerifyOptions) ([][]*x509.Certificate, error)`.
- `(*x509.Certificate).VerifyHostname(string) error`.
- `x509.NewCertPool()` and `(*x509.CertPool).AddCert`.

`x509.VerifyOptions` supports `DNSName`, `Intermediates`, `Roots`, `CurrentTime`, and `KeyUsages`.
Nil roots select system trust. Empty key usages select server authentication.
The source files are `crypto/tls/tls.go:191`, `crypto/tls/conn.go:1512`, and `crypto/x509/verify.go:193`.

### Verification and stop gate

- Record the existing test results before source edits.
- Record relevant benchmark results before performance changes.
- Read the Modern Go Guidelines skill before Go edits.
- Verify each named API against its current declaration.
- Preserve the existing untracked `CLAUDE.md` and `docs/investigations/security-audit-2026-09-30.md`.
- Stop dependent implementation until the baseline results are available.

The baseline passed the three local gnet modes and the SYN limit gate.
The complete default race suite passed, including `tools/dcfetch`.
The `gc_opt` race suite started and passed through `pkg/netx`.
The aggregate then stopped with exit 143 before that suite finished.
The production suite did not run during the baseline.
The baseline does not establish a complete `make test` pass.

## Phase 1: Toolchain and all dependencies

### Implementation

1. Copy the version declarations from `go.mod:5` and `third_party/gnet/go.mod`.
2. Update both modules to the latest compatible stable Go release.
3. Update all direct and indirect Go dependencies in both modules.
4. Update every pinned package in `tools/web-transfer/requirements.txt`.
5. Verify compatibility for each package that cannot use its latest stable release.
6. Record every compatibility exception and its source evidence.
7. Update the builder images in `dist/Dockerfile.build:7` and `dist/Dockerfile.ci:3`.
8. Update toolchain statements in `README.md:116`, `README.ru.md:302`, and `third_party/gnet/TELEGO.md:14`.
9. Search the repository for other toolchain pins and package declarations.
10. Update those declarations consistently.

Preserve the local gnet replacement and the patches documented in `third_party/gnet/TELEGO.md`.
Inspect upstream gnet changes before any source update to the local module.
Do not replace the patched tree with an upstream archive.
CI already selects Go through `go-version-file` in the test, lint, and release workflows.
Verify that all builders resolve the same selected toolchain.

The Docker registry has no `golang:1.27.3-alpine` manifest on 2026-10-20.
The builders retain the available `golang:1.27.2-alpine` base and explicitly select `GOTOOLCHAIN=go1.27.3`.
The compiler version is Go 1.27.3. The base image version is Go 1.27.2.

Playwright 1.64.0 requires `pyee>=13,<15`.
The harness uses pyee 14.0.0, the latest compatible stable release.
The latest pyee 15.0.0 does not satisfy that requirement.

### Verification and stop gate

- Verify both module graphs and sums after dependency updates.
- Verify the Python dependency graph in an isolated environment.
- Run the local gnet regression gates from `Makefile:85`.
- Run focused build and test gates before feature edits.
- Stop on unresolved package incompatibility or loss of a local gnet patch.

## Phase 2: FakeTLS compatibility and certificate diagnostic

Phases 2 through 5 form parallel implementation tracks after the dependency baseline passes.
Each track has separate file ownership and focused verification.
All four tracks must finish before the independent audit in Phase 6.

### Implementation

1. Copy the cipher-vector test pattern from `pkg/transport/faketls/handshake_test.go:473`.
2. Select the first actual cipher within the complete declared vector.
3. Skip exactly the 16 GREASE values.
4. Preserve the existing synthetic default for an unavailable cipher choice.
5. Copy the full handshake pattern from `pkg/gproxy/handshake_test.go:171`.
6. Add bounded record reassembly before `ParseClientHello`.
7. Preserve an allocation-free ordinary record path.
8. Support fragments within the four-byte handshake header.
9. Retain original wire bytes until successful authentication.
10. Preserve the HMAC, replay, and output-budget order in `pkg/gproxy/handshake.go:252`.
11. Copy the Kong command pattern from `cmd/telego/main.go:437` for a separate TLS diagnostic.
12. Report the target address, SNI hostname, certificate trust, hostname match, and expiry.
13. Bound connection and handshake time with a context deadline.

Runtime certificate and template fetchers intentionally skip certificate verification.
Their existing behavior remains separate from the diagnostic.
The certificate fetcher uses the mask hostname as SNI at `pkg/gproxy/startup.go:23`.
The template fetcher uses its target hostname at `pkg/tlsfront/serverhello.go:116`.
The diagnostic must report its exact target and verification hostname.

### Verification and stop gate

- Cover every GREASE value and preserve ordinary suite ordering.
- Cover empty, odd, truncated, and inconsistent cipher vectors.
- Cover fragmented handshake headers, SNI, random fields, and continuation headers.
- Cover invalid continuation type, version, size, and fragment count.
- Verify exact splice bytes after authentication failure.
- Verify coalesced O2 bytes remain available after the final handshake record.
- Copy logical retry coverage from `pkg/gproxy/logical_stream_test.go:398`.
- Copy local certificate fixtures from `pkg/tlsfront/certfetch_test.go:389`.
- Cover trusted, untrusted, expired, mismatched, and stalled TLS endpoints.
- Stop on unbounded memory, blocking event-loop work, or changed ordinary handshake behavior.

## Phase 3: Partial Middle-End availability

### Implementation

1. Extend factory construction through the slot pattern in `generation_factory.go:233`.
2. Retain failed slot identities for recovery.
3. Admit reachable slots after startup and probe success.
4. Update manager startup and startup-event behavior together.
5. Copy independent maintenance from `generation_supervisor.go:644` for failed-slot recovery.
6. Preserve exact signed DC identities throughout construction, admission, and recovery.
7. Publish only slots with completed startup and admission probes.
8. Preserve currently ready coverage for desired DCs during artifact rotation.
9. Reject a candidate that loses that coverage.
10. Preserve healthy existing bindings on the previous generation.
11. Preserve the shared response pool and the limit of two live managers.
12. Make cancellation and complete startup failure terminal and bounded.

Use the supervisor publication pattern at `generation_supervisor.go:545`.
Do not publish slots directly from factory workers.
Do not equate positive and negative DC IDs.
Do not count failed bootstrap slots as lost coverage from an admitted generation.

### Verification and stop gate

- Cover a failed replica, an unavailable DC, recovery, and total failure.
- Cover failures during `Start`, asynchronous startup events, and admission probes.
- Cover cancellation at construction, startup, probe, and publication.
- Verify unavailable DCs use existing direct fallback.
- Verify rotation preserves each currently ready desired DC.
- Verify old bindings survive candidate rejection and successful publication.
- Verify manager count and response-budget limits under repeated rotation.
- Stop on false readiness, cross-DC routing, a leaked worker, or coverage regression.

## Phase 4: Bounded HTTPS conveyor

### Implementation

1. Extend `Session.ProcessUp` through the existing reservation pattern at `pkg/webproxy/session.go:1280`.
2. Add a maximum of four concurrent uplink requests.
3. Advance ACKs only across the contiguous committed sequence frontier.
4. Keep digest history for the last four committed sequences.
5. Reject conflicting sequence replays without duplicate backend writes.
6. Bound waiting requests, bodies, and reservations before retention.
7. Refund each reservation on success, rejection, timeout, or close.
8. Extend browser `runUp` and `runLaneUp` through their existing lifecycle pattern.
9. Deliver complete downlink frames incrementally from finite responses.
10. Verify replay prefixes byte-for-byte before any new delivery.
11. Verify the final cursor and complete response size.
12. Bound aggregate JavaScript memory across requests, replay data, partial frames, and queued output.
13. Preserve legacy HTTPS and WebSocket carrier behavior.

Use `PollCarrier` and its lease through the HTTP worker pattern at `pkg/webproxy/http_server.go:1303`.
Preserve finite `Content-Length` responses and the parser limits at `pkg/webproxy/bridge.go:494`.
Do not introduce an unbounded response or an indefinitely open upload.
Do not deliver a replay prefix twice after a partial response failure.

### Verification and stop gate

- Cover reordered arrival, out-of-order completion, missing sequences, and retries.
- Cover conflicting replay bodies, old replay windows, and sequence exhaustion.
- Verify bounded body admission and exact reservation refunds.
- Cover partial response interruption at frame boundaries and within frames.
- Cover changed replay prefixes, malformed frames, lengths, and cursors.
- Run Go session tests and the JavaScript bridge tests in `pkg/webproxy/testdata/bridge_io_test.cjs`.
- Run browser transfer tests with matching source and destination hashes.
- Stop on duplicate delivery, ACK gaps, unbounded retention, or legacy carrier regression.

## Phase 5: Optional small ServerHello segments

### Implementation

1. Inspect current gnet owner APIs before selecting a scheduling mechanism.
2. Add an explicit configuration value with the feature disabled by default.
3. Add Linux support for small initial ServerHello TCP segments.
4. Schedule paced writes without blocking a gnet owner.
5. Bound feature lifetime, pending bytes, and completion deadlines.
6. Cancel pending work during connection close and shutdown.
7. Restore ordinary bulk output after the handshake.
8. Preserve supported behavior on other platforms.

The current response write occurs at `pkg/gproxy/handshake.go:383`.
This phase concerns TCP wire segments, separate from TLS record shaping.
Use the ownership rules in `pkg/gproxy/client_endpoint.go` and the local gnet patch notes.
Do not assume that application write boundaries create TCP segment boundaries.
Do not use sleeps on an event-loop owner.

### Verification and stop gate

- Verify the default path produces the existing response behavior.
- Verify actual TCP segment sizes and ordering on Linux.
- Cover partial output, close during pacing, deadlines, and shutdown.
- Verify unsupported platforms compile and preserve their documented behavior.
- Compare handshake latency and bulk throughput before and after this phase.
- If segment evidence is absent, stop this phase.
- If cleanup depends on an unbounded worker, stop this phase.

## Phase 6: Independent audit and fixes

### Implementation

1. Review each feature against its source contracts and acceptance conditions.
2. Run independent reviews for correctness, resource bounds, compatibility, and security.
3. Fix actionable findings at their responsible layer.
4. Repeat focused verification after each fix.
5. Repeat independent review until no actionable finding remains.
6. Update release documentation around the final implementation.

### Verification and stop gate

- Run focused tests first.
- Run `make test`, including the local gnet gates.
- Run `make test-32`.
- Run the repository lint workflow commands and `go vet ./...`.
- Run production builds and supported cross-builds.
- Run `make test-web-nginx` if Docker is available.
- Record an unavailable gate and its exact cause.
- Compare relevant benchmark results against the baseline.
- Search for stale toolchain pins and omitted package updates.
- Search for blocking handshake operations, unbounded queues, and unmanaged goroutines.
- Verify the local gnet patch inventory after all dependency changes.
- Stop on an actionable audit finding, required gate failure, or unexplained performance regression.

The existing verification commands are in `Makefile:71`, `:90`, `:99`, `:107`, and `:130`.
The repository benchmark guidance is in `README.md:824`.

## Phase 7: Commits, VPS candidate, and release

### Implementation

1. Create verified commits on the working branch.
2. Push the working branch after the audit gates pass.
3. Prepare the `v0.8.0` release notes and version metadata.
4. Copy deployment lifecycle patterns from `.local/deploy-v0.7.0/vps_deploy.py`.
5. Prepare an exact-commit candidate and record its binary SHA-256.
6. Record the VPS preflight state and current deployment identity.
7. Prepare backups and a concrete rollback command before deployment.
8. Deploy the candidate to the VPS for real network tests.
9. Verify the running commit, binary hash, protocol behavior, and health metrics.
10. Run browser transfers through HTTPS and WebSocket with identical data and network conditions.
11. Record transfer hashes, throughput, latency, retries, and failures for each carrier.
12. Record the user mobile result before attributing an improvement to the transport change.
13. After candidate evidence passes, publish the release and tag.
14. Verify published artifacts, image digests, checksums, and release workflows.

The established deployment script exposes `prepare`, `apply`, `health`, and `rollback` at `.local/deploy-v0.7.0/vps_deploy.py:35`.
The script verifies the candidate hash at `:47` and prepares rollback state at `:56`.
Use `.local/deploy-v0.7.0/web_check.py` for the established protocol health pattern.
The release promotion pattern is `.local/deploy-v0.7.0/promote_vps.py`.
Inspect current VPS state before adapting these local scripts.
Do not copy old release identifiers into new deployment commands.
Do not expose secrets in logs, commits, release notes, or browser test artifacts.

### Verification and final stop gates

- Require the exact candidate commit and binary identity before real tests.
- Require successful protocol and transfer-hash evidence before release.
- Require matched HTTPS and WebSocket measurements before transport conclusions.
- Keep release publication pending until the candidate evidence is complete.
- If the real mobile result is unavailable, report that limitation without a performance claim.
- If the candidate fails, use the prepared rollback and retain diagnostic evidence.
- Complete the release only after published artifacts and workflows pass verification.
