#!/bin/sh
# Keep writing /data for the whole life of the habitat.
echo known-v1 > /data/known
# Stable unique bytes. The second snapshot should add only what changed.
dd if=/dev/urandom of=/data/stable bs=1048576 count=16 status=none
sync
echo KNOWN_READY
i=0
while true; do
  i=$((i + 1))
  echo "churn-$i" > /data/churn
  echo "WRITE_$i"
  sleep 0.2
done
