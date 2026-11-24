# The gate CA, so tools that do not read the system store still trust the gate.
# Sources into every login shell (via /etc/profile). The gate writes the file at
# /etc/ssl/hermetarium-mitm.crt when the habitat boots; the day-one guide
# (/etc/hermetarium/day-one.md) names that path and these variables.
export SSL_CERT_FILE=/etc/ssl/hermetarium-mitm.crt
export NODE_EXTRA_CA_CERTS=/etc/ssl/hermetarium-mitm.crt
export REQUESTS_CA_BUNDLE=/etc/ssl/hermetarium-mitm.crt
export CURL_CA_BUNDLE=/etc/ssl/hermetarium-mitm.crt
export GIT_SSL_CAINFO=/etc/ssl/hermetarium-mitm.crt
export PIP_CERT=/etc/ssl/hermetarium-mitm.crt
