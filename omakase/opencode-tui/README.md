# OpenCode through the terminal

One inhabitant, OpenCode, on the strong wall. You talk to it in its terminal. This folder is one way to put Hermetarium together that is meant to be used, not a second product. Hermetarium stays the kit.

The image is the OpenCode inhabitant, [inhabitants/opencode](../../inhabitants/opencode). There is no second OpenCode image. Porter runs inside that image and listens on port 8080. This setup does not publish that port. You do not send questions to Porter. You attach, and OpenCode runs in `tmux`.

The wall is strong. The microVM's files are the root disk, `var/<id>/rootfs.ext4`. `destroy` deletes that disk. Nothing is kept for the next `create`. Do not pass `--data`. This setup has no data disk.

The Firecracker container is `htm-fc-<id>`. It holds the microVM. The gate is `htm-gate-<id>`. The gate is Squid. It is not the Firecracker container.

## What this folder is

- The image reference: build `inhabitants/opencode` and pass `--image opencode`.
- The gate file, `squid.conf`.
- These steps, the token rules, and what the setup holds.

Git Warden is not in this folder. There is no Push Guard configuration and no script that starts one. Backup of the root disk is not in this folder either. `omakase/backup.sh` still expects a data disk. Do not point it at this habitat.

## Image

From this repository, the image is `inhabitants/opencode`. That Dockerfile pins OpenCode, and it installs `git`, `rg`, `tmux`, an SSH server at `/usr/sbin/sshd`, and the usual build tools. OpenCode's update check and its model-list download are off. The image command is Porter. The strong wall also starts `sshd` when that binary is there, and Hermetarium keeps the habitat's private key on the host.

Inside the image, OpenCode's key is the placeholder `not-the-supervisor-key`. Leave it. The real key stays in the gate file. OpenCode is pointed at `http://model.hermetarium.test/v1` and the model id `hermetarium/mock`. That stand-in host is the gate, not the upstream.

For a real model, change the id in `/root/.config/opencode/opencode.json` after the habitat is up, or change `inhabitants/opencode/opencode.json` and build the image again. Keep the provider id `hermetarium`, keep `baseURL`, and keep `apiKey` as `{env:OPENCODE_API_KEY}`. Add the upstream's model id next to `mock`. Do not write the real key into that file. A change made inside the habitat is on the root disk and is deleted by `destroy`.

## Gate file

