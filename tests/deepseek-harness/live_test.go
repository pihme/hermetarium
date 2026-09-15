//go:build live

package deepseekharness

import (
	"os"
	"strings"
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

func TestDeepseekLive(t *testing.T) {
	if os.Getenv("HERMETARIUM_DEEPSEEK_API_KEY") == "" {
		t.Skip("HERMETARIUM_DEEPSEEK_API_KEY unset")
	}
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	inst, err := supervisor.CreateWeakExample(root, id, supervisor.ExampleDeepseek)
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
}
