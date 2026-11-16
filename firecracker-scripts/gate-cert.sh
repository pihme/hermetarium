#!/bin/sh
# Install the gate CA into the trust store. fc-init sources this so the
# exports reach the inhabitant. The weak wall executes it after mounting
# the certificate at the same path. Do not copy these steps anywhere else.
install_gate_cert() {
  dest=/etc/ssl/hermetarium-mitm.crt
  mkdir -p /usr/local/share/ca-certificates /etc/ssl || return 1
  if [ ! -f "$dest" ]; then
    url=${GATE_CA_URL:-}
    fetched=0
    if [ -n "$url" ] && [ -x /bin/busybox ]; then
      if /bin/busybox timeout 3 /bin/busybox wget -q -T 2 -O "$dest" "$url"; then
        fetched=1
      fi
    fi
    if [ "$fetched" -eq 0 ] && [ -n "$url" ] && command -v timeout >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
      if timeout 3 curl -fsS --connect-timeout 1 --max-time 2 -o "$dest" "$url"; then
        fetched=1
      fi
    fi
    if [ "$fetched" -eq 0 ]; then
      echo "fc-init: could not fetch the gate CA"
      return 1
    fi
  fi
  cp "$dest" /usr/local/share/ca-certificates/hermetarium-mitm.crt || return 1
  if command -v update-ca-certificates >/dev/null 2>&1; then
    update-ca-certificates >/dev/null 2>&1 || return 1
  elif [ -f /etc/ssl/certs/ca-certificates.crt ]; then
    cat "$dest" >> /etc/ssl/certs/ca-certificates.crt || return 1
  fi
  export SSL_CERT_FILE="$dest"
  export NODE_EXTRA_CA_CERTS="$dest"
  export REQUESTS_CA_BUNDLE="$dest"
  export CURL_CA_BUNDLE="$dest"
  export GIT_SSL_CAINFO="$dest"
  export PIP_CERT="$dest"
  return 0
}
install_gate_cert
