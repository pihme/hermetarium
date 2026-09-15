package grokbuild

import (
	"testing"

	"github.com/pihme/hermetarium/supervisor"
	"github.com/pihme/hermetarium/tests/harness"
)

func TestGrokBuild(t *testing.T) {
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("weak", func(t *testing.T) { testWall(t, root, false) })
	t.Run("strong", func(t *testing.T) { testWall(t, root, true) })
}

func testWall(t *testing.T, root string, strong bool) {
	id, err := supervisor.NewID()
	if err != nil {
		t.Fatal(err)
	}
	ex := supervisor.ExampleGrok
	if strong {
		inst, err := supervisor.CreateStrongExample(root, id, ex)
		if err != nil {
			t.Fatal(err)
		}
		defer supervisor.DestroyStrong(id)
		harness.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath, supervisor.GrokHost)
		return
	}
	inst, err := supervisor.CreateWeakExample(root, id, ex)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.DestroyWeak(id)
	harness.AssertNoKeyInBox(t, inst)
	harness.Exercise(t, inst.InboundURL, inst.Dir, inst.LogPath, supervisor.GrokHost)
}
