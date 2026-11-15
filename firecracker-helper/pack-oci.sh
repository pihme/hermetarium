#!/bin/sh
# Pack a docker-export tarball into an ext4 rootfs with /fc-init.
# Expects /in/rootfs.tar, /in/fc-cmd, writes /out/rootfs.ext4.
set -eu
SIZE_MB="${SIZE_MB:-256}"
apk add --no-cache e2fsprogs tar >/dev/null
truncate -s "${SIZE_MB}M" /out/rootfs.ext4
# mkfs must not allocate the journal and inode tables.
mkfs.ext4 -F -E lazy_itable_init=1,lazy_journal_init=1 /out/rootfs.ext4 >/dev/null
mkdir -p /rootfs
mount /out/rootfs.ext4 /rootfs
tar -xf /in/rootfs.tar -C /rootfs
mkdir -p /rootfs/proc /rootfs/sys /rootfs/dev /rootfs/tmp /rootfs/etc
cp /in/fc-cmd /rootfs/fc-cmd
chmod +x /rootfs/fc-cmd 2>/dev/null || true
if [ -f /in/fc-env ]; then
  cp /in/fc-env /rootfs/fc-env
fi
# amd64 ELF, no libc: getrandom(buf, 16, 0), then exit 0 only when 16 bytes
# came back. fc-init bounds it so a missing CPU RNG cannot stall boot.
base64 -d > /rootfs/fc-getrandom << 'END'
f0VMRgIBAQAAAAAAAAAAAAIAPgABAAAAeABAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAEAAOAABAAAAAAAAAAEAAAAFAAAAAAAAAAAAAAAAAEAAAAAAAAAAQAAAAAAAqQAAAAAAAACpAAAAAAAAAAAQAAAAAAAASMfAPgEAAEiNfCTwSMfGEAAAAEgx0g8FSIP4EEjHxwEAAAB1BUgx/0jHwDwAAAAPBQ==
END
chmod +x /rootfs/fc-getrandom
cat > /rootfs/fc-init << 'EOF'
#!/bin/sh
exec >/dev/ttyS0 2>&1 || exec >/dev/console 2>&1 || true
mount -t proc none /proc 2>/dev/null || true
mount -t sysfs none /sys 2>/dev/null || true
mount -t devtmpfs none /dev 2>/dev/null || true
# A container runtime in the guest needs these.
mkdir -p /dev/pts /dev/shm /run /sys/fs/cgroup
mount -t devpts devpts /dev/pts 2>/dev/null || true
mount -t tmpfs tmpfs /dev/shm 2>/dev/null || true
mount -t tmpfs tmpfs /run 2>/dev/null || true
mount -t cgroup2 cgroup2 /sys/fs/cgroup 2>/dev/null || true
# virtio-rng (VM config "entropy") shows up as virtio_rng.N. Wait briefly:
# the misc node can lag devtmpfs the same way /dev/vdb does.
rng=missing
i=0
while [ "$i" -lt 50 ]; do
  if [ -r /sys/class/misc/hw_random/rng_current ]; then
    rng=$(cat /sys/class/misc/hw_random/rng_current)
    case "$rng" in
      *virtio_rng*) break ;;
    esac
  fi
  sleep 0.1
  i=$((i + 1))
done
echo "GUEST_RNG=$rng"
if [ -x /bin/busybox ] && /bin/busybox timeout 2 /fc-getrandom; then
  echo GUEST_GETRANDOM_OK
else
  echo GUEST_GETRANDOM_FAIL
fi
# hermetarium.data=1 means create --data. The node can lag devtmpfs.
if grep -q 'hermetarium.data=1' /proc/cmdline 2>/dev/null; then
  i=0
  while [ "$i" -lt 50 ]; do
    if [ -b /dev/vdb ]; then
      break
    fi
    sleep 0.1
    i=$((i + 1))
  done
  mkdir -p /data
  # destroy powers the machine off. sync makes a finished write durable.
  # Do not start the inhabitant when the disk will not mount.
  if ! mount -t ext4 -o sync,noatime /dev/vdb /data; then
    echo DATA_MOUNT_FAIL
    exit 1
  fi
  echo DATA_MOUNT_OK
fi
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
# speaks plain HTTP. Linux 6.1 has entropy at boot, so curl is safe, and
# timeout 3 still bounds the call.
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
