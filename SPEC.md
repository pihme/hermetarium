# Hermetarium

A sealed habitat for software agents. Status: hello-world implemented in Go with Squid (both walls, fail-closed egress, probe, I/O log, inbound echo). Same OCI image on Firecracker. Operator-supplied Squid ACL (`create --acl`). One agentd coding-agent example with mock tests. Official CLIs (Claude Code, Grok Build, and optional OpenCode) as inhabitant templates.

## 1. Name

**Hermetarium** is a sealed, instrumented place an agent lives in.

- Binary and CLI: `hermetarium` in full. Do not shorten to `herm` (collides with hermes, hermetic, hermit).
- Exact name was unused as a runtime (checked 2026-09-06). Nearby names: Hermetic (credential broker), Hermes Agent, Hermeto, Hermit. Say we are not those.

## 2. Problem

An agent that can run a shell and install packages must not run on the operator’s machine. Wrapping each command in a host-side sandbox is the wrong shape: the agent still lives on the host. Hermetarium is the world the agent wakes up in.

## 3. What it is

An **inhabitant** boots **inside** an OCI image. It may use that image’s Linux freely: shell, package managers, rewriting files. It has no sandbox API and does not ask permission per command.

It must not leave. The **wall** is outside the image. The **supervisor** is a Go binary: it boots the image, starts Squid as the only network path, installs the operator-supplied ACL (`create --acl FILE`), and maps Squid’s access log into the I/O log. If that path cannot be applied, Hermetarium does not start.

```
operator machine or cluster
  supervisor (Go binary)
    ├── wall (weak runc | strong Firecracker)
    └── Squid (spawned; per-habitat ACL file; access.log → I/O log)
          └── habitat (the image, running behind the wall)
                └── inhabitant (HTTP server: echo, or a coding-agent harness)
```

The outside network reaches the inhabitant only through Squid. The operator also has `hermetarium attach`: a terminal into a running habitat on both walls. What passes over that terminal is not in the I/O log. The weak wall terminal is `docker exec`. The strong wall terminal is SSH to the guest. An image that wants a terminal includes an SSH server, the same kind of contract as having `ip`. At `create`, Hermetarium makes a key pair for that habitat, passes the public key to the guest at start, and keeps the private key on the host. The guest SSH port is reachable only from the Firecracker container's network namespace. The inhabitant's only network route remains the gate. Escape sequences from the habitat reach the operator's terminal as they are.

## 4. What it is not

- Not a per-command sandbox the inhabitant invokes.
- Not a host Unix shell, chroot, or jail as the product. A Unix shell still exists **inside** the image.
- Not a host-side exec API as the way the operator talks to the inhabitant. Chat is HTTP through the logged path. `hermetarium attach` is the operator terminal, and those bytes stay out of the I/O log.
- Not Kubernetes-as-security. Kubernetes may run the strong wall; ordinary `runc` pods on Kubernetes are still the weak wall.

## 5. Vocabulary

| Term | Meaning |
| --- | --- |
| Hermetarium | The product. One running instance is a habitat. |
| Habitat | One running instance: image + wall + network path. |
| Image | OCI image; what the inhabitant sees as Linux. |
| Wall | Isolation around the image: **weak** (`runc`) or **strong** (Firecracker microVM). |
| Supervisor | Control plane that boots and kills habitats, owns the network path, writes the I/O log. |
| Inhabitant | Agent or process that lives inside. |
| I/O log | Record of traffic that **crosses the wall** (required). |

The words to use, and the ones to avoid, are in [GLOSSARY.md](GLOSSARY.md).

## 6. Walls

**Same image.** Isolation is the wall. Both walls boot the **same OCI image**: same root filesystem, same inhabitant process (that image’s entrypoint/CMD). The strong wall is not a second, smaller guest world. How Firecracker gets a disk from the OCI image (export to ext4, or equivalent) is an implementation detail of that wall. `create --wall strong --rootfs FILE` uses that file as the strong wall's root disk instead of a fresh pack of that image. `--image` and `--acl` are still required. `destroy` deletes the copy and leaves `FILE`.

| Wall | Mechanism | Kernel | Typical supervisor |
| --- | --- | --- | --- |
| Weak | OCI via `runc` (Docker Engine, rootless Podman, ordinary Kubernetes pods) | Shared with the host | A machine running Docker |
| Strong | Firecracker (Kata `RuntimeClass` if the supervisor is Kubernetes) | Own guest kernel | Kubernetes, or Firecracker and its jailer without Kubernetes |

