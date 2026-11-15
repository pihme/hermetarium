#!/bin/sh
if [ -f /data/marker ]; then
  echo "DATA_MARKER=$(cat /data/marker)"
else
  echo DATA_MARKER=missing
  echo persisted > /data/marker
  printf '#!/bin/sh\necho ran\n' > /data/runme
  chmod +x /data/runme
fi
echo DATA_READY
sleep 3600
