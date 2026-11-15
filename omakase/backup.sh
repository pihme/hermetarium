#!/bin/sh
# Back up one running habitat with restic: I/O log, Squid access log,
# Git Warden journal, and the data disk. squid.conf and the gate private
# key are not in that set.
#
# A restic read of the disk file takes longer than one write. Freeze /data
# in the guest for the read so the image is one moment. The freeze is bounded:
# restic is stopped, and the guest thaws /data even if this process is killed.
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
# A walk that fails is treated as a match so the backup stops.
dir_holds_inode() {
  dir=$1
  shift
  list=$(find -P "$dir" -type f -printf '%D:%i\n') || return 0
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
# directory. "/" and any ancestor of var/ contain them.
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
if [ -z "${HERMETARIUM_DATA_DISK:-}" ]; then
  die "HERMETARIUM_DATA_DISK is not set"
fi
if [ -z "${GIT_WARDEN_JOURNAL:-}" ]; then
  die "GIT_WARDEN_JOURNAL is not set"
fi
if [ ! -e "$GIT_WARDEN_JOURNAL" ]; then
  die "GIT_WARDEN_JOURNAL does not exist"
fi
# Leading zeros would be octal in $(( )). Reject a value that cannot be a
# short bound before any $(( )) or [ -gt ]: bash wraps a huge integer and
# dash errors. 6 hours is the longest freeze.
freeze_seconds=${HERMETARIUM_FREEZE_SECONDS:-900}
case "$freeze_seconds" in
  *[!0-9]*) die "HERMETARIUM_FREEZE_SECONDS must be a number of seconds" ;;
  ????????????????????*) die "HERMETARIUM_FREEZE_SECONDS is too large" ;;
esac
while [ -n "$freeze_seconds" ] && [ "${freeze_seconds#0}" != "$freeze_seconds" ]; do
  freeze_seconds=${freeze_seconds#0}
done
[ -n "$freeze_seconds" ] || freeze_seconds=0
case "$freeze_seconds" in
  ??????????*) die "HERMETARIUM_FREEZE_SECONDS is too large" ;;
esac
if [ "$freeze_seconds" -eq 0 ]; then
  die "HERMETARIUM_FREEZE_SECONDS must be at least 1"
fi
if [ "$freeze_seconds" -gt 21600 ]; then
  die "HERMETARIUM_FREEZE_SECONDS is too large"
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
# The helper name is only the habitat id. A different HERMETARIUM_ROOT can
# hold var/<id>/squid.conf and would otherwise be the secret check's root.
if ! logdir=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/log"}}{{.Source}}{{end}}{{end}}' "htm-fc-$HERMETARIUM_ID"); then
  die "cannot inspect htm-fc-$HERMETARIUM_ID"
fi
if [ -z "$logdir" ] || [ ! -d "$logdir" ] || [ "$(realpath "$logdir")" != "$(realpath "$inst")" ]; then
  die "htm-fc-$HERMETARIUM_ID is not habitat $HERMETARIUM_ID under $root"
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
freeze_ok=0
thaw_token=

# Under one guest lock: if this run still owns the token, thaw and consume it.
# The sleeper re-reads the token under that same lock before fsfreeze -u.
# A token already consumed is success, so a backstop that thawed first is
# not reported as "stayed frozen", and a newer freeze is left alone.
release_script=$(cat <<'END'
flock -x /tmp/hermetarium-thaw.lock sh -c 'cur=$(cat /tmp/hermetarium-thaw.current 2>/dev/null || true); if [ "$cur" != "$1" ]; then exit 0; fi; fsfreeze -u /data || exit 1; printf "done\n" > /tmp/hermetarium-thaw.current' sh "$1"
END
)

