# Hermetarium

A sealed habitat for software agents. Status: hello-world implemented in Go with Squid (both walls, fail-closed path, I/O log).

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
                └── inhabitants
```

## 4. What it is not

- Not a per-command sandbox the inhabitant invokes.
- Not a host Unix shell, chroot, or jail as the product. A Unix shell still exists **inside** the image.
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
- I/O log: append-only, keyed by Hermetarium id. The supervisor maps Squid `access.log` into that log. Default fields: time, direction, protocol, destination, bytes, allowed or denied. Packet bodies are off unless turned on.
- Inbound (operator, APIs, UI) and outbound (inhabitant to the network) use that same path.
- A process that opens a connection without going through an inhabitant “tool” still appears on the I/O log.
- Prefer keeping operator secrets on the supervisor and attaching them only for allowlisted destinations.

Whether bodies are captured (metadata only versus TLS interception) and whether secrets may live in the image are not decided.

**Parked (not this build):** Squid `external_acl_type` helper, Envoy/xDS, OPA/Rego, replacing Squid. ICAP on Squid is the natural plug for a later wall scanner / pseudo-artifactory.

## 7a. Supervisor language

**Go**, compiled to a native binary. Talks to Docker, Firecracker, and Squid as **processes** (`os/exec` + config files + unix sockets). Stdlib covers JSON, HTTP, and exec; do not pull `firecracker-go-sdk` or the Docker client library unless exec is proven insufficient.

Rust was the alternative. It is not smaller here: even a thin supervisor needs `serde`/`serde_json`, and the Firecracker/Docker crates pull Tokio/Hyper. Go stdlib does that work with no modules. Do not rewrite the supervisor in TypeScript, Rust, or a WASM-only runtime.

## 8. Inhabitants

Hermetarium does not care which program it holds. One or more inhabitants start already inside and may change the image. They talk to the outside only through the logged path.

## 9. Lifecycle

1. Supervisor creates a Hermetarium: image, wall, network path, I/O log.
2. Inhabitants start inside and may mutate filesystem and packages.
3. Supervisor talks to them only through the logged path.
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
