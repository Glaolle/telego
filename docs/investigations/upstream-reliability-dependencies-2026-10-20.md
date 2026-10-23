# Dependency refresh for v0.8.0

Date: 2026-10-20.

## Toolchain and builders

Both Go modules now require Go 1.27.3.
The [official Go release metadata](https://go.dev/dl/?mode=json) identifies this version as stable.
The local compiler reports `go version go1.27.3 linux/amd64`.

The Docker registry has no `golang:1.27.3-alpine` manifest.
Both builders retain the available `golang:1.27.2-alpine` base and set `GOTOOLCHAIN=go1.27.3`.
The selected compiler is Go 1.27.3 in both builders.
The active Alpine certificate image exists and requires no declaration change.

## Go package versions

The refresh used `go get -u all` and `go mod tidy` in both modules.
Explicit updates also cover four modules used by dependency tests.
`go list -m -u -json all` reports no update for any declared requirement.

| Root module | Previous | Current |
| --- | --- | --- |
| `github.com/alecthomas/repr` | `v0.5.2` | `v0.5.4` |
| `github.com/klauspost/compress` | `v1.20.1` | `v1.20.3` |
| `github.com/klauspost/cpuid/v2` | `v2.4.0` | `v2.4.1` |
| `github.com/pion/dtls/v3` | `v3.1.10` | `v3.1.14` |
| `github.com/prometheus/common` | `v0.72.0` | `v0.72.1` |
| `github.com/rogpeppe/go-internal` | `v1.15.0` | `v1.16.0` |
| `github.com/shopspring/decimal` | `v1.5.0` | `v1.6.0` |
| `github.com/xyproto/randomstring` | `v1.0.5` | `v1.2.0` |
| `github.com/zeebo/assert` | `v1.1.0` | `v1.3.1` |
| `golang.org/x/crypto` | `v0.57.0` | `v0.58.0` |
| `golang.org/x/exp` | `v0.0.0-20261007192929-f45ad48fbe92` | `v0.0.0-20261014143641-f0251eb4aa3e` |
| `golang.org/x/mod` | `v0.41.0` | `v0.42.0` |
| `golang.org/x/net` | `v0.60.0` | `v0.61.0` |
| `golang.org/x/sync` | `v0.23.0` | `v0.24.0` |
| `golang.org/x/sys` | `v0.48.0` | `v0.49.0` |
| `golang.org/x/text` | `v0.42.0` | `v0.43.0` |
| `golang.org/x/tools` | `v0.51.0` | `v0.52.0` |

The local gnet module updates `golang.org/x/sync` to `v0.24.0` and `golang.org/x/sys` to `v0.49.0`.
The latest upstream gnet release remains `v2.10.0`.
SHA-256 comparison preserves all 118 local gnet source files.

Version evidence comes from the [Go module proxy](https://proxy.golang.org/) through `go list`.

## Python harness versions

The refresh resolved every pin against the [PyPI package metadata](https://pypi.org/).
The updated pins are:

| Package | Previous | Current |
| --- | --- | --- |
| `aiohttp` | `3.14.4` | `3.14.5` |
| `multidict` | `6.9.1` | `7.1.0` |
| `playwright` | `1.63.0` | `1.64.0` |
| `pycparser` | `3.1` | `3.11` |
| `pyee` | `13.0.1` | `14.0.0` |

All other pins already match their latest stable package versions.
[Playwright 1.64.0](https://pypi.org/pypi/playwright/1.64.0/json) requires `pyee>=13,<15`.
The selected pyee 14.0.0 is the latest compatible stable release.
The latest pyee 15.0.0 conflicts with that requirement.
The ignored local environment includes these pins and pip 26.2.1.

## Upstream graph metadata

The following modules still have newer upstream metadata.
`go mod why -m` reports that the main module does not need each module.
These modules belong to upstream module graphs and supply no package required by this build.
No artificial requirement overrides were added for these unused modules.

| Graph | Module | Selected metadata | Latest metadata |
| --- | --- | --- | --- |
| root | `github.com/PuerkitoBio/goquery` | `v1.11.0` | `v1.13.0` |
| root | `github.com/andybalholm/cascadia` | `v1.3.3` | `v1.3.5` |
| root | `github.com/awnumar/memcall` | `v0.4.0` | `v0.5.0` |
| root | `github.com/beevik/ntp` | `v1.5.0` | `v1.6.0` |
| root | `github.com/benbjohnson/clock` | `v1.3.0` | `v1.3.5` |
| root | `github.com/go-faster/sdk` | `v0.34.0` | `v0.39.1` |
| root | `github.com/go-openapi/inflect` | `v1.0.0` | `v1.0.1` |
| root | `github.com/golang/protobuf` | `v1.5.0` | `v1.5.4` |
| root | `github.com/k0kubun/pp/v3` | `v3.5.2` | `v3.5.3` |
| root | `github.com/pion/datachannel` | `v1.6.2` | `v1.6.3` |
| root | `github.com/pion/ice/v4` | `v4.4.0` | `v4.4.8` |
| root | `github.com/pion/interceptor` | `v0.1.47` | `v0.1.49` |
| root | `github.com/pion/mdns/v2` | `v2.1.0` | `v2.2.2` |
| root | `github.com/pion/rtcp` | `v1.2.17` | `v1.2.19` |
| root | `github.com/pion/sctp` | `v1.11.1` | `v1.12.0` |
| root | `github.com/pion/sdp/v3` | `v3.0.19` | `v3.0.20` |
| root | `github.com/pion/srtp/v3` | `v3.0.12` | `v3.1.3` |
| root | `github.com/pion/turn/v5` | `v5.0.12` | `v5.1.2` |
| root | `github.com/pion/webrtc/v4` | `v4.2.18` | `v4.2.24` |
| root | `github.com/sergi/go-diff` | `v1.1.0` | `v1.4.0` |
| root | `github.com/valyala/fasthttp` | `v1.73.0` | `v1.76.0` |
| root | `github.com/xhit/go-str2duration/v2` | `v2.1.0` | `v2.2.0` |
| root | `golang.org/x/telemetry` | `v0.0.0-20261009151841-97e8ea77aa2a` | `v0.0.0-20261016163328-824aa3d1d8dd` |
| root | `golang.org/x/time` | `v0.14.0` | `v0.16.0` |
| gnet | `github.com/stretchr/objx` | `v0.5.3` | `v0.8.0` |

## Verification

- Both module tidies passed.
- The focused compile gate passed for `cmd/telego`, `pkg/transport/faketls`, `pkg/transport/middleend`, and `pkg/webproxy`.
- `go vet` passed for those packages.
- `pip check` found no broken requirements.
- The upgraded browser started and rendered a local page.
- The browser reports Chromium `156.0.8078.4`.
- The gnet source hash comparison found no changes.

Full tests, production builds, cross-builds, and integration tests remain release gates.
This dependency phase does not establish a complete release test result.

Raw metadata and command evidence remain in the ignored `.local/deps-v0.8.0/` directory.
