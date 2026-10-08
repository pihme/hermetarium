package supervisor

import (
	_ "embed"
	"strings"
)

//go:embed helper/Dockerfile
var helperDockerfile string

// AlpineImage is the image named by helper/Dockerfile.
// Dependabot bumps that FROM line; this variable follows it.
var AlpineImage = mustAlpineTag(helperDockerfile)

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
	panic("supervisor/helper/Dockerfile: missing FROM")
}
