package supervisor

import (
	"fmt"
	"os"
)

const (
	ClaudeHost   = "claude.hermetarium.test"
	GrokHost     = "grok.hermetarium.test"
	DeepseekHost = "deepseek.hermetarium.test"
)

// CreateOpts is the product create input: an OCI image and optional vendor attach.
type CreateOpts struct {
	Image string

	VendorHost string
	KeyEnv     string
	LivePeer   string
	LivePort   int
	VendorPeer string
	VendorPort int
	VendorSSL  bool
	VendorKey  string

	ProbeHost string
	ProbePort int

	// AfterGate runs in the gate/helper netns after Squid's network exists
	// and before the inhabitant is reachable. Tests use it for probe/mock sidecars.
	AfterGate func(id, netns string) error

	MemMiB int
	DiskMB int
}

type Vendor struct {
	Host     string
	KeyEnv   string
	LivePeer string
	LivePort int
}

func LookupVendor(name string) (Vendor, bool) {
	switch name {
	case "claude", "claude-code":
		return Vendor{Host: ClaudeHost, KeyEnv: "HERMETARIUM_ANTHROPIC_API_KEY", LivePeer: "api.anthropic.com", LivePort: 443}, true
	case "grok", "grok-build":
		return Vendor{Host: GrokHost, KeyEnv: "HERMETARIUM_XAI_API_KEY", LivePeer: "api.x.ai", LivePort: 443}, true
	case "deepseek", "deepseek-harness":
		return Vendor{Host: DeepseekHost, KeyEnv: "HERMETARIUM_DEEPSEEK_API_KEY", LivePeer: "api.deepseek.com", LivePort: 443}, true
	default:
		return Vendor{}, false
	}
}

func VendorHosts() []string {
	return []string{ClaudeHost, GrokHost, DeepseekHost}
}

func (o CreateOpts) vendorPeer() (peer string, port int, ssl bool, key string, err error) {
	if o.VendorHost == "" {
		return "", 0, false, "", nil
	}
	if o.VendorPeer != "" {
		return o.VendorPeer, o.VendorPort, o.VendorSSL, o.VendorKey, nil
	}
	if o.KeyEnv == "" {
		return "", 0, false, "", fmt.Errorf("vendor %s missing key environment", o.VendorHost)
	}
	key = os.Getenv(o.KeyEnv)
	if key == "" {
		return "", 0, false, "", fmt.Errorf("%s is unset (required for vendor %s)", o.KeyEnv, o.VendorHost)
	}
	return o.LivePeer, o.LivePort, true, key, nil
}

func memMiB(o CreateOpts) int {
	if o.MemMiB > 0 {
		return o.MemMiB
	}
	return 128
}

func diskMB(o CreateOpts) int {
	if o.DiskMB > 0 {
		return o.DiskMB
	}
	return 256
}

func runAfterGate(opts CreateOpts, id, netns string) error {
	if opts.AfterGate == nil {
		return nil
	}
	return opts.AfterGate(id, netns)
}
