package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestReadArgsKeepsNewline(t *testing.T) {
	arg := "echo hi\necho there"
	in := fmt.Sprintf("3\n2\nsh2\n-c%d\n%s", len(arg), arg)
	argv, err := readArgs(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 3 || argv[0] != "sh" || argv[1] != "-c" || argv[2] != arg {
		t.Fatalf("%#v", argv)
	}
}

func TestHandleArgNewline(t *testing.T) {
	resetActive(t)
	client, server := socketpair(t)
	defer client.Close()
	script := "printf 'A\nB\n'\nexit 4\n"
	go handle(server)
	if err := writeTestArgs(client, []string{"sh", "-c", script}); err != nil {
		t.Fatal(err)
	}
	out, status := readUntilExit(t, client)
	// The pty's ONLCR turns the script's newlines into CR LF. Both lines ran.
	if status != 4 || strings.ReplaceAll(out, "\r", "") != "A\nB\n" {
		t.Fatalf("status %d output %q", status, out)
	}
}

func TestHandleDrainsFastExit(t *testing.T) {
	resetActive(t)
	client, server := socketpair(t)
	defer client.Close()
	go handle(server)
	if err := writeTestArgs(client, []string{"sh", "-c", "echo MARK"}); err != nil {
		t.Fatal(err)
	}
	out, status := readUntilExit(t, client)
	if status != 0 || !strings.Contains(strings.ReplaceAll(out, "\r", ""), "MARK\n") {
		t.Fatalf("status %d output %q", status, out)
	}
}

func TestHandleDrainsWhileSlaveHeld(t *testing.T) {
	resetActive(t)
	client, server := socketpair(t)
	defer client.Close()
	go handle(server)
	// A background process keeps the slave after the shell exits, as a
	// detached tmux client does. It ignores SIGHUP so the session leader's
	// exit does not drop that fd. The shell's last write still has to arrive.
	script := "trap '' HUP\necho MARK\nsleep 30 &\necho PID:$!\nexit 0\n"
	if err := writeTestArgs(client, []string{"sh", "-c", script}); err != nil {
		t.Fatal(err)
	}
	out, status := readUntilExit(t, client)
	killHeld(out)
	if status != 0 || !strings.Contains(strings.ReplaceAll(out, "\r", ""), "MARK\n") {
		t.Fatalf("status %d output %q", status, out)
	}
}

func TestHandleHangupReturnsStatus(t *testing.T) {
	resetActive(t)
	client, server := socketpair(t)
	defer client.Close()
	go handle(server)
	if err := writeTestArgs(client, []string{"sh", "-c", "sleep 60"}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Shutdown(int(client.Fd()), syscall.SHUT_WR); err != nil {
		t.Fatal(err)
	}
	_, status := readUntilExit(t, client)
	if status != 128+int(syscall.SIGHUP) {
		t.Fatalf("status %d", status)
	}
}

func TestHandleAttachErr(t *testing.T) {
	resetActive(t)
	client, server := socketpair(t)
	defer client.Close()
	go handle(server)
	if err := writeTestArgs(client, []string{"/no/such/fc-attach-bin"}); err != nil {
		t.Fatal(err)
	}
	typ, payload := readOneFrame(t, client)
	if typ != sessErr || !strings.Contains(string(payload), "ATTACH_ERR") {
		t.Fatalf("type %d payload %q", typ, payload)
	}
}

func TestResizeStoredUntilPTY(t *testing.T) {
	resetActive(t)
	master, raw, slave, err := openPTY(24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	closed := false
	defer func() {
		if !closed {
			syscall.Close(raw)
		}
	}()

	resizeActive(40, 100)
	if r, c := mustPTYSize(t, raw); r != 24 || c != 80 {
		t.Fatalf("applied before publish: %d %d", r, c)
	}
	publishActive(raw)
	if r, c := mustPTYSize(t, raw); r != 40 || c != 100 {
		t.Fatalf("stored size %d %d", r, c)
	}
	resizeActive(41, 101)
	if r, c := mustPTYSize(t, raw); r != 41 || c != 101 {
		t.Fatalf("live size %d %d", r, c)
	}

	master2, raw2, slave2, err := openPTY(24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer master2.Close()
	defer slave2.Close()
	defer syscall.Close(raw2)
	publishActive(raw2)

	dropActive(raw)
	closed = true
	activeMu.Lock()
	got := activeFD
	activeMu.Unlock()
	if got != raw2 {
		t.Fatalf("drop cleared fd %d, active %d", raw2, got)
	}
	resizeActive(11, 22)
	if r, c := mustPTYSize(t, raw2); r != 11 || c != 22 {
		t.Fatalf("new pty %d %d", r, c)
	}

	activeMu.Lock()
	activeFD = -1
	activeMu.Unlock()
	applyWinsize(raw2, 5, 6)
	resizeActive(9, 9)
	if r, c := mustPTYSize(t, raw2); r != 5 || c != 6 {
		t.Fatalf("ioctl after clear: %d %d", r, c)
	}
}

func killHeld(out string) {
	const tag = "PID:"
	flat := strings.ReplaceAll(out, "\r", "")
	i := strings.LastIndex(flat, tag)
	if i < 0 {
		return
	}
	field := flat[i+len(tag):]
	if nl := strings.IndexByte(field, '\n'); nl >= 0 {
		field = field[:nl]
	}
	pid, err := strconv.Atoi(strings.TrimSpace(field))
	if err != nil || pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func resetActive(t *testing.T) {
	t.Helper()
	activeMu.Lock()
	activeFD = -1
	haveSize = false
	activeMu.Unlock()
	t.Cleanup(func() {
		activeMu.Lock()
		activeFD = -1
		haveSize = false
		activeMu.Unlock()
	})
}

func mustPTYSize(t *testing.T, fd int) (uint16, uint16) {
	t.Helper()
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		t.Fatal(errno)
	}
	return ws.Row, ws.Col
}

func socketpair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(fds[0]), "client"), os.NewFile(uintptr(fds[1]), "server")
}

func writeTestArgs(w io.Writer, argv []string) error {
	if _, err := fmt.Fprintf(w, "24 80\n%d\n", len(argv)); err != nil {
		return err
	}
	for _, arg := range argv {
		if _, err := fmt.Fprintf(w, "%d\n%s", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func readUntilExit(t *testing.T, r io.Reader) (string, int) {
	t.Helper()
	var out bytes.Buffer
	status := -1
	errCh := make(chan error, 1)
	go func() {
		for {
			typ, payload, err := readTestFrame(r)
			if err != nil {
				errCh <- err
				return
			}
			switch typ {
			case sessData:
				out.Write(payload)
			case sessExit:
				if len(payload) != 1 {
					errCh <- fmt.Errorf("status payload %q", payload)
					return
				}
				status = int(payload[0])
				errCh <- nil
				return
			case sessErr:
				errCh <- fmt.Errorf("guest error %s pty %q", payload, out.String())
				return
			default:
				errCh <- fmt.Errorf("frame %d", typ)
				return
			}
		}
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	return out.String(), status
}

func readOneFrame(t *testing.T, r io.Reader) (byte, []byte) {
	t.Helper()
	type got struct {
		typ     byte
		payload []byte
		err     error
	}
	ch := make(chan got, 1)
	go func() {
		typ, payload, err := readTestFrame(r)
		ch <- got{typ, payload, err}
	}()
	select {
	case g := <-ch:
		if g.err != nil {
			t.Fatal(g.err)
		}
		return g.typ, g.payload
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	return 0, nil
}

func readTestFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(hdr[1])<<24 | int(hdr[2])<<16 | int(hdr[3])<<8 | int(hdr[4])
	if n < 0 || n > 1<<20 {
		return 0, nil, fmt.Errorf("frame %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return hdr[0], buf, nil
}
