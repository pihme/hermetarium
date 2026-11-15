package supervisor

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	vsockSessionPort = 10000
	vsockResizePort  = 10001
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
		return attachVsock(filepath.Join(inst.Dir, "vsock.sock"), argv, in, out, errOut, sig, stop)
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
	if _, err := fmt.Fprintf(conn, "%d %d\n%s\n", row, col, strings.Join(argv, "\x1f")); err != nil {
		return 1, err
	}
	_ = conn.SetDeadline(time.Time{})
	go func() {
		for {
			select {
			case <-sig:
				_ = resizeVsock(sock, in)
			case <-stop:
				return
			}
		}
	}()
	go func() {
		io.Copy(conn, in)
		conn.Close()
	}()
	io.Copy(out, conn)
	return 0, nil
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
