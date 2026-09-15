# Hermetarium

Sealed habitat where agents live. They wake up inside an OCI image and may change that world freely. They cannot leave. Every packet in or out is logged on the wall.

Not a tool an agent calls.

- [SPEC.md](SPEC.md) — product spec

Supervisor: **Go** native binary. Logged path: **Squid** (spawned, per-habitat ACL file). CLI name is `hermetarium` in full; do not shorten to `herm`.

## Layout

One directory per process:

| Directory | Process |
| --- | --- |
| `supervisor/` | Go CLI that boots walls, writes the ACL file, maps Squid `access.log` |
| `squid/` | Squid image (intercept proxy + probe origin + inbound reverse-proxy) |
| `examples/echo/` | Inhabitant example: HTTP echo server |
| `examples/claude-code/` | Claude Code harness behind HTTP (specified) |
| `examples/grok-build/` | Grok Build harness behind HTTP (specified) |
| `examples/deepseek-harness/` | DeepSeek Harness behind HTTP (specified) |
| `firecracker-helper/` | Strong-wall helper: TAP + Squid + Firecracker (not the inhabitant) |
| `tests/hello-world/` | Both walls, fail-closed egress, probe, I/O log, echo example |
| `tests/echo/` | Shared checks for the echo example |
| `tests/claude-code/` | Claude Code tests (mock default; live tagged) |
| `tests/grok-build/` | Grok Build tests (mock default; live tagged) |
| `tests/deepseek-harness/` | DeepSeek Harness tests (mock default; live tagged) |

Instance state is `var/<id>/`. Firecracker assets cache in `.cache/`. Both are gitignored.

## Hello world

Needs Docker and Go 1.24+ on `PATH`. The strong wall also needs `/dev/kvm` and **x86_64** (the downloaded Firecracker binary and kernel are x86_64). First strong `create` or `make test` fetches those into `.cache/`. Firecracker runs in a privileged helper container; no host `sudo`.

If the binary is not run from this tree, set `HERMETARIUM_ROOT` to the repo root (the directory that contains `squid/` and `supervisor/`).

```bash
make test
```

```bash
make build
id=$(./bin/hermetarium create --wall weak)
./bin/hermetarium url "$id"
curl -sS -d 'hello' "$(./bin/hermetarium url "$id")"
./bin/hermetarium logs "$id"
./bin/hermetarium destroy "$id"
```

`url` is the host HTTP address that reaches the inhabitant **through Squid** (logged inbound). The default inhabitant is the echo example in `examples/echo/`: POST body comes back as the response body. The process stays up, so a second `curl` is the “running server” case.

`create --wall strong` and `logs` / `destroy` / `url` work for both walls. `make test` covers both walls and the echo example.

## Tests

Two suites. Spec: [SPEC.md §11](SPEC.md#11-tests). Coding-agent tests land with the §9c examples; `make test` already runs hello-world.

### Mocked (CI default)

Always run. No vendor account. Hello-world (both walls, probe, echo) and each coding-agent example against a **mock** vendor API: open session, a scripted root shell command, key not in the box.

```bash
make test
```

GitHub Actions job `test` runs this on every push and pull request.

### Live (optional, manual)

Real harness talking to the real vendor. Proves a model will take a natural-language ask (run a command / install something) and do it as root. **Not** a merge gate.

The supervisor reads keys from its environment. Do not put them in the image. Unset keys skip that example.

```bash
export HERMETARIUM_ANTHROPIC_API_KEY=sk-ant-...   # Claude Code
export HERMETARIUM_XAI_API_KEY=xai-...            # Grok Build
export HERMETARIUM_DEEPSEEK_API_KEY=sk-...        # DeepSeek Harness
make test-live
```

`make test-live` is `go test -tags live` (those files are invisible to `make test`).

On GitHub: Actions → CI → **Run workflow**. That is the only way the live job starts (not on push/PR). Set the matching repository secrets; the job exports them so the supervisor can inject them on Squid. Secrets must not appear in logs.

## License

[PolyForm Noncommercial License 1.0.0](LICENSE). Source-available; not OSI Open Source. Commercial use is not granted. Other licenses can be negotiated with the copyright holder.

Squid is a separate GPLv2 program, run as a sibling process, not linked into the supervisor.
