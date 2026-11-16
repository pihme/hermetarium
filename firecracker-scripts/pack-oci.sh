#!/bin/sh
# Pack a docker-export tarball into an ext4 rootfs with /fc-init.
# Expects /in/rootfs.tar, /in/fc-cmd, writes /out/rootfs.ext4.
# mke2fs -d writes the image from a directory. Loop-mounting it would need
# a privileged container, which is host root outside the wall.
set -eu
SIZE_MB="${SIZE_MB:-256}"
apk add --no-cache e2fsprogs tar >/dev/null
mkdir -p /rootfs
tar -xf /in/rootfs.tar -C /rootfs
mkdir -p /rootfs/proc /rootfs/sys /rootfs/dev /rootfs/tmp /rootfs/etc
cp /in/fc-cmd /rootfs/fc-cmd
chmod +x /rootfs/fc-cmd 2>/dev/null || true
if [ -f /in/fc-env ]; then
  cp /in/fc-env /rootfs/fc-env
fi
# Static amd64 ELF: ET_EXEC, one PT_LOAD (R+X) at vaddr 0x400000, filesz 0xa9,
# entry 0x400078. Code is file offset 0x78. buf is in the red zone.
#   78: 48 c7 c0 3e 01 00 00   mov rax, 0x13e   # getrandom
#   7f: 48 8d 7c 24 f0         lea rdi, [rsp-0x10]
#   84: 48 c7 c6 10 00 00 00   mov rsi, 0x10
#   8b: 48 31 d2               xor rdx, rdx
#   8e: 0f 05                  syscall
#   90: 48 83 f8 10            cmp rax, 0x10
#   94: 48 c7 c7 01 00 00 00   mov rdi, 1
#   9b: 75 03                  jne 0xa0         # short by 16: rdi stays 1
#   9d: 48 31 ff               xor rdi, rdi     # 16 bytes: exit 0
#   a0: 48 c7 c0 3c 00 00 00   mov rax, 0x3c    # exit(rdi)
#   a7: 0f 05                  syscall
# fc-init bounds the call so a missing CPU RNG cannot stall boot.
base64 -d > /rootfs/fc-getrandom << 'END'
f0VMRgIBAQAAAAAAAAAAAAIAPgABAAAAeABAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAEAAOAABAAAAAAAAAAEAAAAFAAAAAAAAAAAAAAAAAEAAAAAAAAAAQAAAAAAAqQAAAAAAAACpAAAAAAAAAAAQAAAAAAAASMfAPgEAAEiNfCTwSMfGEAAAAEgx0g8FSIP4EEjHxwEAAAB1A0gx/0jHwDwAAAAPBQ==
END
chmod +x /rootfs/fc-getrandom
cat > /rootfs/fc-init << 'EOF'
#!/bin/sh
exec >/dev/ttyS0 2>&1 || exec >/dev/console 2>&1 || true
mount -t proc none /proc 2>/dev/null || true
mount -t sysfs none /sys 2>/dev/null || true
mount -t devtmpfs none /dev 2>/dev/null || true
# attach needs a pty. devtmpfs does not provide /dev/pts.
# A container runtime in the guest needs these.
mkdir -p /dev/pts /dev/shm /run /sys/fs/cgroup
mount -t devpts -o ptmxmode=0666,mode=0620 devpts /dev/pts 2>/dev/null || mount -t devpts devpts /dev/pts 2>/dev/null || true
mount -t tmpfs tmpfs /dev/shm 2>/dev/null || true
mount -t tmpfs tmpfs /run 2>/dev/null || true
mount -t cgroup2 cgroup2 /sys/fs/cgroup 2>/dev/null || true
# virtio-rng (VM config "entropy") shows up as virtio_rng.N. Wait briefly:
# the misc node can lag devtmpfs.
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
if [ ! -x /bin/busybox ]; then
  echo GUEST_GETRANDOM_SKIPPED
elif /bin/busybox timeout 2 /fc-getrandom; then
  echo GUEST_GETRANDOM_OK
else
  echo GUEST_GETRANDOM_FAIL
fi
# Same shell, so the daemon inherits the image environment. Before the
# network wait and before the inhabitant.
if [ -f /fc-env ]; then
  set -a
  # shellcheck disable=SC1091
  . /fc-env
  set +a
