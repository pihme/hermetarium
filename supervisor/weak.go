package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type WeakInstance struct {
	ID      string `json:"id"`
	Wall    string `json:"wall"`
	Network string `json:"network"`
	Gate    string `json:"gate"`
	Box     string `json:"box"`
	GateIP  string `json:"gateIp"`
	BoxIP   string `json:"boxIp"`
	LogPath string `json:"logPath"`
	Dir     string `json:"dir"`
}

func CreateWeak(root, id string) (*WeakInstance, error) {
	if err := EnsureHelloImage(root); err != nil {
		return nil, err
	}
	if err := EnsureSquidImage(root); err != nil {
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
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return nil, err
	}
	sub := SubnetForID(id)
	network := "htm-" + id
	gate := "htm-gate-" + id
	box := "htm-box-" + id

	if _, err := Docker(30*time.Second,
		"network", "create", "--driver", "bridge", "--subnet", sub.CIDR,
		"-o", "com.docker.network.bridge.enable_ip_masquerade=false",
		network,
	); err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = DestroyWeak(id)
		}
	}()

	if _, err := Docker(30*time.Second,
		"run", "-d", "--name", gate,
		"--network", network, "--ip", sub.Gate,
		"--cap-add", "NET_ADMIN",
		"--sysctl", "net.ipv4.ip_forward=1",
		"-v", dir+":/log",
		SquidImage,
	); err != nil {
		return nil, err
	}
	if err := WaitSquid(gate, 20*time.Second); err != nil {
		return nil, err
	}
	if _, err := Docker(15*time.Second,
		"exec", gate, "sh", "-c",
		"iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports 3128",
	); err != nil {
		return nil, err
	}

	if _, err := Docker(30*time.Second,
		"run", "-d", "--name", box,
		"--network", network, "--ip", sub.Box,
		"--cap-add", "NET_ADMIN",
		"--add-host", ProbeHost+":"+sub.Gate,
		HelloImage,
	); err != nil {
		return nil, err
	}
	if _, err := Docker(15*time.Second,
		"exec", box, "sh", "-c",
		"ip route replace default via "+sub.Gate+" || ip route add default via "+sub.Gate,
	); err != nil {
		return nil, err
	}

	inst := &WeakInstance{
		ID: id, Wall: "weak", Network: network, Gate: gate, Box: box,
		GateIP: sub.Gate, BoxIP: sub.Box, LogPath: logPath, Dir: dir,
	}
	b, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), b, 0o644); err != nil {
		return nil, err
	}
	cleanup = false
	return inst, nil
}

func ExecWeak(inst *WeakInstance, argv []string) (stdout, stderr string, code int) {
	args := append([]string{"exec", inst.Box}, argv...)
	return DockerExec(60*time.Second, args...)
}

func DestroyWeak(id string) error {
	DockerIgnore("rm", "-f", "htm-box-"+id)
	DockerIgnore("rm", "-f", "htm-gate-"+id)
	DockerIgnore("network", "rm", "htm-"+id)
	return nil
}

func LoadWeak(root, id string) (*WeakInstance, error) {
	b, err := os.ReadFile(filepath.Join(VarDir(root), id, "instance.json"))
	if err != nil {
		return nil, err
	}
	var inst WeakInstance
	if err := json.Unmarshal(b, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

func SyncInstanceLog(dir string) error {
	return SyncAccessLog(filepath.Join(dir, "access.log"), filepath.Join(dir, "io.jsonl"))
}
