package supervisor

import (
	"fmt"
	"strconv"
	"strings"
)

// Plan is the gate address, the inhabitant address, and the stand-in names.
// Each wall has its own subnet. Callers use this instead of writing the
// numbers down again.
type Plan struct {
	Gate   string
	Guest  string
	CIDR   string
	Prefix int
	Names  []string
}

// StandInNames is the only list of names that always resolve to the gate.
func StandInNames() []string {
	return []string{ProbeHost, ClaudeHost, GrokHost, ModelHost}
}

func WeakPlan(id string) Plan {
	sub := SubnetForID(id)
	return Plan{Gate: sub.Gate, Guest: sub.Box, CIDR: sub.CIDR, Prefix: 24, Names: StandInNames()}
}

// StrongPlan is the only place the strong-wall subnet is written down.
func StrongPlan() Plan {
	return Plan{
		Gate:   "172.16.0.1",
		Guest:  "172.16.0.2",
		CIDR:   "172.16.0.0/24",
		Prefix: 24,
		Names:  StandInNames(),
	}
}

func PlanFor(wall, id string) Plan {
	if wall == "strong" {
		return StrongPlan()
	}
	return WeakPlan(id)
}

func (p Plan) CAURL() string {
	return "http://" + p.Gate + ":" + strconv.Itoa(GateCAPort) + "/ca.pem"
}

// KernelArgs is what the strong guest reads. Names are one token.
func (p Plan) KernelArgs() string {
	return fmt.Sprintf("hermetarium.gate=%s hermetarium.guest=%s hermetarium.prefix=%d hermetarium.names=%s",
		p.Gate, p.Guest, p.Prefix, strings.Join(p.Names, ","))
}
