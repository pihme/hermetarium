# Built by the supervisor on the first create.
# The FROM line is the public Squid image. Dependabot tracks this file.
# squid-openssl replaces the GnuTLS build so the gate can decrypt HTTPS.
FROM ubuntu/squid:6.6-24.04_beta
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends squid-openssl \
 && rm -rf /var/lib/apt/lists/* \
 && test -x /usr/lib/squid/security_file_certgen \
 && squid -v | grep -q -- --with-openssl
