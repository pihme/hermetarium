package harness

import (
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

// AssertNeighborBlocked checks that another container on Docker's default
// bridge cannot reach the Squid ports of a habitat's gate container. Squid
// listens on 0.0.0.0 and would add the vendor key to a neighbor's request.
func AssertNeighborBlocked(t *testing.T, gateContainer string) {
	t.Helper()
	ip, err := supervisor.Docker(15*time.Second, "inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", gateContainer)
	if err != nil {
		t.Fatal(err)
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		t.Fatalf("%s has no address on the default bridge", gateContainer)
	}
	if err := supervisor.EnsureImageExists(supervisor.BusyboxImage); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"3128", "3129", "18081", "80"} {
		if _, err := supervisor.Docker(30*time.Second, "run", "--rm", supervisor.BusyboxImage,
			"nc", "-z", "-w", "3", ip, port); err == nil {
			t.Fatalf("a neighbor container reached %s:%s", ip, port)
		}
	}
}
