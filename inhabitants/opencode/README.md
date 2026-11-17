# OpenCode

[OpenCode](https://opencode.ai) is a coding inhabitant. This folder is a home for it inside Hermetarium. It is optional. Echo and agentd stay the images for the wall, the egress checks, and the log checks.

You send a question to an address on your own machine. Porter, inside the image, runs `opencode run` for that question. OpenCode reads files and runs commands in its container, then answers. When it needs the model, the call goes out through a gate. The gate is `squid.conf`. Your key stays in that file, and Squid adds it as the call leaves.

Inside the box, OpenCode may run commands and edit files without stopping to ask. `opencode.json` is what points it at the stand-in and turns that on. A second question to the same address continues the same conversation.

The picture of the gate is in the [inhabitants README](../README.md). In that picture the model is whatever OpenAI-compatible endpoint you name. The box calls `http://model.hermetarium.test/v1`.

This image is also what [omakase/opencode-tui](../../omakase/opencode-tui/README.md) boots on the strong wall. That setup does not publish Porter's port. You attach, and OpenCode runs in `tmux`. The image has `tmux`, an SSH server at `/usr/sbin/sshd`, `git`, `gh`, `rg`, `nano`, Docker, and the usual build tools for that. An interactive login shell from `attach` reads `/root/.bash_profile`. A local plugin reads the model list from `baseURL` when OpenCode starts. This gate file stays closed. The terminal setup's gate file is the one that allows ordinary HTTP.

Free OpenCode Zen models, such as `opencode/big-pickle`, need no key. This image does not use them, and this gate file does not open a path to them. The smoke path with no paid key is a pretend model on the gate.

## squid.conf

The shared shape of this file is in the [inhabitants README](../README.md). This file is the gate. [Squid](https://www.squid-cache.org/) reads it. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). Hermetarium copies the file as you wrote it. Fill four words in a copy of the file before you start.

`__MODEL_PEER__` is the rest of the `cache_peer` line, and it includes `name=model`. One HTTPS upstream is `api.openai.com parent 443 0 no-query no-digest originserver tls tls-cafile=/etc/ssl/certs/ca-certificates.crt name=model`. A pretend model on the gate is `127.0.0.1 parent 18082 0 no-query no-digest originserver name=model`.

`__MODEL_HOST__` is the `Host` value that upstream expects.

`__MODEL_PREFIX__` is a path prefix with a leading slash, such as `/openai`. Delete the word to leave the box path unchanged. The box path for a chat turn is `/v1/chat/completions`.

`__MODEL_KEY__` is the bearer key. It stays in this file for as long as the habitat lives. Hermetarium copies the file once, at `create`. A key that expires makes every call fail until you run `destroy` and `create` again with a new key. OpenCode reads no real key from the container. The image holds a stand-in so the program will start. Squid drops that `Authorization` and sets the key from this file.

```
   you         ---------------->  OpenCode                              open
   OpenCode    ---------------->  the upstream you named                open, key attached
   OpenCode    ---------------->  anywhere else, including Zen          closed
```

Squid keeps a log of what passed. `hermetarium logs` prints that log. A question looks like this. The path may be empty. The size may be absent when it is zero.

```json
{"direction":"in","protocol":"http","destination":"inhabitant","port":18081,"method":"POST","allowed":true}
```

A model call is plain HTTP to the stand-in. OpenCode asks for a stream, and one turn can be more than one POST. The path has your prefix in front when you set one.

```json
{"direction":"out","protocol":"http","destination":"model.hermetarium.test","port":80,"method":"POST","path":"/v1/chat/completions","allowed":true}
```

The key, the page body, and the prompt are not in that line.

This file does not allow a git host, so a push is not in the log. Git Warden is not part of this image, and its journal is not this log. If you allow a git host in your copy, a push is an outbound line for that host: protocol, destination, port, method, path, and whether it was allowed. The token stays out of the line. `examples/openweb/squid.conf` is a different gate file. It keeps a GitHub token for a few API writes and denies Git data writes. A push token stays with Git Warden, not in this file.

The container can call the model. The rest of the web is closed. To let it fetch ordinary pages over plain HTTP, edit your copy of `squid.conf`.

Comment these lines in. Remove the `#` at the start of each one. In the file they are marked `OPEN-IN`.

```
acl container_out myportname intercept
always_direct allow container_out !modelhost
never_direct deny container_out !modelhost
never_direct allow modelhost
never_direct allow inbound
http_access allow container_out
```

Comment these two lines out. Put a `#` at the start of each one. In the file they are marked `OPEN-OUT`.

```
always_direct deny all
never_direct allow all
```

The model host still uses its own path, and your key stays on that path. This opens plain HTTP, port 80.

To let the container download packages and open ordinary sites over HTTPS, edit the same copy. Squid decrypts those calls. The log names the site and the path. The page body stays out of the log. The inhabitant sets no proxy and installs no certificate. When you start the habitat, Hermetarium puts the gate's certificate into the container. The private key stays on the gate. The gate is an OpenSSL Squid. A program that carries its own certificate list and ignores the container trust store still refuses the gate certificate.

Comment these lines in. Remove the `#` at the start of each one. In the file they are marked `OPEN-HTTPS`.

```
https_port 0.0.0.0:3130 intercept name=tlsin ssl-bump cert=/log/mitm/ca.pem key=/log/mitm/ca.key generate-host-certificates=on dynamic_cert_mem_cache_size=4MB
sslcrtd_program /usr/lib/squid/security_file_certgen -s /log/mitm/ssl_db -M 4MB
sslcrtd_children 8 startup=1 idle=1
acl container_tls myportname tlsin
acl step1 at_step SslBump1
ssl_bump peek step1
ssl_bump bump all
always_direct allow container_tls
never_direct deny container_tls
never_direct allow modelhost
never_direct allow inbound
http_access allow container_tls
```

Comment out the two `OPEN-OUT` lines, the same ones as for plain HTTP. If they are already commented out, leave them that way. Start the habitat after the edit. `create` reads the file then.

## Run it

From the repository folder. You need Docker.

Build Hermetarium:

```bash
make build
```

Build porter, which carries your question to OpenCode, then build the image:

```bash
go build -o inhabitants/opencode/porter ./porter
```

```bash
docker build -t opencode inhabitants/opencode
```

Copy the gate file and fill the four words. The model id is the `model` field in `opencode.json`, written as `provider/model`. The pretend model is `hermetarium/mock`. A real catalog has no model with that id, so selecting `hermetarium/mock` for a turn fails and OpenCode reports the model is not found or inactive. The [terminal setup](../../omakase/opencode-tui/README.md) says how to point one habitat at a real catalog. The `models` key under that provider is only the part after the slash (`mock`, not `hermetarium/mock`). Porter does not pass `-m`, so that file is what OpenCode requests. Change the field and that key together when the upstream uses another id.

```bash
cp inhabitants/opencode/squid.conf /tmp/opencode.acl
```

Start it. The command prints an id: `YOUR_ID`. Use that printed line in the commands below. To choose it, add `--id NAME`, for example `--id opencode`. The command prints that name.

On the Docker wall:

```bash
./bin/hermetarium create --wall weak --image opencode --acl /tmp/opencode.acl
```

On the Firecracker wall. It needs `/dev/kvm`. Give it more memory than the default:

```bash
./bin/hermetarium create --wall strong --mem 2048 --disk 2048 --image opencode --acl /tmp/opencode.acl
```

How each wall lets only the gate reach the Internet is in the [inhabitants README](../README.md#how-the-gate-reaches-the-internet).

Ask where it is listening:

```bash
./bin/hermetarium url YOUR_ID
```

That prints an address like `http://127.0.0.1:12345/`. Send a question there:

```bash
curl -d '<your prompt>' http://127.0.0.1:12345/
```

Send another question to the same address to continue the conversation.

A terminal in the habitat is `hermetarium attach YOUR_ID`. Porter stays the HTTP path. Bytes on that terminal are not in the log.

See what went in and out:

```bash
./bin/hermetarium logs YOUR_ID
```

Stop it:

```bash
./bin/hermetarium destroy YOUR_ID
```

The automated smoke is the Docker wall and a pretend model. It does not need a paid key. The same image is what the Firecracker wall boots. That smoke does not start it there.
