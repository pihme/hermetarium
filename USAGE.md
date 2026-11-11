# Usage

This page is how you run a habitat, and how a container of your own must behave. Install steps are in the [readme](README.md).

The command is `hermetarium`.

## Build

From the repository folder.

```bash
make build
```

```bash
./bin/hermetarium version
```

If you run the command from another folder, set `HERMETARIUM_ROOT` to this repository so it can find its files.

```bash
export HERMETARIUM_ROOT=/path/to/hermetarium
```

The Firecracker helper scripts are inside the `hermetarium` binary. Squid is an image Docker pulls. Away from a checkout, a habitat's files live in `~/.local/share/hermetarium/var/` and the cache lives in `~/.cache/hermetarium`.

You need Docker and Go 1.24 or newer. The strong wall also needs Linux, `/dev/kvm`, and x86_64.

## Two walls

The assistant image is the same on either wall. The wall is outside the image.

```
   weak    a Docker container    shares this machine's kernel
   strong  its own tiny machine  needs Linux with KVM
```

On the Docker wall, root in the container reaches the host if the kernel is exploited. On the strong wall the container is a virtual machine with its own kernel, and root stays inside it.

Both walls let only the gate reach the Internet. The Docker wall puts the container on an internal network with no NAT, and gives only the gate a second path out. [How that works](inhabitants/README.md#how-the-gate-reaches-the-internet).

Build the hello-world image. It needs Docker. The tag is `hermetarium/echo:dev`.

```bash
make echo-image
```

Start it on Docker. The command prints an id: `YOUR_ID`. Use that printed line below. To choose it, add `--id NAME`, for example `--id echo`. The command prints that name. A name uses letters, digits, `.`, `_`, and `-`, starts with a letter or a digit, and stays within 63 characters.

```bash
./bin/hermetarium create --wall weak --image hermetarium/echo:dev --acl examples/echo/squid.conf
```

Start the same image in a virtual machine. The defaults are 512 MiB of memory and 1024 MiB of disk.

```bash
./bin/hermetarium create --wall strong --image hermetarium/echo:dev --acl examples/echo/squid.conf
```

To choose the size, pass MiB. The disk size is in MiB too.

```bash
./bin/hermetarium create --wall strong --mem 2048 --disk 2048 --image hermetarium/echo:dev --acl examples/echo/squid.conf
```

`--image` is required. Any local tag works, and so does a name Docker can pull. Something in the image must listen on port 8080. `--acl FILE` is the gate file. Hermetarium copies it to `var/<id>/squid.conf` as you wrote it.

`--mem` and `--disk` belong to the strong wall. The Docker wall refuses them.

Ask where it is listening:

```bash
./bin/hermetarium url YOUR_ID
```

That prints an address like `http://127.0.0.1:12345/`. Send a line there:

```bash
curl -d 'hello' http://127.0.0.1:12345/
```

See what went in and out:

```bash
./bin/hermetarium logs YOUR_ID
```

Stop it:

```bash
./bin/hermetarium destroy YOUR_ID
```

The address from `url` is how you talk to the habitat. `exec` runs a command on the Docker wall.

```bash
./bin/hermetarium exec YOUR_ID -- hostname
```

The folders `examples/` and `inhabitants/` are images you build. They are not command names.

## The gate file

`create` needs `--acl FILE`. Hermetarium copies that file as you wrote it and starts Squid with `-f /log/squid.conf`. The habitat's directory is mounted at `/log` inside the Squid container. The habitat starts only when that file can be read and Squid starts.

The real API key stays in the gate file. In the assistant files the word to replace is `__VENDOR_KEY__`. Put your key in a copy of the file before `create`.

Each example and each assistant ships a Dockerfile and a `squid.conf`. The shared shape is in the [inhabitants README](inhabitants/README.md). [Squid](https://www.squid-cache.org/) reads the file. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). A gate of your own still needs the lines below. `url`, `logs`, and the container's web path depend on them.

The habitat directory is `/log` for Squid. `create` waits until the pid file exists.

```
pid_filename /log/squid.pid
```

Squid's own logs land in that same directory. `hermetarium logs` reads the access log.

```
cache_log /log/cache.log
access_log stdio:/log/access.log htm
```

`hermetarium logs` splits the access log on spaces. Keep this field order. The last word is the local port. Port 18081 means the line came in from you.

```
logformat htm %ts.%03tu %6tr %>a %Ss/%03>Hs %<st %rm %ru %[un %Sh/%<a %mt %>lp
access_log stdio:/log/access.log htm
```

Squid listens on three ports. The first is a plain local port Squid wants open. Port 18081 is your door, and `url` prints it on this machine. Port 3128 catches the container's own web calls. The wall sends the container's port 80 there.

```
http_port 127.0.0.1:3129
http_port 0.0.0.0:18081 accel defaultsite=inhabitant vhost
http_port 0.0.0.0:3128 intercept
```

The container's name on the gate is `inhabitant`. Something inside must listen on port 8080. `proxy` is the user in the Squid image.

```
cache_peer inhabitant parent 8080 0 no-query no-digest originserver name=inhabitant
cache_effective_user proxy
```

Allowed calls use a named peer. Your question uses `inhabitant`. A model call uses the vendor peer you add.

```
always_direct deny all
never_direct allow all
```

Your door is `myport 18081`, and that traffic goes only to `inhabitant`. Squid uses the first matching line, so name `inbound` first and put the allow before the deny.

```
acl inbound myport 18081
cache_peer_access inhabitant allow inbound
cache_peer_access inhabitant deny all
http_access allow inbound
http_access deny all
```

A vendor allowlist, and the lines that add your key, are yours to write. `examples/agentd/squid.conf` and the files under `inhabitants/` are the examples.

The wall sends the container's port 80 to Squid. Other ports from the container stay closed. Each assistant README says which lines to comment in, and which to comment out, to allow plain HTTP.

Ordinary HTTPS is the block marked `OPEN-HTTPS` in those same files. Comment those lines in, and comment out the two `OPEN-OUT` lines. `create` then sends the container's port 443 to Squid. Squid decrypts the call, so the log can name the site and the path. The page body stays out of the log. The assistant sets no proxy. `create` mounts the gate certificate into the container and adds it to the trust store. The private key stays in the instance directory, on the gate. The model call is unchanged: the container still speaks plain HTTP to its stand-in host, and Squid opens HTTPS to the model company.

The gate is an OpenSSL Squid. A program that carries its own certificate list and ignores the container trust store still refuses the gate certificate.

## Logs

```bash
./bin/hermetarium logs YOUR_ID
```

That reads Squid's access log, writes `var/<id>/io.jsonl`, and prints one JSON line per record. A line has `time`, `direction` (`in` or `out`), `protocol`, `destination`, `port`, `method`, `path` when the address has one, `allowed`, and `bytes`. Message bodies stay out of the log.

The same directory holds:

```
io.jsonl       the log the command prints
access.log     Squid's own log
cache.log      Squid's internals
squid.conf     the gate file that is running
instance.json  the id, the wall, and the address
serial.log     the strong wall only, the guest console
fc.log         the strong wall only, Firecracker
```

## Inside the container

Every stock image runs as root. On the strong wall that is root inside the virtual machine. On the Docker wall, a kernel exploit reaches the host. On the strong wall, root stays inside the machine.

Inside the box, the assistant may run commands and edit files without stopping to ask. The wall is outside the image.

Claude Code refuses `--dangerously-skip-permissions` when it is root, unless it thinks it is sandboxed. The stock image sets `IS_SANDBOX=1`, `CLAUDE_CODE_BUBBLEWRAP=1`, and `CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS=1`. `settings.json` sets `skipDangerousModePermissionPrompt` and `permissions.defaultMode` to `bypassPermissions`.

Grok Build starts with `--always-approve`. `config.toml` sets `permission_mode` to `always-approve` and turns the sandbox off.

## Your own image

`inhabitants/claude-code/` and `inhabitants/grok-build/` are starting points. Build and run steps for each one are in that folder's README. Copy one, install the program you want, and keep this contract:

The image listens on port 8080. A POST is one turn. A GET may answer `ok`.

The image runs as root.

The image contains `iproute2`, and usually `iptables`, so the wall can set the default route.

Model calls go to the stand-in host on the gate: `http://claude.hermetarium.test` or `http://grok.hermetarium.test`. Squid is the one that connects to the real API.

The image may set a stand-in key, `not-the-supervisor-key`, so the program will start. Squid replaces the header from the gate file.

Pass `--image` your tag, and `--acl` a gate file that names that model, when you want the key added on the way out.

Claude Code, plus git, still using porter:

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
ENV CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS=1
ENV CI=true
ENV ANTHROPIC_API_KEY=not-the-supervisor-key
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
```

Build porter for the image:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o inhabitants/claude-code/porter ./porter
```

Build the image. The folder you name holds `settings.json` and `porter`.

```bash
docker build -t myorg/claude:dev -f Dockerfile inhabitants/claude-code
```

Copy the gate file and replace `__VENDOR_KEY__` with your key:

```bash
cp inhabitants/claude-code/squid.conf /tmp/claude.acl
```

Start it. The command prints an id: `YOUR_ID`.

```bash
./bin/hermetarium create --wall weak --image myorg/claude:dev --acl /tmp/claude.acl
```

Take the Claude install from one folder and Grok's model address from another, or skip porter and put your own program on port 8080. The wall cares that something listens.

The stock bases are `debian:bookworm-slim` and `node:26-bookworm-slim`. Another base is fine when it keeps the contract above: root, port 8080, `ip`, the stand-in model address, and a stand-in key.

**Docker in Docker.** Start from an image that can run Docker, such as `docker:24-dind`, or Debian plus Docker Engine. Install the assistant and porter on top. The engine inside the box wants cgroup access: `--privileged` on the box, or the right cgroup mounts. The Docker wall adds `NET_ADMIN` to the box. It leaves out `--privileged`, and it leaves the host Docker socket outside. Mounting that socket opens the wall. Nested Docker on the strong wall is a poor fit. Firecracker's guest kernel is old and small, and Docker in Docker wants a modern kernel and a lot of memory.

**Kubernetes in Docker.** The image can contain `kind`, `k3s`, or `minikube`. Those want privileged mode, nested cgroups, and gigabytes of memory. The Docker wall is the one to try. On the strong wall, raise the size with `--mem` and `--disk`. The defaults are 512 MiB and 1024 MiB. The tests ask for 128 MiB for echo and agentd, and 2 GiB for the official assistants. KinD wants more still.

**The kernel.** The strong wall boots the image on Firecracker's kernel, today 4.14. Grok Build and Claude Code have been run there by hand. The kernel has no random-number device, so porter credits a seed from the kernel command line to the entropy pool; a program that calls `getrandom()` before porter starts can still block. A base that needs systemd, cgroup v2, or a very new system call can run on Docker and fail in the virtual machine. Try the Docker wall first.

## Porter in your image

Porter listens on port 8080 and runs `claude` or `grok` for each POST. Claude and Grok get `--continue` after the first turn. Claude's `-p` is a switch, so `--continue` stays in front of it. Grok's `-p` is `--single` and takes the prompt, so `--continue` follows the prompt. Grok also gets `--always-approve`.

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o porter ./porter
```

In the Dockerfile:

```dockerfile
COPY porter /usr/local/bin/porter
ENV HARNESS=claude
ENV HARNESS_WORKDIR=/work
EXPOSE 8080
CMD ["/usr/local/bin/porter"]
```

`HARNESS` picks the command. Use `claude` or `grok`, and put that program on `PATH`. The matching lines are in `porter/main.go`. Porter holds no real API key. Point the assistant at the stand-in host, and let Squid add the key.

You can skip porter and serve HTTP yourself on 8080. Echo does. Porter is the piece that turns one POST into one turn of an official assistant.
