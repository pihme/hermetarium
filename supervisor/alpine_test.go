package supervisor

import "testing"

func TestAlpineImageFromDockerfile(t *testing.T) {
	if AlpineImage != "alpine:3.24" {
		t.Fatalf("AlpineImage = %q, want alpine:3.24", AlpineImage)
	}
	if mustAlpineTag(helperDockerfile) != AlpineImage {
		t.Fatalf("embed parse %q != AlpineImage %q", mustAlpineTag(helperDockerfile), AlpineImage)
	}
}
