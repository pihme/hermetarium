# Cookbook: changing a habitat's wall rules

This page is how you change a gate file (the allowlist, or what is passed through) of a habitat that already exists, and how you move a habitat onto a newer Hermetarium, using ordinary host tooling.

There is no command for this. The CLI has `create` to start and `destroy` to stop; it has no `acl`, `reload`, or `restart`. The path below is done by hand, on the host, against the habitat's containers.

The example is a real cold change on a strong-wall OpenCode habitat. The Docker wall has one container fewer; the rest is the same idea.

## The containers

A habitat keeps its state in its instance directory, `var/<id>`, under the data root that `hermetarium` resolves: `HERMETARIUM_ROOT` if set, else a checkout found from the current directory, else `$XDG_DATA_HOME/hermetarium` (`~/.local/share/hermetarium`). Every `var/<id>/...` path below is relative to that root, so resolve it once first:

```sh
# Same order the tool uses: explicit root, then a checkout from here, then XDG.
if [ -n "${HERMETARIUM_ROOT:-}" ]; then ROOT=$HERMETARIUM_ROOT
elif [ -d ./var ] && [ -f ./go.mod ]; then ROOT=$PWD
else ROOT=${XDG_DATA_HOME:-$HOME/.local/share}/hermetarium
fi
ls "$ROOT/var/<id>/squid.conf"
```

| Container | Wall | Role |
| --- | --- | --- |
| `htm-<id>-fc` | strong | Firecracker container. Owns the network namespace (the TAP device). Its entrypoint `/entrypoint.sh` sets iptables (`gate-rules.sh`, then `rules.ready`), serves the CA when HTTPS is on (`ca.ready`), then **waits for `squid.pid` and `dns.ready` before it boots the microVM**. |
| `htm-<id>-gate` | both | Squid, started as `/usr/sbin/squid -N -f /log/squid.conf`. On the strong wall it runs in the fc container's network namespace. Reads `var/<id>/squid.conf`, mounted at `/log`. |
| `htm-<id>-dns` | both | Gate DNS, in the same network namespace. Writes `dns.ready`. |

The allowlist lives in `$ROOT/var/<id>/squid.conf`; that is the one file you edit. The strong wall's root disk (`var/<id>/rootfs.ext4`) is untouched by a rule change. A cold change boots the same root disk, so the work you did inside the habitat is still there.

A name made before the current naming uses `htm-fc-<id>`, `htm-gate-<id>`, and `htm-dns-<id>`.

## Order

**Start: fc, then gate, then dns.** The fc container is the network namespace owner, and it holds the boot until the gate has written `squid.pid` and DNS has written `dns.ready`. Each wait allows about 20 seconds (`200 × 0.1`), so start the three quickly and in this order.

**Stop: the reverse.** Stop the dependent containers before the network namespace owner, so nothing is left pointing at a namespace that is gone.

## The recipe

1. Stop the gate and DNS, then the Firecracker container.

   ```bash
   docker stop htm-<id>-gate htm-<id>-dns
   docker stop htm-<id>-fc
   ```

2. Edit `$ROOT/var/<id>/squid.conf`. A host that should be passed through (spliced) instead of decrypted is added to the `acl passthrough ssl::server_name ...` list. The registry and blob hosts are in the list below.

3. Remove the stale pid file. Squid refuses to start while `squid.pid` is present, and the file outlives the stopped container.

   ```bash
   rm -f $ROOT/var/<id>/squid.pid
   ```

4. Start the Firecracker container. Its entrypoint sets the firewall, starts the certificate server, and then waits.

   ```bash
   docker start htm-<id>-fc
   ```

5. Start the gate. It writes `squid.pid`.

   ```bash
   docker start htm-<id>-gate
   ```

6. Start DNS. It writes `dns.ready`, and the microVM boots.

   ```bash
   docker start htm-<id>-dns
   ```

The I/O log and `hermetarium url` keep working; nothing in `instance.json` changes.

## Restart only the gate (leave the habitat running)

The recipe above stops the Firecracker container, which shuts down the microVM and the work inside it. When the change is a **pure gate-file edit** — an allowlist entry, a pass-through host, a header rule — you do not need to touch the habitat at all. Only the gate needs a restart, and it can rejoin the running network namespace.

On the strong wall the gate (`htm-<id>-gate`) runs *in* the Firecracker container's network namespace, but it is a **separate Docker container**. The `htm-<id>-fc` container owns that namespace and holds the microVM; as long as it stays up, the gate can be stopped and replaced on its own, and the habitat keeps running.

The change is the same cold change, but the scope is just the gate:

1. Stop only the gate. Leave `htm-<id>-fc` and `htm-<id>-dns` running.

   ```bash
   docker stop htm-<id>-gate
   ```

2. Edit `$ROOT/var/<id>/squid.conf`.

