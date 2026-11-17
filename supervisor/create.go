package supervisor

import (
	"fmt"
	"sync"
	"time"
)

const (
	ClaudeHost = "claude.hermetarium.test"
	GrokHost   = "grok.hermetarium.test"
	ModelHost  = "model.hermetarium.test"
)

// CreateOpts is the product create input: an OCI image and a Squid ACL file.
type CreateOpts struct {
	Image string
	ACL   string // host path to squid.conf; copied into the instance dir and mounted

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

var (
	gateReadyMu sync.Mutex
	gateReady   = map[string]func(root, id string) error{}
	gateNetNS   sync.Map
)

// OnGateReady registers a test sidecar for one habitat. It runs after Squid
// is listening and before gate DNS, which is what releases the strong guest.
// The product create input does not carry this hook.
func OnGateReady(id string, fn func(root, id string) error) {
	gateReadyMu.Lock()
	defer gateReadyMu.Unlock()
	gateReady[id] = fn
}

func takeGateReady(id string) func(root, id string) error {
	gateReadyMu.Lock()
	defer gateReadyMu.Unlock()
	fn := gateReady[id]
	delete(gateReady, id)
	return fn
}

func runGateReady(root, id, netns string) error {
	gateNetNS.Store(id, netns)
	fn := takeGateReady(id)
	if fn == nil {
		return nil
	}
	return fn(root, id)
}

// JoinGate starts name in the gate netns create recorded for id.
// Callers do not pass htm-<id>-gate or htm-<id>-fc.
func JoinGate(id, name string, args ...string) error {
	v, ok := gateNetNS.Load(id)
	if !ok {
		return fmt.Errorf("join gate: habitat %s has no gate network", id)
	}
	full := append([]string{"run", "-d", "--name", name, "--network", "container:" + v.(string)}, args...)
	if _, err := Docker(30*time.Second, full...); err != nil {
		return err
	}
	return nil
}
