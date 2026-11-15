#!/bin/bash
# The restart right after the Squid image is named sometimes returns in
# about 40ms with "Job for docker.service failed". The same command
# succeeds on a later try. Retry it, and keep the daemon log when every
# try fails.
set -u
attempts=5
i=1
while [ "$i" -le "$attempts" ]; do
  sudo systemctl reset-failed docker >/dev/null 2>&1 || true
  if sudo systemctl restart docker; then
    exit 0
  fi
  echo "systemctl restart docker failed (attempt ${i}/${attempts})" >&2
  sleep $((i * 2))
  i=$((i + 1))
done
sudo systemctl status docker --no-pager >&2 || true
sudo journalctl -u docker --no-pager -n 80 >&2 || true
exit 1
