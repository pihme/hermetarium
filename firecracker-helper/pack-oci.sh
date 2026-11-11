#!/bin/sh
# Pack a docker-export tarball into an ext4 rootfs with /fc-init.
# Expects /in/rootfs.tar, /in/fc-cmd, writes /out/rootfs.ext4.
set -eu
SIZE_MB="${SIZE_MB:-256}"
apk add --no-cache e2fsprogs tar >/dev/null
dd if=/dev/zero of=/out/rootfs.ext4 bs=1M count="$SIZE_MB"
mkfs.ext4 -F /out/rootfs.ext4 >/dev/null
mkdir -p /rootfs
mount /out/rootfs.ext4 /rootfs
tar -xf /in/rootfs.tar -C /rootfs
mkdir -p /rootfs/proc /rootfs/sys /rootfs/dev /rootfs/tmp /rootfs/etc
cp /in/fc-cmd /rootfs/fc-cmd
chmod +x /rootfs/fc-cmd 2>/dev/null || true
if [ -f /in/fc-env ]; then
  cp /in/fc-env /rootfs/fc-env
fi
cat > /rootfs/fc-init << 'EOF'
#!/bin/sh
exec >/dev/ttyS0 2>&1 || exec >/dev/console 2>&1 || true
mount -t proc none /proc 2>/dev/null || true
mount -t sysfs none /sys 2>/dev/null || true
mount -t devtmpfs none /dev 2>/dev/null || true
IP=/sbin/ip
[ -x "$IP" ] || IP="ip"
$IP link set lo up 2>/dev/null || true
i=0
while [ "$i" -lt 30 ]; do
  if $IP link show eth0 >/dev/null 2>&1; then
    break
  fi
  sleep 1
  i=$((i + 1))
done
$IP link set eth0 up 2>/dev/null || true
$IP addr add 172.16.0.2/24 dev eth0 2>/dev/null || true
$IP route add default via 172.16.0.1 2>/dev/null || true
printf '172.16.0.1 probe.hermetarium.test claude.hermetarium.test grok.hermetarium.test\n' >> /etc/hosts
echo "GUEST_UNAME=$(uname -r)"
if [ -x /bin/busybox ] && /bin/busybox ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then
  echo LEAK_OK
elif command -v ping >/dev/null 2>&1 && ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then
  echo LEAK_OK
else
  echo LEAK_FAIL
fi
if [ -f /fc-env ]; then
  set -a
  # shellcheck disable=SC1091
  . /fc-env
  set +a
fi
# Port 3131 serves the gate CA only when HTTPS is open. busybox wget
# speaks plain HTTP. curl initializes TLS and blocks in getrandom on the
# 4.14 guest, before --max-time, so the server never starts.
mkdir -p /etc/ssl /usr/local/share/ca-certificates
ca=/etc/ssl/hermetarium-mitm.crt
ca_url=http://172.16.0.1:3131/ca.pem
fetched=0
if [ -x /bin/busybox ]; then
  if /bin/busybox timeout 3 /bin/busybox wget -q -T 2 -O "$ca" "$ca_url"; then
    fetched=1
  fi
elif command -v timeout >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
  if timeout 3 curl -fsS --connect-timeout 1 --max-time 2 -o "$ca" "$ca_url"; then
    fetched=1
  fi
fi
if [ "$fetched" -eq 1 ]; then
  cp "$ca" /usr/local/share/ca-certificates/hermetarium-mitm.crt
  if command -v update-ca-certificates >/dev/null 2>&1; then
    update-ca-certificates >/dev/null 2>&1 || true
  elif [ -f /etc/ssl/certs/ca-certificates.crt ]; then
    cat "$ca" >> /etc/ssl/certs/ca-certificates.crt
  fi
  export SSL_CERT_FILE="$ca"
  export NODE_EXTRA_CA_CERTS="$ca"
  export REQUESTS_CA_BUNDLE="$ca"
  export CURL_CA_BUNDLE="$ca"
  export GIT_SSL_CAINFO="$ca"
  export PIP_CERT="$ca"
fi
cmd=$(cat /fc-cmd)
echo "FC_CMD=$cmd"
exec $cmd
EOF
chmod +x /rootfs/fc-init
umount /rootfs
echo "rootfs ready"
