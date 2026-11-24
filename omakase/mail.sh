#!/bin/sh
# Send one message on stdin with the wall mail settings.
# The denied-request digest and the Push Guard's mail for a red push use
# this program and the same settings file, so the host has one mail
# configuration. A password stays in a file that MAIL_COMMAND reads.
# It is not a value in the settings file.
set -eu

die() {
  echo "$1" >&2
  exit 1
}

mail_env_path() {
  if [ -n "${HERMETARIUM_MAIL_ENV:-}" ]; then
    printf '%s\n' "$HERMETARIUM_MAIL_ENV"
    return
  fi
  if [ -n "${XDG_CONFIG_HOME:-}" ]; then
    printf '%s\n' "$XDG_CONFIG_HOME/hermetarium/mail.env"
  else
    printf '%s\n' "$HOME/.config/hermetarium/mail.env"
  fi
}

# One line, no control characters, so a value cannot add a mail header.
one_line() {
  if printf '%s' "$2" | grep -q '[[:cntrl:]]'; then
    die "$1 is not one line"
  fi
}

read_mail_env() {
  file=$1
  [ -f "$file" ] || die "mail settings not found: $file"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      '' | '#'*) continue ;;
    esac
    key=${line%%=*}
    val=${line#*=}
    case "$key" in
      MAIL_TO | MAIL_FROM | MAIL_COMMAND) ;;
      *) die "unknown mail setting $key" ;;
    esac
    case "$val" in
      \"*\") val=${val#\"}; val=${val%\"} ;;
      \'*\') val=${val#\'}; val=${val%\'} ;;
    esac
    one_line "$key" "$val"
    case "$key" in
      MAIL_TO) MAIL_TO=$val ;;
      MAIL_FROM) MAIL_FROM=$val ;;
      MAIL_COMMAND) MAIL_COMMAND=$val ;;
    esac
  done < "$file"
}

env_file=$(mail_env_path)
read_mail_env "$env_file"
[ -n "${MAIL_TO:-}" ] || die "MAIL_TO is not set"
[ -n "${MAIL_FROM:-}" ] || die "MAIL_FROM is not set"
case "$MAIL_TO" in
  *" "* | *"	"*) die "MAIL_TO is not one address" ;;
esac
case "$MAIL_FROM" in
  *" "* | *"	"*) die "MAIL_FROM is not one address" ;;
esac
if [ -n "${MAIL_COMMAND:-}" ]; then
  case "$MAIL_COMMAND" in
    /*) ;;
    *) die "MAIL_COMMAND must be an absolute path" ;;
  esac
  [ -x "$MAIL_COMMAND" ] || die "MAIL_COMMAND is not executable"
fi

# digest.sh asks first, so a timer with no new denials still fails closed
# when the settings are missing.
if [ "${1:-}" = "--check" ]; then
  exit 0
fi

subject=${MAIL_SUBJECT:-hermetarium}
one_line MAIL_SUBJECT "$subject"

msg=$(mktemp)
trap 'rm -f "$msg"' EXIT
{
  printf 'To: %s\n' "$MAIL_TO"
  printf 'From: %s\n' "$MAIL_FROM"
  printf 'Subject: %s\n' "$subject"
  printf 'Date: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '\n'
  cat
} > "$msg"

if [ -n "${MAIL_COMMAND:-}" ]; then
  "$MAIL_COMMAND" < "$msg" || die "mail failed"
  exit 0
fi
if [ ! -x /usr/sbin/sendmail ]; then
  die "sendmail is not installed (set MAIL_COMMAND)"
fi
/usr/sbin/sendmail -oi -t < "$msg" || die "mail failed"