- Strength is the wall, not “laptop versus cluster.”
- Do not mount the operator’s home directory, SSH keys, or host Docker socket unless that is an explicit, logged choice.
- Root inside the image is allowed. On the weak wall, a kernel exploit is a host exploit. On the strong wall, root is only guest root.

The weak wall inserts `FORWARD -i br-<id> -o br-<id> -j ACCEPT` in both iptables-nft and iptables-legacy, through a privileged host-network container. Destroy removes nft `table ip filter` only while the owned marker is still held and the table contains nothing but iptables-nft's FORWARD, INPUT, and OUTPUT chains, each with no rules and policy accept. Anything else stays, including a drop policy or a table the host restored over a stale marker.

Hello-world implements **both** walls. CLI `create` takes `--image` (any OCI image), `--wall weak|strong`, `--acl FILE` (Squid config mounted into the gate), and an optional `--id NAME`. Without `--id`, `create` prints a new id. The id may be a name.

## 7. Network and logging

- No default route except Squid. Setting `HTTP_PROXY` inside the image is not the wall.
- Fail closed: if the route, firewall, or Squid cannot be applied, do not start.
- The supervisor **installs an operator-supplied Squid ACL** (`create --acl FILE`, copied to `var/<id>/squid.conf`) and starts Squid against it. Squid stays a **sibling process** (GPLv2). The supervisor does not link Squid. Each example and inhabitant ships its wall config next to its Dockerfile.
- I/O log: append-only, keyed by Hermetarium id. The supervisor maps Squid `access.log` into that log. Default fields: time, direction (`in` or `out`), protocol, destination, port, method, path, bytes, and allowed or denied. A CONNECT line also has `upstream`, the server address. `tls` (`decrypted` or `passthrough`) is set when the gate file logs the bump mode. A CONNECT closed before a tunnel existed (`NONE_*_ABORTED`) has `aborted` set, and its destination is the host name when SNI was logged. A tunnel that later closes does not. Packet bodies are not captured.
- Inbound (operator, APIs, UI) and outbound (inhabitant to the network) use that same path. Fail closed on inbound the same as outbound: if the path cannot be applied, do not start.
- A process that opens a connection without going through an inhabitant “tool” still appears on the I/O log.
- Secrets: see §8.

On both walls, the habitat may reach the gate only on TCP 3128, TCP 3130, TCP 3131, and TCP/UDP 53. Other packets from the habitat to the gate, including ICMP, are dropped.

Message bodies stay out of the log. Optional decryption of ordinary HTTPS is `OPEN-HTTPS` in the gate file.

The gate resolves names for the habitat. `probe.hermetarium.test`, `claude.hermetarium.test`, and `grok.hermetarium.test` always resolve to the gate address, which is what a process started inside the habitat needs when it does not see `/etc/hosts`. Any other name resolves only when the gate file opens the web: an uncommented `http_access allow` whose ACL is not `inbound`, `vendorhost`, `probe`, or `probe_dst`. The resolver forwards those queries to the same servers Squid uses. A query received on TCP is forwarded on TCP. A truncated UDP answer is fetched again on TCP; when that message is larger than 512 octets, the UDP client keeps the truncated answer and retries on TCP. At most 32 queries are in flight. A TCP query takes a slot only once the query has arrived, and a TCP query that arrives when the cap is full is closed. Further UDP packets are dropped. The habitat cannot resolve outside names through any resolver except the gate. Each query is an I/O log line (`protocol` `dns`, `destination` the name, `method` the type, `result` the answer) in `dns.jsonl`. That file is capped at 1 MiB, and one previous file, `dns.jsonl.1`, is kept. A file that is already over the cap when gate DNS starts is rotated, not discarded. `hermetarium logs` copies both files into the I/O log. The strong-wall guest also has `127.0.0.1 localhost` in `/etc/hosts`.

**Parked (not this build):** Squid `external_acl_type`, Envoy/xDS, OPA/Rego, replacing Squid. ICAP on Squid is the natural plug for a later wall scanner / pseudo-artifactory.

## 7a. Supervisor language

**Go**, compiled to a native binary. Talks to Docker, Firecracker, and Squid as **processes** (`os/exec` + config files + unix sockets). Stdlib covers JSON, HTTP, and exec; do not pull `firecracker-go-sdk` or the Docker client library unless exec is proven insufficient.

