package supervisor

const (
	ClaudeHost   = "claude.hermetarium.test"
	GrokHost     = "grok.hermetarium.test"
	DeepseekHost = "deepseek.hermetarium.test"
)

// CreateOpts is the product create input: an OCI image and a Squid ACL file.
type CreateOpts struct {
	Image string
	ACL   string // host path to squid.conf; copied into the instance dir and mounted

	// AfterGate runs in the gate/helper netns after Squid's network exists
	// and before the inhabitant is reachable. Tests use it for probe/mock sidecars.
	AfterGate func(id, netns string) error

	MemMiB int
	DiskMB int
}

func VendorHosts() []string {
	return []string{ClaudeHost, GrokHost, DeepseekHost}
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