fi
# Address plan is on the kernel command line. Create is the only writer.
# A token that is not an address must not reach the shell.
ipv4() {
  old=$IFS
  set -f
  IFS=.
  set -- $1
  IFS=$old
  set +f
  [ $# -eq 4 ] || return 1
  for o do
    case $o in
      ''|*[!0-9]*) return 1 ;;
    esac
    [ ${#o} -le 3 ] || return 1
    n=$o
    while [ "${n#0}" != "$n" ]; do n=${n#0}; done
    [ -n "$n" ] || n=0
    [ "$n" -le 255 ] || return 1
  done
}
hostnames() {
  old=$IFS
  set -f
  IFS=,
  set -- $1
  IFS=$old
  set +f
  [ $# -ge 1 ] || return 1
  for h do
    case $h in
      ''|*[!A-Za-z0-9.-]*|.*|*.|-*|*-|*..*) return 1 ;;
    esac
  done
}
b64=
ca=
gate=
guest=
prefix=
names=
for tok in $(cat /proc/cmdline); do
  case $tok in
    hermetarium.pub=*) b64=${tok#hermetarium.pub=} ;;
    hermetarium.ca=*) ca=${tok#hermetarium.ca=} ;;
    hermetarium.gate=*) gate=${tok#hermetarium.gate=} ;;
    hermetarium.guest=*) guest=${tok#hermetarium.guest=} ;;
    hermetarium.prefix=*) prefix=${tok#hermetarium.prefix=} ;;
    hermetarium.names=*) names=${tok#hermetarium.names=} ;;
  esac
done
case $b64 in
  *[!A-Za-z0-9+/=]*|"") b64="" ;;
esac
case $ca in
  *[!A-Za-z0-9:/._-]*) ca="" ;;
esac
if ! ipv4 "$gate" || ! ipv4 "$guest" || ! hostnames "$names"; then
  echo "fc-init: gate address missing"
  exit 1
fi
case $prefix in
  ''|*[!0-9]*) echo "fc-init: gate address missing"; exit 1 ;;
esac
n=$prefix
while [ "${n#0}" != "$n" ]; do n=${n#0}; done
[ -n "$n" ] || n=0
if [ "$n" -lt 1 ] || [ "$n" -gt 32 ]; then
  echo "fc-init: gate address missing"
  exit 1
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
$IP addr add "$guest/$prefix" dev eth0 2>/dev/null || true
$IP route add default via "$gate" 2>/dev/null || true
# The image ships an empty resolv.conf, and this guest's hosts file has no localhost.
printf 'nameserver %s\n' "$gate" > /etc/resolv.conf
printf '127.0.0.1 localhost\n' >> /etc/hosts
list=$(printf '%s' "$names" | tr ',' ' ')
printf '%s %s\n' "$gate" "$list" >> /etc/hosts
# The image that wants a terminal brings sshd. The public key is per habitat,
# on the kernel command line, so one cached rootfs can boot many habitats.
if [ -n "$b64" ] && [ -x /usr/sbin/sshd ]; then
  # The tmpfs on /run hides the directory Debian's sshd requires.
  mkdir -p /root/.ssh /var/empty /run/sshd
  chmod 700 /root /root/.ssh
  chmod 755 /run/sshd
  printf 'ssh-ed25519 %s hermetarium\n' "$b64" > /root/.ssh/authorized_keys
  chmod 600 /root/.ssh/authorized_keys
  if [ -x /usr/bin/ssh-keygen ]; then
    /usr/bin/ssh-keygen -A >/dev/null 2>&1 || true
  fi
  cat > /fc-sshd.conf << END
ListenAddress $guest
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
UseDNS no
PidFile /run/sshd.pid
AuthorizedKeysFile /root/.ssh/authorized_keys
AllowTcpForwarding no
AllowAgentForwarding no
X11Forwarding no
PermitTunnel no
PrintMotd no
AddressFamily inet
END
  # This sshd refuses a bare command name.
  if ! /usr/sbin/sshd -f /fc-sshd.conf; then
    echo "fc-init: sshd did not start"
  fi
fi
echo "GUEST_UNAME=$(uname -r)"
if [ -x /bin/busybox ] && /bin/busybox ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then
  echo LEAK_OK
elif command -v ping >/dev/null 2>&1 && ping -c 1 -W 2 1.1.1.1 >/dev/null 2>&1; then
  echo LEAK_OK
else
  echo LEAK_FAIL
fi
# Create passes hermetarium.ca only when HTTPS interception is on.
# gate-cert.sh is the same install the weak wall executes.
if [ -n "$ca" ]; then
  GATE_CA_URL="$ca"
  # shellcheck disable=SC1091
  . /gate-cert.sh
fi
cmd=$(cat /fc-cmd)
echo "FC_CMD=$cmd"
exec $cmd
EOF
chmod +x /rootfs/fc-init
cp /gate-cert.sh /rootfs/gate-cert.sh
chmod +x /rootfs/gate-cert.sh
truncate -s "${SIZE_MB}M" /out/rootfs.ext4
# Do not allocate the journal or inode tables; a large --disk must stay sparse.
mke2fs -t ext4 -F -d /rootfs -E lazy_itable_init=1,lazy_journal_init=1 /out/rootfs.ext4 >/dev/null
echo "rootfs ready"
