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
printf '172.16.0.1 probe.hermetarium.test claude.hermetarium.test grok.hermetarium.test deepseek.hermetarium.test\n' >> /etc/hosts
echo "GUEST_UNAME=$(uname -r)"
body=""
if [ -x /bin/busybox ]; then
  body=$(/bin/busybox wget -qO- -T 5 http://probe.hermetarium.test/hello 2>/dev/null || true)
  if [ -z "$body" ]; then
    body=$(/bin/busybox wget -qO- -T 5 http://172.16.0.1/hello 2>/dev/null || true)
  fi
elif command -v curl >/dev/null 2>&1; then
  body=$(curl -sS --connect-timeout 3 --max-time 8 http://probe.hermetarium.test/hello 2>/dev/null || true)
fi
echo "PROBE_BODY=$body"
case "$body" in
  *hermetarium-ok*) echo PROBE_OK ;;
  *) echo PROBE_FAIL ;;
esac
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
cmd=$(cat /fc-cmd)
echo "FC_CMD=$cmd"
exec $cmd
EOF
chmod +x /rootfs/fc-init
umount /rootfs
echo "rootfs ready"
