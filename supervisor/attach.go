package supervisor

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	vsockSessionPort = 10000
	vsockResizePort  = 10001

	// Guest session frames: type byte, big-endian length, payload.
	// Exit status stays off the operator terminal.
	sessData byte = 0
	sessExit byte = 1
	sessErr  byte = 2
)

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

type running struct {
	Wall string `json:"wall"`
	Box  string `json:"box"`
	Dir  string `json:"dir"`
}

// Attach opens a terminal in a running habitat. argv defaults to sh when empty.
// Bytes on this path are not written to the I/O log.
func Attach(root, id string, argv []string) (int, error) {
	return attachTerm(root, id, argv, os.Stdin, os.Stdout, os.Stderr)
}

func attachTerm(root, id string, argv []string, in, out, errOut *os.File) (int, error) {
	if len(argv) == 0 {
		argv = []string{"sh"}
	}
	inst, err := loadRunning(root, id)
	if err != nil {
		return 1, err
	}
	// Escape sequences from the habitat must reach the operator unchanged.
	restore := ttyRaw(in)
	defer restore()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	defer signal.Stop(sig)
	stop := make(chan struct{})
	defer close(stop)

	switch inst.Wall {
	case "weak":
		if inst.Box == "" {
			return 1, fmt.Errorf("attach: habitat %s has no container", id)
		}
		return attachDocker(inst.Box, argv, in, out, errOut, sig, stop)
	case "strong":
		return attachVsock(vsockSockPath(root, id), argv, in, out, errOut, sig, stop)
	default:
		return 1, fmt.Errorf("attach: unknown wall %q", inst.Wall)
	}
}

func loadRunning(root, id string) (running, error) {
	var inst running
	b, err := os.ReadFile(filepath.Join(VarDir(root), id, "instance.json"))
	if err != nil {
		return inst, err
	}
	if err := json.Unmarshal(b, &inst); err != nil {
		return inst, err
	}
	if inst.Dir == "" {
		inst.Dir = filepath.Join(VarDir(root), id)
	}
	return inst, nil
}

func attachDocker(box string, argv []string, in, out, errOut *os.File, sig <-chan os.Signal, stop <-chan struct{}) (int, error) {
	args := []string{"exec", "-i"}
	if isTTY(in) {
		args = append(args, "-t")
	}
	args = append(args, box)
	args = append(args, argv...)
	cmd := exec.Command("docker", args...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	go func() {
		for {
			select {
			case <-sig:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGWINCH)
				}
			case <-stop:
				return
			}
		}
	}()
	err := cmd.Wait()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

func attachVsock(sock string, argv []string, in, out, errOut *os.File, sig <-chan os.Signal, stop <-chan struct{}) (int, error) {
	_ = errOut
	conn, err := dialVsock(sock, vsockSessionPort)
	if err != nil {
		return 1, err
	}
	defer conn.Close()
	row, col := termSize(in)
	if err := writeSession(conn, row, col, argv); err != nil {
		return 1, err
	}
	_ = conn.SetDeadline(time.Time{})
	// Stop using the terminal before returning. The caller's stop is closed
	// only after this function returns, which is too late.
	halt := make(chan struct{})
	resizeDone := make(chan struct{})
	var once sync.Once
	shutdown := func() { once.Do(func() { close(halt) }) }
	go func() {
		defer close(resizeDone)
		relayResize(sock, in, sig, halt)
	}()
	go func() {
		select {
		case <-stop:
			shutdown()
		case <-resizeDone:
		}
	}()
	defer func() {
		shutdown()
		<-resizeDone
	}()
	go func() {
		io.Copy(conn, in)
		// The guest still has to send the exit status on this connection.
		closeWrite(conn)
	}()
	return readSession(conn, out)
}

func writeSession(w io.Writer, row, col uint16, argv []string) error {
	if _, err := fmt.Fprintf(w, "%d %d\n%d\n", row, col, len(argv)); err != nil {
		return err
	}
	for _, arg := range argv {
		if _, err := fmt.Fprintf(w, "%d\n", len(arg)); err != nil {
			return err
		}
		if err := writeFull(w, []byte(arg)); err != nil {
			return err
		}
	}
	return nil
}

func readSession(conn net.Conn, out *os.File) (int, error) {
	hdr := make([]byte, 5)
	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return 1, fmt.Errorf("attach: session closed")
			}
			return 1, err
		}
		n := binary.BigEndian.Uint32(hdr[1:])
		if n > 1<<20 {
			return 1, fmt.Errorf("attach: frame too large")
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return 1, err
		}
		switch hdr[0] {
		case sessData:
			if _, err := out.Write(buf); err != nil {
				return 1, err
			}
		case sessExit:
			if len(buf) != 1 {
				return 1, fmt.Errorf("attach: bad status")
			}
			return int(buf[0]), nil
		case sessErr:
			return 1, fmt.Errorf("%s", buf)
		default:
			return 1, fmt.Errorf("attach: bad frame")
		}
	}
}

func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func closeWrite(conn net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if c, ok := conn.(closeWriter); ok {
		_ = c.CloseWrite()
		return
	}
	_ = conn.Close()
}

// relayResize sends the current size once the session exists, and retries a
// dial that fails because the guest is not listening yet. The header size can
// already be stale by the time the pty is created.
func relayResize(sock string, term *os.File, sig <-chan os.Signal, stop <-chan struct{}) {
	var retry <-chan time.Time
	pending := true
	for {
		if pending {
			if resizeVsock(sock, term) == nil {
				pending = false
				retry = nil
			} else if retry == nil {
				retry = time.After(25 * time.Millisecond)
			}
		}
		select {
		case <-stop:
			return
		case <-sig:
			pending = true
			retry = nil
		case <-retry:
			retry = nil
		}
	}
}

func resizeVsock(sock string, term *os.File) error {
	conn, err := dialVsock(sock, vsockResizePort)
	if err != nil {
		return err
	}
	defer conn.Close()
	row, col := termSize(term)
	_, err = fmt.Fprintf(conn, "%d %d\n", row, col)
	return err
}

func dialVsock(sock string, port int) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		conn.Close()
		return nil, err
	}
	line, err := readLine(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Firecracker replies "OK <host-port>" and the host port is not the guest port.
	if !strings.HasPrefix(line, "OK ") {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake %q", line)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func readLine(r io.Reader) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for len(b) < 4096 {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				return string(b), nil
			}
			b = append(b, one[0])
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("line too long")
}

func termSize(f *os.File) (uint16, uint16) {
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 || ws.Row == 0 || ws.Col == 0 {
		return 24, 80
	}
	return ws.Row, ws.Col
}

func isTTY(f *os.File) bool {
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	return errno == 0
}

func ttyRaw(f *os.File) func() {
	if !isTTY(f) {
		return func() {}
	}
	cmd := exec.Command("stty", "raw", "-echo")
	cmd.Stdin = f
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return func() {}
	}
	return func() {
		cmd := exec.Command("stty", "sane")
		cmd.Stdin = f
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
	}
}
