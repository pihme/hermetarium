package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type WeakInstance struct {
	ID         string `json:"id"`
	Wall       string `json:"wall"`
	Network    string `json:"network"`
	Gate       string `json:"gate"`
	Box        string `json:"box"`
	GateIP     string `json:"gateIp"`
	BoxIP      string `json:"boxIp"`
	InboundURL string `json:"inboundUrl"`
	LogPath    string `json:"logPath"`
	Dir        string `json:"dir"`
}

func CreateWeak(root, id string) (*WeakInstance, error) {
	return CreateWeakExample(root, id, ExampleEcho)
}

func CreateWeakExample(root, id, example string) (*WeakInstance, error) {
	ex, ok := LookupExample(example)
	if !ok {
		return nil, fmt.Errorf("unknown example %q", example)
	}
	return CreateWeakEx(root, id, ex)
}

func CreateWeakInhabitant(root, id, name string) (*WeakInstance, error) {
	ex, ok := LookupInhabitant(name)
	if !ok {
		return nil, fmt.Errorf("unknown inhabitant %q", name)
	}
	ex.Probe = true
	ex.UseMock = true
	return CreateWeakEx(root, id, ex)
}

func CreateWeakEx(root, id string, ex Example) (*WeakInstance, error) {
	if err := EnsureInhabitant(root, ex); err != nil {
		return nil, err
	}
	if err := EnsureNetTools(root); err != nil {
		return nil, err
	}
	dir, err := InstanceDir(root, id)
	if err != nil {
		return nil, err
	}
	sub := SubnetForID(id)
	if err := WriteSquidACL(root, dir, sub.Box, ex); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "io.jsonl")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return nil, err
	}
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

	if err := runSquid(gate, dir, []string{
		"--network", network, "--ip", sub.Gate,
		"--sysctl", "net.ipv4.ip_forward=1",
		"-p", fmt.Sprintf("127.0.0.1::%d", InboundPort),
	}); err != nil {
		return nil, err
	}
	if err := WaitSquid(dir, gate, 20*time.Second); err != nil {
		return nil, err
	}
	if err := applyIntercept(gate); err != nil {
		return nil, err
	}
	if err := startTestGateExtras(root, id, gate, ex); err != nil {
		return nil, err
	}

	boxArgs := []string{
		"run", "-d", "--name", box,
		"--network", network, "--ip", sub.Box,
		"--cap-add", "NET_ADMIN",
		"--add-host", ProbeHost + ":" + sub.Gate,
	}
	for _, h := range VendorHosts() {
		boxArgs = append(boxArgs, "--add-host", h+":"+sub.Gate)
	}
	boxArgs = append(boxArgs, ex.Image)
	if _, err := Docker(30*time.Second, boxArgs...); err != nil {
		return nil, err
	}
	if _, err := Docker(15*time.Second,
		"exec", box, "sh", "-c",
		"ip route replace default via "+sub.Gate+" || ip route add default via "+sub.Gate,
	); err != nil {
		return nil, err
	}

	hostPort, err := containerHostPort(gate, InboundPort)
	if err != nil {
		return nil, err
	}
	url := inboundURL(hostPort)
	if err := waitInbound(url, 20*time.Second); err != nil {
		return nil, err
	}

	inst := &WeakInstance{
		ID: id, Wall: "weak", Network: network, Gate: gate, Box: box,
		GateIP: sub.Gate, BoxIP: sub.Box, InboundURL: url,
		LogPath: logPath, Dir: dir,
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
	DockerIgnore("rm", "-f", "htm-probe-"+id)
	DockerIgnore("rm", "-f", "htm-mock-"+id)
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
