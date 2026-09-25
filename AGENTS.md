# AGENTS.md

This file is for the coding agent working in this repo. Read it at the start of a session.

## What this repo is

**Hermetarium** is a sealed habitat for software agents. Spec: `SPEC.md`.

An inhabitant boots inside an OCI image and may change that world freely. It cannot leave. The wall is outside the image (weak `runc` / Docker, or strong Firecracker). The supervisor owns the only network path (Squid) and the I/O log.

Not a per-command sandbox. Do not implement this as DeepSeek-style `ctx.sandbox` subprocess wrapping on the host.

## How to work here

- Layout is **one directory per process**: `supervisor/`, `firecracker-helper/`, plus `examples/` (stand-in loops), `inhabitants/` (official CLIs), `porter/` (HTTP adapter for those CLIs), and `tests/`. Each example/inhabitant is a pair: Dockerfile plus `squid.conf` for the wall.
- **Supervisor is Go**: native binary, module `github.com/pihme/hermetarium`, stdlib-first, `os/exec` Docker/Firecracker/Squid. Do not import their SDKs unless exec is proven insufficient. Do not rewrite the supervisor in TypeScript, Rust, JVM, .NET, or a WASM-only runtime.
- **Logged path is Squid**, spawned as a sibling (GPLv2 stays in Squid; do not link it). Policy is a **per-habitat ACL file**. Parked: `external_acl_type`, Envoy/xDS, OPA, replacing Squid.
- CLI name is `hermetarium` in full, never `herm`.
- Firecracker helper scripts are embedded. Squid is a pulled image. `create --acl FILE` copies that Squid config into the instance dir and mounts it. Wall configs live next to the habitat Dockerfile (`examples/*/squid.conf`, `inhabitants/*/squid.conf`). Data dir is `HERMETARIUM_ROOT`, else this checkout, else XDG. Probe and vendor-mock are test sidecars.
- **Versions:** two semver artifacts, tags `hermetarium/vX.Y.Z` and `porter/vX.Y.Z`. Conventional commits (`feat:` minor, `fix:`/`perf:` patch, `feat!:` or `BREAKING CHANGE:` major). A commit only bumps the artifact whose paths it touches (`supervisor/`, `firecracker-helper/`, `go.mod`, `Makefile` → hermetarium; `porter/` → porter). Docs/tests/examples/inhabitants do not bump. `.github/scripts/release.py` on push to `main` after CI.
- `examples/` = stand-in inhabitant images (echo, agentd). `inhabitants/` = official `claude` / `grok` / `dsh`. `tests/` holds integration tests, vendor-mock, and probe. Do not add Universal APP / TypeScript host / scanners until asked.
- Prefer small, reversible files. Hello-world is both walls, fail-closed egress, probe, I/O log, inbound echo (`examples/echo/`). Coding-agent stand-in is one `examples/agentd/` image, not three vendor-named copies.
- License: **PolyForm Noncommercial 1.0.0** (`LICENSE`). Source-available, not OSI Open Source. Do not relicense to Apache/MIT/GPL.

## Do not invent

- Supervisor shape beyond the CLI (Compose, Kubernetes, …)
- Habitat persistence (ephemeral vs long-lived)
- TLS bodies in the I/O log (metadata-only vs opt-in MITM)
- Live ACL helpers / OPA / a different proxy

Decided in SPEC.md (do not reopen): API keys must not live in the inhabitant image; they go in the host-side ACL passed to `create --acl`. Both walls boot the same OCI image.
