#!/bin/sh
# One mail for requests the habitat was denied since the last run.
# hermetarium logs rewrites the I/O log first. A run with nothing new
# sends nothing. Several denials are one mail, not one mail each.
# A CONNECT the client closed during the TLS handshake is listed too,
# under the host name from that log line.
# A denied DNS name is mailed only while logs still has it (dns.jsonl
# and dns.jsonl.1). An older lookup is omitted. The mail names
# destinations and counts. It does not include a DNS answer, a path, or a key.
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

if [ -z "${HERMETARIUM_ID:-}" ]; then
  die "HERMETARIUM_ID is not set"
fi
case "$HERMETARIUM_ID" in
  *[!A-Za-z0-9._-]*) die "HERMETARIUM_ID is not a habitat id" ;;
esac
case "$HERMETARIUM_ID" in
  [A-Za-z0-9]*) ;;
  *) die "HERMETARIUM_ID is not a habitat id" ;;
esac
if [ "${#HERMETARIUM_ID}" -gt 63 ]; then
  die "HERMETARIUM_ID is not a habitat id"
fi

here=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
root=$(data_root)
inst=$root/var/$HERMETARIUM_ID
if [ ! -d "$inst" ]; then
  die "no habitat directory $inst"
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
if ! command -v awk >/dev/null 2>&1; then
  die "awk is not on PATH"
fi

# Fail before reading the log so a broken mail setup does not move the cursor.
"$here/mail.sh" --check

# logs prints every record. Paths and queries stay out of the timer journal.
"$bin" logs "$HERMETARIUM_ID" >/dev/null || die "hermetarium logs failed"
if [ ! -f "$inst/io.jsonl" ]; then
  die "hermetarium logs did not write $inst/io.jsonl"
fi

state=$inst/digest.state
in_state=$(mktemp "$inst/digest.in.XXXXXX")
newstate=$(mktemp "$inst/digest.new.XXXXXX")
events=$(mktemp "$inst/digest.events.XXXXXX")
denyfile=$(mktemp "$inst/digest.deny.XXXXXX")
handfile=$(mktemp "$inst/digest.hand.XXXXXX")
body=$(mktemp "$inst/digest.body.XXXXXX")
trap 'rm -f "$in_state" "$newstate" "$events" "$denyfile" "$handfile" "$body"' EXIT

if [ -f "$state" ]; then
  read -r ver < "$state" || die "digest state is empty"
  if [ "$ver" != "v1" ]; then
    die "digest state is not v1"
  fi
  cp "$state" "$in_state"
else
  printf 'v1\n' > "$in_state"
fi

