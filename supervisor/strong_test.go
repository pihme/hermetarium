package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFcHelperDirExtractsWithoutTree(t *testing.T) {
	root := t.TempDir()
	dir, err := fcHelperDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"entrypoint.sh", "pack-oci.sh"} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() == 0 || st.Mode()&0o111 == 0 {
			t.Fatalf("%s: size=%d mode=%v", p, st.Size(), st.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "firecracker-helper")); !os.IsNotExist(err) {
		t.Fatalf("extract wrote into tree path: %v", err)
	}
}
