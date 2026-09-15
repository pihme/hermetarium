#!/bin/sh
# Squid process plus a local origin for the hello-world probe.
set -eu
mkdir -p /log /var/spool/squid /run
httpd -p 0.0.0.0:18080 -h /probe
MOCK_EXPECT_KEY="${MOCK_EXPECT_KEY:-htm-test-key}" /usr/local/bin/vendor-mock &
exec squid -N -f /log/squid.conf