Rust was the alternative. It is not smaller here: even a thin supervisor needs `serde`/`serde_json`, and the Firecracker/Docker crates pull Tokio/Hyper. Go stdlib does that work with no modules. Do not rewrite the supervisor in TypeScript, Rust, or a WASM-only runtime.

## 8. Secrets

Secrets must not live in the inhabitant image, filesystem, or process environment. They belong in the **host-side ACL file** passed to `create --acl` (header inject/replace in Squid). If the inhabitant sends its own `x-api-key` or `Authorization`, that ACL should replace those headers. Direct access from the box to the real vendor origin is denied.

Secrets-in-image is decided: no. The model path stays plain HTTP inside the container. Squid opens HTTPS to the vendor and adds the key. The container does not encrypt that call.

Ordinary HTTPS from the container is closed unless the gate file's `OPEN-HTTPS` lines are on. Those lines make Squid decrypt that traffic (package downloads, curl, and the like). The I/O log then records the site and the path. Message bodies stay out of the log. The private key stays on the gate. `create` installs the public certificate into the container, so the inhabitant sets no proxy. The gate image is OpenSSL Squid for every habitat (`supervisor/squid/Dockerfile`, tag `hermetarium-squid:local`). A client that pins its own certificate list and ignores the container trust store still rejects the gate certificate.

`examples/openweb/squid.conf` decrypts HTTPS except a short pass-through list, which keeps the site's certificate. For `api.github.com` it drops the box `Authorization` and sets the gate token from that file. Reads are allowed. Creating a repository, a pull request, an issue, or a comment is allowed. Other writes on that host are denied, including contents, Git data, deletes, and repository settings. `tls` in the I/O log is `decrypted` or `passthrough`.

Before an operator turns decryption on, the state of the keys behind it, and what a leak of one costs:

- **One CA per habitat.** `create` makes a fresh self-signed CA (`PrepareMITM`, under the instance directory). Two habitats never share a CA.
- **One leaf key per habitat.** When Squid decrypts, it signs a certificate for every intercepted host (`github.com`, `api.github.com`, a package registry, a CDN, the model endpoint, …) under that one root. The certificates differ in their names only. The key inside them is the same key for every host in that habitat.
- **One root, about ten years, no intermediate.** The root is self-signed (`CN=Hermetarium`) and valid for ten years. The chain the inhabitant sees is `leaf -> CN=Hermetarium`.
- **The private key never enters the habitat.** It stays on the host, in the instance directory (`mitm/ca.key`), mounted on the gate only. The habitat receives the public certificate alone (`/etc/ssl/hermetarium-mitm.crt`, served at `GateCAPort` `/ca.pem`, installed by `gate-cert.sh`), and `MITMTrustEnv` points the usual TLS libraries at it. The primary threat model, the inhabitant as attacker, therefore cannot read the key out of its world.

What a leak of that key buys, and the limits of it:

1. **Inside one habitat, every host.** Anyone who holds the key can impersonate any host the gate connects to the inhabitant for, and the inhabitant cannot tell two of those hosts apart by key. There is no per-host attribution.
2. **For the life of the root.** The ten-year root, with no rotation, keeps the key usable in every image built while it is trusted. The inhabitant cannot notice, because it keeps trusting the root.
3. **How the key can leak at all.** Not through the habitat. The realistic paths are an exploit of Squid or of the host, or a compromised Squid image.

This is the accepted risk of "key on the gate". Per-host keys and root rotation are tracked separately and are not part of this build.

How a credentialed destination is attached:

- The operator’s Squid config allowlists the path and contains the credential headers.
- That file is copied into `var/<id>/squid.conf` on the **gate** (the box never mounts it).
- The inhabitant is pointed at an HTTP host on the logged path. It may have no key, or a non-secret placeholder so the program will start.
- Squid reverse-proxies that host to the vendor over HTTPS and sets the real credential header. For xAI it also sets `Host` to `api.x.ai`. xAI answers 403 when that name is still the stand-in. Squid 6.14 has no `forcedomain` peer option, so the gate file replaces the header.

| Vendor | Typical inhabitant origin (HTTP on the path) |
| --- | --- |
| Anthropic | `ANTHROPIC_BASE_URL` |
| xAI | `GROK_XAI_API_BASE_URL` set to `http://grok.hermetarium.test/v1`. On Grok 1.0.77, `GROK_CLI_CHAT_PROXY_BASE_URL` does not move the sampler. The gate credential must be an xAI API key. A login session token gets 401 from `GET /v1/api-key`, and Grok stops. |
| OpenAI-compatible | `http://model.hermetarium.test/v1`. The gate file names the upstream peer, the `Host` value, a path prefix, and the bearer key. Squid drops the box's `Authorization` and sets that key. The OpenCode image uses this row. |