3. Validate the gate file (see below) — before you remove the gate, `docker exec` it; after it is gone, use the one-off `docker run` with the same image.

4. Remove the stale pid file, then remove the old gate container.

   ```bash
   rm -f $ROOT/var/<id>/squid.pid
   docker rm htm-<id>-gate
   ```

5. Create a new gate that joins the running Firecracker container's network namespace, the same way `create` does:

   ```bash
   docker run -d --name htm-<id>-gate \
     --network container:htm-<id>-fc \
     --tmpfs /var/log/squid \
     --tmpfs /var/spool/squid \
     -v "$ROOT/var/<id>:/log" \
     --entrypoint /usr/sbin/squid \
     hermetarium-squid:local \
     -N -f /log/squid.conf
   ```

   The netns owner must be running for this to work; if `docker run` reports it cannot join a non-running container, the Firecracker container has stopped and you are doing the full recipe instead.

6. Confirm the gate is alive and the habitat is still up:

   ```bash
   cat $ROOT/var/<id>/squid.pid
   docker ps --format '{{.Names}} {{.Status}}' | grep htm-<id>
   ```

The only impact is a few seconds of no traffic through the gate while it is down. The microVM, the agent, and any in-habitat work are unaffected. This works on the Docker wall too, where the gate does not join a netns: the recipe below already restarts just the gate.

A `docker start` on the stopped gate is enough when the image did not change (the bind-mounted `squid.conf` is re-read at start). Recreate the container, as above, when the `hermetarium-squid:local` image changed or you want a clean slate.

## Validate the gate file first

Squid can check a gate file without a running habitat. Do this on the same image with the same mount, and do it **before** you stop the gate — a stopped container cannot be `exec`'d, and a gate that will not start on a bad file cannot either.

While the habitat is still up:

```bash
docker exec htm-<id>-gate squid -k parse -f /log/squid.conf
```

With the containers down, run the same image directly and mount the instance directory where the gate expects it:

```bash
docker run --rm -v "$ROOT/var/<id>:/log:ro" hermetarium-squid:local squid -k parse -f /log/squid.conf
```

Either must exit 0. A file that fails here will not start, and on the strong wall the fc container then gives up after its 20-second wait and the habitat does not boot.

## Do not hot-reconfigure when the change touches `splice`

`squid -k reconfigure` (SIGHUP) is not the way to change a pass-through list.

On a real habitat it loaded the new config, and then **every** passed-through host failed with `TLS: wrong version number`. The decrypted (bumped) path kept working. A clean restart, as in the recipe, fixed it.

For any change to `ssl_bump splice`, use the cold change. Treat `squid -k reconfigure` as unsupported for wall changes until it has been shown to work for the change you are making.

## Registry hosts for nested Docker

A container inside the habitat that pulls from Docker Hub goes through the wall. The registry and the blob CDN must be on the pass-through list:

```
registry-1.docker.io
auth.docker.io
production.cloudfront.docker.com
```

The blob CDN is `production.cloudfront.docker.com` (CloudFront). It is **not** `production.cloudflare.docker.com`; that name is a common first guess and pulling fails with it.

Two things to know:

- **The guest `dockerd` trusts the gate CA anyway.** It inherits the wall CA variables (`SSL_CERT_FILE`, `CURL_CA_BUNDLE`, …), so it accepts even a decrypted registry certificate. Passing the registry through is still the cleaner choice: it keeps the real certificates and a smaller surface.
- **The root disk is a snapshot.** A rule change does not touch it. A cold change boots the same `rootfs.ext4`, unchanged.

## Docker wall

The Docker wall has no `htm-<id>-fc`. The gate writes `squid.pid` and the box does not wait on it. Stop and start the gate, and the box if you restarted the network:

```bash
docker stop htm-<id>-gate
rm -f $ROOT/var/<id>/squid.pid
docker start htm-<id>-gate
```

The rest of this page applies unchanged.

## Migrating a habitat to newer rules

A habitat is a stopped set of containers plus the files in `$ROOT/var/<id>/`; nothing else ties it to the version that created it. To move it onto a newer Hermetarium configuration, keep the instance and replace only what changed:

1. Stop the containers in the reverse order above, so the VM shuts down cleanly.
2. Take the new gate file — from the newer example, inhabitant or omakase setup — and re-apply the operator-specific lines you had added (passthrough hosts, extra allowlist entries). Do not copy it over blind: the shipped file is a starting point, not your habitat's history.
3. Validate with `squid -k parse` as above.
4. Start in the order above. `rootfs.ext4` is untouched, so the work inside the habitat survives the migration; only the wall changed.

What this does **not** cover: moving a habitat to a **newer inhabitant image**. That replaces the root disk, so it is a new `create` plus whatever you copy across yourself. There is no in-place image upgrade, and this page does not pretend there is.
