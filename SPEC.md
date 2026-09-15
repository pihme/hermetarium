# Hermetarium

A sealed habitat for software agents. Status: hello-world implemented in Go with Squid (both walls, fail-closed egress, probe, I/O log, inbound echo example).

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
                └── inhabitant (HTTP server: echo now, agent later)
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

Same image. Isolation is the wall.

| Wall | Mechanism | Kernel | Typical supervisor |
| --- | --- | --- | --- |
| Weak | OCI via `runc` (Docker Engine, rootless Podman, ordinary Kubernetes pods) | Shared with the host | A machine running Docker |
| Strong | Firecracker (Kata `RuntimeClass` if the supervisor is Kubernetes) | Own guest kernel | Kubernetes, or Firecracker and its jailer without Kubernetes |

- Strength is the wall, not “laptop versus cluster.”
- Do not mount the operator’s home directory, SSH keys, or host Docker socket unless that is an explicit, logged choice.
- Root inside the image is allowed. On the weak wall, a kernel exploit is a host exploit. On the strong wall, root is only guest root.

Hello-world implements **both** walls. CLI `create` defaults to weak.

## 7. Network and logging

- No default route except Squid. Setting `HTTP_PROXY` inside the image is not the wall.
- Fail closed: if the route, firewall, or Squid cannot be applied, do not start.
- The supervisor **writes a per-habitat ACL file** (default deny, allowlisted destinations only) and starts Squid against it. Squid stays a **sibling process** (GPLv2). The supervisor does not link Squid.
- I/O log: append-only, keyed by Hermetarium id. The supervisor maps Squid `access.log` into that log. Default fields: time, direction (`in` or `out`), protocol, destination, bytes, allowed or denied. Packet bodies are off unless turned on.
- Inbound (operator, APIs, UI) and outbound (inhabitant to the network) use that same path. Fail closed on inbound the same as outbound: if the path cannot be applied, do not start.
- A process that opens a connection without going through an inhabitant “tool” still appears on the I/O log.
- Prefer keeping operator secrets on the supervisor and attaching them only for allowlisted destinations.

Whether bodies are captured (metadata only versus TLS interception) and whether secrets may live in the image are not decided.

**Parked (not this build):** Squid `external_acl_type` helper, Envoy/xDS, OPA/Rego, replacing Squid. ICAP on Squid is the natural plug for a later wall scanner / pseudo-artifactory.

## 7a. Supervisor language

**Go**, compiled to a native binary. Talks to Docker, Firecracker, and Squid as **processes** (`os/exec` + config files + unix sockets). Stdlib covers JSON, HTTP, and exec; do not pull `firecracker-go-sdk` or the Docker client library unless exec is proven insufficient.

Rust was the alternative. It is not smaller here: even a thin supervisor needs `serde`/`serde_json`, and the Firecracker/Docker crates pull Tokio/Hyper. Go stdlib does that work with no modules. Do not rewrite the supervisor in TypeScript, Rust, or a WASM-only runtime.

## 8. Inhabitants

Hermetarium does not care which program it holds. One or more inhabitants start already inside and may change the image. They talk to the outside only through the logged path. The outside talks in the same way: the inhabitant **is** an HTTP server on that path.

Hello-world inbound is an **echo** service (request body returned as response body). Later the same shape is an agent that accepts commands. The operator does not name a host argv to run inside; they send HTTP to the process that is already serving.

## 8a. Operator HTTP

The supervisor publishes a host URL that reaches the inhabitant through Squid. Both walls. CLI `hermetarium url <id>` prints `http://127.0.0.1:<port>/`.

Two uses of that one server:

1. **Call and wait.** Operator sends one HTTP request, waits until the inhabitant has finished handling it, reads the result (status, body). Echo: payload out equals payload in.
2. **Talk to a running server.** The same process stays up. Operator can send further HTTP requests on the same URL. Echo still.

Hello-world inhabitant is `examples/echo/`. Later examples replace that process with an agent that accepts commands.

TLS and auth on this hop are not decided.

## 9. Lifecycle

1. Supervisor creates a Hermetarium: image, wall, network path (in and out), I/O log.
2. Inhabitant HTTP server starts inside and may mutate filesystem and packages.
3. Operator talks to it only through the logged path (call-and-wait, or further requests to the running server).
4. Destroy drops the wall (container or microVM). A snapshot keeps a mutated world.

Whether instances are ephemeral or long-lived is not decided.

## 10. Open questions

- Supervisor shape beyond the CLI: Compose plus Squid, Kubernetes, or only the Go binary
- Persistence: ephemeral or long-lived
- Secrets: supervisor only, or allowed in the image
- I/O log bodies: metadata only, or optional TLS interception (mitmproxy is a candidate later; not the wall proxy)
- When to revisit Squid: `external_acl_type`, OPA beside Squid, or a different proxy

## 11. License

[PolyForm Noncommercial License 1.0.0](LICENSE). Source-available; not OSI Open Source. Other licenses can be negotiated with the copyright holder.
