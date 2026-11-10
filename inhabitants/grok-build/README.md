# Grok Build

[Grok Build](https://x.ai/cli) is xAI's coding assistant. This folder is a home for it inside Hermetarium.

You send a question to an address on your own machine. Grok reads files and runs commands in its container, then answers. When it needs the model, the call goes out through a gate. The gate is `squid.conf`. Your xAI key stays in that file, and Squid adds it as the call leaves.

Inside the box, Grok may run commands and edit files without stopping to ask. `config.toml` is what turns that on. A second question to the same address continues the same conversation.

The picture of the gate is in the [inhabitants README](../README.md). In that picture the model company is xAI, `api.x.ai`.

## squid.conf

The shared shape of this file is in the [inhabitants README](../README.md). This file is the gate. [Squid](https://www.squid-cache.org/) reads it. Every setting is described in the [Squid 6 config guide](https://www.squid-cache.org/Versions/v6/cfgman/). Hermetarium copies the file as you wrote it. Put your real key in a copy of the file before you start. The word to replace is `__VENDOR_KEY__`.

```
   you        ---------------->  Grok Build      open
   Grok Build ---------------->  api.x.ai        open, key attached
   Grok Build ---------------->  anywhere else   closed
```

The image points Grok's model client at `http://grok.hermetarium.test/v1` (`GROK_XAI_API_BASE_URL`). On Grok 1.0.77 that environment variable is what moves the client. `GROK_CLI_CHAT_PROXY_BASE_URL` only moves feedback and trace calls. Without the model setting, Grok dials `api.x.ai` on port 443, and the closed gate drops that call.

`__VENDOR_KEY__` must be an xAI API key. Grok asks `GET /v1/api-key` before it takes a question. xAI answers 200 for an API key. A login session token, the kind `grok login` stores, gets 401, and Grok then stops. The gate attaches that API key to the calls that go on to xAI.

Squid opens `api.x.ai` and sets the site name to `api.x.ai`. xAI answers 403 when the name is still `grok.hermetarium.test`. The gate file replaces the Host line on that path. Squid 6.14 has no `forcedomain` option on the peer.

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

Build the small helper that carries your question to Grok, then build the image:

```bash
go build -o inhabitants/grok-build/porter ./porter
```

```bash
docker build -t grok inhabitants/grok-build
```

Copy the gate file and replace `__VENDOR_KEY__` with your xAI API key. A login token from `grok login` is not that key.

```bash
cp inhabitants/grok-build/squid.conf /tmp/grok.acl
```

Start it. The command prints an id: `YOUR_ID`. Use that printed line in the commands below. To choose it, add `--id NAME`, for example `--id grok`. The command prints that name.

```bash
./bin/hermetarium create --image grok --acl /tmp/grok.acl
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

Firecracker has not been shown to work for any inhabitant. This image was started on that wall, and a question never came back. Use the Docker wall.