Fail closed if `--acl` is missing or Squid cannot start. Claude and Grok gate files use `__VENDOR_KEY__` for the inject header; tests substitute a mock key or a live env var before `create`. OpenCode uses the `__MODEL_*` words, filled with the pretend-model key, and has no live case. The supervisor does not read vendor API keys from its environment.

## 9. Inhabitants

Hermetarium does not care which program it holds. One or more inhabitants start already inside and may change the image. They talk to the outside only through the logged path. The outside talks in the same way: the inhabitant **is** an HTTP server on that path.

`hermetarium attach <id> [-- CMD]` opens a terminal in a running habitat. With no command, the shell is an interactive login bash when the image has bash, and `sh` otherwise. The image entrypoint stays the habitat's own process. Bytes on the terminal are not in the I/O log, and the command gains no network path.

On the weak wall the terminal is `docker exec` with a TTY. On the strong wall it is SSH. The image brings the SSH server. Images are reused across habitats, so the key is not baked into the image: `create` writes a key pair under `var/.attach/<id>/` on the host, and the guest init writes that public key into `authorized_keys` when it starts. The private key is mounted only into the short-lived SSH client, which shares the Firecracker container's network namespace. No SSH port is published. The client does not check the guest host key (`StrictHostKeyChecking=no`, known hosts `/dev/null`). That is acceptable because the client reaches the habitat only through the Firecracker container's network namespace, and there is no published port. Guest init mounts `/dev/pts` so the session has a pty. Closing the strong-wall terminal sends SIGHUP to a command that is not in `tmux`. A `tmux` session keeps running, and a later `attach` shows it. An image with no SSH server still boots. When nothing listens on port 22, `attach` on that strong-wall habitat says nothing is listening: the image has no SSH server at `/usr/sbin/sshd`, or it did not start (see USAGE). Other SSH errors stay the client's text.

### 9a. Operator HTTP

The supervisor publishes a host URL that reaches the inhabitant through Squid. Both walls. CLI `hermetarium url <id>` prints `http://127.0.0.1:<port>/`. `create --image NAME` boots any local or pullable OCI image. `create --no-inbound` publishes no host port and does not wait for port 8080. The gate file may then omit the inhabitant peer and the inbound port. The bridge gateway is not allowed onto port 18081, so nothing on the host reaches that listener even when the gate file still opens it. `url` prints `this habitat has no inbound address`. Without the flag, create still publishes the port and waits for port 8080.

Two uses of that one server:

1. **Call and wait.** Operator sends one HTTP request, waits until the inhabitant has finished handling it, reads the result (status, body). Echo: payload out equals payload in.
2. **Talk to a running server.** The same process stays up. Operator can send further HTTP requests on the same URL.

TLS and auth on this hop (operator → supervisor URL) are not decided.

### 9b. Example: echo

`examples/echo/`. Implemented. HTTP echo: request body returned as response body. Hello-world inbound.

### 9c. Coding-agent example

`examples/agentd/`. Same inhabitant shape as echo: HTTP on 8080, one open session, a bash tool, Anthropic-shaped origin on the logged path (`claude.hermetarium.test`). It is a stand-in loop, not Claude Code, Grok Build, or OpenCode. Those CLIs are `inhabitants/` (porter + the real binary). OpenCode is optional. Echo and agentd stay the images for the wall, egress, and log tests.

Shared with those inhabitants:

- Same OCI image on **both** walls. Runs as **root** inside the image (guest root on the strong wall).
- HTTP server that **stays up** and holds **one open session** for the life of the habitat.
- Operator chat is HTTP turns on `hermetarium url`: POST a message, wait until that turn finishes, read the reply. For Claude Code, Grok Build, and OpenCode, a later POST is the next turn in the **same** session (not a new process per message). Porter, inside the image, runs the CLI.
- Model “home” uses §8. Direct vendor API from the box is denied.
- No SSE/WebSocket. A turn is still call-and-wait.
- Operator chat for this harness is this HTTP server. A terminal program inside the image is reached with `hermetarium attach`. What passes over the terminal is not in the I/O log.

