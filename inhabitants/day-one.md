# Day one in a Hermetarium habitat

This file is the same in every habitat built from this image. It holds only general facts. It does not list this habitat's allowlist.

## The wall CA

The wall's public certificate is at `/etc/ssl/hermetarium-mitm.crt`. A copy is also at `/usr/local/share/ca-certificates/hermetarium-mitm.crt`. On boot it is added to the system trust store when `update-ca-certificates` is available.

Its SHA-256 fingerprint is:

```
openssl x509 -noout -fingerprint -sha256 -in /etc/ssl/hermetarium-mitm.crt
```

These environment variables already point at that file: `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`, `GIT_SSL_CAINFO`, and `PIP_CERT`. curl, git, Node, and pip use them. Some other toolchains do not.

### JVM

```
keytool -importcert -noprompt -alias hermetarium \
  -file /etc/ssl/hermetarium-mitm.crt \
  -keystore "$JAVA_HOME/lib/security/cacerts" \
  -storepass changeit
```

Use the `cacerts` file your JDK actually reads if it is not that path.

### Rust / cargo

```
export CARGO_HTTP_CAINFO=/etc/ssl/hermetarium-mitm.crt
```

### .NET

The system trust store already contains the CA after boot, and many .NET builds on Linux use that store. If a process still rejects the certificate:

```
export SSL_CERT_FILE=/etc/ssl/hermetarium-mitm.crt
```

## Fail closed

Only hosts the operator has allowlisted can be reached from this habitat. Everything else is refused. That is the design. This guide does not name the allowlist; it lives in the gate file on the host.

## Nested Docker

When this image includes a Docker daemon, that daemon is started at boot. It leaves iptables off, so a container has no NAT of its own. A container that needs the wall's network path must be started with `--network host`.

Registry pulls go through the wall, like any other HTTPS. Whether the wall intercepts a registry host or passes it through (so the original certificate is kept) is decided by this habitat's gate file on the host, not by this guide.

## Root and tests

The inhabitant runs as root. For tests that must run as a non-root user, create one and run the suite as that user, for example:

```
adduser --disabled-password --gecos '' testuser
su testuser -c 'your-test-command'
```
