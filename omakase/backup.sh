#!/bin/sh
# Back up one running habitat with restic: the root file, the I/O log,
# Squid's access log, and the Git Warden journal. squid.conf and the gate
# private key are not in that set.
#
# A restic read of the root file takes longer than one write. Pause the
# Firecracker container for the read so the file is one moment. Do not
# restart it: a restart kills Firecracker. The pause is bounded. INT and
# TERM unpause without waiting out that bound. SIGKILL skips the trap;
# then run docker unpause htm-<id>-fc. A habitat from before the rename
# is htm-fc-<id>; the script pauses whichever of those is running.
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

# 0 when $1 is $2 or a path inside $2. "/" contains every path.
# case "/"/* is "//*" and matches nothing, so the root is its own test.
path_under() {
  if [ "$2" = / ] || [ "$1" = "$2" ]; then
    return 0
  fi
  case "$1" in
    "$2"/*) return 0 ;;
  esac
  return 1
}

# 0 when directory $1 holds a regular file with one of the device:inode
# values in the remaining arguments. Do not follow symlinks or read bytes.
# No -xdev: restic backup crosses filesystems, so a nested mount is archived.
# A match counts only after find itself succeeds. A failed walk is that
# error, not a gate-file refusal, and it still stops before restic backup.
dir_holds_inode() {
  dir=$1
  shift
  list=$(find -P "$dir" -type f -printf '%D:%i\n') || die "could not walk $dir"
  for ino in "$@"; do
    case "
$list
" in
      *"
$ino
"*) return 0 ;;
    esac
  done
  return 1
}

# True when backing up $1 would store a gate file or a gate private key.
# Every habitat under this root counts. A hard link counts even from another
# directory. "/" and any ancestor of var/ contain them. The root file under
# var/<id>/ is not one of those two secrets.
is_secret() {
  target=$(realpath "$1")
  var_dir=$(realpath "$root/var")
  if path_under "$var_dir" "$target"; then
    return 0
  fi
  # Device and inode are digits, so splitting this list is the comparison.
  inos=
  for secret in "$root"/var/*/squid.conf "$root"/var/*/mitm/ca.key; do
    [ -e "$secret" ] || continue
    # -ef follows the inode, so a hard link is the same file as the path.
    if [ "$target" -ef "$secret" ]; then
      return 0
    fi
    if [ -d "$target" ] && path_under "$(realpath "$secret")" "$target"; then
      return 0
    fi
    ino=$(stat -c '%d:%i' "$secret") || return 0
    inos="$inos $ino"
  done
  if [ -d "$target" ] && [ -n "$inos" ] && dir_holds_inode "$target" $inos; then
    return 0
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
if [ -z "${GIT_WARDEN_JOURNAL:-}" ]; then
  die "GIT_WARDEN_JOURNAL is not set"
fi
if [ ! -e "$GIT_WARDEN_JOURNAL" ]; then
  die "GIT_WARDEN_JOURNAL does not exist"
fi
# Leading zeros would be octal in $(( )). Reject a value that cannot be a
# short bound before any $(( )) or [ -gt ]: bash wraps a huge integer and
# dash errors. 6 hours is the longest pause.
pause_seconds=${HERMETARIUM_PAUSE_SECONDS:-900}
case "$pause_seconds" in
  *[!0-9]*) die "HERMETARIUM_PAUSE_SECONDS must be a number of seconds" ;;
  ????????????????????*) die "HERMETARIUM_PAUSE_SECONDS is too large" ;;
esac
while [ -n "$pause_seconds" ] && [ "${pause_seconds#0}" != "$pause_seconds" ]; do
  pause_seconds=${pause_seconds#0}
done
[ -n "$pause_seconds" ] || pause_seconds=0
case "$pause_seconds" in
  ??????????*) die "HERMETARIUM_PAUSE_SECONDS is too large" ;;
esac
if [ "$pause_seconds" -eq 0 ]; then
  die "HERMETARIUM_PAUSE_SECONDS must be at least 1"
fi
if [ "$pause_seconds" -gt 21600 ]; then
  die "HERMETARIUM_PAUSE_SECONDS is too large"
fi
if ! command -v restic >/dev/null 2>&1; then
  die "restic is not on PATH"
fi
if ! command -v timeout >/dev/null 2>&1; then
  die "timeout is not on PATH"
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

journal=$(realpath "$GIT_WARDEN_JOURNAL")
rootfile=$inst/rootfs.ext4
if [ ! -f "$rootfile" ]; then
  die "missing $rootfile"
fi
if is_secret "$journal" || is_secret "$rootfile"; then
  die "refusing to back up the gate file or the gate private key"
fi

# The Firecracker container name is only the habitat id. A different HERMETARIUM_ROOT can
# hold var/<id>/squid.conf and would otherwise be the secret check's root.
# A habitat from before the rename is htm-fc-<id>.
fc=htm-$HERMETARIUM_ID-fc
if ! docker inspect "$fc" >/dev/null 2>&1; then
  if docker inspect "htm-fc-$HERMETARIUM_ID" >/dev/null 2>&1; then
    fc=htm-fc-$HERMETARIUM_ID
  fi
fi
if ! logdir=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/log"}}{{.Source}}{{end}}{{end}}' "$fc"); then
  die "cannot inspect $fc"
fi
if [ -z "$logdir" ] || [ ! -d "$logdir" ] || [ "$(realpath "$logdir")" != "$(realpath "$inst")" ]; then
  die "$fc is not habitat $HERMETARIUM_ID under $root"
fi

# Rewrite io.jsonl from the access log before the pause, so the snapshot
# matches the access log.
"$bin" logs "$HERMETARIUM_ID" >/dev/null
for f in "$inst/io.jsonl" "$inst/access.log" "$rootfile"; do
  if [ ! -f "$f" ]; then
    die "missing $f"
  fi
done

# paused stays 1 until this run unpauses. SIGKILL skips this trap.
# backup_pid is timeout. timeout calls setpgid, so it is not in this
# shell's process group. A foreground wait would defer INT and TERM
# until the pause bound, and a later SIGKILL would leave the container
# paused.
paused=0
backup_pid=0
cleanup() {
  rc=${1:-$?}
  trap - EXIT INT TERM
  if [ "$backup_pid" != 0 ]; then
    # Stop restic now. Do not wait out the pause bound. The group kill
    # is timeout's own process group; this shell is not in it.
    kill -TERM -"$backup_pid" 2>/dev/null || true
    kill -TERM "$backup_pid" 2>/dev/null || true
    backup_pid=0
  fi
  if [ "$paused" = 1 ]; then
    if docker unpause "$fc"; then
      paused=0
    else
      echo "Firecracker container stayed paused; docker unpause $fc" >&2
      if [ "$rc" -eq 0 ]; then
        rc=1
      fi
    fi
  fi
  exit "$rc"
}
trap 'cleanup $?' EXIT
trap 'cleanup 130' INT
trap 'cleanup 143' TERM

# Before docker pause returns. A signal during pause runs this trap
# with the flag already set, including when the pause has succeeded.
paused=1
docker pause "$fc"

# -k so a restic that ignores TERM cannot hold the pause past the bound.
# Background timeout so this trap can run without waiting for it.
timeout -k 10 "$pause_seconds" restic backup --json --tag hermetarium \
  "$rootfile" \
  "$inst/io.jsonl" \
  "$inst/access.log" \
  "$journal" &
backup_pid=$!
backup_status=0
wait "$backup_pid" || backup_status=$?
backup_pid=0
if [ "$backup_status" -ne 0 ]; then
  exit "$backup_status"
fi

docker unpause "$fc"
paused=0
