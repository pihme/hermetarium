# Claude Code

[Claude Code](https://code.claude.com/docs/en/quickstart) is Anthropic's coding assistant. This folder is a home for it inside Hermetarium.

You send a question to an address on your own machine. Claude reads files and runs commands in its container, then answers. When it needs the model, the call goes out through a gate. The gate is `squid.conf`. Your Anthropic key stays in that file, and Squid adds it as the call leaves.

Inside the box, Claude may run commands and edit files without stopping to ask. `settings.json` is what turns that on. A second question to the same address continues the same conversation.

The picture of the gate is in the [inhabitants README](../README.md). In that picture the model company is Anthropic, `api.anthropic.com`.

## squid.conf

The shared shape of this file is in the [inhabitants README](../README.md). This file is the gate. [Squid](https://www.squid-cache.org/) reads it. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). Hermetarium copies the file as you wrote it. Put your real key in a copy of the file before you start. The word to replace is `__VENDOR_KEY__`.

```
   you         ---------------->  Claude Code         open
   Claude Code ---------------->  api.anthropic.com   open, key attached
   Claude Code ---------------->  anywhere else       closed
```

Squid keeps a log of what passed. `hermetarium logs` prints that log.

The container can call the model. The rest of the web is closed. To let it fetch ordinary pages over plain HTTP, edit your copy of `squid.conf`.

Comment these lines in. Remove the `#` at the start of each one. In the file they are marked `OPEN-IN`.

```
acl container_out myport 3128
always_direct allow container_out !vendorhost
never_direct deny container_out !vendorhost
never_direct allow vendorhost
never_direct allow inbound
http_access allow container_out
```

Comment these two lines out. Put a `#` at the start of each one. In the file they are marked `OPEN-OUT`.

```
always_direct deny all
never_direct allow all
```

The model host still uses its own path, and your key stays on that path. This opens plain HTTP, port 80.

To let the container download packages and open ordinary sites over HTTPS, edit the same copy. Squid decrypts those calls. The log names the site and the path. The page body stays out of the log. The assistant sets no proxy and installs no certificate. When you start the habitat, Hermetarium puts the gate's certificate into the container. The private key stays on the gate. The gate is an OpenSSL Squid. A program that carries its own certificate list and ignores the container trust store still refuses the gate certificate.

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
never_direct allow vendorhost
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

Build the small helper that carries your question to Claude, then build the image:

```bash
go build -o inhabitants/claude-code/porter ./porter
```

```bash
docker build -t claude inhabitants/claude-code
```

Copy the gate file and replace `__VENDOR_KEY__` with your Anthropic key:

```bash
cp inhabitants/claude-code/squid.conf /tmp/claude.acl
```

Start it. The command prints an id: `YOUR_ID`. Use that printed line in the commands below. To choose it, add `--id NAME`, for example `--id claude`. The command prints that name.

```bash
./bin/hermetarium create --image claude --acl /tmp/claude.acl
```

Ask where it is listening:

```bash
./bin/hermetarium url YOUR_ID
```

That prints an address like `http://127.0.0.1:12345/`. Send a question there:

```bash
curl -d '<your prompt>' http://127.0.0.1:12345/
```

Send another question to the same address to continue the conversation.

See what went in and out:

```bash
./bin/hermetarium logs YOUR_ID
```

Stop it:

```bash
./bin/hermetarium destroy YOUR_ID
```

Firecracker has not been shown to work for this inhabitant. Use the Docker wall.
