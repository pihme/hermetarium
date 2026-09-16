# Hermetarium

A sealed habitat for software agents. Status: hello-world implemented in Go with Squid (both walls, fail-closed egress, probe, I/O log, inbound echo). Same OCI image on Firecracker. Supervisor-held API keys. One agentd coding-agent example with mock tests. Official CLIs (Claude Code, Grok Build, DeepSeek Harness) as inhabitant templates.

## 1. Name

**Hermetarium** is a sealed, instrumented place an agent lives in.

- Binary and CLI: `hermetarium` in full. Do not shorten to `herm` (collides with hermes, hermetic, hermit).
- Exact name was unused as a runtime (checked 2026-09-06). Nearby names: Hermetic (credential broker), Hermes Agent, Hermeto, Hermit. Say we are not those.

## 2. Problem

An agent that can run a shell and install packages must not run on the operator’s machine. Wrapping each command in a host-side sandbox is the wrong shape: the agent still lives on the host. Hermetarium is the world the agent wakes up in.

## 3. What it is

An **inhabitant** boots **inside** an OCI image. It may use that image’s Linux freely: shell, package managers, rewriting files. It has no sandbox API and does not ask permission per command.

It must not leave. The **wall** is outside the image. The **supervisor** is a Go binary: it boots the image, starts Squid as the only network path, writes a per-habitat ACL file, and maps Squid’s access log into the I/O log. If that path cannot be applied, Hermetarium does not start.

```
operator machine or cluster
  supervisor (Go binary)
    ├── wall (weak runc | strong Firecracker)
    └── Squid (spawned; per-habitat ACL file; access.log → I/O log)
          └── Hermetarium = that OCI image, running
                └── inhabitant (HTTP server: echo, or a coding-agent harness)
```

Operator and outside network both reach that inhabitant only through Squid. There is no product side channel (`docker exec`, SSH, a console) into the box.

## 4. What it is not

- Not a per-command sandbox the inhabitant invokes.
- Not a host Unix shell, chroot, or jail as the product. A Unix shell still exists **inside** the image.
- Not a host-side exec API as the way the operator talks to the inhabitant. That talk is HTTP through the logged path.
- Not Kubernetes-as-security. Kubernetes may run the strong wall; ordinary `runc` pods on Kubernetes are still the weak wall.

## 5. Vocabulary

| Term | Meaning |
| --- | --- |
| Hermetarium | One running habitat: image + wall + network path. |
| Image | OCI image; what the inhabitant sees as Linux. |
| Wall | Isolation around the image: **weak** (`runc`) or **strong** (Firecracker microVM). |
| Supervisor | Control plane that boots and kills Hermetaria, owns the network path, writes the I/O log. |
| Inhabitant | Agent or process that lives inside. |
| I/O log | Record of traffic that **crosses the wall** (required). |

## 6. Walls

**Same image.** Isolation is the wall. Both walls boot the **same OCI image**: same root filesystem, same inhabitant process (that image’s entrypoint/CMD). The strong wall is not a second, smaller guest world. How Firecracker gets a disk from the OCI image (export to ext4, or equivalent) is an implementation detail of that wall.

| Wall | Mechanism | Kernel | Typical supervisor |
| --- | --- | --- | --- |
| Weak | OCI via `runc` (Docker Engine, rootless Podman, ordinary Kubernetes pods) | Shared with the host | A machine running Docker |
| Strong | Firecracker (Kata `RuntimeClass` if the supervisor is Kubernetes) | Own guest kernel | Kubernetes, or Firecracker and its jailer without Kubernetes |

- Strength is the wall, not “laptop versus cluster.”
- Do not mount the operator’s home directory, SSH keys, or host Docker socket unless that is an explicit, logged choice.
- Root inside the image is allowed. On the weak wall, a kernel exploit is a host exploit. On the strong wall, root is only guest root.

Hello-world implements **both** walls. CLI `create` takes `--image` (any OCI image) and `--wall weak|strong`. Optional `--vendor claude|grok|deepseek` is the Squid allowlist and key inject, not an image name.

## 7. Network and logging