## 10. Lifecycle

1. Supervisor creates a Hermetarium: image, wall, network path (in and out), I/O log.
2. Inhabitant HTTP server starts inside and may mutate filesystem and packages.
3. The operator talks to an HTTP inhabitant through the logged path (call-and-wait, or further requests to the running server). `hermetarium attach` is the terminal; those bytes are not in the I/O log.
4. Destroy drops the wall (container or microVM). Keeping a mutated world would need a snapshot; that is not implemented.

Whether instances are ephemeral or long-lived is not decided.

## 11. Tests

What the suite checks today is [TESTING.md](TESTING.md). Operator talk in tests is HTTP (`hermetarium url`), and `hermetarium attach` when a test checks the terminal. Live tests are `//go:build live` so `make test` cannot see them.

### Hello-world (implemented)

Both walls, fail-closed egress, probe, I/O log, inbound echo. Command: `make test`.

### Coding-agent mock suite (required, CI default)

Command: `make test`. GitHub Actions job `test` on push and pull_request. No live vendor key. Both walls. One §9c image (`examples/agentd/`). Official-CLI smoke (`inhabitants/`) only checks that `claude`, `grok`, and `opencode` are on PATH and porter answers. The OpenCode case uses the pretend model on `model.hermetarium.test` and does not need a paid key. It does not replace the agentd wall tests.

**Mock the vendor HTTP API.** The agentd image runs a small tool loop, not an official CLI. A mock origin on the gate (like the probe) speaks enough of the Anthropic Messages API to:

1. Reject requests that lack the ACL-injected key (and reject a dummy key the box might send).
2. Return a tool call that makes the harness run a **shell command as root** (for example `id -u` or `touch /root/hermetarium-root-ok`).
3. After the tool result, return a final assistant message that includes that evidence.

The operator test then:

1. **Chat / open session.** Two sequential POSTs to the same inbound URL. The second turn is handled by the same open harness session (the process is still running).
2. **Root via the agent.** A turn whose tool use runs a shell command; the HTTP reply (or a file the command created under `/root`) shows uid 0. Installing a package is the same path plus an allowlisted package repo; the mock-suite required test is the shell command.
3. **Key stays in the ACL.** The test key does not appear in the inhabitant environment or filesystem. Direct vendor origin from the box fails. The I/O log records outbound to that example’s origin as allowed.

What the mock does **not** prove: that a real model would choose that command from a natural-language ask.

### Coding-agent live suite (optional, not a merge gate)

Command: `make test-live`. agentd live requires `HERMETARIUM_ANTHROPIC_API_KEY`. Each official-CLI live test requires that CLI’s key env var and skips if unset. Tests write the key into a copy of the habitat ACL; the supervisor does not read those env vars.

Squid injects the key from that ACL (§8); the inhabitant still must not contain it. Egress is the real vendor API (allowlisted), not the mock.

The operator test then (per case that has a key):

1. **Chat.** Two sequential POSTs in natural language on the same URL; the second turn depends on the first (same open session, real model).
2. **Root via the agent.** A natural-language ask that the harness should run a shell command as root (for example “run `id -u`” or “install a package”). The HTTP reply or a file under `/root` shows uid 0. This is what the mock cannot prove: the model chose the tool.

Not run on push or pull_request. Must not be required to merge.

### CI

- **Always:** job `test` runs `make test` (hello-world + agentd mock suite + official-CLI smoke).
- **Optional / manual:** job `test-live` on `workflow_dispatch` only. The pipeline supplies keys as repository secrets (`HERMETARIUM_ANTHROPIC_API_KEY`, and `HERMETARIUM_XAI_API_KEY`) and exports whichever are set so tests can substitute them into the habitat ACL. Secrets are not written into the image, logs, or the inhabitant. Cases whose secret is empty skip.

## 12. Open questions

- Supervisor shape beyond the CLI: Compose plus Squid, Kubernetes, or only the Go binary
- Persistence: ephemeral or long-lived
- I/O log bodies stay uncaptured. Optional decryption of the container's ordinary HTTPS is `OPEN-HTTPS` on the Squid gate, not a second proxy.
- When to revisit Squid: `external_acl_type`, OPA beside Squid, or a different proxy
- Streaming chat (SSE) on the operator HTTP hop

## 13. License

[PolyForm Noncommercial License 1.0.0](LICENSE). Source-available; not OSI Open Source. Other licenses can be negotiated with the copyright holder.
