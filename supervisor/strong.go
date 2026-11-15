package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	fchelper "github.com/pihme/hermetarium/firecracker-helper"
)

const (
	fcVersion = "v1.16.1"
	fcURL     = "https://github.com/firecracker-microvm/firecracker/releases/download/" + fcVersion + "/firecracker-" + fcVersion + "-x86_64.tgz"
	// Release asset firecracker-v1.16.1-x86_64.tgz.sha256.txt.
	fcSHA256 = "382a02a869e4d6d5cb14c40577f9545e8458021ea8b0b2d3fc10ec14d9c242e6"
	// Extract stamp: pinned tarball checksum, then the binary's SHA-256.
	// A missing or different stamp is not a cache hit.
	fcStampName = "firecracker.stamp"
	// Firecracker publishes no checksum for CI kernels.
	kernelURL    = "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/20261111-778a26c0594c-0/x86_64/vmlinux-6.1.188"
	kernelFile   = "vmlinux-6.1.188"
	kernelSHA256 = "f5735774e2570d9df451fc5665f3525d3baf723fa6a8a413a03591d5742b39a5"
)

// fcPin is the tarball checksum EnsureStrongAssets accepts. Production
// leaves it at fcSHA256. Tests point it at a fixture so a tarball with no
// binary can be rejected without downloading the release asset.
var fcPin = fcSHA256

const strongGuestIP = "172.16.0.2"

type StrongInstance struct {
	ID         string `json:"id"`
	Wall       string `json:"wall"`
	Helper     string `json:"helper"`
	InboundURL string `json:"inboundUrl"`
	NoInbound  bool   `json:"noInbound"`
	LogPath    string `json:"logPath"`
	SerialPath string `json:"serialPath"`
	Dir        string `json:"dir"`
}

