#!/bin/sh
# Build a tiny ext4 rootfs with busybox + wget. Run inside a privileged Alpine container.
set -eu
apk add --no-cache e2fsprogs busybox
dd if=/dev/zero of=/out/rootfs.ext4 bs=1M count=64
mkfs.ext4 -F /out/rootfs.ext4
mkdir -p /rootfs
mount /out/rootfs.ext4 /rootfs
mkdir -p /rootfs/bin /rootfs/lib /rootfs/proc /rootfs/sys /rootfs/dev /rootfs/tmp /rootfs/etc
cp /bin/busybox /rootfs/bin/busybox
cp /lib/ld-musl-x86_64.so.1 /rootfs/lib/ld-musl-x86_64.so.1
ln -sf ld-musl-x86_64.so.1 /rootfs/lib/libc.musl-x86_64.so.1
/rootfs/bin/busybox --install /rootfs/bin
test -x /out/echo-service
cp /out/echo-service /rootfs/bin/echo-service
chmod +x /rootfs/bin/echo-service
cat > /rootfs/init << 'EOF'
#!/bin/sh
/bin/busybox mkdir -p /proc /sys /dev /tmp
/bin/busybox mount -t proc none /proc
/bin/busybox mount -t sysfs none /sys
/bin/busybox mount -t devtmpfs none /dev 2>/dev/null || true
/bin/busybox ip link set lo up
i=0
while [ "$i" -lt 30 ]; do
  if /bin/busybox ip link show eth0 >/dev/null 2>&1; then
    break
  fi
  /bin/busybox sleep 1
  i=$((i + 1))
done
/bin/busybox ip link set eth0 up
/bin/busybox ip addr add 172.16.0.2/24 dev eth0
/bin/busybox ip route add default via 172.16.0.1
/bin/echo-service &
echo "GUEST_UNAME=$(/bin/busybox uname -r)"
body=""
if body=$(/bin/busybox wget -qO- -T 8 http://probe.hermetarium.test/hello); then
  :
elif body=$(/bin/busybox wget -qO- -T 8 http://172.16.0.1/hello); then
  :
fi
echo "PROBE_BODY=$body"
case "$body" in
  *hermetarium-ok*) echo PROBE_OK ;;
  *) echo PROBE_FAIL ;;
esac
if /bin/busybox ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then
  echo LEAK_OK
else
  echo LEAK_FAIL
fi
while true; do
  /bin/busybox sleep 3600
done
EOF
chmod +x /rootfs/init
echo "172.16.0.1 probe.hermetarium.test" > /rootfs/etc/hosts
echo "rootfs ready"
umount /rootfs
