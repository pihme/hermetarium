package supervisor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const ProbeHost = "probe.hermetarium.test"
const ProbePort = 18080
const SquidPort = 3128

const SquidImage = "hermetarium-squid:local"
const HelloImage = "hermetarium-hello:local"

func Root() (string, error) {
	if r := os.Getenv("HERMETARIUM_ROOT"); r != "" {
		return r, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "squid", "Dockerfile")); err == nil {
			if _, err := os.Stat(filepath.Join(d, "supervisor")); err == nil {
				return d, nil
			}
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("hermetarium root not found from %s (set HERMETARIUM_ROOT)", wd)
		}
	}
}

func CacheDir(root string) string {
	return filepath.Join(root, ".cache")
}

func VarDir(root string) string {
	return filepath.Join(root, "var")
}

func InstanceDir(root, id string) (string, error) {
	dir := filepath.Join(VarDir(root), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func NewID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

type Subnet struct {
	CIDR string
	Gate string
	Box  string
}

func SubnetForID(id string) Subnet {
	n := 20
	if len(id) >= 2 {
		v, err := strconv.ParseInt(id[:2], 16, 64)
		if err == nil {
			n = int(v%200) + 20
		}
	}
	return Subnet{
		CIDR: fmt.Sprintf("10.%d.0.0/24", n),
		Gate: fmt.Sprintf("10.%d.0.2", n),
		Box:  fmt.Sprintf("10.%d.0.3", n),
	}
}