# io.jsonl is rewritten on every logs run, so a byte offset is not stable.
# Remember how many identical reportable lines were already mailed.
awk -v stateout="$newstate" '
function skip_ws(s, i,    c) {
  while (i <= length(s)) {
    c = substr(s, i, 1)
    if (c != " " && c != "\t" && c != "\r" && c != "\n") return i
    i++
  }
  return i
}
function parse_string(s, i,    c) {
  pval = ""
  i++
  while (i <= length(s)) {
    c = substr(s, i, 1)
    if (c == "\\") {
      i++
      if (i > length(s)) break
      c = substr(s, i, 1)
      if (c == "u") {
        i += 4
      } else if (c == "n") {
        pval = pval "\n"
      } else if (c == "t") {
        pval = pval "\t"
      } else {
        pval = pval c
      }
      i++
      continue
    }
    if (c == "\"") {
      pnext = i + 1
      return 1
    }
    pval = pval c
    i++
  }
  pnext = i
  return 0
}
function parse_literal(s, i,    start, c) {
  start = i
  while (i <= length(s)) {
    c = substr(s, i, 1)
    if (c == "," || c == "}" || c == "]" || c == " " || c == "\t" || c == "\r" || c == "\n") break
    i++
  }
  pval = substr(s, start, i - start)
  pnext = i
  return 1
}
function time_key(s,    z, body, dot, frac) {
  z = index(s, "Z")
  if (z < 2) return s
  body = substr(s, 1, z - 1)
  dot = index(body, ".")
  if (dot == 0) return body ".000000000"
  frac = substr(body, dot + 1)
  while (length(frac) < 9) frac = frac "0"
  return substr(body, 1, dot) frac
}
function load_obj(s,    i, c, key) {
  delete f
  i = skip_ws(s, 1)
  if (substr(s, i, 1) != "{") return 0
  i++
  while (i <= length(s)) {
    i = skip_ws(s, i)
    if (i > length(s)) return 0
    c = substr(s, i, 1)
    if (c == "}") return 1
    if (c != "\"") return 0
    if (!parse_string(s, i)) return 0
    key = pval
    i = skip_ws(s, pnext)
    if (substr(s, i, 1) != ":") return 0
    i = skip_ws(s, i + 1)
    if (i > length(s)) return 0
    c = substr(s, i, 1)
    if (c == "\"") {
      if (!parse_string(s, i)) return 0
      f[key] = pval
      i = pnext
    } else if (c == "{" || c == "[") {
      return 0
    } else {
      if (!parse_literal(s, i)) return 0
      f[key] = pval
      i = pnext
    }
    i = skip_ws(s, i)
    if (i > length(s)) return 0
    c = substr(s, i, 1)
    if (c == ",") { i++; continue }
    if (c == "}") return 1
    return 0
  }
  return 0
}
FNR == NR {
  if ($0 == "v1" || $0 == "") next
  n = split($0, a, " ")
  if (n == 2) prev[a[1]] = a[2] + 0
  next
}
{
  if (!load_obj($0)) next
  if (f["direction"] != "out") next
  dest = f["destination"]
  if (dest == "" || dest ~ /[[:space:]|]/) next
  method = f["method"]
  if (method ~ /[[:space:]|]/) next
  time = f["time"]
  if (time == "" || time ~ /[[:space:]|]/) next
  proto = f["protocol"]
  if (proto ~ /[[:space:]|]/) proto = ""
  deny = (f["allowed"] == "false")
  hand = (f["aborted"] == "true" && method == "CONNECT")
  if (deny) hand = 0
  if (!deny && !hand) next
  sig = time "|" f["direction"] "|" proto "|" dest "|" method "|" f["allowed"] "|" f["aborted"]
  ord[sig]++
  if (hand) kind[sig] = "HAND"
  else kind[sig] = "DENY"
  sdest[sig] = dest
  stime[sig] = time
}
END {
  print "v1" > stateout
  min = ""
  max = ""
  for (sig in ord) {
    base = 0
    if (sig in prev) base = prev[sig] + 0
    extra = ord[sig] - base
    if (extra > 0) {
      key = kind[sig] "\t" sdest[sig]
      add[key] += extra
      t = stime[sig]
      # RFC3339Nano drops trailing zeros, so "...01Z" > "...01.1Z" as text.
      tk = time_key(t)
      if (min == "" || tk < mink) {
        min = t
        mink = tk
      }
      if (max == "" || tk > maxk) {
        max = t
        maxk = tk
      }
    }
    print sig " " ord[sig] > stateout
  }
  if (min != "") {
    printf "SPAN\t%s\t%s\n", min, max
    for (key in add) {
      n = split(key, k, "\t")
      if (n == 2) printf "%s\t%s\t%d\n", k[1], k[2], add[key]
    }
  }
}
' "$in_state" "$inst/io.jsonl" > "$events"

if [ ! -s "$events" ]; then
  mv "$newstate" "$state"
  exit 0
fi

: > "$denyfile"
: > "$handfile"
total=0
tab=$(printf '\t')
while IFS="$tab" read -r kind a b; do
  case "$kind" in
    SPAN)
      span_from=$a
      span_to=$b
      ;;
    DENY)
      printf '%s %s\n' "$a" "$b" >> "$denyfile"
      total=$((total + b))
      ;;
    HAND)
      printf '%s %s\n' "$a" "$b" >> "$handfile"
      total=$((total + b))
      ;;
    "")
      ;;
    *)
      die "digest event is not a denial"
      ;;
  esac
done < "$events"
if [ -z "${span_from:-}" ] || [ "$total" -lt 1 ]; then
  die "digest event has no span"
fi

{
  printf 'Habitat: %s\n' "$HERMETARIUM_ID"
  printf 'Span: %s to %s\n' "$span_from" "$span_to"
  printf '\n'
  if [ -s "$denyfile" ]; then
    printf 'Denied:\n'
    LC_ALL=C sort "$denyfile"
    printf '\n'
  fi
  if [ -s "$handfile" ]; then
    printf 'Closed during the TLS handshake:\n'
    LC_ALL=C sort "$handfile"
    printf '\n'
  fi
} > "$body"

# State moves only after the mail is accepted, so a failed send is retried.
MAIL_SUBJECT="hermetarium $HERMETARIUM_ID: denied requests" \
  "$here/mail.sh" < "$body" || die "mail failed"
mv "$newstate" "$state"
