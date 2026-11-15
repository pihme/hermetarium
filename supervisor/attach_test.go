package supervisor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestAttachProgramEmbedded(t *testing.T) {
	if len(fcAttachBin) < 100 || string(fcAttachBin[:4]) != "\x7fELF" {
		t.Fatalf("fcattach.bin is not an ELF (%d bytes)", len(fcAttachBin))
	}
}

func TestAttachUsage(t *testing.T) {
	if code := Run([]string{"attach"}); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if code := Run([]string{"attach", "missing-habitat-zz"}); code != 1 {
		t.Fatalf("code=%d", code)
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
	df := "FROM " + AlpineImage + "\nRUN apk add --no-cache tmux\nCMD [\"sleep\", \"3600\"]\n"
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
				_ = DestroyStrong(id)
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
		_, aerr := attachTerm(root, id, argv, slave, slave, slave)
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

func TestWriteSessionKeepsNewline(t *testing.T) {
	arg := "echo hi\necho there"
	var b bytes.Buffer
	if err := writeSession(&b, 24, 80, []string{"sh", "-c", arg}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("24 80\n3\n2\nsh2\n-c%d\n%s", len(arg), arg)
	if b.String() != want {
		t.Fatalf("header %q", b.String())
	}
}

func TestAttachNewlineAndStatus(t *testing.T) {
	arg := "echo hi\necho there"
	got := make(chan []string, 1)
	sock := listenFakeVsock(t, func(c net.Conn) {
		argv, err := readWireArgs(c)
		if err != nil {
			return
		}
		got <- argv
		payload := []byte("ready\n")
		_, _ = c.Write([]byte{sessData, 0, 0, 0, byte(len(payload))})
		_, _ = c.Write(payload)
		_, _ = c.Write([]byte{sessExit, 0, 0, 0, 1, 9})
	})
	in, out, text := attachPipes(t)
	stop := make(chan struct{})
	defer close(stop)
	code, err := attachVsock(sock, []string{"sh", "-c", arg}, in, out, out, make(chan os.Signal, 1), stop)
	if err != nil {
		t.Fatal(err)
	}
	if code != 9 {
		t.Fatalf("status %d", code)
	}
	if gotText := text(); gotText != "ready\n" {
		t.Fatalf("terminal %q", gotText)
	}
	select {
	case argv := <-got:
		if len(argv) != 3 || argv[2] != arg {
			t.Fatalf("argv %#v", argv)
		}
	default:
		t.Fatal("guest did not see argv")
	}
}

func TestAttachErrStatus(t *testing.T) {
	msg := []byte("ATTACH_ERR open pty")
	if len(msg) > 255 {
		t.Fatal("message")
	}
	sock := listenFakeVsock(t, func(c net.Conn) {
		if _, err := readWireArgs(c); err != nil {
			return
		}
		_, _ = c.Write(append([]byte{sessErr, 0, 0, 0, byte(len(msg))}, msg...))
	})
	in, out, text := attachPipes(t)
	stop := make(chan struct{})
	defer close(stop)
	code, err := attachVsock(sock, []string{"sh"}, in, out, out, make(chan os.Signal, 1), stop)
	if code == 0 || err == nil || err.Error() != string(msg) {
		t.Fatalf("code %d err %v", code, err)
	}
	if gotText := text(); strings.Contains(gotText, "ATTACH_ERR") {
		t.Fatalf("error was written to the terminal: %q", gotText)
	}
}

func TestRelayResizeRetries(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "vsock.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mu sync.Mutex
	attempts, oks := 0, 0
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if _, err := readLine(c); err != nil {
					return
				}
				mu.Lock()
				attempts++
				drop := attempts == 1
				mu.Unlock()
				if drop {
					return
				}
				if _, err := io.WriteString(c, "OK 1\n"); err != nil {
					return
				}
				mu.Lock()
				oks++
				mu.Unlock()
				_, _ = readLine(c)
			}(c)
		}
	}()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inW.Close()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		relayResize(sock, inR, make(chan os.Signal, 1), stop)
	}()
	defer func() {
		close(stop)
		<-done
		inR.Close()
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := oks
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if oks == 0 || attempts < 2 {
		t.Fatalf("attempts %d oks %d", attempts, oks)
	}
}

func listenFakeVsock(t *testing.T, session func(net.Conn)) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "vsock.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := readLine(c)
				if err != nil || !strings.HasPrefix(line, "CONNECT ") {
					return
				}
				if _, err := io.WriteString(c, "OK 1\n"); err != nil {
					return
				}
				if strings.TrimSpace(strings.TrimPrefix(line, "CONNECT ")) != strconv.Itoa(vsockSessionPort) {
					_, _ = readLine(c)
					return
				}
				session(c)
			}(c)
		}
	}()
	return sock
}

func readWireArgs(r io.Reader) ([]string, error) {
	if _, err := readLine(r); err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 0 || n > 4096 {
		return nil, err
	}
	argv := make([]string, n)
	for i := 0; i < n; i++ {
		line, err = readLine(r)
		if err != nil {
			return nil, err
		}
		sz, err := strconv.Atoi(line)
		if err != nil || sz < 0 || sz > 1<<20 {
			return nil, err
		}
		buf := make([]byte, sz)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		argv[i] = string(buf)
	}
	return argv, nil
}

func attachPipes(t *testing.T) (in, out *os.File, text func() string) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// inR stays open. Closing it races the stdin copy blocked in Read.
		inW.Close()
		outR.Close()
		outW.Close()
	})
	return inR, outW, func() string {
		outW.Close()
		b, _ := io.ReadAll(outR)
		return string(b)
	}
}

func habitatLogs(wall, id, dir string) string {
	if wall == "strong" {
		return helperLogs("htm-fc-"+id) + extraFC(&StrongInstance{Dir: dir})
	}
	out, _ := Docker(10*time.Second, "logs", "htm-box-"+id)
	return out
}
