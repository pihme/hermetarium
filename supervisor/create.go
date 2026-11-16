package supervisor

const (
	ClaudeHost = "claude.hermetarium.test"
	GrokHost   = "grok.hermetarium.test"
	ModelHost  = "model.hermetarium.test"
)

// CreateOpts is the product create input: an OCI image and a Squid ACL file.
type CreateOpts struct {
	Image string
	ACL   string // host path to squid.conf; copied into the instance dir and mounted

	// AfterGate runs in the gate or Firecracker container netns after Squid's
	// network exists and before the inhabitant is reachable. Tests use it for
	// probe/mock sidecars.
	AfterGate func(id, netns string) error

	MemMiB int
	DiskMB int
	CPUs   int // 0 means the wall default: one guest CPU, no Docker quota

	// Rootfs is a host ext4 file. On the strong wall it is copied, keeping
	// holes, to var/<id>/rootfs.ext4 and booted instead of a fresh pack of
	// Image. Empty means pack Image. The Docker wall rejects it.
	Rootfs string

	// NoInbound publishes no host port, does not wait for port 8080, and
	// does not accept the inbound port from the bridge gateway. The strong
	// wall still waits until the guest prints GUEST_UNAME=.
	NoInbound bool

	// TestLabel is applied as hermetarium.test=<value> to containers this
	// create starts (gate, box, DNS, Firecracker container). Empty for the
	// operator CLI; tests set it so cleanup is scoped to one test.
	TestLabel string
}

func memMiB(o CreateOpts) int {
	if o.MemMiB > 0 {
		return o.MemMiB
	}
	return 512
}

func diskMB(o CreateOpts) int {
	if o.DiskMB > 0 {
		return o.DiskMB
	}
	return 1024
}

func cpuCount(o CreateOpts) int {
	if o.CPUs > 0 {
		return o.CPUs
	}
	return 1
}

func runAfterGate(opts CreateOpts, id, netns string) error {
	if opts.AfterGate == nil {
		return nil
	}
	return opts.AfterGate(id, netns)
}
