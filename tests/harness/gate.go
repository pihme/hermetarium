package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	TestVendorKey  = "htm-test-key"
	VendorMockPort = 18082
	MockVendorPeer = "127.0.0.1 parent 18082 0 no-query no-digest originserver"
	ClaudeLivePeer = "api.anthropic.com parent 443 0 no-query no-digest originserver ssl tls-cafile=/etc/ssl/certs/ca-certificates.crt"
	GrokLivePeer   = "api.x.ai parent 443 0 no-query no-digest originserver ssl tls-cafile=/etc/ssl/certs/ca-certificates.crt"
)

func StartProbe(root, id, label string) error {
	if err := supervisor.EnsureImageExists(supervisor.BusyboxImage); err != nil {
		return err
	}
	probeDir := filepath.Join(root, "tests", "probe")
	args := []string{"-v", probeDir + ":/probe:ro"}
	args = append(args, supervisor.TestLabelArgs(label)...)
	args = append(args, supervisor.BusyboxImage, "httpd", "-f", "-p", "0.0.0.0:18080", "-h", "/probe")
	return supervisor.JoinGate(id, "htm-probe-"+id, args...)
}

func StartVendorMock(root, id, label string) error {
	bin, err := ensureVendorMock(root)
	if err != nil {
		return err
	}
	if err := supervisor.EnsureImageExists(supervisor.AlpineImage); err != nil {
		return err
	}
	args := []string{
		"-e", "MOCK_EXPECT_KEY=" + TestVendorKey,
		"-v", bin + ":/usr/local/bin/vendor-mock:ro",
	}
	args = append(args, supervisor.TestLabelArgs(label)...)
	args = append(args, supervisor.AlpineImage, "/usr/local/bin/vendor-mock")
	return supervisor.JoinGate(id, "htm-mock-"+id, args...)
}

func registerSidecars(root, id, label string, probe, mock bool) {
	supervisor.OnGateReady(id, func(string, string) error {
		if probe {
			if err := StartProbe(root, id, label); err != nil {
				return err
			}
		}
		if mock {
			if err := StartVendorMock(root, id, label); err != nil {
				return err
			}
		}
		return nil
	})
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

func echoOpts(root, label string) supervisor.CreateOpts {
	return supervisor.CreateOpts{
		Image:     EchoImage,
		ACL:       filepath.Join(root, "examples", "echo", "squid.conf"),
		TestLabel: label,
		MemMiB:    128,
		DiskMB:    256,
	}
}

func agentdOpts(root, label string, mock bool) (supervisor.CreateOpts, error) {
	acl, err := fillACL(filepath.Join(root, "examples", "agentd", "squid.conf"), mock, ClaudeLivePeer, "HERMETARIUM_ANTHROPIC_API_KEY")
	if err != nil {
		return supervisor.CreateOpts{}, err
	}
	return supervisor.CreateOpts{
		Image:     AgentdImage,
		ACL:       acl,
		TestLabel: label,
		MemMiB:    128,
		DiskMB:    256,
	}, nil
}

func inhabitantOpts(root, name, label string, mock bool) (supervisor.CreateOpts, error) {
	var image string
	var acl string
	var err error
	switch name {
	case InhabitantClaude:
		image = InClaudeImage
		acl, err = fillACL(filepath.Join(root, "inhabitants", name, "squid.conf"), mock, ClaudeLivePeer, "HERMETARIUM_ANTHROPIC_API_KEY")
	case InhabitantGrok:
		image = InGrokImage
		acl, err = fillACL(filepath.Join(root, "inhabitants", name, "squid.conf"), mock, GrokLivePeer, "HERMETARIUM_XAI_API_KEY")
	case InhabitantOpenCode:
		// No paid key. The smoke fills the generic model stand-in with the mock.
		image = InOpenCodeImage
		if !mock {
			err = fmt.Errorf("opencode smoke uses the mock endpoint")
			break
		}
		var raw []byte
		raw, err = os.ReadFile(filepath.Join(root, "inhabitants", name, "squid.conf"))
		if err != nil {
			break
		}
		s := string(raw)
		// An empty prefix must not become an extra argument. The box path stays /v1/...
		s = strings.ReplaceAll(s, "url_rewrite_program /log/model-rewrite __MODEL_PREFIX__", "url_rewrite_program /log/model-rewrite")
		s = strings.ReplaceAll(s, "__MODEL_PEER__", "127.0.0.1 parent 18082 0 no-query no-digest originserver name=model")
		s = strings.ReplaceAll(s, "__MODEL_HOST__", "upstream.example")
		s = strings.ReplaceAll(s, "__MODEL_PREFIX__", "")
		s = strings.ReplaceAll(s, "__MODEL_KEY__", TestVendorKey)
		if strings.Contains(s, "__MODEL_") {
			err = fmt.Errorf("unfilled model gate file")
			break
		}
		acl, err = writeTempACL(s)
	default:
		return supervisor.CreateOpts{}, fmt.Errorf("unknown inhabitant %q", name)
	}
	if err != nil {
		return supervisor.CreateOpts{}, err
	}
	return supervisor.CreateOpts{
		Image:     image,
		ACL:       acl,
		TestLabel: label,
		MemMiB:    2048,
		DiskMB:    2048,
	}, nil
}

func fillACL(src string, mock bool, livePeer, keyEnv string) (string, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	s := string(b)
	key := TestVendorKey
	if mock {
		s = strings.ReplaceAll(s, livePeer, MockVendorPeer)
	} else {
		key = os.Getenv(keyEnv)
	}
	s = strings.ReplaceAll(s, "__VENDOR_KEY__", key)
	return writeTempACL(s)
}

func writeTempACL(s string) (string, error) {
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
