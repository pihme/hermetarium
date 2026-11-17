package supervisor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// dockerFn exists so tests can stub Docker.
var dockerFn = runDockerCLI

func Docker(timeout time.Duration, args ...string) (string, error) {
	return dockerFn(timeout, args...)
}

func runDockerCLI(timeout time.Duration, args ...string) (string, error) {
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("docker %v\n%s%s", args, stderr.String(), err)
	}
	return stdout.String(), nil
}

func DockerExec(timeout time.Duration, args ...string) (stdout, stderr string, code int) {
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	stdout, stderr = outb.String(), errb.String()
	if err == nil {
		return stdout, stderr, 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return stdout, stderr, ee.ExitCode()
	}
	return stdout, stderr + err.Error(), 1
}

func DockerIgnore(args ...string) {
	_, _ = Docker(30*time.Second, args...)
}

// dockerRemove runs docker rm on each distinct name. A missing name does not
// stop the others: the CLI removes every name it finds and DockerIgnore
// drops the non-zero status.
func dockerRemove(flag string, names ...string) {
	args := []string{"rm", flag}
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		args = append(args, name)
	}
	if len(args) == 2 {
		return
	}
	DockerIgnore(args...)
}

func EnsureImageExists(name string) error {
	if _, err := Docker(15*time.Second, "inspect", "-f", "{{.Id}}", name); err == nil {
		return nil
	}
	_, err := Docker(5*time.Minute, "pull", name)
	return err
}
