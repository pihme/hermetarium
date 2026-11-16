#!/bin/sh
# Firecracker scripts: TAP + Firecracker. Squid is a sibling in this netns.
set -eu
# A reused id keeps this file. Clear it before Squid's pid can appear, or
# Firecracker would start on the previous boot's listener.
mkdir -p /log
rm -f /log/dns.ready /log/rules.ready /log/ca.ready
test -c /dev/kvm || { echo "fc-scripts: no /dev/kvm" >&2; exit 1; }
test -c /dev/net/tun || { echo "fc-scripts: no /dev/net/tun" >&2; exit 1; }

ip tuntap add tap0 mode tap
ip addr add "${GATE_IP:?}/${GATE_PREFIX:-24}" dev tap0
ip link set tap0 up
# gate-rules.sh is the only allow-list. The weak wall runs that file in its
# own netns and does not start this process.
sh /log/gate-rules.sh
: > /log/rules.ready
# The weak wall bind-mounts the certificate. httpd has to stay in this
# process: create cannot see a second container exit, and the guest must
# not be released until the port accepts.
if [ "${GATE_HTTPS:-}" = 1 ]; then
  /usr/sbin/httpd -f -p "${GATE_IP:?}:${GATE_CA_PORT:?}" -h /log/mitm-pub &
  hp=$!
  i=0
  while [ "$i" -lt 150 ]; do
    if ! kill -0 "$hp" 2>/dev/null; then
      echo "fc-scripts: certificate server exited" >&2
      exit 1
    fi
    if wget -q -T 1 -O /dev/null "http://${GATE_IP}:${GATE_CA_PORT}/ca.pem"; then
      : > /log/ca.ready
      break
    fi
    sleep 0.1
    i=$((i + 1))
  done
  if [ ! -f /log/ca.ready ]; then
    echo "fc-scripts: certificate server did not accept" >&2
    exit 1
  fi
fi

printf '%s inhabitant\n' "${GUEST_IP:?}" >> /etc/hosts
mkdir -p /log
i=0
while [ "$i" -lt 200 ]; do
  if test -f /log/squid.pid; then
    break
  fi
  sleep 0.1
  i=$((i + 1))
done
test -f /log/squid.pid || { echo "fc-scripts: squid.pid missing" >&2; exit 1; }
# squid.pid is written before gatedns listens. Do not boot on that alone.
i=0
while [ "$i" -lt 200 ]; do
  if test -f /log/dns.ready; then
    break
  fi
  sleep 0.1
  i=$((i + 1))
done
test -f /log/dns.ready || { echo "fc-scripts: /log/dns.ready missing" >&2; exit 1; }

cp /opt/firecracker /tmp/firecracker
chmod +x /tmp/firecracker
/tmp/firecracker \
  --no-api \
  --log-path /log/fc.log \
  --level Info \
  --config-file /log/vm.json &
fc=$!
wait "$fc"
