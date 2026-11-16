package supervisor

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

type running struct {
	Wall                 string `json:"wall"`
	Box                  string `json:"box"`
	FirecrackerContainer string `json:"firecrackerContainer"`
	Dir                  string `json:"dir"`
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
		// Escape sequences from the habitat must reach the operator unchanged.
		restore := ttyRaw(in)
		defer restore()
		return attachDocker(inst.Box, argv, in, out, errOut, sig, stop)
	case "strong":
		fcContainer := inst.FirecrackerContainer
		if fcContainer == "" {
			fcContainer = "htm-fc-" + id
		}
		return attachSSH(root, id, fcContainer, argv, in, out, errOut, sig, stop)
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
	tty := isTTY(in)
	var token string
	if tty {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 1, fmt.Errorf("attach: token: %w", err)
		}
		token = hex.EncodeToString(raw[:])
		args = append(args, "-t")
	}
	args = append(args, box)
	if token != "" {
		// $0 is the token, so the operator argv and its exit status stay
		// as given. Inspect can see the token. It is not written to the I/O log.
		args = append(args, "sh", "-c", `exec "$@"`, token)
	}
	args = append(args, argv...)
	cmd := exec.Command("docker", args...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut
	if !tty {
		return waitSignal(cmd, sig, stop, nil)
	}
	// docker exec -t copies the window once, at start. A later SIGWINCH
	// on the CLI does not resize that session. ExecIDs already running,
	// including the weak-wall route install, are not this session.
	before, err := dockerExecIDs(box)
	if err != nil {
		return 1, err
	}
	var execID string
	skip := false
	return waitSignal(cmd, sig, stop, func() error {
		if skip {
			return nil
		}
		if execID == "" {
			var err error
			execID, err = oneNewExecID(box, before, token)
			if errors.Is(err, errSkipResize) {
				// This session cannot be named. Keep it and skip the API resize.
				skip = true
				return nil
			}
			if err != nil {
				return err
			}
		}
		return resizeDockerExec(execID, in)
	})
}

// noSSHServerPhrase is the text omakase/backup.sh greps in an attach error.
const noSSHServerPhrase = "no SSH server at /usr/sbin/sshd"

// AttachPort22Closed is the strong-wall error when the client reports
// Connection refused. Nothing is listening on port 22: no sshd, or sshd
// did not start.
const AttachPort22Closed = "attach: nothing listens on port 22 in the guest: the image has " +
	noSSHServerPhrase + ", or it did not start (see USAGE)"

// attachSSH runs ssh in a throwaway container that shares the Firecracker
// container network namespace. Nothing publishes the guest port. The private
// key is mounted only into that container, not into the guest.
func attachSSH(root, id, fcContainer string, argv []string, in, out, errOut *os.File, sig <-chan os.Signal, stop <-chan struct{}) (int, error) {
	if err := EnsureNetTools(root); err != nil {
		return 1, err
	}
	// stty raw clears ISIG. Ctrl-C has to interrupt the image build above.
	// Escape sequences from the habitat must reach the operator unchanged.
	restore := ttyRaw(in)
	defer restore()
	key := attachKeyPath(root, id)
	if _, err := os.Stat(key); err != nil {
		return 1, fmt.Errorf("attach: %w", err)
	}
	// Without a pty the guest command's stderr is on the client's stderr.
	// ssh -E keeps connect and auth failures in this file instead.
	logFile, err := os.CreateTemp("", "hermetarium-ssh-")
	if err != nil {
		return 1, err
	}
	logPath := logFile.Name()
	if err := logFile.Close(); err != nil {
		_ = os.Remove(logPath)
		return 1, err
	}
	defer os.Remove(logPath)
	args := []string{"run", "--rm", "-i"}
	tty := isTTY(in)
	if tty {
		args = append(args, "-t")
	}
	args = append(args,
		"--network", "container:"+fcContainer,
		"-v", key+":/key:ro",
		"-v", logPath+":/ssh.log",
		"--entrypoint", "ssh",
		NetToolsImage,
	)
	if tty {
		args = append(args, "-tt")
	}
	args = append(args,
		"-E", "/ssh.log",
		"-i", "/key",
		"-o", "IdentitiesOnly=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "UpdateHostKeys=no",
		"-o", "BatchMode=yes",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=2",
		"root@"+strongGuestIP,
		"--",
		sshRemoteCommand(argv),
	)
	cmd := exec.Command("docker", args...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut
	code, err := waitSignal(cmd, sig, stop, nil)
	if err != nil {
		return 1, err
	}
	diag, err := os.ReadFile(logPath)
	if err != nil {
		return 1, err
	}
	// Connection refused means nothing is listening on port 22: no sshd, or
	// sshd did not start. Other client errors stay as ssh wrote them.
	// Guest stderr is not in this file.
	if msg := strings.TrimSpace(string(diag)); msg != "" {
		if strings.Contains(msg, "Connection refused") {
			return 1, errors.New(AttachPort22Closed)
		}
		return 1, fmt.Errorf("attach: %s", msg)
	}
	return code, nil
}

func waitSignal(cmd *exec.Cmd, sig <-chan os.Signal, stop <-chan struct{}, resize func() error) (int, error) {
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	if resize != nil {
		if err := resize(); err != nil && !resizeGone(err) {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
			return 1, err
		}
	}
	// 404, or "cannot resize a stopped container", means this session
	// has already exited. Keep Wait's code. Other resize errors do not.
	resizeErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-sig:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGWINCH)
				}
				if resize == nil {
					continue
				}
				err := resize()
				if err == nil {
					continue
				}
				if resizeGone(err) {
					return
				}
				resizeErr <- err
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				return
			case <-stop:
				return
			}
		}
	}()
	err := cmd.Wait()
	select {
	case rerr := <-resizeErr:
		return 1, rerr
	default:
	}
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

