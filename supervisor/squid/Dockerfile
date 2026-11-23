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
# The page a gate file names in deny_info. It says why, and nothing about the
# allowlist or the gate. hermetarium logs names the same refusal not-allowlisted.
# Release-Notes: A request the wall denies gets `403` and the body `hermetarium wall: not allowlisted` when the gate file has `deny_info ERR_HTM_NOT_ALLOWLISTED` for its deny rules, as the shipped gate files do.
RUN printf 'hermetarium wall: not allowlisted\nThis request is not on the allowlist of this habitat.\n' \
    > /usr/share/squid/errors/templates/ERR_HTM_NOT_ALLOWLISTED
