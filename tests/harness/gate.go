package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pihme/hermetarium/supervisor"
)

const (
	TestVendorKey  = "htm-test-key"
	VendorMockPort = 18082
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
		ProbeHost: supervisor.ProbeHost,
		ProbePort: supervisor.ProbePort,
		AfterGate: extras(root, true, false),
		MemMiB:    128,
		DiskMB:    256,
	}
}

func agentdOpts(root string, mock bool) supervisor.CreateOpts {
	opts := supervisor.CreateOpts{
		Image:      AgentdImage,
		VendorHost: supervisor.ClaudeHost,
		KeyEnv:     "HERMETARIUM_ANTHROPIC_API_KEY",
		LivePeer:   "api.anthropic.com",
		LivePort:   443,
		ProbeHost:  supervisor.ProbeHost,
		ProbePort:  supervisor.ProbePort,
		AfterGate:  extras(root, true, mock),
		MemMiB:     128,
		DiskMB:     256,
	}
	if mock {
		opts.VendorPeer = "127.0.0.1"
		opts.VendorPort = VendorMockPort
		opts.VendorKey = TestVendorKey
	}
	return opts
}

func inhabitantOpts(root, name string, mock bool) (supervisor.CreateOpts, error) {
	var image string
	switch name {
	case InhabitantClaude:
		image = InClaudeImage
	case InhabitantGrok:
		image = InGrokImage
	case InhabitantDeepseek:
		image = InDeepseekImage
	default:
		return supervisor.CreateOpts{}, fmt.Errorf("unknown inhabitant %q", name)
	}
	v, ok := supervisor.LookupVendor(name)
	if !ok {
		return supervisor.CreateOpts{}, fmt.Errorf("unknown vendor %q", name)
	}
	opts := supervisor.CreateOpts{
		Image:      image,
		VendorHost: v.Host,
		KeyEnv:     v.KeyEnv,
		LivePeer:   v.LivePeer,
		LivePort:   v.LivePort,
		ProbeHost:  supervisor.ProbeHost,
		ProbePort:  supervisor.ProbePort,
		AfterGate:  extras(root, true, mock),
		MemMiB:     2048,
		DiskMB:     2048,
	}
	if mock {
		opts.VendorPeer = "127.0.0.1"
		opts.VendorPort = VendorMockPort
		opts.VendorKey = TestVendorKey
	}
	return opts, nil
}
