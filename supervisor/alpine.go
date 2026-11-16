package supervisor

import (
	_ "embed"
	"strings"
)

//go:embed build-image/Dockerfile
var buildImageDockerfile string

// AlpineImage is the image named by build-image/Dockerfile.
// Dependabot bumps that FROM line; this variable follows it.
var AlpineImage = mustAlpineTag(buildImageDockerfile)

func mustAlpineTag(df string) string {
	for _, line := range strings.Split(df, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "FROM ") {
			continue
		}
		tag := strings.TrimSpace(strings.TrimPrefix(line, "FROM "))
		if tag == "" {
			break
		}
		return tag
	}
	panic("supervisor/build-image/Dockerfile: missing FROM")
}
