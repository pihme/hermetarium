# OpenCode through the terminal

One inhabitant, OpenCode, on the strong wall. You talk to it in its terminal. This folder is one way to put Hermetarium together that is meant to be used, not a second product. Hermetarium stays the kit.

The image is the OpenCode inhabitant, [inhabitants/opencode](../../inhabitants/opencode). There is no second OpenCode image. Porter runs inside that image and listens on port 8080. This setup does not publish that port. You do not send questions to Porter. You attach, and OpenCode runs in `tmux`.

The wall is strong. The microVM's files are the root disk, `var/<id>/rootfs.ext4`. `destroy` deletes that disk. Nothing is kept for the next `create`. Do not pass `--data`. This setup has no data disk.

The Firecracker container is `htm-<id>-fc`. It holds the microVM. The gate is `htm-<id>-gate`. The gate is Squid. It is not the Firecracker container.

## What this folder is

- The image reference: build `inhabitants/opencode` and pass `--image opencode`.
- The gate file, `squid.conf`.
- These steps, the token rules, and what the setup holds.

Git Warden is not in this folder. There is no Push Guard configuration and no script that starts one. Backup of the root disk is not in this folder either. `omakase/backup.sh` pauses `htm-<id>-fc` and reads the root file, and it requires a Git Warden journal.

## Image

From this repository, the image is `inhabitants/opencode`. That Dockerfile installs the current OpenCode and pins `gh`, and it installs `git`, `rg`, `tmux`, an SSH server at `/usr/sbin/sshd`, Docker, and the usual build tools. OpenCode updates itself. Its download of the public model list from `models.opencode.ai` stays off. The image command starts `dockerd` in the background, then Porter. If `dockerd` cannot start, Porter still starts. `dockerd` does not change iptables. A container that should reach the gate uses `--network host`. Its log is `/var/log/dockerd.log`. The strong wall also starts `sshd` when that binary is there, and Hermetarium keeps the habitat's private key on the host.

Inside the image, OpenCode's key is the placeholder `not-the-supervisor-key`. Leave it. The real key stays in the gate file. OpenCode is pointed at `http://model.hermetarium.test/v1` and the model id `hermetarium/mock`. That stand-in host is the gate, not the upstream.

A local plugin, `/root/.config/opencode/plugins/auto-models.ts`, reads `{baseURL}/models` when OpenCode starts. The image does not install that plugin from npm, and it does not store the model catalog. The shared config stays on `/v1` and `hermetarium/mock`. For a real catalog, set `baseURL` in the habitat after it is up. The plugin fills a missing output limit, so a catalog that sends `null` still loads.

`hermetarium attach` with no command starts an interactive login bash when the image has bash. The prompt is the current directory. Up and down recall commands, and Tab completes them. `nano` is the editor. The shell reads `/root/.bash_profile`, which sets the placeholder `OPENCODE_API_KEY` and `GH_TOKEN` and starts in `/work`. An image without bash still gets `sh`.

For a real model, change the id in `/root/.config/opencode/opencode.json` after the habitat is up, or change `inhabitants/opencode/opencode.json` and build the image again. Keep the provider id `hermetarium`, keep `settings.baseURL`, and keep `apiKey` as `{env:OPENCODE_API_KEY}`. The shared file uses `/v1`, which is the OpenAI-compatible path the mock speaks. Eden's catalog is `http://model.hermetarium.test/v3`; that path belongs in this file inside one habitat, because the gate does not rewrite `/v1` into `/v3`. Do not write the real key into that file. A change made inside the habitat is on the root disk and is deleted by `destroy`.

`hermetarium/mock` stays in that file so the mock tests have a model, and the plugin leaves it in the list. A real catalog has no model with that id. Selecting `hermetarium/mock` for a turn fails, and OpenCode reports the model is not found or inactive. Pick an id the plugin loaded from `{baseURL}/models`.

## Gate file

