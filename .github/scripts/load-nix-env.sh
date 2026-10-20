#!/usr/bin/env bash
# Put one Nix dev shell on the PATH of later steps in this GitHub Actions job.
# Locally, enter the shell directly:
#   nix develop ./nix/reference-env -c <cmd>
set -euo pipefail
flake="${1:?usage: load-nix-env.sh ./nix/<shell>}"
if [[ -z "${GITHUB_PATH:-}" || -z "${GITHUB_ENV:-}" ]]; then
  echo "load-nix-env.sh is for GitHub Actions (GITHUB_PATH and GITHUB_ENV)." >&2
  exit 2
fi
envfile="${RUNNER_TEMP:-/tmp}/nix-env"
# Not a login shell: bash -l would reset PATH and drop the dev shell.
nix develop "$flake" -c bash -c 'printenv' > "$envfile"
# GITHUB_PATH prepends each line, so the last line becomes the front of PATH.
# Reverse the dev-shell PATH to keep its own order.
awk -F= '$1=="PATH" { print substr($0, index($0,"=")+1) }' "$envfile" | tr ':' '\n' | sed '/^$/d' | tac >> "$GITHUB_PATH"
vars=(
  PKG_CONFIG_PATH LD_LIBRARY_PATH LIBRARY_PATH
  C_INCLUDE_PATH CPLUS_INCLUDE_PATH
  NIX_CFLAGS_COMPILE NIX_LDFLAGS
  CC CXX AR NM RANLIB
  CHROME_PATH PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS
  PIP_PREFIX PYTHONPATH PIP_NO_BINARY
  LLVM_COV LLVM_PROFDATA
)
for var in "${vars[@]}"; do
  line=$(awk -F= -v v="$var" '$1==v { print substr($0, index($0,"=")+1); exit }' "$envfile" || true)
  if [[ -n "${line:-}" ]]; then
    printf '%s=%s\n' "$var" "$line" >> "$GITHUB_ENV"
  fi
done
# The cc wrapper applies NIX_LDFLAGS only when the role marker for this
# machine is present. The suffix is the config triple.
while IFS= read -r line; do
  [[ "$line" == *=* ]] || continue
  var=${line%%=*}
  case "$var" in
    NIX_*_WRAPPER_TARGET_HOST_*)
      printf '%s\n' "$line" >> "$GITHUB_ENV"
      ;;
  esac
done < "$envfile"
