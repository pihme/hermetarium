# Day one

You are a process inside a habitat: one image on one wall, booted by Hermetarium.
This file is part of the image. Nothing in it is generated per habitat. It holds only facts that are true for every habitat that boots this image.

You are root. You may install packages and change files. The image is yours.

## The gate

The only way out of the habitat is a Squid gate on a host inside the image's network.
Every HTTP and HTTPS call leaves through it, including connections made by a program that knows nothing about a proxy.

The gate allows a list of hosts that the operator wrote for this habitat. Everything else is refused, by design.
Fail-by-design is the point: a refused connection is not a bug in your setup, and there is no setting in this image that opens it.
The list is not in this image. It is the gate file, on the host, outside the wall, and you cannot read it. Ask the operator.

The stand-in model hosts resolve to the gate: `claude.hermetarium.test`, `grok.hermetarium.test`, and `model.hermetarium.test`.
No other outside name resolves unless the gate file allows the web. Outside names go through the same gate, never to another resolver.

## The gate certificate

The gate can decrypt HTTPS and re-sign it, so it can record the site and the path.
The habitat trusts that certificate at one path:

```
/etc/ssl/hermetarium-mitm.crt
```

The private key is not in the habitat. Keep the public file where it is; do not replace or delete it.
It is already installed in the system trust store (`/etc/ssl/certs/ca-certificates.crt`) when the image has a trust store.

The gate sets these variables, so these tools trust the gate without further work:

```
SSL_CERT_FILE
NODE_EXTRA_CA_CERTS
REQUESTS_CA_BUNDLE
CURL_CA_BUNDLE
GIT_SSL_CAINFO
PIP_CERT
```

A program that does not read them needs the file itself. The fingerprint, if you need to check it, is the SHA-256 of the file:

```sh
openssl x509 -in /etc/ssl/hermetarium-mitm.crt -noout -fingerprint -sha256
```

- **JVM.** Import it into the JDK trust store: `keytool -importcert -noprompt -alias hermetarium -file /etc/ssl/hermetarium-mitm.crt -keystore "$JAVA_HOME/lib/security/cacerts" -storepass changeit`. A PEM file cannot be passed as `-Djavax.net.ssl.trustStore`: the JVM reads that setting as a JKS/PKCS12 store, not PEM. Import it first, or point `trustStore` at a store you built with `keytool` from it.
- **Rust / cargo.** `cargo` honours `CARGO_HTTP_CAINFO`. Set it to `/etc/ssl/hermetarium-mitm.crt`. A Rust program that builds its own TLS client must load that file, for example with `rustls-pemfile`, or pass `--cacert` to the `curl` it shells out to.
- **.NET.** .NET reads its own bundle, not the system one. Set `SSL_CERT_FILE` (it is honoured on Linux), or set `X509Chain` trust for the file in code.

A program that ships its own certificate list and ignores all of this will refuse the gate certificate. The gate file can pass such a host through undecrypted, if the operator allows it.

## Nested Docker

The image may run its own Docker daemon.

- The daemon runs. Start it if the image does not start it for you (`dockerd` or the image's start script).
- A container that should reach the gate uses `--network host`. The habitat's own container has no Docker bridge that leads out.
- Image pulls from a registry go through the gate. Whether the registry and its blob host are passed through is decided by the gate file, not by this image. If they are, the daemon sees the registry's real certificates. If they are not, `dockerd` falls back to the wall CA it inherits (`SSL_CERT_FILE`, `CURL_CA_BUNDLE`), which works but is the weaker arrangement.

## Root and tests

The inhabitant runs as root on both walls. A shell command is uid 0, and `id -u` prints `0`.

Some test suites refuse to run as root. Create the user in the image or in the habitat, and run those tests as that user:

```sh
adduser --disabled-password --gecos '' tester
su tester -c 'cd /work && ./run-tests.sh'
```

Your own files under `/root` are not readable by that user. Put anything the test needs under a directory the test user can read.
