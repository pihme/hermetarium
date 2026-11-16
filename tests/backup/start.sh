#!/bin/sh
# The first boot creates /known. A restored root file already has it.
if [ ! -f /known ]; then
  echo known-v1 > /known
  echo FIRST_BOOT
else
  echo "KNOWN=$(cat /known)"
  echo RESTORED_BOOT
fi
# Stable unique bytes. The second snapshot should add only what changed.
if [ ! -f /stable ]; then
  dd if=/dev/urandom of=/stable bs=1048576 count=16 status=none
fi
sync
echo KNOWN_READY
i=0
while true; do
  i=$((i + 1))
  echo "churn-$i" > /churn
  echo "WRITE_$i"
  sleep 0.2
done
