# Release checklist

Each release must refresh the toolchain and every declared dependency to a compatible stable version.
This includes both Go modules, Python tools, container builders, browser tooling, and CI actions.
The release record must identify exceptions and their evidence.

## Refresh dependencies

1. Read the official stable Go metadata and record the selected patch version.
2. Update `go.mod` and `third_party/gnet/go.mod` to that version.
3. Update compatible dependencies in both modules.
4. Run `go mod tidy` in both modules.
5. Run `go list -m -u -json all` in both modules.
6. Compare every declared requirement with the update metadata.
7. For unused upstream graph entries, record `go mod why -m` evidence before you omit an override.
8. Resolve every Python pin against its package metadata and dependency constraints.
9. Install the pins in an isolated environment.
10. Run `pip check` in that environment.
11. Update compatible browser binaries and run a local browser launch check.
12. Check container manifests, certificates, release tooling, and CI actions against their official metadata.

If a builder image tag is unavailable, select an available base with an explicit compatible compiler.
Record the registry result and the compiler version from the build.
For v0.8.0, the missing `golang:1.27.3-alpine` manifest requires the available base plus `GOTOOLCHAIN=go1.27.3`.

If a newer package conflicts with another dependency, select the latest compatible stable version.
Record both constraints and the selected version.
For v0.8.0, Playwright 1.64.0 requires `pyee>=13,<15`, so the harness uses pyee 14.0.0.
The [v0.8.0 dependency report](investigations/upstream-reliability-dependencies-2026-10-20.md) contains the evidence for both exceptions.
These examples do not authorize the same exception in a later release.

## Preserve local gnet changes

The root module replaces upstream gnet with `third_party/gnet`.
The [patch inventory](../third_party/gnet/TELEGO.md) describes the required local behavior.

1. Record source hashes before the dependency refresh.
2. Compare those hashes after the refresh.
3. Review each intentional source change against the patch inventory.
4. Run `make test-gnet`.
5. If upstream replaces a patch, retain equivalent regression coverage before you remove the local change.

## Run release gates

1. Run `make test` with Node.js 24 available.
2. Run `make test-32` on a Linux host with 32-bit executable support.
3. Run `make test-web-nginx` with Docker available.
4. Run the lint workflow checks and `go vet`.
5. Run utility tests for changed release and transfer tools.
6. Build every supported release target with production tags.
7. For performance changes, retain comparable benchmark results before and after the change.
8. For optional ServerHello pacing, retain the isolated privileged packet-capture result.

`make test` includes the local gnet and SYN limiter checks.
The [Contributing section](../README.md#contributing) describes resource limits and test modes.
An interrupted aggregate run does not establish a complete gate result.

## Test an exact candidate

1. Complete the implementation and independent audit/fix loop.
2. Commit the reviewed source before the candidate build.
3. Record the complete commit revision, compiler, build tags, and binary SHA-256.
4. Put the same revision in the candidate image labels.
5. Record the current VPS image, container identity, configuration hashes, and other containers.
6. Prepare rollback before you replace the Telego container.
7. Deploy only the candidate service.
8. Check HTTP, WebSocket, TLS, Middle-End readiness, restart counts, memory, and session cleanup.
9. Run matched HTTPS and WebSocket transfers with the same private payload file.
10. Restore the configured carrier after each temporary trial.
11. Keep configuration, credentials, captures, and endpoint files outside tracked documentation.

Carrier comparisons must use the same host, payload, candidate revision, and test conditions.
Repeat transfers in an alternating order and retain hashes, times, cleanup results, and resource snapshots.
Rare mobile stalls do not need reproduction for release readiness.
Release notes must distinguish observed test results from an unconfirmed network hypothesis.

## Publish and promote

1. Write release notes for the final implementation and its measured limits.
2. Make sure that the release tag identifies the tested commit.
3. Push the tag and wait for the test, image, and release workflows.
4. Check published archive checksums and image revision labels.
5. Promote the verified image by immutable digest.
6. Check the running binary checksum against the verified archive.
7. Repeat health, resource, and cleanup checks after promotion.
8. Retain the previous image, configuration backup, and rollback command.

If promotion fails, restore the tested candidate or the previous release with its recorded configuration.
Rollback must preserve unrelated containers.
