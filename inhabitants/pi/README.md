# Pi

[Pi](https://pi.dev) (Earendil Works) is a coding inhabitant. This folder is its home inside Hermetarium. It is optional. Echo and agentd stay the images for the wall, the egress checks, and the log checks.

You send a question to an address on your own machine. Porter, inside the image, runs `pi -p` for that question. Pi reads files and runs commands in its container, then answers. When it needs the model, the call goes out through a gate. The gate is `squid.conf`. Your key stays in that file, and Squid adds it as the call leaves.

The picture of the gate is in the [inhabitants README](../README.md). In that picture the model is whatever OpenAI-compatible endpoint you name. The box calls `http://model.hermetarium.test/v1`.

This image is also what [omakase/pi-tui](../../omakase/pi-tui/README.md) boots on the strong wall. That setup does not publish Porter's port. You attach, and Pi runs in `tmux`. The image has `tmux`, an SSH server at `/usr/sbin/sshd`, `git`, `gh`, `rg`, `nano`, Docker, and the usual build tools for that. `tini` is PID 1 (a subreaper), so finished background processes are reaped instead of staying as `<defunct>` zombies. An interactive login shell from `attach` reads `/root/.bash_profile`. The terminal setup's gate file is the one that allows ordinary HTTP; this file stays closed.

What Pi adds over OpenCode is a **wake**: a separate process can inject an external event into the running session and trigger follow-up work, without the operator prompting. OpenCode 2 has no wake scheduler and a detached `opencode run` cannot reach a live TUI. Pi's `file-trigger` extension delivers into the live coordinator session. The smoke path with no paid key is a pretend model on the gate.

## squid.conf

The shared shape of this file is in the [inhabitants README](../README.md). This file is the gate. [Squid](https://www.squid-cache.org/) reads it. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). Hermetarium copies the file as you wrote it. Fill six words in a copy of the file before you start.

`__MODEL_PEER__` is the rest of the `cache_peer` line, and it includes `name=model`. One HTTPS upstream is `api.openai.com parent 443 0 no-query no-digest originserver tls tls-cafile=/etc/ssl/certs/ca-certificates.crt name=model`. A pretend model on the gate is `127.0.0.1 parent 18082 0 no-query no-digest originserver name=model`.

`__MODEL_HOST__` is the `Host` value that upstream expects.

`__MODEL_PREFIX__` is a path prefix with a leading slash, such as `/openai`. Delete the word to leave the box path unchanged. The box path for a chat turn is `/v1/chat/completions`.

`__MODEL_KEY__` is the bearer key. It stays in this file for as long as the habitat lives. Hermetarium copies the file once, at `create`. A key that expires makes every call fail until you run `destroy` and `create` again with a new key. Pi reads no real key from the container. The image holds a stand-in so the program will start. Squid drops that `Authorization` and sets the key from this file.

`__GITHUB_TOKEN__` and `__GITHUB_GIT_BASIC__` are the one GitHub token, in `Bearer` form for `api.github.com` and in the `Basic` form git HTTPS accepts for `github.com`. `gh` uses `api.github.com`, including pull requests and Projects. The image holds no real GitHub token.

```
   you         ---------------->  Pi                                    open
   Pi          ---------------->  the upstream you named                open, key attached
   Pi          ---------------->  anywhere else                         closed
```

Squid keeps a log of what passed. `hermetarium logs` prints that log. The key, the page body, and the prompt are not in that line.

## The wake

The image installs `file-trigger.ts` into `~/.pi/agent/extensions/`. It watches `/tmp/agent-trigger.txt`. Any separate process can write a marker, and Pi injects it into the running session and triggers a turn:

```bash
echo "PR#74 was labeled ready-for-qa - investigate and comment" > /tmp/agent-trigger.txt
```

The injected message renders in the live TUI as `[file-trigger]` and starts a model turn (follow-up work). The file is cleared after reading, so the event does not re-fire. This is the mechanism a PR-label or CI watcher uses to wake the coordinator session without a human prompt. See [omakase/pi-tui](../../omakase/pi-tui/README.md) for the terminal setup.

## From a fresh clone

You need Docker, Go 1.24 or newer, and a Linux machine with `/dev/kvm`, on x86_64.

```bash
git clone https://github.com/pihme/hermetarium.git
cd hermetarium
make build
docker build -t pi inhabitants/pi
```

Copy the gate file and fill the six words. Edit the copy, not the file in git.

```bash
cp inhabitants/pi/squid.conf /tmp/pi.acl
```

Start the habitat on the strong wall:

```bash
./bin/hermetarium create --wall strong --no-inbound --mem 2048 --disk 4096 --image pi --acl /tmp/pi.acl
```

## Optional HTTP path (Porter)

Porter carries operator HTTP turns to `pi -p` (print mode), one session per working directory, `--continue` on later turns. It is optional; the terminal is the primary use. To use it, publish the inbound port instead of `--no-inbound` and POST to `hermetarium url`:

```bash
curl -d 'reply with OK' http://127.0.0.1:PORT/
```
