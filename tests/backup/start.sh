#!/bin/sh
# Keep writing /data for the whole life of the habitat.
echo known-v1 > /data/known
sync
echo KNOWN_READY
i=0
while true; do
  i=$((i + 1))
  echo "churn-$i" > /data/churn
  echo "WRITE_$i"
  sleep 0.2
done
