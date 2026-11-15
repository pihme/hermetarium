package supervisor

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestAttachUsage(t *testing.T) {
	if code := Run([]string{"attach"}); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if code := Run([]string{"attach", "missing-habitat-zz"}); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestSSHRemoteCommand(t *testing.T) {
	got := sshRemoteCommand([]string{"sh", "-c", "echo hi\necho 'there'"})
	want := "exec 'sh' '-c' 'echo hi\necho '\"'\"'there'\"'\"''"
	if got != want {
		t.Fatalf("command %q", got)
	}
}

func TestAttachKeyOutsideGate(t *testing.T) {
	root := t.TempDir()
	id := "hab"
	dir, err := InstanceDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	arg, err := writeAttachKey(root, id)
	if err != nil {
		t.Fatal(err)
	}
	other, err := writeAttachKey(root, "other")
	if err != nil {
		t.Fatal(err)
	}
	if arg == other || !strings.HasPrefix(arg, "hermetarium.pub=") || strings.Contains(arg, " ") {
		t.Fatalf("boot args %q %q", arg, other)
	}
	key := attachKeyPath(root, id)
	rel, err := filepath.Rel(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	// runSquid bind-mounts the instance directory. A path inside it is on the gate.
	if rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("private key %s is inside the gate mount %s", key, dir)
	}
	for _, p := range []string{filepath.Dir(key), filepath.Dir(filepath.Dir(key))} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %o, want 0700", p, st.Mode().Perm())
		}
	}
	st, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode %o", st.Mode().Perm())
	}
	priv, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	seed := openSSHSeed(priv)
	if len(seed) != 32 {
		t.Fatalf("seed %d", len(seed))
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(arg), bytes.Fields(pub)[1]) {
		t.Fatalf("boot arg is not the public key")
	}
	if bytes.Contains(pub, seed) || bytes.Contains([]byte(arg), seed) {
		t.Fatal("public key holds the private seed")
	}
	if _, err := exec.LookPath("ssh-keygen"); err == nil {
		out, err := exec.Command("ssh-keygen", "-y", "-f", key).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(pub) {
			t.Fatalf("ssh-keygen -y\n%s\nwant\n%s", out, pub)
		}
	}
	for _, name := range []string{"strong.go", "attach.go", "oci.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "vsock") || strings.Contains(string(b), "fc-attach") || strings.Contains(string(b), "fcattach") {
			t.Fatalf("%s still has the vsock terminal", name)
		}
	}
	ep, err := os.ReadFile(filepath.Join("..", "firecracker-helper", "entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile(filepath.Join("..", "firecracker-helper", "pack-oci.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ep), "vsock") || strings.Contains(string(pack), "vsock") || strings.Contains(string(pack), "fc-attach") {
		t.Fatal("helper scripts still build the vsock terminal")
	}
	src, err := os.ReadFile("strong.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), `attachKeyDir(root, id)+":`) {
		t.Fatal("private key is mounted into the helper")
	}
	att, err := os.ReadFile("attach.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(att), `"-p"`) || !strings.Contains(string(att), `--network", "container:"`) || !strings.Contains(string(att), `":/key:ro"`) {
		t.Fatal("attach publishes a port or does not keep the key on the client mount")
	}
}

