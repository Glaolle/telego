# TLS implementation evidence for v0.8.0

Date: 2026-10-20. Independent review and full release gates remain pending.

## Implementation boundaries

- `pkg/transport/faketls/handshake.go` selects the first non-GREASE cipher within the complete declared vector.
- The GREASE predicate excludes exactly the sixteen reserved equal-byte values.
- `pkg/gproxy/tls_client_hello.go` accepts at most 32 records and 16640 handshake bytes.
- The ordinary record path aliases the input and allocates no reconstruction buffer.
- Fragmented input retains its original wire bytes until authentication succeeds.
- Existing HMAC, replay, logical output reservations, and splice ownership remain in the handshake path.
- `pkg/tlsfront/doctor.go` uses normal TLS verification with an explicit target and mask hostname.
- The CLI diagnostic defaults to the actual probe fallback and sends its configured PROXY preface.
- `--cert-source` selects and labels the separate certificate source.

## Pacing lifecycle

The optional configuration key is `tls-fronting.server-hello-segment-size`.
Zero disables the feature. The accepted range is 64 through 1460 TCP payload bytes.
The value excludes TCP headers and timestamps.
The implementation does not change the listener or established MSS.

Linux native TCP connections enter `StateWriteServerHello` during pacing.
The connection owner retains one response, one timer, and a three-second deadline.
The response limit is 64 KiB.
The existing handshake timeout also remains active.
The pacing state limits buffered client input through the existing connection budget.

The owner checks gnet output and `TCP_INFO.Notsent_bytes` before each write.
The timer submits the next step through `EventLoop.Execute`.
The implementation does not use the unsupported `EventLoop.Schedule` method.
An owner rejection releases the response and closes the connection.
Connection cleanup stops the timer and releases the response.
Successful completion explicitly resumes a buffered O2 handshake.

`stopConnectionTimers` now cancels pacing during close.
Runtime failure accounting maps the new state to the TLS ServerHello stage.
Logical WEB streams, authenticated internal WEB connections, Unix sockets, and other platforms retain the ordinary response path.
The non-Linux helper uses a build constraint and requires no Linux socket API.

## Completed local verification

- GREASE tests cover every reserved value, malformed vectors, and bounds on cipher selection.
- Reassembly tests cover every split point, split handshake headers, continuation errors, and aggregate limits.
- An allocation assertion establishes zero allocations for ordinary extraction.
- Native and logical tests preserve O2 routing, logical output retries, replay protection, and original splice bytes.
- Diagnostic tests cover trust, hostname, expiry, future validity, timeout, and both PROXY versions.
- Configuration tests cover disabled, boundary, valid, and invalid segment sizes.
- Pacing tests cover close, a stalled receive window, deadline, and rejected owner work.
- The focused tests passed with `GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`, `-p=1`, and `-parallel=2`.
- The focused gproxy and certificate tests also passed under the race detector.
- `go vet` passed for gproxy, FakeTLS, tlsfront, config, and the CLI.
- The gproxy test binary compiled for `darwin/amd64`.

The Linux counter test sent 4096 bytes through 45 data segments.
The later 65536-byte bulk write used two data segments.
The observed MSS increased naturally from 32768 to 47616 bytes.
The test did not reduce the bulk MSS.
The raw result is `.local/deploy-v0.8.0/tls-pacing-counters.txt`.

## Ordinary-path benchmark

The benchmark used three 200ms samples with `GOMAXPROCS=2` and `GOMEMLIMIT=512MiB`.
The final ordinary path avoids fragment bookkeeping on its stack.

| Benchmark | Baseline samples (ns/op) | Final samples (ns/op) | Final memory |
| --- | --- | --- | --- |
| TLS header | 158.3, 155.2, 157.2 | 188.0, 155.7, 160.5 | 485 B/op, 2 allocations/op |
| TLS payload | 3905, 3786, 3925 | 3987, 3868, 3913 | 1296 B/op, 22 allocations/op |

The median payload cost changed from 3905 to 3913 ns/op.
The short samples do not establish a significant performance change.
The extraction allocation assertion remains zero.
The baseline and final samples are `.local/deploy-v0.8.0/tls-baseline-bench.txt` and `.local/deploy-v0.8.0/tls-after-bench-final.txt`.

## Mandatory VPS packet-capture gate

The local packet capture did not run because the host denied raw socket access.
Tool escalation did not grant the required capture capability.
The available `sudo -n` command requires a password.
The exact capture failure is `.local/deploy-v0.8.0/tls-pacing-capture.txt`.
Local counters establish segment counts and bulk behavior. They do not establish maximum TCP payload sizes.
The release must wait for the privileged VPS capture result.

1. Build the production-tagged Linux gproxy test binary from the reviewed candidate commit.
2. Copy that binary to the VPS through the existing deployment procedure.
3. Select a private artifact path for `TELEGO_TEST_PCAP`.
4. Run only `TestServerHelloPacingSegmentsAndBulk` with root capture permission.
5. Retain the test log and the packet capture as candidate evidence.
6. Require the test to report handshake payloads of at most 92 bytes and larger later bulk packets.

The local build command is:

```sh
rtk proxy env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c \
  -tags=poll_opt,gc_opt \
  -o .local/deploy-v0.8.0/gproxy-pacing.test ./pkg/gproxy
```

The test binary arguments are:

```text
-test.run=^TestServerHelloPacingSegmentsAndBulk$ -test.count=1 -test.v
```

`TELEGO_TEST_PCAP` must contain an absolute path writable by the test process.
The optional capture uses only the isolated connection ports on loopback.
The test starts tcpdump before enrollment and stops it after the synthetic bulk response.
The capture contains synthetic test data.
The test parses packet payload lengths and rejects handshake packets larger than the configured size.

## Remaining release evidence

Independent audit, full tests, supported production builds, and the VPS capture remain required.
Mobile throughput and filtering improvements remain unproven until matched tests run on the affected route.
