// Guest end of hermetarium attach. Listens on Firecracker vsock and
// runs the operator's command on a pty. Started from fc-init.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	afVsock       = 40
	vmaddrCIDAny  = 0xffffffff
	sessionPort   = 10000
	resizePort    = 10001
	sockaddrVMLen = 16

	// One type byte on the session connection. Exit status is not pty data.
	sessData byte = 0
	sessExit byte = 1
	sessErr  byte = 2

	// Wake pipe. The child was reaped, or the operator closed stdin.
	wakeExit   byte = 1
	wakeHangup byte = 2
)

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

var (
	activeMu sync.Mutex
	activeFD = -1
	// Latest size that arrived before a pty was published.
	haveSize bool
	sizeRow  uint16
	sizeCol  uint16
)

func main() {
	// fc-init starts this before a login shell. An empty PATH would hide sh.
	if os.Getenv("PATH") == "" {
		os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	if os.Getenv("TERM") == "" {
		os.Setenv("TERM", "xterm")
	}
	// virtio-vsock can show up after userspace has started.
	sess, err := listenRetry(sessionPort)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fc-attach:", err)
		os.Exit(1)
	}
	rsz, err := listenRetry(resizePort)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fc-attach:", err)
		os.Exit(1)
	}
	go serveResize(rsz)
	for {
		fd, err := acceptRaw(sess)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		handle(os.NewFile(uintptr(fd), "vsock"))
	}
}

func listenRetry(port uint32) (int, error) {
	var err error
	for i := 0; i < 50; i++ {
		var fd int
		fd, err = listenVsock(port)
		if err == nil {
			return fd, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return -1, err
}

func listenVsock(port uint32) (int, error) {
	fd, err := syscall.Socket(afVsock, syscall.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	// syscall.Bind needs a Sockaddr, and that interface cannot be
	// implemented outside the syscall package.
	addr := sockaddrVM(port, vmaddrCIDAny)
	_, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&addr[0])), uintptr(len(addr)))
	if errno != 0 {
		syscall.Close(fd)
		return -1, errno
	}
	if err := syscall.Listen(fd, 4); err != nil {
		syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func sockaddrVM(port, cid uint32) [sockaddrVMLen]byte {
	var addr [sockaddrVMLen]byte
	*(*uint16)(unsafe.Pointer(&addr[0])) = afVsock
	*(*uint32)(unsafe.Pointer(&addr[4])) = port
	*(*uint32)(unsafe.Pointer(&addr[8])) = cid
	return addr
}

func acceptRaw(fd int) (int, error) {
	// syscall.Accept closes families it does not know, including AF_VSOCK.
	nfd, _, errno := syscall.Syscall6(syscall.SYS_ACCEPT4, uintptr(fd), 0, 0, uintptr(syscall.SOCK_CLOEXEC), 0, 0)
	if errno != 0 {
		return -1, errno
	}
	return int(nfd), nil
}

func serveResize(fd int) {
	for {
		cfd, err := acceptRaw(fd)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		conn := os.NewFile(uintptr(cfd), "resize")
		line, err := readLine(conn)
		conn.Close()
		if err != nil {
			continue
		}
		row, col, ok := parseSize(line)
		if !ok {
			continue
		}
		resizeActive(row, col)
	}
}

func resizeActive(row, col uint16) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if activeFD < 0 {
		sizeRow, sizeCol = row, col
		haveSize = true
		return
	}
	applyWinsize(activeFD, row, col)
}

func publishActive(fd int) {
	activeMu.Lock()
	defer activeMu.Unlock()
	activeFD = fd
	if haveSize {
		applyWinsize(fd, sizeRow, sizeCol)
		haveSize = false
	}
}

// dropActive forgets fd only when it is still this session's, and closes it
// before any later resize can ioctl a descriptor the kernel has reused.
func dropActive(fd int) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if activeFD == fd {
		activeFD = -1
	}
	syscall.Close(fd)
}

