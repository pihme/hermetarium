package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

const strongGuestIP = "172.16.0.2"

type StrongInstance struct {
	ID         string `json:"id"`
	Wall       string `json:"wall"`
	Helper     string `json:"helper"`
	InboundURL string `json:"inboundUrl"`
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
	discardFCStampIfTarballRejected(tgz, stamp, fcSHA256)
	if err := download(fcURL, tgz, fcSHA256); err != nil {
		return "", "", err
	}
	current, err := fcBinaryCurrent(firecracker, stamp)
	if err != nil {
		return "", "", err
	}
	if !current {
		_, err = Docker(2*time.Minute, "run", "--rm",
			"-v", CacheDir(root)+":/out",
			AlpineImage, "sh", "-c",
			"apk add --no-cache tar >/dev/null && tar -xzf /out/"+filepath.Base(tgz)+" -C /tmp && find /tmp -name 'firecracker-*x86_64' ! -name '*debug*' | head -1 | xargs -I{} cp {} /out/firecracker && chmod +x /out/firecracker",
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
	cached, err := EnsureImageRootfs(root, opts.Image, diskMB(opts))
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
	rootfs, err := habitatRootfs(dir, cached)
	if err != nil {
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
	vm := map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": "/opt/vmlinux.bin",
			"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/fc-init",
		},
		"drives": []map[string]any{{
			"drive_id":       "rootfs",
			"path_on_host":   "/opt/rootfs.ext4",
			"is_root_device": true,
			"is_read_only":   false,
		}},
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
		"-v", helperScript+":/fc-helper.sh:ro",
		"-v", dir+":/log",
		NetToolsImage,
		"/fc-helper.sh",
	)
	if _, err := Docker(30*time.Second, helperArgs...); err != nil {
		return nil, err
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

	url, err := finishInbound(helper, opts.NoInbound, 40*time.Second)
	if err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	inst := &StrongInstance{
		ID: id, Wall: "strong", Helper: helper, InboundURL: url,
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
	out, err := Docker(10*time.Second, "logs", name)
	if err != nil {
		return ""
	}
	return out
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

func DestroyStrong(id string) error {
	DockerIgnore("rm", "-f", "htm-probe-"+id)
	DockerIgnore("rm", "-f", "htm-mock-"+id)
	removeGate(id)
	DockerIgnore("rm", "-f", "htm-fc-"+id)
	return nil
}
