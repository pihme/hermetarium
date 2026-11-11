package main

import (
	"encoding/hex"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// seedEntropy credits the hermetarium.seed=<hex> kernel argument to the
// guest's entropy pool. The Firecracker guest kernel (4.14) has no virtio-rng
// and does not trust the CPU, so getrandom() would otherwise block forever.
// No-op when the argument is absent (Docker wall, other hosts).
func seedEntropy() {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return
	}
	for _, f := range strings.Fields(string(cmdline)) {
		v, ok := strings.CutPrefix(f, "hermetarium.seed=")
		if !ok {
			continue
		}
		seed, err := hex.DecodeString(v)
		if err != nil || len(seed) == 0 {
			return
		}
		addEntropy(seed)
		return
	}
}

// addEntropy issues RNDADDENTROPY (struct rand_pool_info: entropy_count, buf_size, buf).
func addEntropy(seed []byte) {
	fd, err := syscall.Open("/dev/random", syscall.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer syscall.Close(fd)
	buf := make([]byte, 8+len(seed))
	*(*int32)(unsafe.Pointer(&buf[0])) = int32(len(seed) * 8)
	*(*int32)(unsafe.Pointer(&buf[4])) = int32(len(seed))
	copy(buf[8:], seed)
	const rndAddEntropy = 0x40085203
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), rndAddEntropy, uintptr(unsafe.Pointer(&buf[0])))
}
