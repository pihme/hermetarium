# Usage

Operator CLI is `hermetarium` in full. See [README](README.md) for install. Build from this tree:

```bash
make build
./bin/hermetarium version
export HERMETARIUM_ROOT=$(pwd)   # if you run from another directory (data dir / checkout)
```

Firecracker helper scripts are embedded. Squid is a pulled image. `create` needs `--image` and `--acl FILE`. Without a checkout, `var/` is `~/.local/share/hermetarium/var/` and cache is `~/.cache/hermetarium`.

Needs Docker and Go 1.24+. The **strong** wall also needs `/dev/kvm` and **x86_64**.

## Two walls

`--wall` is the isolation mode. The inhabitant image is the same; the wall is outside it.

| Mode | Flag | What it is | Needs |
| --- | --- | --- | --- |
| Weak | `--wall weak` (default) | Docker/`runc` container. Kernel shared with the host. Root in the box is host-root if the kernel is exploited. | Docker |
| Strong | `--wall strong` | Firecracker microVM. Own guest kernel. Root is guest root only. | Docker, `/dev/kvm`, x86_64 |

```bash
id=$(./bin/hermetarium create --wall weak --image myorg/box:1 --acl examples/echo/squid.conf)
id=$(./bin/hermetarium create --wall strong --image myorg/box:1 --acl examples/echo/squid.conf)
```

`--image` is required: any local tag or a name Docker can `pull`. The image must listen on **TCP 8080** (porter or your own HTTP server). `--acl FILE` is the Squid config mounted into the gate (copied to `var/<id>/squid.conf`).

```bash
./bin/hermetarium create --wall weak --image myorg/box:1 --acl examples/echo/squid.conf
```

Templates in `examples/` and `inhabitants/` are how you *build* images; they are not CLI names. Strong wall: 512 MiB / 1 GiB disk by default.

Talk, then tear down:

```bash
./bin/hermetarium url "$id"          # http://127.0.0.1:<port>/ through Squid
curl -sS -d 'hello' "$(./bin/hermetarium url "$id")"
./bin/hermetarium logs "$id"
./bin/hermetarium destroy "$id"
```

`exec` exists only on the **weak** wall and is not the product path. Operator talk is HTTP on `url`.

## Squid

```bash
./bin/hermetarium create --wall weak --image myorg/box:1 --acl examples/echo/squid.conf
```

`--acl FILE` is required. The supervisor copies that file to `var/<id>/squid.conf` and starts Squid with `-f /log/squid.conf`. The instance directory is bind-mounted at `/log` in the Squid container. Create fails closed if the file cannot be read or Squid will not start.

`create` does not rewrite the file. Habitat vendor configs use `__VENDOR_KEY__` for the inject header; substitute your key before passing `--acl`. The image must not hold the real key.

Each example and inhabitant is a pair: Dockerfile plus `squid.conf` (`examples/echo/squid.conf`, `inhabitants/claude-code/squid.conf`, …). Copy one of those and edit the ACL. These settings are the wall contract; if they are wrong, `url`, `logs`, or intercept will not work.

**Paths (instance dir = `/log`)**

| Directive | Required value | Why |
| --- | --- | --- |
| (the file itself) | `/log/squid.conf` | `squid -N -f /log/squid.conf` |
| `pid_filename` | `/log/squid.pid` | create waits until this file exists |
| `access_log` | `stdio:/log/access.log …` | `hermetarium logs` reads this |
| `cache_log` | `/log/cache.log` | Squid cache log on the host |

**Ports and names**

| Directive | Required value | Why |
| --- | --- | --- |
| `http_port` intercept | `0.0.0.0:3128 intercept` | iptables redirects box `:80` here |
| `http_port` inbound | `0.0.0.0:18081 accel … vhost` | `hermetarium url` is this port on localhost |
| extra `http_port` | e.g. `127.0.0.1:3129` | Squid 6 needs at least one non-intercept port |
| `cache_peer` inhabitant | `inhabitant parent 8080 … originserver` | supervisor names the box `inhabitant`; it must listen on 8080 |
| `cache_effective_user` | `proxy` | user in the Ubuntu Squid image |

**I/O log format.** `hermetarium logs` parses Squid `access.log` as whitespace fields. Use this `logformat` (or keep the same field order) and attach it to `access_log`. The last token is the local port; 18081 is tagged `direction: in`.

```
logformat htm %ts.%03tu %6tr %>a %Ss/%03>Hs %<st %rm %ru %[un %Sh/%<a %mt %>lp
access_log stdio:/log/access.log htm
```

**Policy the wall assumes.** Default deny (`http_access deny all`). Inbound (`myport 18081`) reverse-proxies only to `inhabitant`. Box HTTP is intercepted, not a default route around Squid. `always_direct deny all` / `never_direct allow all` so allowlisted destinations go through `cache_peer`s. Vendor allowlists and header inject (`request_header_add x-api-key …`) are yours to add; see `examples/agentd/squid.conf` and `inhabitants/*/squid.conf`.

## Logs

```bash
./bin/hermetarium logs <id>
```

That syncs Squid `access.log` into `var/<id>/io.jsonl` and prints JSON lines (`time`, `direction` `in`/`out`, `protocol`, `destination`, `allowed`, `bytes`, …). Bodies are off.

On disk, same directory:

| File | What |
| --- | --- |
| `io.jsonl` | I/O log (product) |
| `access.log` | raw Squid access log |
| `cache.log` | Squid internals |
| `squid.conf` | ACL actually running |
| `instance.json` | id, wall, `inboundUrl` |
| `serial.log` / `fc.log` | strong wall only (guest console / Firecracker) |

