#!/bin/sh
# Guest dockerd, then Porter. A daemon that cannot start must not block Porter.
# iptables stay off: this kernel has no CONFIG_IP_NF_RAW, and the gate is the
# only path out. A container that should use that path runs with --network host.
if command -v dockerd >/dev/null 2>&1; then
	export DOCKER_INSECURE_NO_IPTABLES_RAW=1
	if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
		mkdir -p /sys/fs/cgroup/init
		while read -r p; do
			echo "$p" > /sys/fs/cgroup/init/cgroup.procs 2>/dev/null || true
		done < /sys/fs/cgroup/cgroup.procs
		for c in $(cat /sys/fs/cgroup/cgroup.controllers); do
			echo "+$c" > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true
		done
	fi
	mount --make-rshared / 2>/dev/null || true
	if command -v setsid >/dev/null 2>&1; then
		setsid dockerd --host=unix:///var/run/docker.sock >/var/log/dockerd.log 2>&1 &
	else
		dockerd --host=unix:///var/run/docker.sock >/var/log/dockerd.log 2>&1 &
	fi
fi
exec /usr/local/bin/porter