func cacheFile(root, name string) (string, error) {
	dir := CacheDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func fcHelperDir(root string) (string, error) {
	dir := filepath.Join(CacheDir(root), "firecracker-helper")
	if err := fchelper.Extract(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func download(url, dest, want string) error {
	if err := requireSHA256(dest, want); err == nil {
		return nil
	}
	tmp := dest + ".part"
	if _, err := Docker(3*time.Minute, "run", "--rm",
		"-v", filepath.Dir(dest)+":/out",
		AlpineImage, "sh", "-c",
		"apk add --no-cache curl >/dev/null && curl -fL -o /out/"+filepath.Base(tmp)+" "+url+" && mv /out/"+filepath.Base(tmp)+" /out/"+filepath.Base(dest),
	); err != nil {
		return err
	}
	return requireSHA256(dest, want)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func requireSHA256(path, want string) error {
	got, err := sha256File(path)
	if err != nil {
		return err
	}
	if got != want {
		_ = os.Remove(path)
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(path))
	}
	return nil
}

func fcBinaryCurrent(bin, stamp string) (bool, error) {
	sum, err := sha256File(bin)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	b, err := os.ReadFile(stamp)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(b)) == fcSHA256+"\n"+sum, nil
}

func writeFCStamp(bin, stamp string) error {
	sum, err := sha256File(bin)
	if err != nil {
		return err
	}
	return os.WriteFile(stamp, []byte(fcSHA256+"\n"+sum+"\n"), 0o644)
}

// discardFCStampIfTarballRejected drops the extract stamp when the cached
// tarball is missing or is not the pinned release. download then replaces it.
func discardFCStampIfTarballRejected(tgz, stamp, want string) {
	if err := requireSHA256(tgz, want); err != nil {
		_ = os.Remove(stamp)
	}
}

func EnsureStrongAssets(root string) (firecracker, kernel string, err error) {
	tgz, err := cacheFile(root, "firecracker-"+fcVersion+"-x86_64.tgz")
	if err != nil {
		return "", "", err
	}
	firecracker, err = cacheFile(root, "firecracker")
	if err != nil {
		return "", "", err
	}
	kernel, err = cacheFile(root, kernelFile)
	if err != nil {
		return "", "", err
	}
	stamp, err := cacheFile(root, fcStampName)
	if err != nil {
		return "", "", err
	}
	discardFCStampIfTarballRejected(tgz, stamp, fcPin)
	if err := download(fcURL, tgz, fcPin); err != nil {
		return "", "", err
	}
	current, err := fcBinaryCurrent(firecracker, stamp)
	if err != nil {
		return "", "", err
	}
	if !current {
		// Drop the cached binary before the copy. xargs exits 0 when find
		// prints nothing, so chmod would succeed on the old file and the
		// stamp would record it as this tarball. An empty path must fail
		// the shell, and the stamp is written only after that succeeds.
		_, err = Docker(2*time.Minute, "run", "--rm",
			"-v", CacheDir(root)+":/out",
			AlpineImage, "sh", "-c",
			"apk add --no-cache tar >/dev/null && rm -f /out/firecracker && tar -xzf /out/"+filepath.Base(tgz)+" -C /tmp && bin=$(find /tmp -name 'firecracker-*x86_64' ! -name '*debug*' | head -1) && test -n \"$bin\" && cp \"$bin\" /out/firecracker && chmod +x /out/firecracker && test -f /out/firecracker",
		)
		if err != nil {
			return "", "", err
		}
		if err := writeFCStamp(firecracker, stamp); err != nil {
			return "", "", err
		}
	}
	if err := download(kernelURL, kernel, kernelSHA256); err != nil {
		return "", "", err
	}
	return firecracker, kernel, nil
}

func CreateStrong(root, id string, opts CreateOpts) (*StrongInstance, error) {
	if err := EnsureImageExists(opts.Image); err != nil {
		return nil, err
	}
	if err := EnsureNetTools(root); err != nil {
		return nil, err
	}
	firecracker, kernel, err := EnsureStrongAssets(root)
	if err != nil {
		return nil, err
	}
	rootfs, err := EnsureImageRootfs(root, opts.Image, diskMB(opts))
	if err != nil {
		return nil, err
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		return nil, err
	}
	if err := InstallACL(dir, opts.ACL); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "io.jsonl")
	serialPath := filepath.Join(dir, "serial.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return nil, err
	}
	conf, err := os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		return nil, err
	}
	if ACLWantsHTTPS(conf) {
		if err := PrepareMITM(dir); err != nil {
			return nil, err
		}
		if err := InitMITMDB(root, dir); err != nil {
			return nil, err
		}
	}
	helper := "htm-fc-" + id
	helpers, err := fcHelperDir(root)
	if err != nil {
		return nil, err
	}
	helperScript := filepath.Join(helpers, "entrypoint.sh")
	if opts.DataPath != "" {
		abs, err := filepath.Abs(opts.DataPath)
		if err != nil {
			return nil, err
		}
		// A colon breaks the helper's docker -v mount of this file.
		if strings.Contains(abs, ":") {
			return nil, fmt.Errorf("data disk path %s must not contain ':'", abs)
		}
		opts.DataPath = abs
		// Held until this boot's data.claimed is visible (function return is
		// after waitDataClaim). A second create is "in use" for that window,
		// including mkfs.
		lock, err := lockDataDisk(opts.DataPath)
		if err != nil {
			return nil, err
		}
		defer lock.Close()
		if err := ensureDataDisk(opts.DataPath, opts.DataMiB); err != nil {
			return nil, err
		}
	}
	bootArgs := "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/fc-init"
	drives := []map[string]any{{
		"drive_id":       "rootfs",
		"path_on_host":   "/opt/rootfs.ext4",
		"is_root_device": true,
		"is_read_only":   false,
	}}
	if opts.DataPath != "" {
		bootArgs += " hermetarium.data=1"
		drives = append(drives, map[string]any{
			"drive_id":       "data",
			"path_on_host":   "/opt/data.ext4",
			"is_root_device": false,
			"is_read_only":   false,
		})
	}
	vm := map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": "/opt/vmlinux.bin",
			"boot_args":         bootArgs,
		},
		"drives":         drives,
		"machine-config": map[string]any{"vcpu_count": cpuCount(opts), "mem_size_mib": memMiB(opts)},
		"network-interfaces": []map[string]any{{
			"iface_id":      "eth0",
			"guest_mac":     "AA:FC:00:00:00:01",
			"host_dev_name": "tap0",
		}},
	}
	raw, err := json.MarshalIndent(vm, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "vm.json"), raw, 0o644); err != nil {
		return nil, err
	}

	helperArgs := []string{
		"run", "-d", "--name", helper,
		"--device", "/dev/kvm",
		"--device", "/dev/net/tun",
		"--cap-add", "NET_ADMIN",
		// /proc/sys is read-only in this container. default/ is copied onto
		// tap0 when it appears, and the kernel uses max(all, interface).
		"--sysctl", "net.ipv4.ip_forward=1",
		"--sysctl", "net.ipv4.conf.all.rp_filter=0",
		"--sysctl", "net.ipv4.conf.default.rp_filter=0",
		"--entrypoint", "sh",
	}
	helperArgs = append(helperArgs, inboundPublish(opts.NoInbound)...)
	helperArgs = append(helperArgs,
		"-v", firecracker+":/opt/firecracker:ro",
		"-v", kernel+":/opt/vmlinux.bin:ro",
		"-v", rootfs+":/opt/rootfs.ext4",
	)
	if opts.DataPath != "" {
		helperArgs = append(helperArgs,
			"-v", opts.DataPath+":/opt/data.ext4",
			"-v", opts.DataPath+".lock:/opt/data.lock",
		)
		// A reused id keeps the instance dir. An old claim would look like
		// this boot's helper already holds the disk.
		if err := os.Remove(filepath.Join(dir, "data.claimed")); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	helperArgs = append(helperArgs,
		"-v", helperScript+":/fc-helper.sh:ro",
		"-v", dir+":/log",
		NetToolsImage,
		"/fc-helper.sh",
	)
	if _, err := Docker(30*time.Second, helperArgs...); err != nil {
		return nil, err
	}
	if opts.DataPath != "" {
		if err := waitDataClaim(dir, helper); err != nil {
			_ = DestroyStrong(id)
			return nil, err
		}
	}

	gate := "htm-gate-" + id
	if err := runAfterGate(opts, id, helper); err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	if err := runSquid(root, gate, dir, []string{"--network", "container:" + helper}); err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	if err := WaitSquid(dir, gate, 20*time.Second); err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	if opts.DataPath != "" {
		if err := waitDataMounted(helper); err != nil {
			_ = DestroyStrong(id)
			return nil, err
		}
	}

	// No inbound port, so the serial line is the signal the guest reached
	// user space. It is printed only after a data disk has mounted.
	inboundTimeout := 40 * time.Second
	if opts.NoInbound {
		if err := waitGuestUname(dir, inboundTimeout, func() (string, bool) {
			return helperLogs(helper), helperRunning(helper)
		}); err != nil {
			_ = DestroyStrong(id)
			return nil, err
		}
	}
	url, err := finishInbound(helper, opts.NoInbound, inboundTimeout)
	if err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	inst := &StrongInstance{
		ID: id, Wall: "strong", Helper: helper, InboundURL: url, NoInbound: opts.NoInbound,
		LogPath: logPath, SerialPath: serialPath, Dir: dir,
	}
	b, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), b, 0o644); err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	return inst, nil
}