## Inhabitants run as root

Every stock inhabitant and example runs as **root** in the image (guest root on the strong wall). That is allowed. On the weak wall, a kernel exploit is a host exploit. On the strong wall, root cannot leave the microVM.

Claude Code refuses `--dangerously-skip-permissions` as root unless it thinks it is sandboxed. Stock images set `IS_SANDBOX=1` and `CLAUDE_CODE_BUBBLEWRAP=1` for that. Do not treat those as a security boundary; the wall is outside the image.

## Mix-and-match inhabitant images

`inhabitants/claude-code/`, `inhabitants/grok-build/`, and `inhabitants/deepseek-harness/` are **templates**, not a closed set. Copy one, swap the CLI install, keep the contract the supervisor already assumes:

1. Process listens on **TCP 8080**. Operator POSTs a turn; GET can be a health check.
2. Runs as **root**.
3. Image contains `iproute2` (and usually `iptables`) so the wall can set the default route.
4. Vendor traffic goes to the HTTP host on the logged path (`ANTHROPIC_BASE_URL=http://claude.hermetarium.test`, `GROK_CLI_CHAT_PROXY_BASE_URL=http://grok.hermetarium.test`, or DeepSeek/OpenAI base URL `http://deepseek.hermetarium.test`), **not** straight to the public API.
5. Dummy key env so the CLI starts (`not-the-supervisor-key`). Squid replaces the header.

Boot the image with `--image` and pass an `--acl` that allowlists that vendor if you want the logged path to inject a key.

Example: Claude Code plus extra tools, still using porter:

```dockerfile
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates curl bash iproute2 iptables procps git \
	&& rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://claude.ai/install.sh | bash \
	&& ln -sf /root/.local/bin/claude /usr/local/bin/claude
WORKDIR /work
COPY porter /usr/local/bin/porter
COPY settings.json /root/.claude/settings.json
ENV HARNESS=claude
ENV HARNESS_WORKDIR=/work
ENV ANTHROPIC_BASE_URL=http://claude.hermetarium.test
ENV IS_SANDBOX=1
ENV CLAUDE_CODE_BUBBLEWRAP=1
ENV CI=true
ENV ANTHROPIC_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
```

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o inhabitants/claude-code/porter ./porter
docker build -t myorg/claude:dev -f Dockerfile inhabitants/claude-code   # context holds settings.json + porter
sed 's/__VENDOR_KEY__/sk-ant-.../' inhabitants/claude-code/squid.conf > /tmp/claude.acl
./bin/hermetarium create --wall weak --image myorg/claude:dev --acl /tmp/claude.acl
```

Mix: take the Claude install + `HARNESS=claude` block from one template, Grok’s `GROK_CLI_CHAT_PROXY_BASE_URL` from another, or drop porter and put your own HTTP server on 8080. The wall does not care which program answers, only that something listens.

## Different base images (Docker-in-Docker, Kubernetes-in-Docker)

The stock bases are `debian:bookworm-slim` and `node:22-bookworm-slim`. You can `FROM` something else if you keep the contract above (root, 8080, `ip`, vendor URL, dummy key).

**Docker-in-Docker.** Start from a dind-capable image (for example `docker:24-dind` or Debian plus Docker Engine). Install the CLI and porter on top. The engine inside the box typically needs cgroup access (`--privileged` on the **box**, or the right cgroup mounts). The supervisor’s weak wall today only adds `NET_ADMIN` to the box, not `--privileged` and not the **host** Docker socket. Do not mount the host Docker socket into the habitat (that is a hole in the wall). Nested Docker on the **strong** wall is a poor fit: Firecracker’s guest kernel is old and small; dind wants a modern kernel and a lot of RAM.

**Kubernetes-in-Docker (KinD and similar).** Same story: the image can contain `kind` / `k3s` / `minikube`, but those want privileged, nested cgroups, and gigabytes of memory. Prefer the **weak** wall, extra capabilities you add in the supervisor if you take that on, and a larger Firecracker `mem_size_mib` / disk if you insist on strong. `create --wall strong` itself defaults to 512 MiB / 1 GiB disk (see above); the test suite asks for less for the echo/agentd examples (128 MiB) and more for the official-CLI inhabitants (2 GiB) by passing explicit sizes — KinD needs more still.

**Kernel vs userspace.** Strong wall boots the OCI root filesystem on the Firecracker kernel (currently 4.14). A base that needs systemd, cgroup v2, or a new glibc syscall may run on weak and fail on strong. Try weak first.

## Porter in a custom image

Porter is a small Go binary: listen on `:8080`, exec `claude` / `grok` / `dsh` for each POST. `claude` and `grok` get `--continue` after the first turn. `dsh --profile headless` has no resume flag (it answers one task and exits), so each `dsh` turn runs standalone, not as a continuation of the prior one.

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o porter ./porter
```

In the Dockerfile:

```dockerfile
COPY porter /usr/local/bin/porter
ENV HARNESS=claude          # or grok, or dsh
ENV HARNESS_WORKDIR=/work
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
```

`HARNESS` selects the argv (see `porter/main.go`). Put the matching CLI on `PATH`. Porter does not hold the real API key; point the CLI at the hermetarium vendor host and let Squid inject credentials.

You can skip porter and serve HTTP yourself on 8080 (echo does). Porter is only the adapter for “operator POST → official CLI turn.”
