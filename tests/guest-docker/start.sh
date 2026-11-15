#!/bin/sh
# create waits for HTTP 200 on the inbound port, which Squid forwards here.
while true; do
  printf 'HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok' | nc -l -p 8080 || true
done >/dev/null 2>&1 &

export DOCKER_TLS_CERTDIR=
export DOCKER_INSECURE_NO_IPTABLES_RAW=1
export container=docker

# The dind wrapper writes every controller in one shot and retries forever
# when the kernel rejects one of them.
echo "CGROUP_CONTROLLERS=$(cat /sys/fs/cgroup/cgroup.controllers 2>/dev/null || echo missing)"
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
  mkdir -p /sys/fs/cgroup/init
  while read -r p; do
    echo "$p" > /sys/fs/cgroup/init/cgroup.procs 2>/dev/null || true
  done < /sys/fs/cgroup/cgroup.procs
  for c in $(cat /sys/fs/cgroup/cgroup.controllers); do
    if echo "+$c" > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null; then
      echo "CGROUP_ON=$c"
    else
      echo "CGROUP_OFF=$c"
    fi
  done
fi
mount --make-rshared / 2>/dev/null || true
if ! iptables -nL >/dev/null 2>&1 && [ -x /usr/local/sbin/.iptables-legacy/iptables ]; then
  export PATH="/usr/local/sbin/.iptables-legacy:$PATH"
fi

dockerd --host=unix:///var/run/docker.sock >/var/log/dockerd.log 2>&1 &

i=0
while [ "$i" -lt 90 ]; do
  if docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
  i=$((i + 1))
done
if ! docker info >/dev/null 2>&1; then
  echo DOCKERD_FAIL
  cat /var/log/dockerd.log
  sleep 3600
  exit 1
fi
docker load -i /images/busybox.tar || {
  echo DOCKER_LOAD_FAIL
  cat /var/log/dockerd.log
  sleep 3600
  exit 1
}
docker run --rm --network none busybox:1.37 echo hermetarium-docker-ok || {
  echo DOCKER_RUN_FAIL
  cat /var/log/dockerd.log
  sleep 3600
  exit 1
}
echo DOCKER_INNER_OK
sleep 3600
