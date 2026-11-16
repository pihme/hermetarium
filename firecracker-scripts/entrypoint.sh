#!/bin/sh
# Firecracker scripts: TAP + Firecracker. Squid is a sibling in this netns.
set -eu
# A reused id keeps this file. Clear it before Squid's pid can appear, or
# Firecracker would start on the previous boot's listener.
mkdir -p /log
rm -f /log/dns.ready /log/rules.ready
test -c /dev/kvm || { echo "fc-scripts: no /dev/kvm" >&2; exit 1; }
test -c /dev/net/tun || { echo "fc-scripts: no /dev/net/tun" >&2; exit 1; }

ip tuntap add tap0 mode tap
ip addr add "${GATE_IP:?}/${GATE_PREFIX:-24}" dev tap0
ip link set tap0 up
# The weak wall runs this same file. Do not add a second allow-list here.
sh /log/gate-rules.sh
: > /log/rules.ready

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