# frozen stays 1 until this run's freeze is released. A failed thaw is retried.
thaw() {
  attempt=1
  while [ "$attempt" -le 5 ]; do
    if "$bin" attach "$HERMETARIUM_ID" -- sh -c "$release_script" sh "$thaw_token" <&3 >&2; then
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
    if [ "$freeze_ok" = 1 ]; then
      thaw || echo "data disk stayed frozen; hermetarium attach $HERMETARIUM_ID -- fsfreeze -u /data" >&2
    elif "$bin" attach "$HERMETARIUM_ID" -- sh -c "$release_script" sh "$thaw_token" <&3 >/dev/null 2>&1; then
      # The freeze command failed after the guest may already have frozen.
      # One thaw is enough. Do not claim the disk stayed frozen.
      frozen=0
    fi
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

# Past timeout -k 10. A wrapped add would sleep less than freeze_seconds
# and thaw while restic is still running. Abort before the freeze.
guest_seconds=$((freeze_seconds + 30))
if [ "$guest_seconds" -le "$freeze_seconds" ]; then
  die "HERMETARIUM_FREEZE_SECONDS is too large"
fi
thaw_token=$(date +%s)-$$

# ssh client errors and a missing fsfreeze happen before fsfreeze -f.
# A later failure may mean the guest froze /data and attach still failed.
ssh_never_ran() {
  # OpenSSH reports every failed TCP connect as
  # "ssh: connect to host <host> port <port>: <strerror>". That is before
  # fsfreeze -f, whatever the strerror is.
  grep -E -q \
    'ssh: connect to host |Permission denied|Could not resolve hostname|Host key verification failed|kex_exchange_identification|banner exchange|exec: "ssh"' \
    "$1"
}

# Armed only after fsfreeze -f returns, so a slow freeze cannot eat the cushion.
# The spliced quotes are one script: sleep, then re-read the token under the
# lock and thaw. setsid -f returns at once; the child survives a host SIGKILL.
freeze_script=$(cat <<'END'
sync
command -v fsfreeze >/dev/null 2>&1 || { echo "fsfreeze is not installed in this habitat" >&2; exit 1; }
command -v flock >/dev/null 2>&1 || { echo "flock is not installed in this habitat" >&2; exit 1; }
command -v setsid >/dev/null 2>&1 || { echo "setsid is not installed in this habitat" >&2; exit 1; }
token=$1
seconds=$2
mkdir -p /tmp
thaw_file=/tmp/hermetarium-thaw.current
lock=/tmp/hermetarium-thaw.lock
flock -x "$lock" sh -c 'printf "%s\n" "$1" > "$2"' sh "$token" "$thaw_file" || {
  echo "could not arm the thaw timer" >&2
  exit 1
}
fsfreeze -f /data || exit 1
setsid -f sh -c 'sleep "$1"; flock -x "$2" sh -c '"'"'cur=$(cat "$2" 2>/dev/null || true); if [ "$cur" = "$1" ]; then fsfreeze -u /data && printf "done\n" > "$2"; fi'"'"' sh "$3" "$4"' sh "$seconds" "$lock" "$token" "$thaw_file" </dev/null >/dev/null 2>&1 || {
  echo "could not arm the thaw timer" >&2
  exit 1
}
END
)

# The guest can freeze /data and still have attach return an error.
# Only a failure after fsfreeze may have run keeps frozen=1.
frozen=1
freeze_log=$hold_dir/freeze.err
if ! "$bin" attach "$HERMETARIUM_ID" -- sh -c "$freeze_script" sh "$thaw_token" "$guest_seconds" <&3 >"$freeze_log" 2>&1; then
  cat "$freeze_log" >&2
  if ssh_never_ran "$freeze_log"; then
    frozen=0
    if grep -q "Connection refused" "$freeze_log"; then
      die "could not freeze /data (the habitat image needs an SSH server at /usr/sbin/sshd)"
    fi
    die "could not freeze /data (attach did not run fsfreeze)"
  fi
  if grep -q "fsfreeze is not installed" "$freeze_log"; then
    frozen=0
    die "could not freeze /data (fsfreeze is not installed in this habitat)"
  fi
  die "could not freeze /data (the habitat image needs fsfreeze from util-linux)"
fi
freeze_ok=1

# -k so a restic that ignores TERM cannot hold the freeze. The guest timer
# started when the freeze returned and stays past this kill wait.
timeout -k 10 "$freeze_seconds" restic backup --json --tag hermetarium \
  "$inst/io.jsonl" \
  "$inst/access.log" \
  "$journal" \
  "$disk"

if ! thaw; then
  die "data disk stayed frozen; hermetarium attach $HERMETARIUM_ID -- fsfreeze -u /data"
fi
