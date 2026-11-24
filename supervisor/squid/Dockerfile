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
# The gate files answer every denial with this page, through deny_info, and
# point error_directory here. Squid only reads an unknown deny_info name from
# error_directory: a file under its own errors/ tree still leaves the answer as
# "Internal Error: Missing Template". error_directory must also hold every
# page Squid may look for; a directory with only our page starts, but writes a
# "loading file ... No such file or directory" ERROR per missing template on
# every start and leaves the other answers to a fallback nobody chose. So the
# stock English set is copied in first and our page layered on top. Only the
# error-details.txt copy is allowed to miss: it sits outside en/ and some
# builds do not ship it. A failed copy of the set itself fails this step, so a
# broken image cannot be built silently.
RUN mkdir -p /usr/local/share/hermetarium/errors \
 && cp -a /usr/share/squid/errors/en/. /usr/local/share/hermetarium/errors/ \
 && { cp /usr/share/squid/errors/error-details.txt /usr/local/share/hermetarium/errors/ 2>/dev/null || true; }
COPY errors/ /usr/local/share/hermetarium/errors/
# Squid's remaining templates and the startup probe report visible_hostname,
# which this image does not set. A defaults file in conf.d ends the warning.
RUN printf 'visible_hostname hermetarium\n' > /etc/squid/conf.d/hermetarium.conf