- No default route except Squid. Setting `HTTP_PROXY` inside the image is not the wall.
- Fail closed: if the route, firewall, or Squid cannot be applied, do not start.
- The supervisor **writes a per-habitat ACL file** (default deny, allowlisted destinations only) and starts Squid against it. Squid stays a **sibling process** (GPLv2). The supervisor does not link Squid.
- I/O log: append-only, keyed by Hermetarium id. The supervisor maps Squid `access.log` into that log. Default fields: time, direction (`in` or `out`), protocol, destination, bytes, allowed or denied. Packet bodies are off unless turned on.
- Inbound (operator, APIs, UI) and outbound (inhabitant to the network) use that same path. Fail closed on inbound the same as outbound: if the path cannot be applied, do not start.
- A process that opens a connection without going through an inhabitant “tool” still appears on the I/O log.
- Secrets: see §8.

Whether bodies are captured (metadata only versus TLS interception) is not decided.

**Parked (not this build):** Squid `external_acl_type` helper, Envoy/xDS, OPA/Rego, replacing Squid. ICAP on Squid is the natural plug for a later wall scanner / pseudo-artifactory.

## 7a. Supervisor language

**Go**, compiled to a native binary. Talks to Docker, Firecracker, and Squid as **processes** (`os/exec` + config files + unix sockets). Stdlib covers JSON, HTTP, and exec; do not pull `firecracker-go-sdk` or the Docker client library unless exec is proven insufficient.

Rust was the alternative. It is not smaller here: even a thin supervisor needs `serde`/`serde_json`, and the Firecracker/Docker crates pull Tokio/Hyper. Go stdlib does that work with no modules. Do not rewrite the supervisor in TypeScript, Rust, or a WASM-only runtime.

## 8. Secrets

Secrets live only on the supervisor. The supervisor attaches them on the logged path for allowlisted destinations (header inject/replace in Squid). The inhabitant image, filesystem, and process environment must not contain the real secret. If the inhabitant sends its own `x-api-key` or `Authorization`, Squid **replaces** those headers. Direct access from the box to the real vendor origin is denied.

Secrets-in-image is decided: no. This is **not** TLS interception of inhabitant traffic. The inhabitant speaks HTTP to the path; Squid is the HTTPS client to the vendor.

How a credentialed destination is attached:

- Supervisor reads the key from its environment (see table).
- It writes that key only into the per-habitat Squid config on the **gate** (the box never mounts that file).
- The inhabitant is pointed at an HTTP host on the logged path. It may have no key, or a non-secret placeholder so the program will start.
- Squid reverse-proxies that host to the vendor over HTTPS and sets the real credential header.
- The ACL allowlists that path only.

| Vendor | Supervisor env | Inhabitant origin (HTTP on the path) |
| --- | --- | --- |
| Anthropic | `HERMETARIUM_ANTHROPIC_API_KEY` | `ANTHROPIC_BASE_URL` |
| xAI | `HERMETARIUM_XAI_API_KEY` | `GROK_CLI_CHAT_PROXY_BASE_URL` (or Grok `config.toml` `base_url`) |
| DeepSeek | `HERMETARIUM_DEEPSEEK_API_KEY` | DeepSeek / OpenAI-compatible base URL |

If a habitat’s ACL includes a credentialed destination and the supervisor has no key for it, fail closed (do not start). Tests: mock suite uses a test key only the mock origin accepts; live suite feeds the matching supervisor env var.

## 9. Inhabitants

Hermetarium does not care which program it holds. One or more inhabitants start already inside and may change the image. They talk to the outside only through the logged path. The outside talks in the same way: the inhabitant **is** an HTTP server on that path.

The operator does not name a host argv to run inside.

### 9a. Operator HTTP

The supervisor publishes a host URL that reaches the inhabitant through Squid. Both walls. CLI `hermetarium url <id>` prints `http://127.0.0.1:<port>/`. `create --image NAME` boots any local or pullable OCI image.

Two uses of that one server:

1. **Call and wait.** Operator sends one HTTP request, waits until the inhabitant has finished handling it, reads the result (status, body). Echo: payload out equals payload in.
2. **Talk to a running server.** The same process stays up. Operator can send further HTTP requests on the same URL.

TLS and auth on this hop (operator → supervisor URL) are not decided.

### 9b. Example: echo

`examples/echo/`. Implemented. HTTP echo: request body returned as response body. Hello-world inbound.

### 9c. Coding-agent example

`examples/agentd/`. Same inhabitant shape as echo: HTTP on 8080, one open session, a bash tool, Anthropic-shaped origin on the logged path (`claude.hermetarium.test`). It is a stand-in loop, not Claude Code / Grok Build / DeepSeek Harness. Those official CLIs are `inhabitants/` (porter + the real binary).

Shared with those inhabitants:

- Same OCI image on **both** walls. Runs as **root** inside the image (guest root on the strong wall).
- HTTP server that **stays up** and holds **one open session** for the life of the habitat.
- Operator chat is HTTP turns on `hermetarium url`: POST a message, wait until that turn finishes, read the reply. A later POST is the next turn in the **same** session (not a new process per message).
- Model “home” uses §8. Direct vendor API from the box is denied.
- No SSE/WebSocket. A turn is still call-and-wait.
- The operator path is this HTTP server, not a harness TUI or local web UI.

## 10. Lifecycle

1. Supervisor creates a Hermetarium: image, wall, network path (in and out), I/O log.
2. Inhabitant HTTP server starts inside and may mutate filesystem and packages.
3. Operator talks to it only through the logged path (call-and-wait, or further requests to the running server).
4. Destroy drops the wall (container or microVM). A snapshot keeps a mutated world.

Whether instances are ephemeral or long-lived is not decided.

## 11. Tests

Operator talk in tests is HTTP (`hermetarium url`), never `docker exec`. Live tests are `//go:build live` so `make test` cannot see them.

### Hello-world (implemented)

Both walls, fail-closed egress, probe, I/O log, inbound echo. Command: `make test`.

### Coding-agent mock suite (required, CI default)

Command: `make test`. GitHub Actions job `test` on push and pull_request. No live vendor key. Both walls. One §9c image (`examples/agentd/`). Official-CLI smoke (`inhabitants/`) only checks that `claude` / `grok` / `dsh` are on PATH and porter answers.

**Mock the vendor HTTP API.** The agentd image runs a small tool loop, not an official CLI. A mock origin on the gate (like the probe) speaks enough of the Anthropic Messages API to:

1. Reject requests that lack the supervisor-injected key (and reject a dummy key the box might send).
2. Return a tool call that makes the harness run a **shell command as root** (for example `id -u` or `touch /root/hermetarium-root-ok`).
3. After the tool result, return a final assistant message that includes that evidence.

The operator test then:

1. **Chat / open session.** Two sequential POSTs to the same inbound URL. The second turn is handled by the same open harness session (the process is still running).
2. **Root via the agent.** A turn whose tool use runs a shell command; the HTTP reply (or a file the command created under `/root`) shows uid 0. Installing a package is the same path plus an allowlisted package repo; the mock-suite required test is the shell command.
3. **Key stays on the supervisor.** The test key does not appear in the inhabitant environment or filesystem. Direct vendor origin from the box fails. The I/O log records outbound to that example’s origin as allowed.

What the mock does **not** prove: that a real model would choose that command from a natural-language ask.

### Coding-agent live suite (optional, not a merge gate)

Command: `make test-live`. agentd live requires `HERMETARIUM_ANTHROPIC_API_KEY`. Each official-CLI live test requires that CLI’s supervisor env var (§8) and skips if unset.

Squid still injects the key (§8); the inhabitant still must not contain it. Egress is the real vendor API (allowlisted), not the mock.

The operator test then (per case that has a key):

1. **Chat.** Two sequential POSTs in natural language on the same URL; the second turn depends on the first (same open session, real model).
2. **Root via the agent.** A natural-language ask that the harness should run a shell command as root (for example “run `id -u`” or “install a package”). The HTTP reply or a file under `/root` shows uid 0. This is what the mock cannot prove: the model chose the tool.

Not run on push or pull_request. Must not be required to merge.

### CI

- **Always:** job `test` runs `make test` (hello-world + agentd mock suite + official-CLI smoke).
- **Optional / manual:** job `test-live` on `workflow_dispatch` only. The pipeline supplies keys as repository secrets (`HERMETARIUM_ANTHROPIC_API_KEY`, `HERMETARIUM_XAI_API_KEY`, `HERMETARIUM_DEEPSEEK_API_KEY`) and exports whichever are set into the job environment so the supervisor can read them. Secrets are not written into the image, logs, or the inhabitant. Examples whose secret is empty skip.

## 12. Open questions

- Supervisor shape beyond the CLI: Compose plus Squid, Kubernetes, or only the Go binary
- Persistence: ephemeral or long-lived
- I/O log bodies: metadata only, or optional TLS interception (mitmproxy is a candidate later; not the wall proxy)
- When to revisit Squid: `external_acl_type`, OPA beside Squid, or a different proxy
- Streaming chat (SSE) on the operator HTTP hop

## 13. License

[PolyForm Noncommercial License 1.0.0](LICENSE). Source-available; not OSI Open Source. Other licenses can be negotiated with the copyright holder.
