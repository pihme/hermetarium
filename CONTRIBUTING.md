# Contributing

Source-available under [PolyForm Noncommercial 1.0.0](LICENSE). Other licenses can be negotiated with the copyright holder.

## Run tests

Needs Docker and Go 1.24+. Strong-wall tests also need `/dev/kvm` and **x86_64**.

```bash
make test
```

That is the merge gate (hello-world, echo, agentd mock suites, official-CLI smoke). Optional live suite (real vendor keys):

```bash
export HERMETARIUM_ANTHROPIC_API_KEY=...
export HERMETARIUM_XAI_API_KEY=...
export HERMETARIUM_DEEPSEEK_API_KEY=...
make test-live
```

Tests use this tree (`squid/squid.conf.tmpl`, `examples/`). Firecracker helper scripts are embedded. Squid is a pulled image. From another directory: `export HERMETARIUM_ROOT=/path/to/hermetarium`.

## Commits and versions

Use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` minor
- `fix:` or `perf:` patch
- `feat!:` / `fix!:` or a `BREAKING CHANGE:` footer: major
- `docs:`, `test:`, `chore:`, `ci:` do not bump a release

Two artifacts, tagged independently:

| Artifact | Tag | Bumped when the commit touches |
| --- | --- | --- |
| hermetarium | `hermetarium/vX.Y.Z` | `supervisor/`, `squid/`, `firecracker-helper/`, `go.mod`, `Makefile` |
| porter | `porter/vX.Y.Z` | `porter/` |

A commit that only changes docs, tests, examples, or inhabitants does not cut a release. After CI on `main`, `.github/scripts/release.py` creates the GitHub Release and attaches a linux-amd64 binary.

## Go module

```
github.com/pihme/hermetarium
```

Do not rewrite the supervisor in TypeScript, Rust, or a WASM-only runtime. Do not link Squid.

## Pull requests

- Keep changes small.
- `make test` should pass on linux-amd64 with Docker and KVM.
- CLI name is `hermetarium` in full, never `herm`.
