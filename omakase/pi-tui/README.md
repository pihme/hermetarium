# Pi through the terminal

One inhabitant, Pi, on the strong wall. You talk to it in its terminal. This folder is one way to put Hermetarium together that is meant to be used, not a second product. Hermetarium stays the kit.

The image is the Pi inhabitant, [inhabitants/pi](../../inhabitants/pi). There is no second Pi image. Porter runs inside that image and listens on port 8080. This setup does not publish that port. You do not send questions to Porter. You attach, and Pi runs in `tmux`.

The wall config is the same as `omakase/opencode-tui`: the model host, the GitHub host, HTTPS decryption, and the Docker registry pass-through are identical. What is connected is identical; the difference is the agent.

The wall is strong. The microVM's files are the root disk, `var/<id>/rootfs.ext4`. `destroy` deletes that disk. Nothing is kept for the next `create`. Do not pass `--data`. This setup has no data disk.

The Firecracker container is `htm-<id>-fc`. It holds the microVM. The gate is `htm-<id>-gate`. The gate is Squid. It is not the Firecracker container.

## What this folder is

- The image reference: build `inhabitants/pi` and pass `--image pi`.
- The gate file, `squid.conf`.
- These steps, the token rules, and what the setup holds.

Git Warden is not in this folder. There is no Push Guard configuration and no script that starts one. Backup of the root disk is not in this folder either.

## Image

From this repository, the image is `inhabitants/pi`. That Dockerfile installs Pi 1.6 and pins `gh`, and it installs `git`, `rg`, `tmux`, an SSH server at `/usr/sbin/sshd`, Docker, and the usual build tools. `tini` is PID 1 (a subreaper), so finished background processes are reaped instead of staying as `<defunct>` zombies — that is what keeps the wake honest. The image command starts `dockerd` in the background, then Porter. Porter is optional; the terminal is the primary path. The strong wall also starts `sshd` when that binary is there, and Hermetarium keeps the habitat's private key on the host.

Inside the image, Pi's key is the placeholder `not-the-supervisor-key`. Leave it. The real key stays in the gate file. Pi is pointed at `http://model.hermetarium.test/v1` and the model id `hermetarium/mock`. That stand-in host is the gate, not the upstream.

`hermetarium attach` with no command starts an interactive login bash when the image has bash. The prompt is the current directory. The shell reads `/root/.bash_profile`, which sets the placeholder `OPENAI_API_KEY` and `GH_TOKEN` and starts in `/work`. An image without bash still gets `sh`.

For a real model, change the id in `~/.pi/agent/models.json` after the habitat is up, or change `inhabitants/pi/models.json` and build the image again. Keep the provider id `hermetarium`, keep `baseUrl`, and keep `apiKey` as a placeholder. A change made inside the habitat is on the root disk and is deleted by `destroy`.

`hermetarium/mock` stays in that file so the mock tests have a model. A real catalog has no model with that id. Selecting `hermetarium/mock` for a turn fails, and Pi reports the model is not found or inactive. Pick an id the catalog lists.

## The wake

Pi's `file-trigger` extension watches `/tmp/agent-trigger.txt`. A separate process (a PR-label watcher, a CI check) can inject an external event into the running coordinator session and trigger a turn:

```bash
echo "PR#74 was labeled ready-for-qa - investigate and comment" > /tmp/agent-trigger.txt
```

The injected message renders in the live TUI as `[file-trigger]` and starts a model turn (follow-up work). The file is cleared after reading. This is what OpenCode 2 cannot do: an external event waking the session the operator is looking at, with no human prompt.

## Gate file

