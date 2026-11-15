package fchelper

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}
	ep := filepath.Join(dir, "entrypoint.sh")
	pack := filepath.Join(dir, "pack-oci.sh")
	for _, p := range []string{ep, pack} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", p, st.Mode())
		}
	}
	b, err := os.ReadFile(ep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "fc-helper") {
		t.Fatalf("entrypoint.sh missing fc-helper marker:\n%s", b)
	}
	if strings.Contains(string(b), "--no-seccomp") {
		t.Fatal("entrypoint.sh disables Firecracker seccomp")
	}
	if strings.Contains(string(b), "vsock") {
		t.Fatal("entrypoint.sh still builds a vsock terminal")
	}
	b, err = os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "rootfs.ext4") {
		t.Fatalf("pack-oci.sh missing rootfs.ext4:\n%s", b)
	}
	if !strings.Contains(string(b), "mke2fs -t ext4 -F -d ") || !strings.Contains(string(b), "lazy_itable_init=1,lazy_journal_init=1") {
		t.Fatal("pack-oci.sh must build a sparse ext4 image with mke2fs -d")
	}
	if strings.Contains(string(b), "umount") {
		t.Fatal("pack-oci.sh mounts the root disk")
	}
	if !strings.Contains(string(b), "/bin/busybox wget") || !strings.Contains(string(b), "timeout 3 curl") {
		t.Fatal("pack-oci.sh CA fetch must not call curl without a timeout")
	}
	if strings.Contains(string(b), "vsock") || strings.Contains(string(b), "fc-attach") {
		t.Fatal("pack-oci.sh still builds a vsock terminal")
	}
	init := fcInitScript(t, string(b))
	pts := strings.Index(init, "/dev/pts")
	ip := strings.Index(init, "172.16.0.2/24")
	priv := strings.Index(init, "mkdir -p /root/.ssh /var/empty /run/sshd")
	mode := strings.Index(init, "chmod 755 /run/sshd")
	start := strings.Index(init, "/usr/sbin/sshd -f /fc-sshd.conf")
	uname := strings.Index(init, "GUEST_UNAME=")
	execAt := strings.LastIndex(init, "\nexec $cmd")
	if pts < 0 || ip < 0 || priv < 0 || mode < 0 || start < 0 || uname < 0 || execAt < 0 || !(pts < ip && ip < priv && priv < mode && mode < start && start < uname && uname < execAt) {
		t.Fatal("sshd must start after the guest address and /run/sshd, and before the inhabitant")
	}
	if !strings.Contains(init, `[ -x /usr/sbin/sshd ]`) {
		t.Fatal("sshd must be skipped when the image has no server")
	}
	if strings.Count(init, ". /fc-env") != 1 {
		t.Fatal("/fc-env sourced more than once")
	}
	failAt := strings.Index(init, "echo DATA_MOUNT_FAIL")
	exitAt := strings.LastIndex(init, "exit 1")
	envAt := strings.Index(init, ". /fc-env")
	if strings.Contains(init, "|| echo DATA_MOUNT_FAIL") || failAt < 0 || exitAt < failAt || envAt < exitAt || execAt < exitAt {
		t.Fatal("data mount failure must stop before the inhabitant")
	}
	if !strings.Contains(string(b), "echo DATA_MOUNT_OK") {
		t.Fatal("pack-oci.sh missing DATA_MOUNT_OK")
	}
	if !strings.Contains(string(b), "nameserver 172.16.0.1") || !strings.Contains(string(b), "127.0.0.1 localhost") {
		t.Fatal("pack-oci.sh missing gate resolver or localhost")
	}
	if !strings.Contains(string(b), "GUEST_RNG=") || !strings.Contains(string(b), "/fc-getrandom") || !strings.Contains(string(b), "echo GUEST_GETRANDOM_OK") || !strings.Contains(string(b), "echo GUEST_GETRANDOM_SKIPPED") {
		t.Fatal("pack-oci.sh must record virtio-rng and a bounded getrandom")
	}
	if !strings.Contains(string(b), "9b: 75 03") || !strings.Contains(string(b), "jne 0xa0") {
		t.Fatal("pack-oci.sh getrandom listing is not the failure path")
	}
	var blob string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "f0VMRg") {
			blob = line
			break
		}
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		t.Fatal(err)
	}
	// Listing: jne at file offset 0x9b is 75 03, and 0xa0 is mov rax, 0x3c.
	if len(raw) < 0xa8 || raw[0x9b] != 0x75 || raw[0x9c] != 0x03 || raw[0xa0] != 0x48 || raw[0xa1] != 0xc7 {
		t.Fatalf("getrandom failure path: %x", raw)
	}

	st1, err := os.Stat(ep)
	if err != nil {
		t.Fatal(err)
	}
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(ep)
	if err != nil {
		t.Fatal(err)
	}
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("second Extract changed mtime of unchanged script")
	}
}

const fakeCA = "hermetarium-test-ca\n"

const caFetchFail = "fc-init: could not fetch the gate CA"

const busyboxNoWget = `#!/bin/sh
set -u
printf 'call %s\n' "$*" >> "$LOG"
if [ "$1" = "timeout" ]; then
  shift 2
  "$@"
  exit $?
fi
if [ "$1" = "wget" ]; then
  printf 'applet wget\n' >> "$LOG"
fi
echo "applet not found" >&2
exit 127
`

const timeoutShim = `#!/bin/sh
set -u
printf 'timeout %s\n' "$*" >> "$LOG"
shift
exec "$@"
`

