package supervisor

import "testing"

func TestResolveCreate(t *testing.T) {
	if _, err := ResolveCreate(nil); err == nil {
		t.Fatal("expected --image required")
	}
	ex, err := ResolveCreate([]string{"--image", "myorg/box:1"})
	if err != nil || ex.Image != "myorg/box:1" || !ex.SkipBuild || ex.VendorHost != "" {
		t.Fatalf("image only %+v %v", ex, err)
	}
	ex, err = ResolveCreate([]string{"--image", "my-claude:dev", "--vendor", "claude"})
	if err != nil || ex.Image != "my-claude:dev" || !ex.SkipBuild || ex.VendorHost != ClaudeHost {
		t.Fatalf("vendor %+v %v", ex, err)
	}
	if _, err = ResolveCreate([]string{"--image", "x", "--vendor", "nope"}); err == nil {
		t.Fatal("expected unknown vendor")
	}
}
