# AGENTS.md

This file is for the coding agent working in this repo. Read it at the start of a session.

## What this repo is

**Hermetarium** is a sealed habitat for software agents. Spec: `SPEC.md`.

An inhabitant boots inside an OCI image and may change that world freely. It cannot leave. The wall is outside the image (weak `runc` / Docker, or strong Firecracker). The supervisor owns the only network path (Squid) and the I/O log.

Not a per-command sandbox. Do not implement this as DeepSeek-style `ctx.sandbox` subprocess wrapping on the host.

## How to work here

- Layout is **one directory per process**: `supervisor/`, `squid/`, `firecracker-helper/`, plus `examples/` (inhabitants) and `tests/`.
- **Supervisor is Go**: native binary, stdlib-first, `os/exec` Docker/Firecracker/Squid. Do not import their SDKs unless exec is proven insufficient. Do not rewrite the supervisor in TypeScript, Rust, JVM, .NET, or a WASM-only runtime.
- **Logged path is Squid**, spawned as a sibling (GPLv2 stays in Squid; do not link it). Policy is a **per-habitat ACL file**. Parked: `external_acl_type`, Envoy/xDS, OPA, replacing Squid.
- CLI name is `hermetarium` in full, never `herm`.
- Inhabitants (Universal APP, TypeScript host, scanners) are out of scope until asked. Do not add that scaffolding here.
- Prefer small, reversible files. Hello-world is both walls, fail-closed egress, probe, I/O log, inbound echo (`examples/echo/`).
- License: **PolyForm Noncommercial 1.0.0** (`LICENSE`). Source-available, not OSI Open Source. Do not relicense to Apache/MIT/GPL.

## Do not invent

- Supervisor shape beyond the CLI (Compose, Kubernetes, …)
- Habitat persistence (ephemeral vs long-lived)
- Where API keys live (supervisor vs in-box)
- TLS bodies in the I/O log (metadata-only vs opt-in MITM)
- Live ACL helpers / OPA / a different proxy