const curlWritesCA = `#!/bin/sh
set -u
printf 'curl-ran %s\n' "$*" >> "$LOG"
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    out=$2
    shift 2
    continue
  fi
  shift
done
if [ -z "$out" ]; then
  echo "curl: missing -o" >&2
  exit 2
fi
printf '%s' "$CA_BODY" > "$out"
`

func TestGateCAFallsThroughToCurl(t *testing.T) {
	dir := t.TempDir()
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile(filepath.Join(dir, "pack-oci.sh"))
	if err != nil {
		t.Fatal(err)
	}
	snippet := gateCASnippet(t, string(pack))

	t.Run("curl", func(t *testing.T) {
		root := t.TempDir()
		bin := filepath.Join(root, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		writeExe(t, filepath.Join(bin, "busybox"), busyboxNoWget)
		writeExe(t, filepath.Join(bin, "timeout"), timeoutShim)
		writeExe(t, filepath.Join(bin, "curl"), curlWritesCA)
		linkExe(t, bin, "mkdir")
		linkExe(t, bin, "cp")
		stdout, calls := runGateCA(t, root, bin, snippet)
		if stdout != "" {
			t.Fatalf("stdout = %q, want empty", stdout)
		}
		ssl := filepath.Join(root, "ssl", "hermetarium-mitm.crt")
		installed := filepath.Join(root, "ca-certificates", "hermetarium-mitm.crt")
		for _, p := range []string{ssl, installed} {
			got, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fakeCA {
				t.Fatalf("%s = %q, want gate CA from curl", p, got)
			}
		}
		w := strings.Index(calls, "applet wget")
		c := strings.Index(calls, "curl-ran")
		if w < 0 || c < 0 || w > c {
			t.Fatalf("curl was not tried after busybox wget:\n%s", calls)
		}
		if !strings.Contains(calls, "timeout 3 curl") {
			t.Fatalf("curl ran without timeout:\n%s", calls)
		}
		if !strings.Contains(calls, "http://172.16.0.1:3131/ca.pem") {
			t.Fatalf("curl did not fetch the gate CA URL:\n%s", calls)
		}
	})

	t.Run("log", func(t *testing.T) {
		root := t.TempDir()
		bin := filepath.Join(root, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		writeExe(t, filepath.Join(bin, "busybox"), busyboxNoWget)
		linkExe(t, bin, "mkdir")
		linkExe(t, bin, "cp")
		stdout, calls := runGateCA(t, root, bin, snippet)
		if stdout != caFetchFail+"\n" {
			t.Fatalf("stdout = %q, want one serial line", stdout)
		}
		if strings.Contains(calls, "curl-ran") {
			t.Fatalf("curl ran without a curl on PATH:\n%s", calls)
		}
		if _, err := os.Stat(filepath.Join(root, "ca-certificates", "hermetarium-mitm.crt")); !os.IsNotExist(err) {
			t.Fatalf("installed CA without a fetch: %v", err)
		}
	})
}

func fcInitScript(t *testing.T, pack string) string {
	t.Helper()
	const start = "cat > /rootfs/fc-init << 'EOF'\n"
	i := strings.Index(pack, start)
	if i < 0 {
		t.Fatal("pack-oci.sh has no fc-init heredoc")
	}
	rest := pack[i+len(start):]
	j := strings.Index(rest, "\nEOF\n")
	if j < 0 {
		t.Fatal("pack-oci.sh fc-init heredoc is not closed")
	}
	return rest[:j]
}

func gateCASnippet(t *testing.T, pack string) string {
	t.Helper()
	const start = "cat > /rootfs/fc-init << 'EOF'\n"
	i := strings.Index(pack, start)
	if i < 0 {
		t.Fatal("pack-oci.sh has no fc-init heredoc")
	}
	rest := pack[i+len(start):]
	j := strings.Index(rest, "\nEOF\n")
	if j < 0 {
		t.Fatal("pack-oci.sh fc-init heredoc is not closed")
	}
	init := rest[:j]
	const mark = "mkdir -p /etc/ssl /usr/local/share/ca-certificates\n"
	k := strings.Index(init, mark)
	if k < 0 {
		t.Fatal("fc-init has no gate CA fetch")
	}
	fetch := init[k:]
	end := strings.Index(fetch, "\ncmd=$(cat /fc-cmd)\n")
	if end < 0 {
		t.Fatal("fc-init gate CA fetch does not stop before /fc-cmd")
	}
	return fetch[:end+1]
}

func runGateCA(t *testing.T, root, bin, snippet string) (string, string) {
	t.Helper()
	ssl := filepath.Join(root, "ssl")
	certs := filepath.Join(root, "ca-certificates")
	busybox := filepath.Join(bin, "busybox")
	script := strings.ReplaceAll(snippet, "/bin/busybox", busybox)
	script = strings.ReplaceAll(script, "/usr/local/share/ca-certificates", certs)
	script = strings.ReplaceAll(script, "/etc/ssl", ssl)
	// The fake busybox path itself ends in /bin/busybox.
	if strings.Contains(strings.ReplaceAll(script, busybox, ""), "/bin/busybox") {
		t.Fatalf("snippet still calls host busybox:\n%s", script)
	}
	for _, hostPath := range []string{"/etc/ssl", "/usr/local/share/ca-certificates"} {
		if strings.Contains(script, hostPath) {
			t.Fatalf("snippet still references %s:\n%s", hostPath, script)
		}
	}
	path := filepath.Join(root, "ca.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "calls.log")
	cmd := exec.Command("sh", path)
	cmd.Dir = root
	cmd.Env = []string{
		"PATH=" + bin,
		"LOG=" + logPath,
		"CA_BODY=" + fakeCA,
		"HOME=" + root,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("snippet: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), string(calls)
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func linkExe(t *testing.T, dir, name string) {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}
