package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	TestVendorKey    = "htm-test-key"
	VendorMockPort   = 18082
	MockVendorPeer   = "127.0.0.1 parent 18082 0 no-query no-digest originserver"
	ClaudeLivePeer   = "api.anthropic.com parent 443 0 no-query no-digest originserver ssl sslflags=DONT_VERIFY_PEER"
	GrokLivePeer     = "api.x.ai parent 443 0 no-query no-digest originserver ssl sslflags=DONT_VERIFY_PEER"
	DeepseekLivePeer = "api.deepseek.com parent 443 0 no-query no-digest originserver ssl sslflags=DONT_VERIFY_PEER"
)

func StartProbe(root, id, netns string) error {
	if err := supervisor.EnsureImageExists(supervisor.BusyboxImage); err != nil {
		return err
	}
	probeDir := filepath.Join(root, "tests", "probe")
	_, err := supervisor.Docker(30*time.Second,
		"run", "-d", "--name", "htm-probe-"+id,
		"--network", "container:"+netns,
		"-v", probeDir+":/probe:ro",
		supervisor.BusyboxImage,
		"httpd", "-f", "-p", "0.0.0.0:18080", "-h", "/probe",
	)
	return err
}

func StartVendorMock(root, id, netns string) error {
	bin, err := ensureVendorMock(root)
	if err != nil {
		return err
	}
	if err := supervisor.EnsureImageExists(supervisor.AlpineImage); err != nil {
		return err
	}
	_, err = supervisor.Docker(30*time.Second,
		"run", "-d", "--name", "htm-mock-"+id,
		"--network", "container:"+netns,
		"-e", "MOCK_EXPECT_KEY="+TestVendorKey,
		"-v", bin+":/usr/local/bin/vendor-mock:ro",
		supervisor.AlpineImage,
		"/usr/local/bin/vendor-mock",
	)
	return err
}

func extras(root string, probe, mock bool) func(id, netns string) error {
	return func(id, netns string) error {
		if probe {
			if err := StartProbe(root, id, netns); err != nil {
				return err
			}
		}
		if mock {
			if err := StartVendorMock(root, id, netns); err != nil {
				return err
			}
		}
		return nil
	}
}

func ensureVendorMock(root string) (string, error) {
	dest := filepath.Join(supervisor.CacheDir(root), "vendor-mock")
	src := filepath.Join(root, "tests", "vendormock", "main.go")
	if st, err := os.Stat(dest); err == nil && st.Size() > 1000 {
		if sc, err := os.Stat(src); err == nil && !st.ModTime().Before(sc.ModTime()) {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	return goBuild(root, dest, "./tests/vendormock")
}

func echoOpts(root string) supervisor.CreateOpts {
	return supervisor.CreateOpts{
		Image:     EchoImage,
		ACL:       filepath.Join(root, "examples", "echo", "squid.conf"),
		AfterGate: extras(root, true, false),
		MemMiB:    128,
		DiskMB:    256,
	}
}

func agentdOpts(root string, mock bool) (supervisor.CreateOpts, error) {
	acl, err := fillACL(filepath.Join(root, "examples", "agentd", "squid.conf"), mock, ClaudeLivePeer, "HERMETARIUM_ANTHROPIC_API_KEY")
	if err != nil {
		return supervisor.CreateOpts{}, err
	}
	return supervisor.CreateOpts{
		Image:     AgentdImage,
		ACL:       acl,
		AfterGate: extras(root, false, mock),
		MemMiB:    128,
		DiskMB:    256,
	}, nil
}

func inhabitantOpts(root, name string, mock bool) (supervisor.CreateOpts, error) {
	var image, peer, keyEnv string
	switch name {
	case InhabitantClaude:
		image, peer, keyEnv = InClaudeImage, ClaudeLivePeer, "HERMETARIUM_ANTHROPIC_API_KEY"
	case InhabitantGrok:
		image, peer, keyEnv = InGrokImage, GrokLivePeer, "HERMETARIUM_XAI_API_KEY"
	case InhabitantDeepseek:
		image, peer, keyEnv = InDeepseekImage, DeepseekLivePeer, "HERMETARIUM_DEEPSEEK_API_KEY"
	default:
		return supervisor.CreateOpts{}, fmt.Errorf("unknown inhabitant %q", name)
	}
	acl, err := fillACL(filepath.Join(root, "inhabitants", name, "squid.conf"), mock, peer, keyEnv)
	if err != nil {
		return supervisor.CreateOpts{}, err
	}
	return supervisor.CreateOpts{
		Image:     image,
		ACL:       acl,
		AfterGate: extras(root, false, mock),
		MemMiB:    2048,
		DiskMB:    2048,
	}, nil
}

func fillACL(src string, mock bool, livePeer, keyEnv string) (string, error) {
	peer, key := MockVendorPeer, TestVendorKey
	if !mock {
		peer, key = livePeer, os.Getenv(keyEnv)
	}
	return MaterializeACL(src, peer, key)
}

func MaterializeACL(src, peer, key string) (string, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	s := strings.ReplaceAll(string(b), "__VENDOR_PEER__", peer)
	s = strings.ReplaceAll(s, "__VENDOR_KEY__", key)
	f, err := os.CreateTemp("", "htm-acl-*.conf")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
