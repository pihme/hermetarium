package model

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	modelKey    = "htm-model-key"
	modelHost   = "upstream.example"
	modelPrefix = "/openai"
	modelPeer   = "127.0.0.1 parent 18082 0 no-query no-digest originserver name=model"
)

func TestModelGate(t *testing.T) {
	if _, err := supervisor.Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := supervisor.Root()
	if err != nil {
		t.Fatal(err)
	}
	image := buildModelImage(t, root)
	acl := filepath.Join(t.TempDir(), "squid.conf")
	src, err := os.ReadFile(filepath.Join(root, "examples", "model", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	conf := strings.ReplaceAll(string(src), "__MODEL_PEER__", modelPeer)
	conf = strings.ReplaceAll(conf, "__MODEL_PREFIX__", modelPrefix)
	conf = strings.ReplaceAll(conf, "__MODEL_HOST__", modelHost)
	conf = strings.ReplaceAll(conf, "__MODEL_KEY__", modelKey)
	if strings.Contains(conf, "__MODEL_") {
		t.Fatalf("unfilled gate file:\n%s", conf)
	}
	if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, wall := range []string{"weak", "strong"} {
		wall := wall
		t.Run(wall, func(t *testing.T) {
			report := t.TempDir()
			id, err := supervisor.NewID()
			if err != nil {
				t.Fatal(err)
			}
			opts := supervisor.CreateOpts{
				Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 128,
				AfterGate: func(hid, netns string) error {
					return startModelMock(root, hid, netns, report)
				},
			}
			if wall == "weak" {
				t.Cleanup(func() { _ = supervisor.DestroyWeak(id) })
				if _, err := supervisor.CreateWeak(root, id, opts); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Cleanup(func() { _ = supervisor.DestroyStrong(id) })
				if _, err := supervisor.CreateStrong(root, id, opts); err != nil {
					t.Fatal(err)
				}
			}
			logs := waitModel(t, wall, id)
			if strings.Contains(logs, "CLIENT_FAIL") || !strings.Contains(logs, "HTTP 200") || !strings.Contains(logs, "STREAM_DONE") {
				t.Fatalf("client\n%s", logs)
			}
			if streamedGaps(logs) < 2 {
				t.Fatalf("stream arrived in one piece\n%s", logs)
			}
			env := between(logs, "ENV_BEGIN", "ENV_END")
			if strings.Contains(env, modelKey) {
				t.Fatalf("key in environment\n%s", env)
			}
			if !strings.Contains(logs, "UPSTREAM_FAIL") {
				t.Fatalf("upstream was reachable\n%s", logs)
			}
			got, err := os.ReadFile(filepath.Join(report, "got"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			if !strings.Contains(text, "auth=Bearer "+modelKey) || strings.Contains(text, "placeholder-key") {
				t.Fatalf("auth %s", text)
			}
			if !strings.Contains(text, "host="+modelHost) || !strings.Contains(text, "path="+modelPrefix+"/v1/chat/completions") {
				t.Fatalf("upstream %s", text)
			}
			if wall == "weak" {
				assertBoxHasNoKey(t, id)
			} else {
				assertRootfsHasNoKey(t, id)
			}
		})
	}
}

func buildModelImage(t *testing.T, root string) string {
	t.Helper()
	ctx := t.TempDir()
	client := filepath.Join(ctx, "modelclient")
	cmd := exec.Command("go", "build", "-o", client, "./tests/modelclient")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("client: %v\n%s", err, out)
	}
	b, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(modelKey)) {
		t.Fatal("client binary contains the gate key")
	}
	script := "#!/bin/sh\n" +
		"echo ENV_BEGIN\nprintenv\necho ENV_END\n" +
		"/modelclient\n" +
		"if wget -q -T 3 -O /dev/null http://upstream.example/; then echo UPSTREAM_OK; else echo UPSTREAM_FAIL; fi\n" +
		"echo MODEL_DONE\nexec sleep 3600\n"
	if err := os.WriteFile(filepath.Join(ctx, "probe.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	df := "FROM " + supervisor.AlpineImage + "\nCOPY modelclient /modelclient\nCOPY probe.sh /probe.sh\nCMD [\"/probe.sh\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-model:local"
	if _, err := supervisor.Docker(5*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	return image
}

func startModelMock(root, id, netns, report string) error {
	bin := filepath.Join(report, "vendor-mock")
	cmd := exec.Command("go", "build", "-o", bin, "./tests/vendormock")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v\n%s", err, out)
	}
	if err := supervisor.EnsureImageExists(supervisor.AlpineImage); err != nil {
		return err
	}
	_, err := supervisor.Docker(30*time.Second,
		"run", "-d", "--name", "htm-mock-"+id,
		"--network", "container:"+netns,
		"-e", "MOCK_EXPECT_KEY="+modelKey,
		"-e", "MOCK_REPORT=/report/got",
		"-v", bin+":/usr/local/bin/vendor-mock:ro",
		"-v", report+":/report",
		supervisor.AlpineImage,
		"/usr/local/bin/vendor-mock",
	)
	return err
}

func assertBoxHasNoKey(t *testing.T, id string) {
	t.Helper()
	out, _, code := supervisor.DockerExec(30*time.Second, "exec", "htm-box-"+id,
		"grep", "-R", "-a", "-F", "-e", modelKey,
		"/etc", "/usr", "/bin", "/lib", "/root", "/tmp", "/modelclient", "/probe.sh")
	if code == 0 || strings.Contains(out, modelKey) {
		t.Fatalf("key in box filesystem\n%s", out)
	}
}

func assertRootfsHasNoKey(t *testing.T, id string) {
	t.Helper()
	src, err := supervisor.Docker(10*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Destination "/opt/rootfs.ext4"}}{{.Source}}{{end}}{{end}}`,
		"htm-fc-"+id)
	if err != nil {
		t.Fatal(err)
	}
	src = strings.TrimSpace(src)
	if src == "" {
		t.Fatal("no rootfs mount")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(modelKey)) {
		t.Fatal("key in the guest rootfs")
	}
}

func waitModel(t *testing.T, wall, id string) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var logs string
	name := "htm-box-" + id
	if wall != "weak" {
		name = "htm-fc-" + id
	}
	for time.Now().Before(deadline) {
		logs, _ = supervisor.Docker(10*time.Second, "logs", name)
		if strings.Contains(logs, "MODEL_DONE") {
			return logs
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("missing MODEL_DONE\n%s", logs)
	return ""
}

func streamedGaps(logs string) int {
	n := 0
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "CHUNK ") || !strings.Contains(line, "data:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "CHUNK" {
			continue
		}
		ms, err := strconv.Atoi(f[1])
		if err == nil && ms >= 250 {
			n++
		}
	}
	return n
}

func between(logs, start, end string) string {
	i := strings.LastIndex(logs, start)
	if i < 0 {
		return ""
	}
	rest := logs[i+len(start):]
	j := strings.Index(rest, end)
	if j >= 0 {
		rest = rest[:j]
	}
	return rest
}
