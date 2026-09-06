package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	fcVersion = "v1.16.1"
	fcURL     = "https://github.com/firecracker-microvm/firecracker/releases/download/" + fcVersion + "/firecracker-" + fcVersion + "-x86_64.tgz"
	kernelURL = "https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin"
)

type StrongInstance struct {
	ID         string `json:"id"`
	Wall       string `json:"wall"`
	Helper     string `json:"helper"`
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

func download(url, dest string) error {
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		return nil
	}
	tmp := dest + ".part"
	_, err := Docker(3*time.Minute, "run", "--rm",
		"-v", filepath.Dir(dest)+":/out",
		"alpine:3.20", "sh", "-c",
		"apk add --no-cache curl >/dev/null && curl -fL -o /out/"+filepath.Base(tmp)+" "+url+" && mv /out/"+filepath.Base(tmp)+" /out/"+filepath.Base(dest),
	)
	return err
}

func EnsureStrongAssets(root string) (firecracker, kernel, rootfs string, err error) {
	tgz, err := cacheFile(root, "firecracker-"+fcVersion+"-x86_64.tgz")
	if err != nil {
		return "", "", "", err
	}
	firecracker, err = cacheFile(root, "firecracker")
	if err != nil {
		return "", "", "", err
	}
	kernel, err = cacheFile(root, "vmlinux.bin")
	if err != nil {
		return "", "", "", err
	}
	rootfs, err = cacheFile(root, "rootfs.ext4")
	if err != nil {
		return "", "", "", err
	}
	if err := download(fcURL, tgz); err != nil {
		return "", "", "", err
	}
	if _, err := os.Stat(firecracker); err != nil {
		_, err = Docker(2*time.Minute, "run", "--rm",
			"-v", CacheDir(root)+":/out",
			"alpine:3.20", "sh", "-c",
			"apk add --no-cache tar >/dev/null && tar -xzf /out/"+filepath.Base(tgz)+" -C /tmp && find /tmp -name 'firecracker-*x86_64' ! -name '*debug*' | head -1 | xargs -I{} cp {} /out/firecracker && chmod +x /out/firecracker",
		)
		if err != nil {
			return "", "", "", err
		}
	}
	if err := download(kernelURL, kernel); err != nil {
		return "", "", "", err
	}
	script := filepath.Join(root, "firecracker-helper", "build-rootfs.sh")
	stale := true
	if st, err := os.Stat(rootfs); err == nil && st.Size() > 10_000 {
		if sc, err := os.Stat(script); err == nil && !st.ModTime().Before(sc.ModTime()) {
			stale = false
		}
	}
	if stale {
		_, err = Docker(3*time.Minute, "run", "--rm", "--privileged",
			"-v", script+":/build-rootfs.sh:ro",
			"-v", CacheDir(root)+":/out",
			"alpine:3.20", "sh", "/build-rootfs.sh",
		)
		if err != nil {
			return "", "", "", err
		}
	}
	return firecracker, kernel, rootfs, nil
}

func CreateStrong(root, id string) (*StrongInstance, error) {
	if err := EnsureSquidImage(root); err != nil {
		return nil, err
	}
	firecracker, kernel, rootfs, err := EnsureStrongAssets(root)
	if err != nil {
		return nil, err
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		return nil, err
	}
	if err := WriteSquidACL(root, dir); err != nil {
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
	helper := "htm-fc-" + id
	helperScript := filepath.Join(root, "firecracker-helper", "entrypoint.sh")
	vm := map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": "/opt/vmlinux.bin",
			"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/init",
		},
		"drives": []map[string]any{{
			"drive_id":       "rootfs",
			"path_on_host":   "/opt/rootfs.ext4",
			"is_root_device": true,
			"is_read_only":   false,
		}},
		"machine-config": map[string]any{"vcpu_count": 1, "mem_size_mib": 128},
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

	if _, err := Docker(30*time.Second,
		"run", "-d", "--name", helper,
		"--privileged", "--network", "none",
		"--device", "/dev/kvm",
		"--entrypoint", "sh",
		"-v", firecracker+":/opt/firecracker:ro",
		"-v", kernel+":/opt/vmlinux.bin:ro",
		"-v", rootfs+":/opt/rootfs.ext4",
		"-v", helperScript+":/fc-helper.sh:ro",
		"-v", dir+":/log",
		SquidImage,
		"/fc-helper.sh",
	); err != nil {
		return nil, err
	}

	inst := &StrongInstance{
		ID: id, Wall: "strong", Helper: helper,
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
		probe := strings.Contains(stdout, "PROBE_OK") || strings.Contains(stdout, "PROBE_FAIL")
		leak := strings.Contains(stdout, "LEAK_OK") || strings.Contains(stdout, "LEAK_FAIL")
		if probe && leak {
			return stdout, nil
		}
		if !helperRunning(inst.Helper) {
			return stdout + extraFC(inst), fmt.Errorf("helper exited before probe/leak finished")
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
	DockerIgnore("rm", "-f", "htm-fc-"+id)
	return nil
}
