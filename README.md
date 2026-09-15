# Hermetarium

Sealed habitat where agents live. They wake up inside an OCI image and may change that world freely. They cannot leave. Every packet in or out is logged on the wall.

Not a tool an agent calls.

- [SPEC.md](SPEC.md) — product spec
- [USAGE.md](USAGE.md) — walls, Squid, logs, custom images, porter

Supervisor: **Go** native binary. Logged path: **Squid** (spawned, per-habitat ACL file). CLI name is `hermetarium` in full; do not shorten to `herm`.

## Layout

One directory per process:

| Directory | Process |
| --- | --- |
| `examples/agentd/` | Stand-in coding loop (not the official CLIs) |
| `examples/claude-code/` etc. | Images that run **agentd** against that vendor’s API shape |
| `examples/echo/` | Tiny HTTP echo inhabitant |
| `firecracker-helper/` | Strong-wall helper: TAP + Squid + Firecracker (not the inhabitant) |
| `inhabitants/` | Images that install the **official** CLIs (`claude`, `grok`, `dsh`) plus `porter` |
| `porter/` | Carries operator HTTP turns to the official CLI (sits in the inhabitant image) |
| `squid/` | Squid image (intercept proxy + probe origin + inbound reverse-proxy) |
| `supervisor/` | Go CLI that boots walls, writes the ACL file, maps Squid `access.log` |
| `tests/claude-code/` etc. | **agentd** examples vs mock API (CI); live tagged |
| `tests/echo/` | Shared echo checks |
| `tests/hello-world/` | Both walls, fail-closed egress, probe, I/O log, echo |
| `tests/inhabitants/` | Official CLIs present + HTTP front (CI); chat/root is live tagged |

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

`url` is the host HTTP address that reaches the inhabitant **through Squid** (logged inbound). The default inhabitant is the echo example in `examples/echo/`: POST body comes back as the response body. The process stays up, so a second `curl` is the “running server” case. More: [USAGE.md](USAGE.md).

`create --wall strong` and `logs` / `destroy` / `url` work for both walls.

Official CLIs (larger images):

```bash
id=$(./bin/hermetarium create --wall weak --inhabitant claude-code)
curl -sS -d 'Run id -u' "$(./bin/hermetarium url "$id")"
```

Same for `--inhabitant grok-build` and `--inhabitant deepseek-harness`.

## Examples vs inhabitants

| | `examples/` | `inhabitants/` |
| --- | --- | --- |
| What runs in the box | `agentd` (a small Go session + bash tool) | Official `claude` / `grok` / `dsh`, wrapped by `porter` |
| Why it exists | Fast, hermetic tests of Squid, keys, walls, I/O log | The product inhabitant: a real coding CLI as root |
| `make test` says | Mock API drives a tool_use; uid 0; key not in the box | The official binary is on PATH, HTTP front is up, key not in the box. If the CLI will not speak our mock API, chat/root is **not** claimed here |
| `make test-live` says | Same loop against the real vendor (optional) | Natural-language turn through the **real CLI** to the real vendor |

`--example claude-code` is the stand-in. `--inhabitant claude-code` is Claude Code.

`porter` listens on `:8080` and execs the official CLI for each operator POST.

Official CLIs often refuse to boot with an empty key env. Inhabitant images set dummies and related boot flags; none of that is the supervisor secret. Squid still replaces `x-api-key` / `Authorization`.

| CLI | Dummy / boot settings in the image |
| --- | --- |
| Claude Code | `ANTHROPIC_API_KEY=not-the-supervisor-key`, `ANTHROPIC_BASE_URL`, `IS_SANDBOX=1`, `CLAUDE_CODE_BUBBLEWRAP=1`, `CI=true`, `~/.claude/settings.json` bypassPermissions |
| Grok Build | `XAI_API_KEY=not-the-supervisor-key`, `GROK_CLI_CHAT_PROXY_BASE_URL` |
| DeepSeek Harness | `DEEPSEEK_API_KEY=not-the-supervisor-key`, `DEEPSEEK_BASE_URL`, `OPENAI_BASE_URL`, `DSH_HOME` |

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
