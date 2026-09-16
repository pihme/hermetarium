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
const InboundPort = 18081
const EchoPort = 8080

const SquidImage = "ubuntu/squid:6.6-24.04_beta"
const AlpineImage = "alpine:3.20"
const BusyboxImage = "busybox:1.36.1"
const NetToolsImage = "hermetarium-nettools:local"

type layout struct {
	data   string
	cache  string
	pinned bool // HERMETARIUM_ROOT or a checkout: cache is data/.cache
}

func lookupLayout() (layout, error) {
	if r := os.Getenv("HERMETARIUM_ROOT"); r != "" {
		return layout{data: r, cache: filepath.Join(r, ".cache"), pinned: true}, nil
	}
	if tree := findCheckout(); tree != "" {
		return layout{data: tree, cache: filepath.Join(tree, ".cache"), pinned: true}, nil
	}
	data, err := xdgJoin("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return layout{}, err
	}
	cache, err := xdgJoin("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return layout{}, err
	}
	return layout{data: data, cache: cache, pinned: false}, nil
}

func findCheckout() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for d := wd; ; d = filepath.Dir(d) {
		if isCheckout(d) {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

func isCheckout(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "supervisor"))
	return err == nil
}

func xdgJoin(env, homeRel string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return filepath.Join(v, "hermetarium"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("hermetarium directory: %w (set HERMETARIUM_ROOT or %s)", err, env)
	}
	return filepath.Join(home, homeRel, "hermetarium"), nil
}

// Root is the data directory: var/<id>/ lives here.
// HERMETARIUM_ROOT, else a checkout found from cwd, else XDG data home.
func Root() (string, error) {
	l, err := lookupLayout()
	if err != nil {
		return "", err
	}
	return l.data, nil
}

func CacheDir(root string) string {
	l, err := lookupLayout()
	if err == nil && !l.pinned && root == l.data {
		return l.cache
	}
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
