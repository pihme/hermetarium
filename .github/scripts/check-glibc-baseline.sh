#!/usr/bin/env bash
# Fail if a Linux release binary could break on common distros:
#  - static binary (no PT_INTERP, no dynamic section): OK
#  - dynamic binary: newest GLIBC symbol must be <= baseline, and
#    RUNPATH/RPATH/interpreter must not point into the Nix store or the CI checkout.
#   check-glibc-baseline.sh <binary> [baseline, default 2.34]
set -euo pipefail
bin="${1:?usage: check-glibc-baseline.sh <binary> [baseline]}"
baseline="${2:-2.34}"

interp=$(readelf -l "$bin" | sed -n 's/.*Requesting program interpreter: \(.*\)]/\1/p')
dyn=$(readelf -d "$bin" 2>/dev/null | grep -c '(NEEDED)' || true)
if [[ -z "$interp" && "$dyn" == 0 ]]; then
  echo "$bin: statically linked (no interpreter, no shared libraries)"
  exit 0
fi

status=0
max=$(objdump -T "$bin" | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)+' | sed 's/^GLIBC_//' | sort -Vu | tail -n 1)
if [[ -n "$max" ]]; then
  newest=$(printf '%s\n%s\n' "$max" "$baseline" | sort -V | tail -n 1)
  if [[ "$newest" != "$baseline" ]]; then
    echo "$bin needs GLIBC_$max, newer than the baseline GLIBC_$baseline" >&2
    status=1
  else
    echo "$bin: newest glibc symbol GLIBC_$max (baseline GLIBC_$baseline)"
  fi
fi
paths=$(readelf -d "$bin" | grep -E '\((RUNPATH|RPATH)\)' || true)
if grep -qE '/nix/store|/home/runner|/github/workspace' <<<"$paths $interp"; then
  echo "$bin has a build-machine RUNPATH/RPATH/interpreter:" >&2
  echo "$paths $interp" >&2
  status=1
fi
exit "$status"
