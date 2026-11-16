package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Instance is the record url, logs, attach, and destroy read.
// Weak and strong habitats use the same fields; a wall leaves the ones
// it does not have empty.
type Instance struct {
	ID                   string `json:"id"`
	Wall                 string `json:"wall"`
	Network              string `json:"network,omitempty"`
	Gate                 string `json:"gate,omitempty"`
	Box                  string `json:"box,omitempty"`
	FirecrackerContainer string `json:"firecrackerContainer,omitempty"`
	GateIP               string `json:"gateIp,omitempty"`
	BoxIP                string `json:"boxIp,omitempty"`
	InboundURL           string `json:"inboundUrl"`
	NoInbound            bool   `json:"noInbound"`
	LogPath              string `json:"logPath"`
	SerialPath           string `json:"serialPath,omitempty"`
	Dir                  string `json:"dir"`
}

type WeakInstance = Instance
type StrongInstance = Instance

func readInstanceFile(dir string) (*Instance, error) {
	b, err := os.ReadFile(filepath.Join(dir, "instance.json"))
	if err != nil {
		return nil, err
	}
	var inst Instance
	if err := json.Unmarshal(b, &inst); err != nil {
		return nil, err
	}
	if inst.Dir == "" {
		inst.Dir = dir
	}
	return &inst, nil
}

func loadInstance(root, id string) (*Instance, error) {
	return readInstanceFile(filepath.Join(VarDir(root), id))
}

func LoadWeak(root, id string) (*WeakInstance, error) {
	return loadInstance(root, id)
}

func writeInstance(inst *Instance) error {
	b, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(inst.Dir, "instance.json"), b, 0o644)
}

// Destroy reads the wall and runs that wall's teardown only.
func Destroy(root, id string) error {
	inst, err := loadInstance(root, id)
	if err != nil {
		return err
	}
	switch inst.Wall {
	case "weak":
		return DestroyWeak(id)
	case "strong":
		endStrong(id, inst.Dir)
		return nil
	default:
		return fmt.Errorf("destroy: unknown wall %q", inst.Wall)
	}
}

// prepareHabitat is the create work both walls share: the instance directory,
// the gate file, MITM material, and the allow-list script. The wall-specific
// bring-up stays in CreateWeak and CreateStrong.
func prepareHabitat(root, id string, opts CreateOpts) (dir string, conf []byte, https bool, err error) {
	if err = EnsureImageExists(opts.Image); err != nil {
		return
	}
	if err = EnsureNetTools(root); err != nil {
		return
	}
	dir, err = InstanceDir(root, id)
	if err != nil {
		return
	}
	if err = InstallACL(dir, opts.ACL); err != nil {
		return
	}
	if err = os.Chmod(dir, 0o777); err != nil {
		return
	}
	if err = os.WriteFile(filepath.Join(dir, "io.jsonl"), nil, 0o644); err != nil {
		return
	}
	conf, err = os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		return
	}
	https = ACLWantsHTTPS(conf)
	if https {
		if err = PrepareMITM(dir); err != nil {
			return
		}
		if err = InitMITMDB(root, dir); err != nil {
			return
		}
	}
	err = writeGateRules(dir)
	return
}
