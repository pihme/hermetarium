package supervisor

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// TestLabelKey is the Docker label tests use to name the containers they start.
	TestLabelKey    = "hermetarium.test"
	testLabelPrefix = "htm"
)

var (
	trackedLabels sync.Map // test name -> label
	testLabelSeq  atomic.Uint64
)

// testTracker is *testing.T without importing testing into the supervisor binary.
type testTracker interface {
	Helper()
	Name() string
	Cleanup(func())
	Errorf(format string, args ...any)
}

// NewTestLabel returns hermetarium.test's value: a fixed prefix, this process's
// pid, and a random uuid. It is unique per call so two tests that start in the
// same second still differ.
func NewTestLabel() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := testLabelSeq.Add(1)
		binary.BigEndian.PutUint64(b[0:8], uint64(os.Getpid()))
		binary.BigEndian.PutUint64(b[8:16], n)
	}
	return fmt.Sprintf("%s-%d-%s", testLabelPrefix, os.Getpid(), hex.EncodeToString(b[:]))
}

// TestLabelArgs is --label hermetarium.test=<label> for docker run and create.
// A blank label adds nothing, so the operator create path stays unlabeled.
func TestLabelArgs(label string) []string {
	if label == "" {
		return nil
	}
	return []string{"--label", TestLabelKey + "=" + label}
}

// DockerArgsWithTestLabel inserts TestLabelArgs after docker run or create.
func DockerArgsWithTestLabel(args []string, label string) []string {
	if label == "" || len(args) == 0 || (args[0] != "run" && args[0] != "create") {
		return args
	}
	want := TestLabelKey + "="
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--label" || a == "-l" {
			if i+1 < len(args) && strings.HasPrefix(args[i+1], want) {
				return args
			}
			continue
		}
		if strings.HasPrefix(a, "--label="+want) || strings.HasPrefix(a, "-l="+want) {
			return args
		}
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, args[0])
	out = append(out, TestLabelArgs(label)...)
	out = append(out, args[1:]...)
	return out
}

// TrackTestContainers assigns this test a label and, when the test ends,
// removes only the containers that carry it. It then fails the test if any of
// those containers remain. Orphan containers whose label pid is not a running
// process may also be removed; a live pid is left alone. Calling it again in
// the same test returns the same label.
func TrackTestContainers(t testTracker) string {
	t.Helper()
	if v, ok := trackedLabels.Load(t.Name()); ok {
		return v.(string)
	}
	label := NewTestLabel()
	actual, loaded := trackedLabels.LoadOrStore(t.Name(), label)
	if loaded {
		return actual.(string)
	}
	t.Cleanup(func() {
		trackedLabels.Delete(t.Name())
		if err := RemoveContainersByTestLabel(label); err != nil {
			t.Errorf("remove test containers %s: %v", label, err)
		}
		left, err := ContainersByTestLabel(label)
		if err != nil {
			t.Errorf("list test containers %s: %v", label, err)
		}
		if len(left) > 0 {
			t.Errorf("containers still present with %s=%s: %s", TestLabelKey, label, strings.Join(left, ", "))
		}
		sweepOrphanTestContainers()
	})
	return label
}

// WithTestLabel sets opts.TestLabel to this test's label so CreateWeak and
// CreateStrong tag the gate, box, DNS, and Firecracker containers.
func WithTestLabel(t testTracker, opts CreateOpts) CreateOpts {
	opts.TestLabel = TrackTestContainers(t)
	return opts
}

// DockerLabeled is Docker with this test's hermetarium.test label on run and create.
func DockerLabeled(t testTracker, timeout time.Duration, args ...string) (string, error) {
	return Docker(timeout, DockerArgsWithTestLabel(args, TrackTestContainers(t))...)
}

// ContainersByTestLabel lists container IDs that have hermetarium.test=<label>.
func ContainersByTestLabel(label string) ([]string, error) {
	if label == "" {
		return nil, fmt.Errorf("empty test label")
	}
	out, err := Docker(15*time.Second, "ps", "-aq", "--filter", "label="+TestLabelKey+"="+label)
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(out), nil
}

// RemoveContainersByTestLabel runs docker rm -fv on containers with that exact
// label and no others.
func RemoveContainersByTestLabel(label string) error {
	ids, err := ContainersByTestLabel(label)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-fv"}, ids...)
	_, err = Docker(30*time.Second, args...)
	return err
}

func sweepOrphanTestContainers() {
	format := "{{.ID}}\t{{.Label \"" + TestLabelKey + "\"}}"
	out, err := Docker(15*time.Second, "ps", "-a",
		"--filter", "label="+TestLabelKey,
		"--format", format)
	if err != nil {
		return
	}
	var orphans []string
	for _, line := range strings.Split(out, "\n") {
		id, value, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || id == "" {
			continue
		}
		pid, ok := parseTestLabelPID(value)
		if !ok || pidRunning(pid) {
			continue
		}
		orphans = append(orphans, id)
	}
	if len(orphans) == 0 {
		return
	}
	DockerIgnore(append([]string{"rm", "-fv"}, orphans...)...)
}

func parseTestLabelPID(value string) (int, bool) {
	prefix, rest, ok := strings.Cut(value, "-")
	if !ok || prefix != testLabelPrefix {
		return 0, false
	}
	pidStr, uuid, ok := strings.Cut(rest, "-")
	if !ok || pidStr == "" || uuid == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func pidRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