`squid.conf` is the gate. [Squid](https://www.squid-cache.org/) reads it. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). Hermetarium copies the file as you wrote it. Fill the five words in a copy before `create`. A new key means `destroy` and `create` again, and `destroy` deletes the root disk.

`__MODEL_PEER__` is the rest of the `cache_peer` line, and it includes `name=model`. One HTTPS upstream is:

```
api.openai.com parent 443 0 no-query no-digest originserver tls tls-cafile=/etc/ssl/certs/ca-certificates.crt name=model
```

`__MODEL_HOST__` is the `Host` value that upstream expects, such as `api.openai.com`.

`__MODEL_PREFIX__` is a path prefix with a leading slash, such as `/openai`. Delete the word to leave the box path unchanged. The box path for a chat turn is `/v1/chat/completions`.

`__MODEL_KEY__` is the model key. One key per habitat. It stays in this file.

`__GITHUB_TOKEN__` is the GitHub API token. It stays in this file. Squid sends it only to `api.github.com`.

The box calls `http://model.hermetarium.test/v1`. Squid drops that call's `Authorization` and sets the model key. It also sets `Host` from this file. The rest of the web is HTTPS. Squid decrypts it. The I/O log then has the host and the path. The page body stays out of the log. Hosts on the pass-through list keep their own certificate. Squid speaks only HTTP/1.1 after it decrypts, so that list is for a program that pins a certificate or needs HTTP/2. The shipped list is `pinned.example`. Replace it with the hosts you need. A passed-through connection is in the log as the name, not the path.

`api.github.com` is decrypted, not passed through. Squid drops any `Authorization` the box sends to that host and sets the API token. Reads are allowed. The writes that are allowed are creating a repository, a pull request, an issue, or a comment. Deletes, contents writes, Git data writes, and repository settings are denied. Cleartext to that host is denied, so the token is not sent on port 80.

Names resolve through the gate. A query to another DNS server gets no answer.

The inhabitant sets no proxy and installs no certificate of its own. When you start the habitat, Hermetarium puts the gate's certificate into the image's trust store. The private key stays on the gate. A program that carries its own certificate list and ignores the container trust store still refuses the gate certificate, unless that host is on the pass-through list.

## Tokens

Nobody logs in to a service from inside the habitat. Do not run `opencode auth login` or `gh auth login`, and do not save a git password in the habitat. Do not put a token in the image, in `opencode.json`, or in the habitat's files.

Three credentials belong to this setup. Two are in the gate file. The third is not in this folder yet.

- Model key, in the gate file, one per habitat. The image holds a placeholder so OpenCode will start.
- API token, in the gate file. A fine-grained GitHub token for all repositories: repository creation, pull requests, and issues. No Contents. No Administration.
- Push token, held by a Push Guard of Git Warden when that exists. A fine-grained token for all repositories: Contents write. No Workflows. No Administration. Do not put this token in the gate file, the image, or the habitat. This folder does not hold it.

Per GitHub's docs, a token without the Workflows permission cannot change files under `.github/workflows`.

## From a fresh clone

You need Docker, Go 1.24 or newer, and a Linux machine with `/dev/kvm`, on x86_64. The first strong `create` downloads Firecracker into the cache.

```bash
git clone https://github.com/pihme/hermetarium.git
cd hermetarium
make build
go build -o inhabitants/opencode/porter ./porter
docker build -t opencode inhabitants/opencode
```

Copy the gate file and fill the five words. Edit the copy, not the file in git.

```bash
cp omakase/opencode-tui/squid.conf /tmp/opencode-tui.acl
```

Start the habitat. The command prints an id: `YOUR_ID`. Use that printed line below. To choose it, add `--id NAME`, for example `--id opencode`.

```bash
./bin/hermetarium create --wall strong --no-inbound --mem 2048 --disk 2048 --image opencode --acl /tmp/opencode-tui.acl
```

`--mem` and `--disk` are MiB. The root disk is sparse. Raise `--disk` if the inhabitant fills it. `--cpus N` is optional. Leave it off and the guest has one processor.

There is no inbound address:

```bash
./bin/hermetarium url YOUR_ID
```

That prints `this habitat has no inbound address`.

How the strong wall lets only the gate reach the Internet is in the [inhabitants README](../../inhabitants/README.md#how-the-gate-reaches-the-internet).

## In the terminal

Attach. This is a terminal on the strong wall, over SSH, to the guest. Bytes on it are not in the I/O log. It opens no path out of the habitat.

```bash
./bin/hermetarium attach YOUR_ID
```

The shell is root. Start OpenCode in `tmux`, so closing the terminal does not stop it. On this wall, closing the terminal sends SIGHUP to a command that is not in `tmux`.

```bash
tmux new-session -s opencode
opencode
```

Leave the terminal. Attach to the same session later:

```bash
./bin/hermetarium attach YOUR_ID -- tmux attach -t opencode
```

See what crossed the gate:

```bash
./bin/hermetarium logs YOUR_ID
```

A model call is plain HTTP to the stand-in. The path has your prefix in front when you set one. The key, the page body, and the prompt are not in that line.

Stop the habitat:

```bash
./bin/hermetarium destroy YOUR_ID
```

`destroy` deletes `var/<id>/rootfs.ext4`. Files the inhabitant wrote are gone with it. Logs and the gate file may still be in `var/<id>/` after that. The gate file holds the keys. Delete that directory when you are finished with it.

The habitat's files live under `HERMETARIUM_ROOT`, else this checkout, else `$XDG_DATA_HOME/hermetarium` or `~/.local/share/hermetarium`.

## What this protects

- The inhabitant is inside the microVM. Root there is not root on the host.
- Nothing is published on the host. The bridge gateway is not allowed onto port 18081.
- The model key and the GitHub API token are not in the image. The gate sets each on its own host. The API token is not attached to other hosts.
- A call leaves only through the gate. A DNS query to another server gets no answer. Names that do resolve go through the gate.
- HTTPS is decrypted, except the pass-through list. The I/O log has the host and the path. Deletes and contents writes on `api.github.com` are denied.

## What this does not protect

- `destroy` does not keep the inhabitant's files. There is no data disk.
- Git push is not checked. A push straight to a remote is ordinary HTTPS, and this gate file does not refuse it. The push token is not here. Until a Push Guard for this habitat exists, do not push from the habitat.
- The open web is a way out. The inhabitant can send a page body to any decrypted host. The log does not store that body.
- The terminal is not in the I/O log. Escape sequences from the habitat reach your terminal as they are.
- A program that pins its own certificate list still refuses the gate certificate, unless you pass that host through.
- `omakase/backup.sh` does not back up this root disk.

## Checklist

The owner fills this in once, against a real model and real repositories. The boxes stay empty in the repository.

- [ ] One turn in the terminal against a real model. The key was only in the gate file.
- [ ] `hermetarium logs` shows that call's host and path, and not the key.
- [ ] A web page loads over HTTPS, and the I/O log has its host and path.
- [ ] `api.github.com` creates a repository with the gate token. The token is not in the habitat.
- [ ] No login to a service from inside the habitat.
- [ ] A repository the owner added to the Push Guard accepts a clean push and refuses a push that breaks a rule.

The last line waits on Git Warden. The others do not.
