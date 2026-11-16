package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddressPlanOneList(t *testing.T) {
	names := StandInNames()
	if len(names) != 4 {
		t.Fatalf("stand-in names: %v", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Fatalf("duplicate stand-in %s", n)
		}
		seen[n] = true
	}
	for _, want := range []string{ProbeHost, ClaudeHost, GrokHost, ModelHost} {
		if !seen[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	strong := StrongPlan()
	weak := WeakPlan("habitat")
	if strong.Gate == weak.Gate || strong.CIDR == weak.CIDR {
		t.Fatalf("walls share a subnet: strong %s weak %s", strong.CIDR, weak.CIDR)
	}
	if !strings.HasPrefix(weak.Gate, "10.") || strong.Gate != "172.16.0.1" || strong.Guest != "172.16.0.2" {
		t.Fatalf("plan drifted: weak %s strong %s %s", weak.Gate, strong.Gate, strong.Guest)
	}
	if PlanFor("strong", "x").Gate != strong.Gate || PlanFor("weak", "habitat").Gate != weak.Gate {
		t.Fatal("PlanFor did not follow the wall")
	}
	args := strong.KernelArgs()
	for _, n := range names {
		if !strings.Contains(args, n) {
			t.Fatalf("kernel args missing %s: %s", n, args)
		}
	}
	if !strings.Contains(strong.CAURL(), strong.Gate) {
		t.Fatal(strong.CAURL())
	}
	conf, err := os.ReadFile(filepath.Join("..", "examples", "echo", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(conf), "172.16.") {
		t.Fatal("echo gate file hardcodes a strong-wall address")
	}
	for _, p := range []string{"../firecracker-scripts/entrypoint.sh", "../firecracker-scripts/pack-oci.sh"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "172.16.0.1") || strings.Contains(string(b), ProbeHost) {
			t.Fatalf("%s carries a gate address or a stand-in name", p)
		}
		if strings.Contains(string(b), "ipv4()") || strings.Contains(string(b), "hostnames()") {
			t.Fatalf("%s re-parses the address plan", p)
		}
	}
}
