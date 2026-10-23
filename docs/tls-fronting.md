# TLS compatibility and diagnostics

Telego accepts a ClientHello split across TLS handshake records.
The complete handshake can contain at most 16640 bytes across 32 records.
Each continuation must use a supported TLS record version and contain a nonempty payload.
Authentication uses the reconstructed handshake.
An unrecognized connection retains its original record bytes for the probe fallback.

Synthetic ServerHello responses select the first offered cipher that is not a GREASE value.
The selection respects the declared cipher list.
Captured ServerHello templates retain their existing cipher selection.

## Certificate diagnostic

Run this command on a host that can reach the configured fallback endpoint:

```sh
telego doctor -c /path/to/config.toml
```

The diagnostic uses the resolved `splice-host` and `splice-port` as its target.
The mask hostname supplies the TLS SNI and certificate hostname.
The diagnostic sends the configured PROXY v1 or v2 preface before TLS.
The preface describes the diagnostic connection.
The diagnostic verifies certificate trust, hostname, validity, and server authentication.
Successful output includes the target, scope, certificate subject, issuer, and validity dates.
A failed connection or certificate verification returns an error.

To inspect a separate certificate source, run:

```sh
telego doctor -c /path/to/config.toml --cert-source
```

This command selects `cert-host` and `cert-port` and reports the `certificate_source` scope.
It sends no PROXY preface to the certificate source.
Certificate-source success does not establish the health of the fallback endpoint or the public route.

The default timeout is 10 seconds. The maximum timeout is 30 seconds.
Use `--timeout 20s` to change it.
The runtime template and certificate fetchers keep their existing capture behavior.
The diagnostic does not change runtime certificate policy.

## Optional TCP segmentation

Linux can pace the ServerHello response through small TCP writes.
The feature is disabled by default.
To enable it, add this value and restart Telego:

```toml
[tls-fronting]
server-hello-segment-size = 92
```

The valid range is 64 through 1460 bytes. Zero disables the feature.
The value counts TCP payload bytes per paced write.
TCP headers, timestamps, and IP headers are separate from this value.
The value is not a TCP MSS declaration.
The listener retains its negotiated MSS for bulk traffic.

Each connection retains at most 64 KiB for a paced response.
The pacing deadline is three seconds, subject to the existing handshake timeout.
The response closes on a pacing error or deadline.
The connection admits the O2 handshake after the paced response drains.
Logical WEB streams, Unix sockets, and other platforms use the ordinary response path.

The Linux path waits for the previous chunk to leave the socket queue before another write.
The event loop remains available during this wait.
Loopback TCP counters and an optional isolated packet capture verify segmentation and later bulk output.
The release requires a privileged packet capture on the VPS to establish maximum TCP payload sizes.
TCP counters alone do not establish that maximum.
Actual mobile behavior requires measurements on the affected route.
An enabled pacing value alone does not establish a filtering or throughput improvement.