func helperLogs(name string) string {
	stdout, stderr, _ := DockerExec(10*time.Second, "logs", name)
	return stdout + stderr
}

func helperRunning(name string) bool {
	out, err := Docker(5*time.Second, "inspect", "-f", "{{.State.Running}}", name)
	return err == nil && strings.TrimSpace(out) == "true"
}

func WaitStrongSerial(inst *StrongInstance, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stdout := helperLogs(inst.Helper)
		_ = os.WriteFile(inst.SerialPath, []byte(stdout), 0o644)
		leak := strings.Contains(stdout, "LEAK_OK") || strings.Contains(stdout, "LEAK_FAIL")
		if leak {
			return stdout, nil
		}
		if !helperRunning(inst.Helper) {
			return stdout + extraFC(inst), fmt.Errorf("helper exited before leak check finished")
		}
		time.Sleep(500 * time.Millisecond)
	}
	stdout := helperLogs(inst.Helper) + extraFC(inst)
	_ = os.WriteFile(inst.SerialPath, []byte(stdout), 0o644)
	return stdout, fmt.Errorf("timeout waiting for strong-wall serial")
}

func extraFC(inst *StrongInstance) string {
	p := filepath.Join(inst.Dir, "fc.log")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return "\n--- fc.log ---\n" + string(b)
}