func applyWinsize(fd int, row, col uint16) {
	ws := winsize{Row: row, Col: col}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

func handle(conn *os.File) {
	defer conn.Close()
	hdr, err := readLine(conn)
	if err != nil {
		return
	}
	argv, err := readArgs(conn)
	if err != nil {
		return
	}
	row, col, ok := parseSize(hdr)
	if !ok {
		row, col = 24, 80
	}
	if len(argv) == 0 || argv[0] == "" {
		argv = []string{"sh"}
	}
	master, raw, slave, err := openPTY(row, col)
	if err != nil {
		writeErr(conn, err)
		return
	}
	ptyClosed := false
	closePTY := func() {
		if ptyClosed {
			return
		}
		ptyClosed = true
		dropActive(raw)
		master.Close()
	}
	defer closePTY()
	defer slave.Close()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	// The parent slave stays open until the child is reaped and the master
	// is drained. Closing the last slave drops bytes still queued.
	var wake [2]int
	if err := syscall.Pipe2(wake[:], syscall.O_CLOEXEC|syscall.O_NONBLOCK); err != nil {
		writeErr(conn, err)
		return
	}
	defer syscall.Close(wake[0])
	defer syscall.Close(wake[1])
	publishActive(raw)
	if err := cmd.Start(); err != nil {
		writeErr(conn, err)
		return
	}
	waitCh := make(chan int, 1)
	go func() {
		waitCh <- exitStatus(cmd.Wait())
		_, _ = syscall.Write(wake[1], []byte{wakeExit})
	}()
	// A detached session can keep the slave after this command exits, so a
	// blocked pty read would never notice. The operator closing stdin is
	// the other reason to stop.
	go func() {
		io.Copy(master, conn)
		_, _ = syscall.Write(wake[1], []byte{wakeHangup})
	}()
	copyPTY(conn, raw, wake[0], cmd)
	// Master close delivers SIGHUP if the command is still running.
	closePTY()
	_ = writeFrame(conn, sessExit, []byte{byte(<-waitCh)})
}

func openPTY(row, col uint16) (master *os.File, raw int, slave *os.File, err error) {
	mfd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, -1, nil, err
	}
	raw, err = syscall.Dup(mfd)
	if err != nil {
		syscall.Close(mfd)
		return nil, -1, nil, err
	}
	syscall.CloseOnExec(raw)
	unlock := int32(0)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(mfd), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		syscall.Close(mfd)
		syscall.Close(raw)
		return nil, -1, nil, errno
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(mfd), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		syscall.Close(mfd)
		syscall.Close(raw)
		return nil, -1, nil, errno
	}
	sfd, err := syscall.Open("/dev/pts/"+strconv.FormatUint(uint64(n), 10), syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		syscall.Close(mfd)
		syscall.Close(raw)
		return nil, -1, nil, err
	}
	ws := winsize{Row: row, Col: col}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(mfd), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
	return os.NewFile(uintptr(mfd), "ptmx"), raw, os.NewFile(uintptr(sfd), "pts"), nil
}

func copyPTY(conn *os.File, raw, wake int, cmd *exec.Cmd) {
	ep, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		return
	}
	defer syscall.Close(ep)
	if err := epollAdd(ep, raw); err != nil {
		return
	}
	if err := epollAdd(ep, wake); err != nil {
		return
	}
	buf := make([]byte, 4096)
	ev := make([]syscall.EpollEvent, 2)
	ptyOpen := true
	signaled := false
	for {
		n, err := syscall.EpollWait(ep, ev, -1)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return
		}
		sawWake := false
		sawPTY := false
		for i := 0; i < n; i++ {
			switch int(ev[i].Fd) {
			case wake:
				sawWake = true
			case raw:
				sawPTY = true
			}
		}
		if sawPTY && ptyOpen {
			ok, alive := readPTY(conn, raw, buf)
			if !ok {
				return
			}
			if !alive {
				ptyOpen = false
				_ = syscall.EpollCtl(ep, syscall.EPOLL_CTL_DEL, raw, nil)
			}
		}
		if !sawWake {
			continue
		}
		// Queued output has to be forwarded before this wake ends the session.
		if !drainPTY(conn, raw, buf) {
			return
		}
		exited, hangup := consumeWake(wake)
		if !drainPTY(conn, raw, buf) {
			return
		}
		if exited {
			return
		}
		if hangup && !signaled {
			signaled = true
			signalHangup(cmd)
		}
	}
}