func dockerExecIDs(box string) (map[string]struct{}, error) {
	out, err := Docker(5*time.Second, "inspect", "-f", `{{range .ExecIDs}}{{.}}{{"\n"}}{{end}}`, box)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{})
	for _, line := range strings.Fields(out) {
		ids[line] = struct{}{}
	}
	return ids, nil
}

// errSkipResize means this session's exec cannot be named, so the API
// resize is skipped. The attach still runs. With no token, more than one
// new id is ambiguous. With a token, more than one new exec carries it.
// A single named id is still resized.
var errSkipResize = errors.New("attach: skip resize")

// errExecNotFound is an exec inspect 404. That exec has already exited
// and is not this session.
var errExecNotFound = errors.New("attach: exec not found")

func oneNewExecID(box string, before map[string]struct{}, token string) (string, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		ids, err := dockerExecIDs(box)
		if err != nil {
			return "", err
		}
		var fresh string
		n := 0
		for id := range ids {
			if _, ok := before[id]; ok {
				continue
			}
			if token != "" {
				ok, err := execHasToken(id, token)
				if errors.Is(err, errExecNotFound) {
					continue
				}
				if err != nil {
					return "", err
				}
				if !ok {
					continue
				}
			}
			fresh = id
			n++
		}
		if n > 1 {
			return "", errSkipResize
		}
		if n == 1 {
			return fresh, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("attach: exec id not found")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func execHasToken(id, token string) (bool, error) {
	sock, err := dockerSocket()
	if err != nil {
		return false, err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false, fmt.Errorf("attach: docker socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := fmt.Sprintf("GET /exec/%s/json HTTP/1.0\r\nHost: docker\r\n\r\n", id)
	if _, err := io.WriteString(conn, req); err != nil {
		return false, fmt.Errorf("attach: exec inspect: %w", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("attach: exec inspect: %w", err)
	}
	if strings.Contains(status, " 404 ") {
		return false, errExecNotFound
	}
	if !strings.Contains(status, " 200 ") {
		rest, _ := io.ReadAll(io.LimitReader(br, 1024))
		return false, fmt.Errorf("attach: exec inspect: %s", strings.TrimSpace(status+string(rest)))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("attach: exec inspect: %w", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	body, err := io.ReadAll(io.LimitReader(br, 1<<20))
	if err != nil {
		return false, fmt.Errorf("attach: exec inspect: %w", err)
	}
	var parsed struct {
		ProcessConfig struct {
			Entrypoint string   `json:"entrypoint"`
			Arguments  []string `json:"arguments"`
		} `json:"ProcessConfig"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, fmt.Errorf("attach: exec inspect: %w", err)
	}
	if strings.Contains(parsed.ProcessConfig.Entrypoint, token) {
		return true, nil
	}
	for _, arg := range parsed.ProcessConfig.Arguments {
		if strings.Contains(arg, token) {
			return true, nil
		}
	}
	return false, nil
}

func dockerSocket() (string, error) {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		return "/var/run/docker.sock", nil
	}
	u, err := url.Parse(host)
	if err != nil || u.Scheme != "unix" || u.Path == "" {
		return "", fmt.Errorf("attach: DOCKER_HOST %q is not a unix socket", host)
	}
	return u.Path, nil
}

func resizeGone(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// Docker 29 answers the first resize after the exec stops with 500
	// and this text. A later call is 404. Any other 500 is still a failure.
	return strings.Contains(s, " 404 ") || strings.Contains(s, "cannot resize a stopped container")
}

func resizeDockerExec(id string, in *os.File) error {
	sock, err := dockerSocket()
	if err != nil {
		return err
	}
	row, col := termSize(in)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return fmt.Errorf("attach: docker socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := fmt.Sprintf("POST /exec/%s/resize?h=%d&w=%d HTTP/1.0\r\nHost: docker\r\n\r\n", id, row, col)
	if _, err := io.WriteString(conn, req); err != nil {
		return fmt.Errorf("attach: resize: %w", err)
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("attach: resize: %w", err)
	}
	if strings.Contains(line, " 200 ") {
		return nil
	}
	rest, _ := io.ReadAll(io.LimitReader(br, 1024))
	return fmt.Errorf("attach: resize: %s", strings.TrimSpace(line+string(rest)))
}

// ssh joins its arguments and the guest shell parses that string. Quoting
// keeps the operator's argv, including spaces and newlines.
func sshRemoteCommand(argv []string) string {
	var b strings.Builder
	b.WriteString("exec")
	for _, a := range argv {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// attachKeyDir is mode 0700 and outside the instance directory. runSquid
// mounts that directory into the gate, and the private key must not be there.
func attachKeyDir(root, id string) string {
	return filepath.Join(VarDir(root), ".attach", id)
}

func attachKeyPath(root, id string) string {
	return filepath.Join(attachKeyDir(root, id), "id_ed25519")
}

// writeAttachKey makes the habitat's key pair. The returned boot argument is
// the public key only. The private key stays in attachKeyDir.
func writeAttachKey(root, id string) (string, error) {
	return generateAttachKey(attachKeyDir(root, id))
}

func generateAttachKey(dir string) (bootArg string, err error) {
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	pubBlob := append(marshalSSHString([]byte("ssh-ed25519")), marshalSSHString(pub)...)
	var check [4]byte
	if _, err = rand.Read(check[:]); err != nil {
		return "", err
	}
	var sec bytes.Buffer
	sec.Write(check[:])
	sec.Write(check[:])
	sec.Write(marshalSSHString([]byte("ssh-ed25519")))
	sec.Write(marshalSSHString(pub))
	sec.Write(marshalSSHString(priv))
	sec.Write(marshalSSHString([]byte("hermetarium")))
	for i := byte(1); sec.Len()%8 != 0; i++ {
		sec.WriteByte(i)
	}
	var raw bytes.Buffer
	raw.WriteString("openssh-key-v1\x00")
	raw.Write(marshalSSHString([]byte("none")))
	raw.Write(marshalSSHString([]byte("none")))
	raw.Write(marshalSSHString(nil))
	var nkeys [4]byte
	binary.BigEndian.PutUint32(nkeys[:], 1)
	raw.Write(nkeys[:])
	raw.Write(marshalSSHString(pubBlob))
	raw.Write(marshalSSHString(sec.Bytes()))
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: raw.Bytes()})
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err = os.Chmod(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	if err = os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	if err = os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		return "", err
	}
	if err = os.Chmod(keyPath, 0o600); err != nil {
		return "", err
	}
	pubLine := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(pubBlob) + " hermetarium\n"
	if err = os.WriteFile(keyPath+".pub", []byte(pubLine), 0o644); err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString(pubBlob)
	if !sshKeyToken(b64) {
		return "", fmt.Errorf("attach: public key")
	}
	return "hermetarium.pub=" + b64, nil
}

func marshalSSHString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

func sshKeyToken(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '=':
		default:
			return false
		}
	}
	return true
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
