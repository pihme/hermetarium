//go:build live

package inhabitants

import (
	"os"
	"strings"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

func TestOfficialCLILive(t *testing.T) {
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		keyEnv string
	}{
		{supervisor.ExampleClaude, "HERMETARIUM_ANTHROPIC_API_KEY"},
		{supervisor.ExampleGrok, "HERMETARIUM_XAI_API_KEY"},
		{supervisor.ExampleDeepseek, "HERMETARIUM_DEEPSEEK_API_KEY"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if os.Getenv(tc.keyEnv) == "" {
				t.Skip(tc.keyEnv + " unset")
			}
			id, err := supervisor.NewID()
			if err != nil {
				t.Fatal(err)
			}
			inst, err := supervisor.CreateWeakInhabitant(root, id, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			defer supervisor.DestroyWeak(id)
			got, err := supervisor.Call(inst.InboundURL, "Run the shell command id -u and reply with only the number.")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, "0") {
				t.Fatalf("live reply %q", got)
			}
			got2, err := supervisor.Call(inst.InboundURL, "What number did you just report?")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("turn2 %q", got2)
		})
	}
}
