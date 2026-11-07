#!/bin/sh
# The shim must be up before Porter starts Grok. Porter is the process Docker stops.
set -eu
perl /usr/local/bin/grok-shim.pl &
exec /usr/local/bin/porter