`squid.conf` is the gate, identical to `omakase/opencode-tui/squid.conf` except for the header. [Squid](https://www.squid-cache.org/) reads it. Hermetarium copies the file as you wrote it. Fill the six words in a copy before `create`. A new key means `destroy` and `create` again, and `destroy` deletes the root disk.

The words are `__MODEL_PEER__`, `__MODEL_HOST__`, `__MODEL_PREFIX__`, `__MODEL_KEY__`, `__GITHUB_TOKEN__`, and `__GITHUB_GIT_BASIC__`. They behave exactly as in `omakase/opencode-tui/README.md`:

- The box calls `http://model.hermetarium.test/v1`. Squid drops the box `Authorization` and sets the model key, and sets `Host` from the file.
- `github.com` and `api.github.com` are decrypted; Squid sets the gate GitHub token. `gh` uses `api.github.com`, including pull requests and Projects. Cleartext to either host is denied.
- The rest of the web over HTTPS is decrypted; the I/O log has the host and path, not the body.
- Docker registry hosts pass through so an in-habitat daemon keeps the original certificate.
- Names resolve through the gate. A query to another DNS server gets no answer.

## Tokens

Nobody logs in to a service from inside the habitat. Do not run `pi auth login` or `gh auth login`, and do not save a token in the habitat. Two credentials belong to this setup, both in the gate file: the model key and one GitHub token (used for `git` and `gh`). The image holds placeholders only.

## From a fresh clone

You need Docker, Go 1.24 or newer, and a Linux machine with `/dev/kvm`, on x86_64. The first strong `create` downloads Firecracker into the cache.

```bash
git clone https://github.com/pihme/hermetarium.git
cd hermetarium
make build
docker build -t pi inhabitants/pi
```

Copy the gate file and fill the six words. Edit the copy, not the file in git.

```bash
cp omakase/pi-tui/squid.conf /tmp/pi.acl
```

Start the habitat. The command prints an id: `YOUR_ID`. Use that printed line below. To choose it, add `--id NAME`, for example `--id pi`.

```bash
./bin/hermetarium create --wall strong --no-inbound --mem 2048 --disk 4096 --image pi --acl /tmp/pi.acl
```

`--mem` and `--disk` are MiB. The root disk is sparse. The Pi image with Docker does not fit in 2048 MiB; 4096 MiB leaves room. `--cpus N` is optional. Leave it off and the guest has one processor.

There is no inbound address:

```bash
./bin/hermetarium url YOUR_ID
```

That prints `this habitat has no inbound address`.

How the strong wall lets only the gate reach the Internet is in the [inhabitants README](../../inhabitants/README.md#how-the-gate-reaches-the-internet).

## In the terminal

Attach. This is a terminal on the strong wall, over SSH, to the guest. With no command the shell is a login bash in `/work`. Bytes on it are not in the I/O log. It opens no path out of the habitat.

```bash
./bin/hermetarium attach YOUR_ID
```

The shell is root. Start Pi in `tmux`, so closing the terminal does not stop it. On this wall, closing the terminal sends SIGHUP to a command that is not in `tmux`.

```bash
tmux new-session -s pi
pi
```

Leave the terminal. Attach to the same session later:

```bash
./bin/hermetarium attach YOUR_ID -- tmux attach -t pi
```

Wake the coordinator from a watcher process:

```bash
echo "PR#74 labeled ready-for-qa - investigate" > /tmp/agent-trigger.txt
```

See what crossed the gate:

```bash
./bin/hermetarium logs YOUR_ID
```

A model call is plain HTTP to the stand-in. The key, the page body, and the prompt are not in that line.

Stop the habitat:

```bash
./bin/hermetarium destroy YOUR_ID
```

`destroy` deletes `var/<id>/rootfs.ext4`. Files the inhabitant wrote are gone with it. Logs and the gate file may still be in `var/<id>/` after that. The gate file holds the keys. Delete that directory when you are finished with it.

The habitat's files live under `HERMETARIUM_ROOT`, else this checkout, else `$XDG_DATA_HOME/hermetarium` or `~/.local/share/hermetarium`.

## What this protects

Same as the OpenCode TUI: the inhabitant is inside the microVM; nothing is published on the host; the model key and GitHub token are not in the image; a call leaves only through the gate; HTTPS is decrypted and logged as host+path; the terminal is not in the I/O log.

## What this does not protect

Same as the OpenCode TUI: `destroy` does not keep the inhabitant's files; the gate does not review a push or a `gh` call; the open web is a way out (page bodies are not logged); a program that pins its own certificate list still refuses the gate certificate unless you pass that host through; `omakase/backup.sh` will not read this root file without a Git Warden journal.

## Checklist

The owner fills this in once, against a real model and real repositories. The boxes stay empty in the repository.

- [ ] One turn in the terminal against a real model. The key was only in the gate file.
- [ ] `hermetarium logs` shows that call's host and path, and not the key.
- [ ] A web page loads over HTTPS, and the I/O log has its host and path.
- [ ] One push over HTTPS to `github.com`. The credential was only in the gate file.
- [ ] `gh` creates a pull request or a Project. The token was only in the gate file.
- [ ] A watcher write to `/tmp/agent-trigger.txt` wakes the running coordinator session.
- [ ] No login to a service from inside the habitat.