func TestAttachTTY(t *testing.T) {
	if _, err := Docker(15*time.Second, "info"); err != nil {
		t.Fatal(err)
	}
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	image := "hermetarium-attach:local"
	ctx := t.TempDir()
	df := "FROM " + AlpineImage + "\nRUN apk add --no-cache openssh tmux\nCMD [\"sleep\", \"3600\"]\n"
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(df), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Docker(5*time.Minute, "build", "-t", image, ctx); err != nil {
		t.Fatal(err)
	}
	acl := filepath.Join(t.TempDir(), "squid.conf")
	conf := "" +
		"http_port 127.0.0.1:3129\n" +
		"http_port 0.0.0.0:3128 intercept\n" +
		"visible_hostname hermetarium\n" +
		"pid_filename /log/squid.pid\n" +
		"coredump_dir /tmp\n" +
		"cache_log /log/cache.log\n" +
		"logformat htm %ts.%03tu %6tr %>a %Ss/%03>Hs %<st %rm %ru %[un %Sh/%<a %mt %>lp\n" +
		"access_log stdio:/log/access.log htm\n" +
		"logfile_rotate 0\n" +
		"shutdown_lifetime 1 seconds\n" +
		"cache_effective_user proxy\n" +
		"cache deny all\n" +
		"http_access deny all\n"
	if err := os.WriteFile(acl, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := CreateOpts{Image: image, ACL: acl, NoInbound: true, MemMiB: 128, DiskMB: 256}
	sizeCmd := []string{"sh", "-c", "echo TTY=$(tty); echo SIZE1=$(stty size); sleep 3; echo SIZE2=$(stty size); echo ATTACH_DONE"}

	for _, wall := range []string{"weak", "strong"} {
		t.Run(wall, func(t *testing.T) {
			id := newTestID(t)
			t.Cleanup(func() {
				_ = DestroyWeak(id)
				endStrong(id, filepath.Join(VarDir(root), id))
			})
			var dir, logPath string
			switch wall {
			case "weak":
				inst, err := CreateWeak(root, id, opts)
				if err != nil {
					t.Fatal(err)
				}
				dir, logPath = inst.Dir, inst.LogPath
			default:
				inst, err := CreateStrong(root, id, opts)
				if err != nil {
					t.Fatal(err)
				}
				dir, logPath = inst.Dir, inst.LogPath
				assertStrongAttachSealed(t, root, id, dir)
			}
			logs := func() string { return habitatLogs(wall, id, dir) }
			out := mustDrive(t, logs, root, id, sizeCmd, 40, 100, "SIZE1=40 100", "ATTACH_DONE", func(master *os.File) {
				setWinsize(master, 24, 80)
				_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
				time.Sleep(500 * time.Millisecond)
			})
			if !strings.Contains(out, "TTY=/dev/pts/") {
				t.Fatalf("not a pty\n%s\n%s", out, logs())
			}
			if !strings.Contains(out, "SIZE2=24 80") {
				t.Fatalf("resize did not follow\n%s\n%s", out, logs())
			}
			mustDrive(t, logs, root, id, []string{"sh", "-c", "tmux new-session -d -s keep 'echo alive > /tmp/kept; sleep 300' && echo TMUX_STARTED"}, 24, 80, "", "TMUX_STARTED", nil)
			mustDrive(t, logs, root, id, []string{"sh", "-c", "timeout 5 tmux has-session -t keep && cat /tmp/kept"}, 24, 80, "", "alive", nil)
			b, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(bytes.TrimSpace(b)) != 0 {
				t.Fatalf("attach wrote the I/O log: %s", b)
			}
		})
	}
}

func assertStrongAttachSealed(t *testing.T, root, id, dir string) {
	t.Helper()
	vm, err := os.ReadFile(filepath.Join(dir, "vm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(vm, []byte("vsock")) {
		t.Fatal("vm.json has a vsock device")
	}
	if !bytes.Contains(vm, []byte("hermetarium.pub=")) {
		t.Fatal("vm.json has no public key")
	}
	priv, err := os.ReadFile(attachKeyPath(root, id))
	if err != nil {
		t.Fatal(err)
	}
	seed := openSSHSeed(priv)
	if len(seed) != 32 {
		t.Fatalf("seed %d", len(seed))
	}
	enc := []byte(base64.StdEncoding.EncodeToString(seed))
	acl, err := os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(dir, "rootfs.ext4"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []struct {
		label string
		b     []byte
	}{
		{"vm.json", vm},
		{"squid.conf", acl},
		{"rootfs", disk},
	} {
		if bytes.Contains(name.b, seed) || bytes.Contains(name.b, enc) || bytes.Contains(name.b, priv) {
			t.Fatalf("private key is in %s", name.label)
		}
	}
	assertNoPublishedPorts(t, "htm-fc-"+id, "htm-gate-"+id)
}

func openSSHSeed(pemBytes []byte) []byte {
	block, _ := pem.Decode(pemBytes)
	if block == nil || !bytes.HasPrefix(block.Bytes, []byte("openssh-key-v1\x00")) {
		return nil
	}
	rest := block.Bytes[len("openssh-key-v1\x00"):]
	var err error
	for i := 0; i < 3; i++ {
		_, rest, err = takeSSHString(rest)
		if err != nil {
			return nil
		}
	}
	if len(rest) < 4 {
		return nil
	}
	rest = rest[4:]
	if _, rest, err = takeSSHString(rest); err != nil {
		return nil
	}
	priv, _, err := takeSSHString(rest)
	if err != nil || len(priv) < 8 {
		return nil
	}
	body := priv[8:]
	if _, body, err = takeSSHString(body); err != nil {
		return nil
	}
	if _, body, err = takeSSHString(body); err != nil {
		return nil
	}
	sec, _, err := takeSSHString(body)
	if err != nil || len(sec) != 64 {
		return nil
	}
	return sec[:32]
}

func takeSSHString(b []byte) ([]byte, []byte, error) {
	if len(b) < 4 {
		return nil, nil, errors.New("short")
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n < 0 || len(b) < 4+n {
		return nil, nil, errors.New("short")
	}
	return b[4 : 4+n], b[4+n:], nil
}

func mustDrive(t *testing.T, logs func() string, root, id string, argv []string, row, col uint16, saw, until string, on func(*os.File)) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last string
	var lastErr error
	for {
		out, err := driveAttach(root, id, argv, row, col, saw, until, on)
		last, lastErr = out, err
		if strings.Contains(out, "ATTACH_ERR") {
			t.Fatalf("attach error\n%s\n%s", out, logs())
		}
		var statusErr attachStatusError
		if errors.As(err, &statusErr) {
			t.Fatalf("attach: %v\n%s\n%s", err, out, logs())
		}
		if err == nil && strings.Contains(out, until) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("attach: %v\n%s\n%s", lastErr, last, logs())
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func driveAttach(root, id string, argv []string, row, col uint16, saw, until string, on func(*os.File)) (out string, err error) {
	master, slave, err := openTestPTY(row, col)
	if err != nil {
		return "", err
	}
	gotRow, gotCol := termSize(slave)
	if gotRow != row || gotCol != col {
		master.Close()
		slave.Close()
		return "", fmt.Errorf("operator pty size %d %d, want %d %d", gotRow, gotCol, row, col)
	}
	buf := &ptyBuf{}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		// A blocking read survives Close, and the first dial can fail
		// before the guest is listening.
		_ = syscall.SetNonblock(int(master.Fd()), true)
		tmp := make([]byte, 256)
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			_ = master.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, rerr := master.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if strings.Contains(buf.String(), until) {
				return
			}
			if rerr == nil || errors.Is(rerr, os.ErrDeadlineExceeded) || errors.Is(rerr, syscall.EAGAIN) || errors.Is(rerr, syscall.EWOULDBLOCK) {
				continue
			}
			return
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		code, aerr := attachTerm(root, id, argv, slave, slave, slave)
		if aerr == nil && code != 0 {
			aerr = attachStatusError{code}
		}
		errCh <- aerr
	}()
	took := false
	defer func() {
		_ = slave.Close()
		_ = master.Close()
		if !took {
			select {
			case waitErr := <-errCh:
				if err == nil {
					err = waitErr
				}
			case <-time.After(10 * time.Second):
				if err == nil {
					err = fmt.Errorf("attach hung")
				}
			}
		}
		select {
		case <-readDone:
		case <-time.After(2 * time.Second):
		}
	}()
	fired := false
	timeout := time.After(20 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		out = buf.String()
		if on != nil && !fired && saw != "" && strings.Contains(out, saw) {
			fired = true
			on(master)
		}
		if strings.Contains(out, until) {
			return out, nil
		}
		select {
		case err = <-errCh:
			took = true
			time.Sleep(200 * time.Millisecond)
			return buf.String(), err
		case <-timeout:
			return buf.String(), fmt.Errorf("session timeout")
		case <-tick.C:
		}
	}
}

// attachStatusError is a command that ran and exited non-zero. mustDrive
// does not run it again; a dial failure before the guest starts still retries.
type attachStatusError struct{ code int }

func (e attachStatusError) Error() string {
	return fmt.Sprintf("attach status %d", e.code)
}

type ptyBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (p *ptyBuf) Write(b []byte) {
	p.mu.Lock()
	p.b.Write(b)
	p.mu.Unlock()
}

func (p *ptyBuf) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b.String()
}

func openTestPTY(row, col uint16) (*os.File, *os.File, error) {
	mfd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	unlock := int32(0)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(mfd), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		syscall.Close(mfd)
		return nil, nil, errno
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(mfd), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		syscall.Close(mfd)
		return nil, nil, errno
	}
	sfd, err := syscall.Open("/dev/pts/"+strconv.FormatUint(uint64(n), 10), syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		syscall.Close(mfd)
		return nil, nil, err
	}
	master := os.NewFile(uintptr(mfd), "ptmx")
	slave := os.NewFile(uintptr(sfd), "pts")
	setWinsize(master, row, col)
	return master, slave, nil
}

func setWinsize(f *os.File, row, col uint16) {
	ws := winsize{Row: row, Col: col}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

func habitatLogs(wall, id, dir string) string {
	if wall == "strong" {
		return helperLogs("htm-fc-"+id) + extraFC(&StrongInstance{Dir: dir})
	}
	out, _ := Docker(10*time.Second, "logs", "htm-box-"+id)
	return out
}