func ensureDataDisk(path string, sizeMiB int) error {
	if strings.Contains(path, ":") {
		return fmt.Errorf("data disk path %s must not contain ':'", path)
	}
	if sizeMiB <= 0 {
		sizeMiB = 1024
	}
	if st, err := os.Stat(path); err == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("data disk %s is not a file", path)
		}
		// 0xEF53 at byte 1080 is the ext4 superblock magic. Anything else
		// stays unattached: a failed guest mount would otherwise write
		// /data onto the shared rootfs cache.
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		var magic [2]byte
		_, rerr := f.ReadAt(magic[:], 0x438)
		f.Close()
		if rerr != nil || magic[0] != 0x53 || magic[1] != 0xEF {
			return fmt.Errorf("data disk %s is not ext4", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := f.Truncate(int64(sizeMiB) << 20); err != nil {
		f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	// Same -E flags as the root disk: lazy init does not fill the sparse file.
	if _, err := Docker(2*time.Minute, "run", "--rm",
		"-v", path+":/data.ext4",
		AlpineImage, "sh", "-c",
		"apk add --no-cache e2fsprogs >/dev/null && mkfs.ext4 -F -E lazy_itable_init=1,lazy_journal_init=1 /data.ext4 >/dev/null",
	); err != nil {
		_ = os.Remove(path)
		return err
	}
	return os.Chmod(path, 0o600)
}

// lockDataDisk holds an exclusive POSIX lock on path.lock until the caller
// closes it. flock does not conflict with that lock, so the helper can
// still flock the file for the life of the VM. A second open of this file
// must not be closed while the lock is held: closing any descriptor drops
// the POSIX lock. The in-use probe is therefore a flock on this same
// descriptor, released before the helper's own nonblocking flock.
func lockDataDisk(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	lk := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: io.SeekStart}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lk); err != nil {
		f.Close()
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EACCES) {
			return nil, fmt.Errorf("data disk %s is in use", path)
		}
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("data disk %s is in use", path)
		}
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func waitDataClaim(dir, helper string) error {
	claimed := filepath.Join(dir, "data.claimed")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(claimed); err == nil {
			return nil
		}
		if !helperRunning(helper) {
			logs := helperLogs(helper)
			if strings.Contains(logs, "data disk in use") {
				return fmt.Errorf("data disk is in use")
			}
			return fmt.Errorf("helper exited before the data disk was claimed: %s", logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("data disk was not claimed")
}

// waitGuestUname blocks until fc-init prints GUEST_UNAME= on the helper
// serial console. A dead or missing helper fails immediately. fc-init prints
// DATA_MOUNT_FAIL and exits before that line, so a mount failure must not
// turn into a boot timeout.
func waitGuestUname(dir string, timeout time.Duration, console func() (string, bool)) error {
	deadline := time.Now().Add(timeout)
	var text string
	var running bool
	for {
		text, running = console()
		if strings.Contains(text, "DATA_MOUNT_FAIL") {
			return fmt.Errorf("data disk did not mount")
		}
		if strings.Contains(text, "GUEST_UNAME=") {
			return nil
		}
		if !running || !time.Now().Before(deadline) {
			return fmt.Errorf("guest did not boot: %s", guestBootTail(dir, text))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func guestBootTail(dir, console string) string {
	parts := make([]string, 0, 2)
	if fc := strings.TrimSpace(tailHostFile(filepath.Join(dir, "fc.log"))); fc != "" {
		parts = append(parts, fc)
	}
	serial := strings.TrimSpace(tailHostFile(filepath.Join(dir, "serial.log")))
	if serial == "" {
		serial = strings.TrimSpace(tailText(console))
	}
	if serial != "" {
		parts = append(parts, serial)
	}
	return strings.Join(parts, "\n")
}

func tailText(s string) string {
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}

// The guest prints one of these after it tries /dev/vdb. Create does not
// succeed on DATA_MOUNT_FAIL; fc-init does not start the inhabitant either.
func waitDataMounted(helper string) error {
	deadline := time.Now().Add(60 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		logs = helperLogs(helper)
		if strings.Contains(logs, "DATA_MOUNT_FAIL") {
			return fmt.Errorf("data disk did not mount")
		}
		if strings.Contains(logs, "DATA_MOUNT_OK") {
			return nil
		}
		if !helperRunning(helper) {
			return fmt.Errorf("helper exited before the data disk mounted: %s", logs)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("data disk was not mounted: %s", logs)
}

func DestroyStrong(id string) error {
	DockerIgnore("rm", "-f", "htm-probe-"+id)
	DockerIgnore("rm", "-f", "htm-mock-"+id)
	removeGate(id)
	DockerIgnore("rm", "-f", "htm-fc-"+id)
	return nil
}