`squid.conf` is the gate. [Squid](https://www.squid-cache.org/) reads it. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). Hermetarium copies the file as you wrote it. Fill the six words in a copy before `create`. A new key means `destroy` and `create` again, and `destroy` deletes the root disk.

`__MODEL_PEER__` is the rest of the `cache_peer` line, and it includes `name=model`. One HTTPS upstream is:

```
api.openai.com parent 443 0 no-query no-digest originserver tls tls-cafile=/etc/ssl/certs/ca-certificates.crt name=model
```

`__MODEL_HOST__` is the `Host` value that upstream expects, such as `api.openai.com`.

`__MODEL_PREFIX__` is a path prefix with a leading slash, such as `/openai`. Delete the word to leave the box path unchanged. The box path for a chat turn is `/v1/chat/completions`.

`__MODEL_KEY__` is the model key. One key per habitat. It stays in this file.

`__GITHUB_TOKEN__` is the one GitHub token. It stays in this file. Squid sends it as `Bearer` to `api.github.com`.

`__GITHUB_GIT_BASIC__` is that same token in the form git HTTPS accepts. On Linux, with the token in `$TOKEN`:

```bash
printf 'x-access-token:%s' "$TOKEN" | base64 -w 0
```

Squid sends that value as `Basic` to `github.com`. A line wrap in the middle of the value breaks the gate file.

The box calls `http://model.hermetarium.test/v1`. Squid drops that call's `Authorization` and sets the model key. It also sets `Host` from this file. Plain HTTP to other hosts is allowed, so a package download works. Cleartext to `github.com` and `api.github.com` stays denied, so the token is not sent on port 80. The rest of the web over HTTPS is decrypted. The I/O log then has the host and the path. The page body stays out of the log. Hosts on the pass-through list keep their own certificate. Squid speaks only HTTP/1.1 after it decrypts, so that list is for a program that pins a certificate or needs HTTP/2. The shipped list is `pinned.example`. Replace it with the hosts you need. A passed-through connection is in the log as the name, not the path.

`github.com` and `api.github.com` are decrypted, not passed through. Squid drops any `Authorization` the box sends to those hosts and sets the gate credential. Git push and fetch use `https://github.com`. `gh` uses `api.github.com`, including pull requests and Projects. The gate does not choose which of those calls succeed. The token's own permissions are the limit. Cleartext to either host is denied, so the token is not sent on port 80. A path containing `..` or an encoded `..` (`%2e%2e`) is denied. A `%` in the query string is allowed, and so is `%2F` in a branch name.

Names resolve through the gate. A query to another DNS server gets no answer.

The inhabitant sets no proxy and installs no certificate of its own. When you start the habitat, Hermetarium puts the gate's certificate into the image's trust store. The private key stays on the gate. A program that carries its own certificate list and ignores the container trust store still refuses the gate certificate, unless that host is on the pass-through list.

## Tokens

Nobody logs in to a service from inside the habitat. Do not run `opencode auth login` or `gh auth login`, and do not save a git password in the habitat. Do not put a token in the image, in `opencode.json`, or in the habitat's files.

Two credentials belong to this setup. Both are in the gate file.

- Model key, one per habitat. The image holds a placeholder so OpenCode will start.
- One GitHub token, used for `git` and for `gh`. Its permissions on GitHub are the limit. The gate file also holds the `Basic` form of that same token. The image holds no real GitHub token. If `gh` will not send a request without one, set `GH_TOKEN` to a placeholder inside the habitat. The gate replaces `Authorization`. Leave git with no credential and no credential helper.

Per GitHub's docs, a token without the Workflows permission cannot change files under `.github/workflows`. The gate does not add that limit.

## From a fresh clone

You need Docker, Go 1.24 or newer, and a Linux machine with `/dev/kvm`, on x86_64. The first strong `create` downloads Firecracker into the cache.

```bash
git clone https://github.com/pihme/hermetarium.git
cd hermetarium
make build
go build -o inhabitants/opencode/porter ./porter
cp inhabitants/day-one.md inhabitants/opencode/day-one.md
docker build -t opencode inhabitants/opencode
```

Copy the gate file and fill the six words. Edit the copy, not the file in git.

```bash
cp omakase/opencode-tui/squid.conf /tmp/opencode-tui.acl
```

Start the habitat. The command prints an id: `YOUR_ID`. Use that printed line below. To choose it, add `--id NAME`, for example `--id opencode`.

```bash
./bin/hermetarium create --wall strong --no-inbound --mem 2048 --disk 4096 --image opencode --acl /tmp/opencode-tui.acl
```

`--mem` and `--disk` are MiB. The root disk is sparse. The OpenCode image with Docker does not fit in 2048 MiB; 4096 MiB leaves room. `--cpus N` is optional. Leave it off and the guest has one processor.

There is no inbound address:

```bash
./bin/hermetarium url YOUR_ID
```

That prints `this habitat has no inbound address`.

How the strong wall lets only the gate reach the Internet is in the [inhabitants README](../../inhabitants/README.md#how-the-gate-reaches-the-internet).

## In the terminal

Attach. This is a terminal on the strong wall, over SSH, to the guest. With no command the shell is a login bash in `/work`. Bytes on it are not in the I/O log. It opens no path out of the habitat.

Mouse tracking stays on. Hold Shift while you drag a selection, then press Ctrl+Shift+C. Ctrl+Shift+V pastes into the prompt. OpenCode's copy command sends OSC 52. The habitat has no clipboard of its own. GNOME Terminal 3.52 with VTE 0.76 drops OSC 52, so that copy does not land on the host clipboard.

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
- The model key and the GitHub token are not in the image. The gate sets the model key on the model host, the GitHub token on `api.github.com`, and the git form of that token on `github.com`. The token is not attached to other hosts.
- A call leaves only through the gate. A DNS query to another server gets no answer. Names that do resolve go through the gate.
- HTTPS is decrypted, except the pass-through list. The I/O log has the host and the path. Plain HTTP to other hosts is allowed. Cleartext to `github.com` and `api.github.com` is denied. A path containing `..` or an encoded `..` is denied. A `%` in the query string is allowed.

## What this does not protect

- `destroy` does not keep the inhabitant's files. There is no data disk.
- The gate does not review a push or a `gh` call. GitHub accepts whatever the token allows. A Push Guard for one repository is Habitat 2.
- The open web is a way out. The inhabitant can send a page body to any decrypted host. The log does not store that body.
- The terminal is not in the I/O log. Escape sequences from the habitat reach your terminal as they are. OSC 52 is one of those sequences. A terminal that does not implement it drops the copy.
- A program that pins its own certificate list still refuses the gate certificate, unless you pass that host through.
- `omakase/backup.sh` will not read this root file without a Git Warden journal.

## Checklist

The owner fills this in once, against a real model and real repositories. The boxes stay empty in the repository.

- [ ] One turn in the terminal against a real model. The key was only in the gate file.
- [ ] `hermetarium logs` shows that call's host and path, and not the key.
- [ ] A web page loads over HTTPS, and the I/O log has its host and path.
- [ ] One push over HTTPS to `github.com`. The credential was only in the gate file.
- [ ] `gh` creates a pull request or a Project. The token was only in the gate file.
- [ ] No login to a service from inside the habitat.
