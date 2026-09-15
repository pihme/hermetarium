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

func EnsureStrongAssets(root string) (firecracker, kernel string, err error) {
	tgz, err := cacheFile(root, "firecracker-"+fcVersion+"-x86_64.tgz")
	if err != nil {
		return "", "", err
	}
	firecracker, err = cacheFile(root, "firecracker")
	if err != nil {
		return "", "", err
	}
	kernel, err = cacheFile(root, "vmlinux.bin")
	if err != nil {
		return "", "", err
	}
	if err := download(fcURL, tgz); err != nil {
		return "", "", err
	}
	if _, err := os.Stat(firecracker); err != nil {
		_, err = Docker(2*time.Minute, "run", "--rm",
			"-v", CacheDir(root)+":/out",
			"alpine:3.20", "sh", "-c",
			"apk add --no-cache tar >/dev/null && tar -xzf /out/"+filepath.Base(tgz)+" -C /tmp && find /tmp -name 'firecracker-*x86_64' ! -name '*debug*' | head -1 | xargs -I{} cp {} /out/firecracker && chmod +x /out/firecracker",
		)
		if err != nil {
			return "", "", err
		}
	}
	if err := download(kernelURL, kernel); err != nil {
		return "", "", err
	}
	return firecracker, kernel, nil
}

func CreateStrong(root, id string) (*StrongInstance, error) {
	return CreateStrongExample(root, id, ExampleEcho)
}

func CreateStrongExample(root, id, example string) (*StrongInstance, error) {
	ex, ok := LookupExample(example)
	if !ok {
		return nil, fmt.Errorf("unknown example %q", example)
	}
	return CreateStrongEx(root, id, ex)
}

func CreateStrongInhabitant(root, id, name string) (*StrongInstance, error) {
	ex, ok := LookupInhabitant(name)
	if !ok {
		return nil, fmt.Errorf("unknown inhabitant %q", name)
	}
	return CreateStrongEx(root, id, ex)
}

func CreateStrongEx(root, id string, ex Example) (*StrongInstance, error) {
	if err := EnsureInhabitant(root, ex); err != nil {
		return nil, err
	}
	firecracker, kernel, err := EnsureStrongAssets(root)
	if err != nil {
		return nil, err
	}
	rootfs, err := EnsureImageRootfs(root, ex.Image, diskMB(ex))
	if err != nil {
		return nil, err
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		return nil, err
	}
	if err := WriteSquidACL(root, dir, strongGuestIP, ex); err != nil {
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
			"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/fc-init",
		},
		"drives": []map[string]any{{
			"drive_id":       "rootfs",
			"path_on_host":   "/opt/rootfs.ext4",
			"is_root_device": true,
			"is_read_only":   false,
		}},
		"machine-config": map[string]any{"vcpu_count": 1, "mem_size_mib": memMiB(ex)},
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
		"--privileged",
		"--device", "/dev/kvm",
		"--entrypoint", "sh",
		"-p", fmt.Sprintf("127.0.0.1::%d", InboundPort),
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

	hostPort, err := containerHostPort(helper, InboundPort)
	if err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	url := inboundURL(hostPort)
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
	if _, err := WaitStrongSerial(inst, 90*time.Second); err != nil {
		_ = DestroyStrong(id)
		return nil, err
	}
	if err := waitInbound(url, 20*time.Second); err != nil {
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