func readPTY(conn *os.File, raw int, buf []byte) (ok, alive bool) {
	nr, err := syscall.Read(raw, buf)
	if nr > 0 {
		if werr := writeFrame(conn, sessData, buf[:nr]); werr != nil {
			return false, true
		}
	}
	if err == syscall.EINTR || err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
		return true, true
	}
	if err != nil || nr == 0 {
		return true, false
	}
	return true, true
}

// drainPTY reads every byte already queued. The fd stays blocking; TIOCINQ
// is how many bytes Read can take without waiting for a later write.
func drainPTY(conn *os.File, raw int, buf []byte) bool {
	for {
		var queued int32
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(raw), syscall.TIOCINQ, uintptr(unsafe.Pointer(&queued)))
		if errno != 0 || queued <= 0 {
			return true
		}
		chunk := int(queued)
		if chunk > len(buf) {
			chunk = len(buf)
		}
		nr, err := syscall.Read(raw, buf[:chunk])
		if nr > 0 {
			if werr := writeFrame(conn, sessData, buf[:nr]); werr != nil {
				return false
			}
		}
		if err == syscall.EINTR {
			continue
		}
		if nr == 0 || err != nil {
			return true
		}
	}
}

func consumeWake(fd int) (exited, hangup bool) {
	var buf [8]byte
	for {
		n, err := syscall.Read(fd, buf[:])
		if n > 0 {
			for i := 0; i < n; i++ {
				switch buf[i] {
				case wakeExit:
					exited = true
				case wakeHangup:
					hangup = true
				}
			}
			continue
		}
		if err == syscall.EINTR {
			continue
		}
		return exited, hangup
	}
}

func signalHangup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}
	// The master stays open so bytes written as the command dies can still be read.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
}

func epollAdd(ep, fd int) error {
	ev := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
	return syscall.EpollCtl(ep, syscall.EPOLL_CTL_ADD, fd, &ev)
}

func parseSize(line string) (uint16, uint16, bool) {
	var row, col uint16
	if _, err := fmt.Sscanf(line, "%d %d", &row, &col); err != nil || row == 0 || col == 0 {
		return 0, 0, false
	}
	return row, col, true
}

func readArgs(r io.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 0 || n > 4096 {
		return nil, fmt.Errorf("argv count")
	}
	argv := make([]string, n)
	for i := 0; i < n; i++ {
		line, err = readLine(r)
		if err != nil {
			return nil, err
		}
		sz, err := strconv.Atoi(line)
		if err != nil || sz < 0 || sz > 1<<20 {
			return nil, fmt.Errorf("argv length")
		}
		buf := make([]byte, sz)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		argv[i] = string(buf)
	}
	return argv, nil
}

func writeErr(w io.Writer, err error) {
	fmt.Fprintln(os.Stderr, "fc-attach:", err)
	_ = writeFrame(w, sessErr, []byte("ATTACH_ERR "+err.Error()))
}

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return 1
	}
	if code := ee.ExitCode(); code >= 0 && code <= 255 {
		return code
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		sig := int(ws.Signal())
		if sig > 0 && 128+sig <= 255 {
			return 128 + sig
		}
	}
	return 1
}

func writeFrame(w io.Writer, typ byte, payload []byte) error {
	var hdr [5]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if err := writeFull(w, hdr[:]); err != nil {
		return err
	}
	return writeFull(w, payload)
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
