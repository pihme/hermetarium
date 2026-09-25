# Hermetarium

[![CI](https://github.com/pihme/hermetarium/actions/workflows/ci.yml/badge.svg)](https://github.com/pihme/hermetarium/actions/workflows/ci.yml)

Sealed habitat where agents live. They wake up inside an OCI image and may change that world freely. They cannot leave. Every packet in or out is logged on the wall.

Not a tool an agent calls. The CLI name is `hermetarium` in full; do not shorten to `herm`.

- [SPEC.md](SPEC.md) — product spec
- [USAGE.md](USAGE.md) — walls, Squid, logs, custom images, porter
- [CONTRIBUTING.md](CONTRIBUTING.md) — tests, commits, versions

## Pieces

`hermetarium version` and `porter version` print semver (`dev` on a local `make build`). GitHub Releases attach linux-amd64 binaries; tags are `hermetarium/vX.Y.Z` and `porter/vX.Y.Z`.

The **supervisor** (`supervisor/`) is a Go binary. It is the only operator-facing command. It creates a habitat: picks a wall (weak Docker/`runc` or strong Firecracker), copies `--acl` into the instance, starts Squid, publishes a localhost URL into the box, and maps Squid’s access log into the I/O log. It talks to Docker, Firecracker, and Squid as processes (`os/exec`), not via their SDKs.

**Squid** is the logged path. The supervisor pulls a public Squid image and mounts the ACL you pass with `--acl`. Squid runs as a sibling (GPLv2 stays in Squid; the supervisor does not link it). Each example and inhabitant ships its wall config next to its Dockerfile.

**Porter** (`porter/`) is a small HTTP adapter that lives *inside* the inhabitant image, not next to the supervisor. It listens on TCP 8080, which is what `hermetarium url` reverse-proxies to. Each operator POST is one CLI turn (`claude`, `grok`, or `dsh`); later POSTs continue the same session for `claude`/`grok` (`--continue`). DeepSeek Harness's `headless` profile has no resume flag, so each `dsh` turn is an independent one-shot task, not a continued session. You can omit porter and serve HTTP on 8080 yourself (the echo example does).

**Inhabitants** (`inhabitants/`) are Dockerfiles for official coding CLIs: Claude Code, Grok Build, DeepSeek Harness. They run as **root**, install the real binary, set dummy boot keys and vendor base URLs, and `CMD` porter. They are templates. Build an image, then `create --image <tag> --acl FILE`. The ACL file is where vendor allowlists and key inject live.

`examples/` is the test stand-in, not those products. `examples/echo/` is a tiny HTTP echo. `examples/agentd/` is one small Go tool loop against a mock Messages API so CI can prove walls, keys, and uid 0 without a live model. The mock origin is `tests/vendormock/`. `make test` uses those. Official-CLI chat through a real model is `make test-live`.

Other trees: `firecracker-helper/` TAP + Firecracker for the strong wall (scripts are embedded); `tests/` for hello-world, agentd, and official-CLI smoke. Instance state is `var/<id>/` under the data root (this checkout, `HERMETARIUM_ROOT`, or `~/.local/share/hermetarium`). Firecracker assets cache in `.cache/` under the checkout, or `~/.cache/hermetarium` off-tree.

## Install

Needs Docker and Go 1.24+ on `PATH`. The strong wall also needs `/dev/kvm` and **x86_64**. First strong `create` or `make test` fetches Firecracker into the cache dir. Firecracker runs in a privileged helper container; no host `sudo`.

Firecracker helper scripts are embedded. Squid is a pulled image. `create` needs `--acl FILE` (for example `examples/echo/squid.conf` or `inhabitants/claude-code/squid.conf`). Off-tree, instance state is `~/.local/share/hermetarium/var/` and cache is `~/.cache/hermetarium` unless you set `HERMETARIUM_ROOT`.

**From source**

```bash
git clone https://github.com/pihme/hermetarium.git
cd hermetarium
make build
./bin/hermetarium version
```

**From a release** (linux-amd64): [hermetarium releases](https://github.com/pihme/hermetarium/releases?q=hermetarium) and [porter releases](https://github.com/pihme/hermetarium/releases?q=porter). Download `hermetarium-linux-amd64`, `chmod +x`, and pass `--acl` a Squid config from the habitat you are running. Porter is copied into inhabitant images, not run next to the supervisor.

Go import path: `github.com/pihme/hermetarium`.

## Hello world

```bash
make test
```

```bash
make build
id=$(./bin/hermetarium create --wall weak --image myorg/box:1 --acl examples/echo/squid.conf)
./bin/hermetarium url "$id"
curl -sS -d 'hello' "$(./bin/hermetarium url "$id")"
./bin/hermetarium logs "$id"
./bin/hermetarium destroy "$id"
```

`--image` is any local or pullable OCI image that listens on TCP 8080. `url` is the host HTTP address that reaches it **through Squid**. `create --wall strong` works the same. More: [USAGE.md](USAGE.md).

```bash
sed 's/__VENDOR_KEY__/sk-ant-.../' inhabitants/claude-code/squid.conf > /tmp/claude.acl
id=$(./bin/hermetarium create --wall weak --image myorg/claude:dev --acl /tmp/claude.acl)
curl -sS -d 'Run id -u' "$(./bin/hermetarium url "$id")"
```

Dummy env in inhabitant images (`ANTHROPIC_API_KEY=not-the-supervisor-key` and the like) only exist so the CLI will start. Squid still replaces `x-api-key` / `Authorization` from the ACL. Boot flags per CLI: [USAGE.md](USAGE.md).

## Tests

Two suites. Spec: [SPEC.md §11](SPEC.md#11-tests).

**Mocked (CI default).** `make test`. No vendor account. Hello-world (both walls, probe, echo) and one agentd example against a mock vendor API. Official-CLI tests only check that `claude` / `grok` / `dsh` are on PATH and porter is up; they do not claim a mock-driven chat session. GitHub Actions job `test` runs this on every push and pull request.

**Live (optional, not a merge gate).** Real CLI to the real vendor. Natural-language “run a command / install something” as root.

```bash
export HERMETARIUM_ANTHROPIC_API_KEY=sk-ant-...
export HERMETARIUM_XAI_API_KEY=xai-...
export HERMETARIUM_DEEPSEEK_API_KEY=sk-...
make test-live
```

Unset keys skip that vendor. `make test-live` is `go test -tags live`. On GitHub: Actions → CI → **Run workflow** only (not push/PR). Repository secrets with those names are exported into the job for the supervisor. They must not appear in logs.

## License

[PolyForm Noncommercial License 1.0.0](LICENSE). Source-available; not OSI Open Source. Commercial use is not granted. Other licenses can be negotiated with the copyright holder.

Squid is a separate GPLv2 program, run as a sibling process, not linked into the supervisor.
