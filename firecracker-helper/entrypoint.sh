#!/bin/sh
# Strong-wall helper: TAP + Squid + Firecracker. Not the inhabitant.
set -eu
test -c /dev/kvm || { echo "fc-helper: no /dev/kvm" >&2; exit 1; }
test -c /dev/net/tun || { echo "fc-helper: no /dev/net/tun" >&2; exit 1; }

ip tuntap add tap0 mode tap
ip addr add 172.16.0.1/24 dev tap0
ip link set tap0 up
sysctl -w net.ipv4.ip_forward=1 >/dev/null
sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null
sysctl -w net.ipv4.conf.tap0.rp_filter=0 >/dev/null
iptables -P FORWARD DROP
# Probe hits 172.16.0.1:80 (accel). Everything else on :80 is intercepted.
iptables -t nat -A PREROUTING -s 172.16.0.0/24 -p tcp --dport 80 ! -d 172.16.0.1 -j REDIRECT --to-ports 3128

mkdir -p /log /var/spool/squid /run
httpd -p 0.0.0.0:18080 -h /probe
MOCK_EXPECT_KEY="${MOCK_EXPECT_KEY:-htm-test-key}" /usr/local/bin/vendor-mock &
trap '' HUP
squid -N -f /log/squid.conf &
i=0
while [ "$i" -lt 50 ]; do
  if test -f /log/squid.pid; then
    break
  fi
  sleep 0.1
  i=$((i + 1))
done

cp /opt/firecracker /tmp/firecracker
chmod +x /tmp/firecracker
/tmp/firecracker \
  --no-api \
  --no-seccomp \
  --log-path /log/fc.log \
  --level Info \
  --config-file /log/vm.json
