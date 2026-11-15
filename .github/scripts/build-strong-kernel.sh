#!/usr/bin/env bash
# Build the x86_64 6.1 CI vmlinux the way Firecracker commit
# 778a26c0594c80025ed798088a66c2511ba618de does for `resources/rebuild.sh kernels 6.1`.
# CI flavour only: not the debug flavour, not 5.10, not 6.18, not a rootfs.
set -euo pipefail

out=${1:?usage: build-strong-kernel.sh OUTPUT}

fc_commit=778a26c0594c80025ed798088a66c2511ba618de
# get_tag follows the newest microvm-kernel 6.1 tag. This one is the newest
# whose Makefile release normalizes to the vmlinux-6.1.188 the strong wall boots.
al_tag=microvm-kernel-6.1.188-56.393.amzn2023
al_commit=8785eb28815fdac7e6743bb6fad34ef67dc24b26

# Published banner user and host. Do not set KBUILD_BUILD_TIMESTAMP or
# KBUILD_BUILD_VERSION: the weak uts copy in version.o stays "# SMP PREEMPT_DYNAMIC"
# with an empty timestamp, and only init/version-timestamp.o gets "#1" and the date.
export KBUILD_BUILD_USER=root
export KBUILD_BUILD_HOST=6e1310c47f92
export ARCH=x86_64
# devctr sets this. A different locale can change generated output.
export LC_ALL=C.UTF-8
# setlocalversion adds the '+' in 6.1.188+ only when this is unset.
unset LOCALVERSION
unset SOURCE_DATE_EPOCH || true

if [[ "$(uname -m)" != x86_64 ]]; then
	echo "this kernel is x86_64 only" >&2
	exit 1
fi

gcc_line=$(gcc --version)
gcc_line=${gcc_line%%$'\n'*}
if [[ "$gcc_line" != "gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0" ]]; then
	echo "need gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0, have ${gcc_line}" >&2
	exit 1
fi
ld_line=$(ld -v)
if [[ "$ld_line" != "GNU ld (GNU Binutils for Ubuntu) 2.42" ]]; then
	echo "need GNU ld (GNU Binutils for Ubuntu) 2.42, have ${ld_line}" >&2
	exit 1
fi
# scripts/pahole-version.sh turns "v1.25" into 125, which the published config records.
pahole_num=$(pahole --version | sed -nE 's/^v([0-9]+)\.([0-9]+)$/\1\2/p')
if [[ "$pahole_num" != 125 ]]; then
	echo "need pahole 1.25 (CONFIG_PAHOLE_VERSION=125), have $(pahole --version)" >&2
	exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# link-vmlinux.sh records `LC_ALL=C date` in version-timestamp.o only.
# The weak banner must not see this string.
mkdir -p "${work}/bin"
cat > "${work}/bin/date" <<'EOF'
#!/bin/sh
if [ "$#" -eq 0 ]; then
	printf '%s\n' 'Wed Nov 11 03:21:09 UTC 2026'
	exit 0
fi
exec /usr/bin/date "$@"
EOF
chmod +x "${work}/bin/date"
export PATH="${work}/bin:${PATH}"

src="${work}/linux"
GIT_TERMINAL_PROMPT=0 git clone --depth 1 --branch "$al_tag" https://github.com/amazonlinux/linux "$src"
got=$(git -C "$src" rev-parse HEAD)
if [[ "$got" != "$al_commit" ]]; then
	echo "tag ${al_tag} is ${got}, want ${al_commit}" >&2
	exit 1
fi

base="https://raw.githubusercontent.com/firecracker-microvm/firecracker/${fc_commit}/resources"
configs="${work}/configs"
mkdir -p "$configs"
curl -fsSL -o "${configs}/microvm-kernel-ci-x86_64-6.1.config" \
	"${base}/guest_configs/microvm-kernel-ci-x86_64-6.1.config"
curl -fsSL -o "${configs}/ci.config" "${base}/guest_configs/ci.config"
curl -fsSL -o "${configs}/nvme.config" "${base}/guest_configs/nvme.config"
curl -fsSL -o "${configs}/efi.config" "${base}/guest_configs/efi.config"
curl -fsSL -o "${work}/dmb.patch" \
	"${base}/patches/dmb/6.1/0001-virtio-pci-add-DMB-support.patch"

# A dirty tree is what makes setlocalversion emit the '+' in 6.1.188+.
git -C "$src" apply "${work}/dmb.patch"
if [[ -z "$(git -C "$src" status --porcelain)" ]]; then
	echo "DMB patch did not change the tree; the published release is 6.1.188+" >&2
	exit 1
fi

# build_al_kernel concatenates these for the CI flavour. Later lines win.
awk 1 \
	"${configs}/microvm-kernel-ci-x86_64-6.1.config" \
	"${configs}/ci.config" \
	"${configs}/nvme.config" \
	"${configs}/efi.config" \
	> "${src}/.config"

make -C "$src" olddefconfig
make -C "$src" -j"$(nproc)" vmlinux

release=$(tr -d '\n' < "${src}/include/config/kernel.release")
# Same sed as rebuild.sh: 6.1.188+ becomes the filename vmlinux-6.1.188.
normalized=$(printf '%s\n' "$release" | sed -E 's/(.*[[:digit:]]).*/\1/')
if [[ "$release" != "6.1.188+" || "$normalized" != 6.1.188 ]]; then
	echo "kernel release ${release} normalizes to ${normalized}, want 6.1.188+ -> 6.1.188" >&2
	exit 1
fi

mkdir -p "$(dirname "$out")"
cp -v "${src}/vmlinux" "$out"
