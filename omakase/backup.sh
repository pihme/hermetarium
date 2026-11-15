#!/bin/sh
# Back up one running habitat with restic: I/O log, Squid access log,
# Git Warden journal, and the data disk. squid.conf and the gate private
# key are not in that set.
#
# A restic read of the disk file takes longer than one write. Freeze /data
# in the guest for the whole read so the image is one moment.
set -eu

die() {
  echo "$1" >&2
  exit 1
}

data_root() {
  if [ -n "${HERMETARIUM_ROOT:-}" ]; then
    printf '%s\n' "$HERMETARIUM_ROOT"
    return
  fi
  d=$(pwd)
  while [ "$d" != "/" ]; do
    if [ -f "$d/go.mod" ] && [ -d "$d/supervisor" ]; then
      printf '%s\n' "$d"
      return
    fi
    d=$(dirname "$d")
  done
  if [ -n "${XDG_DATA_HOME:-}" ]; then
    printf '%s\n' "$XDG_DATA_HOME/hermetarium"
  else
    printf '%s\n' "$HOME/.local/share/hermetarium"
  fi
}

# True when backing up $1 would store the gate file or the gate private key.
is_secret() {
  target=$(realpath "$1")
  squid=$(realpath "$inst/squid.conf")
  if [ "$target" = "$squid" ]; then
    return 0
  fi
  if [ -f "$inst/mitm/ca.key" ]; then
    key=$(realpath "$inst/mitm/ca.key")
    if [ "$target" = "$key" ]; then
      return 0
    fi
  fi
  if [ -d "$target" ]; then
    case "$squid" in
      "$target"/*) return 0 ;;
    esac
    if [ -n "${key:-}" ]; then
      case "$key" in
        "$target"/*) return 0 ;;
      esac
    fi
  fi
  return 1
}

if [ -z "${RESTIC_REPOSITORY:-}" ]; then
  die "RESTIC_REPOSITORY is not set"
fi
if [ -z "${RESTIC_PASSWORD_FILE:-}" ] && [ -z "${RESTIC_PASSWORD:-}" ]; then
  die "set RESTIC_PASSWORD_FILE or RESTIC_PASSWORD"
fi
if [ -n "${RESTIC_PASSWORD_FILE:-}" ] && [ ! -f "$RESTIC_PASSWORD_FILE" ]; then
  die "RESTIC_PASSWORD_FILE does not exist"
fi
if [ -z "${HERMETARIUM_ID:-}" ]; then
  die "HERMETARIUM_ID is not set"
fi
case "$HERMETARIUM_ID" in
  *[!A-Za-z0-9._-]*) die "HERMETARIUM_ID is not a habitat id" ;;
esac
if [ -z "${HERMETARIUM_DATA_DISK:-}" ]; then
  die "HERMETARIUM_DATA_DISK is not set"
fi
if [ -z "${GIT_WARDEN_JOURNAL:-}" ]; then
  die "GIT_WARDEN_JOURNAL is not set"
fi
if [ ! -e "$GIT_WARDEN_JOURNAL" ]; then
  die "GIT_WARDEN_JOURNAL does not exist"
fi
if ! command -v restic >/dev/null 2>&1; then
  die "restic is not on PATH"
fi
if ! command -v docker >/dev/null 2>&1; then
  die "docker is not on PATH"
fi

root=$(data_root)
inst=$root/var/$HERMETARIUM_ID
if [ ! -d "$inst" ]; then
  die "no habitat directory $inst"
fi
if [ ! -f "$inst/squid.conf" ]; then
  die "habitat $HERMETARIUM_ID has no gate file"
fi

if [ -n "${HERMETARIUM:-}" ]; then
  bin=$HERMETARIUM
else
  bin=$(command -v hermetarium || true)
  if [ -z "$bin" ] && [ -x "$root/bin/hermetarium" ]; then
    bin=$root/bin/hermetarium
  fi
fi
if [ -z "${bin:-}" ] || [ ! -x "$bin" ]; then
  die "hermetarium not found (set HERMETARIUM or PATH)"
fi

if ! restic cat config >/dev/null 2>&1; then
  die "restic repository is not initialized (restic init)"
fi

disk=$(realpath "$HERMETARIUM_DATA_DISK")
if [ ! -f "$disk" ]; then
  die "HERMETARIUM_DATA_DISK is not a file"
fi
journal=$(realpath "$GIT_WARDEN_JOURNAL")
if is_secret "$disk" || is_secret "$journal"; then
  die "refusing to back up the gate file or the gate private key"
fi

if ! mounted=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/opt/data.ext4"}}{{.Source}}{{end}}{{end}}' "htm-fc-$HERMETARIUM_ID"); then
  die "cannot inspect htm-fc-$HERMETARIUM_ID"
fi
if [ -z "$mounted" ]; then
  die "habitat $HERMETARIUM_ID has no data disk mounted"
fi
if [ "$(realpath "$mounted")" != "$disk" ]; then
  die "HERMETARIUM_DATA_DISK is not the disk mounted in habitat $HERMETARIUM_ID"
fi

# Rewrite io.jsonl from the access log before the snapshot.
"$bin" logs "$HERMETARIUM_ID" >/dev/null
for f in "$inst/io.jsonl" "$inst/access.log"; do
  if [ ! -f "$f" ]; then
    die "missing $f"
  fi
done

# attach closes stdin with SIGHUP. That can kill fsfreeze before /data is frozen.
hold_dir=$(mktemp -d)
frozen=0

# frozen stays 1 until fsfreeze -u returns zero. A failed thaw is retried.
thaw() {
  attempt=1
  while [ "$attempt" -le 5 ]; do
    if "$bin" attach "$HERMETARIUM_ID" -- fsfreeze -u /data <&3 >&2; then
      frozen=0
      return 0
    fi
    if [ "$attempt" -lt 5 ]; then
      sleep 0.2
    fi
    attempt=$((attempt + 1))
  done
  return 1
}

cleanup() {
  rc=${1:-$?}
  trap - EXIT INT TERM
  if [ "$frozen" = 1 ]; then
    thaw || echo "data disk stayed frozen; hermetarium attach $HERMETARIUM_ID -- fsfreeze -u /data" >&2
  fi
  exec 3>&- 2>/dev/null || true
  rm -rf "$hold_dir"
  exit "$rc"
}
trap 'cleanup $?' EXIT
trap 'cleanup 130' INT
trap 'cleanup 143' TERM

hold_fifo=$hold_dir/stdin
mkfifo "$hold_fifo"
exec 3<>"$hold_fifo"

# The guest can freeze /data and still have attach return an error.
frozen=1
if ! "$bin" attach "$HERMETARIUM_ID" -- sh -c 'sync; command -v fsfreeze >/dev/null 2>&1 || { echo "fsfreeze is not installed in this habitat" >&2; exit 1; }; fsfreeze -f /data' <&3 >&2; then
  die "could not freeze /data (the habitat image needs fsfreeze from util-linux)"
fi

restic backup --json --tag hermetarium \
  "$inst/io.jsonl" \
  "$inst/access.log" \
  "$journal" \
  "$disk"

if ! thaw; then
  die "data disk stayed frozen; hermetarium attach $HERMETARIUM_ID -- fsfreeze -u /data"
fi
