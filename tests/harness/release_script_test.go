package harness

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestReleaseScript runs the Python unit tests of .github/scripts/release.py,
// so the CI unit leg covers them without a separate workflow step.
func TestReleaseScript(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "-m", "unittest", "discover", "-v", "-s", filepath.Join(root, ".github", "scripts"), "-p", "test_*.py")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("python3 -m unittest: %v", err)
	}
}
