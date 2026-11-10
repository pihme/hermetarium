# Inhabitants

An inhabitant is a coding assistant in its own container. [Squid](https://www.squid-cache.org/) is the gate in front of that container. The three `squid.conf` files share one shape. Each file names a different model company.

| Folder | Hermetarium runs | Vendor is | Docker | Firecracker |
| --- | --- | --- | --- | --- |
| [claude-code](claude-code/README.md) | Claude Code | Anthropic, `api.anthropic.com` | untested | not shown to work |
| [grok-build](grok-build/README.md) | Grok Build | xAI, `api.x.ai` | tested | does not work |
| [deepseek-harness](deepseek-harness/README.md) | DeepSeek Harness | DeepSeek, `api.deepseek.com` | untested | not shown to work |

No inhabitant has been shown to work on Firecracker. Grok was started on that wall, and a question never came back. Claude Code and DeepSeek Harness have no successful run there.

## Who is who

**Hermetarium** is the container `create` starts. In `squid.conf` that container is the peer named `inhabitant`. The assistant inside listens on port 8080.

**Vendor** is the model company's API. In `squid.conf` that API is the peer named `vendor`. The container does not call the company by its real name. It calls a stand-in host (`claude.hermetarium.test`, `grok.hermetarium.test`, or `deepseek.hermetarium.test`). Squid is the one that connects to the real API.

**Squid** is one gate with two jobs. Your question comes in through `inbound` and is handed to Hermetarium. Hermetarium's call to the model goes out through `vendor`, and Squid attaches the key from `__VENDOR_KEY__` on that trip. The key stays in `squid.conf`.

![Hermetarium architecture: the supervisor starts a Squid gate and the wall; operator HTTP and all inhabitant egress pass through Squid, which logs to the instance directory and injects vendor keys per ACL.](../docs/images/architecture.svg)

The two trips through Squid are the same gate.

```
   inbound   you           ---------------->  Hermetarium
   vendor    Hermetarium   ---------------->  the model API, key attached
   closed    Hermetarium   ---------------->  anywhere else
```

## What every squid.conf shares

[Squid's own guide](https://www.squid-cache.org/Versions/v6/cfgman/) describes each setting. The lines below are the same idea in every inhabitant file.

Squid listens on three ports. One is a plain local port Squid wants open. Port 18081 is `inbound`, your door to the container. Port 3128 catches the container's own web calls.

Logs go to `/log`. `hermetarium logs` prints them. Nothing is cached.

```
acl inbound myport 18081
acl vendorhost dstdomain <the stand-in host>
```

```
cache_peer inhabitant parent 8080 ... name=inhabitant
cache_peer <the model API> parent 443 ... name=vendor
```

The key lines drop any key the container sends, then add yours on the vendor host only:

```
request_header_access x-api-key deny all
request_header_access Authorization deny all
request_header_add x-api-key "__VENDOR_KEY__" vendorhost
request_header_add Authorization "Bearer __VENDOR_KEY__" vendorhost
```

Grok's file also sets `Host` to `api.x.ai` on that path. xAI answers 403 when the name is still the stand-in host. The value that replaces `__VENDOR_KEY__` in that file must be an xAI API key. A login session token is rejected.

Replace `__VENDOR_KEY__` in a copy of the file before `create`. Hermetarium copies that file as you wrote it.

These two lines keep every allowed call on a named peer. The model call uses `vendor`. Your question uses `inhabitant`.

```
always_direct deny all
never_direct allow all
```

Then who may pass:

```
http_access allow inbound
http_access allow vendorhost
http_access deny all
```

The container's plain HTTP to the rest of the web is present as comments marked `OPEN-IN` and `OPEN-OUT`. Ordinary HTTPS is the block marked `OPEN-HTTPS`. Squid decrypts those calls, and the assistant does not set a proxy. The gate is an OpenSSL Squid.  Each inhabitant README says which lines to comment in and which to comment out.
