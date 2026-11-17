package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	fcscripts "github.com/pihme/hermetarium/firecracker-scripts"
)

const (
	fcVersion = "v1.16.1"
	fcURL     = "https://github.com/firecracker-microvm/firecracker/releases/download/" + fcVersion + "/firecracker-" + fcVersion + "-x86_64.tgz"
	// Release asset firecracker-v1.16.1-x86_64.tgz.sha256.txt.
	fcSHA256 = "382a02a869e4d6d5cb14c40577f9545e8458021ea8b0b2d3fc10ec14d9c242e6"
	// Extract stamp: pinned tarball checksum, then the binary's SHA-256.
	// A missing or different stamp is not a cache hit.
	fcStampName = "firecracker.stamp"
	// Release asset kernel/6.1.188, the build from Actions run 43712585538.
	kernelURL    = "https://github.com/pihme/hermetarium/releases/download/kernel/6.1.188/vmlinux-6.1.188"
	kernelFile   = "vmlinux-6.1.188"
	kernelSHA256 = "b5f8e93cad94f04ccda9870b9959dd39b91551684c78760a17ef9eb4376475ba"
)

// fcPin is the tarball checksum EnsureStrongAssets accepts. Production
// leaves it at fcSHA256. Tests point it at a fixture so a tarball with no
// binary can be rejected without downloading the release asset.
var fcPin = fcSHA256

// freePageBalloon is the virtio-balloon device with free page reporting.
// The target size stays 0, so the balloon does not inflate. Stats polling
// stays off because the API socket is not open.
func freePageBalloon() map[string]any {
	return map[string]any{
		"amount_mib":               0,
		"deflate_on_oom":           false,
		"stats_polling_interval_s": 0,
		"free_page_reporting":      true,
	}
}

func cacheFile(root, name string) (string, error) {
	dir := CacheDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func fcScriptsDir(root string) (string, error) {
	dir := filepath.Join(CacheDir(root), "firecracker-scripts")
	if err := fcscripts.Extract(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func download(url, dest, want string) error {
	if err := requireSHA256(dest, want); err == nil {
		return nil
	}
	tmp := dest + ".part"
	if _, err := Docker(3*time.Minute, "run", "--rm",
		"-v", filepath.Dir(dest)+":/out",
		AlpineImage, "sh", "-c",
		"apk add --no-cache curl >/dev/null && curl -fL -o /out/"+filepath.Base(tmp)+" "+url+" && mv /out/"+filepath.Base(tmp)+" /out/"+filepath.Base(dest),
	); err != nil {
		return err
	}
	return requireSHA256(dest, want)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func requireSHA256(path, want string) error {
	got, err := sha256File(path)
	if err != nil {
		return err
	}
	if got != want {
		_ = os.Remove(path)
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(path))
	}
	return nil
}

func fcBinaryCurrent(bin, stamp string) (bool, error) {
	sum, err := sha256File(bin)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	b, err := os.ReadFile(stamp)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(b)) == fcSHA256+"\n"+sum, nil
}

func writeFCStamp(bin, stamp string) error {
	sum, err := sha256File(bin)
	if err != nil {
		return err
	}
	return os.WriteFile(stamp, []byte(fcSHA256+"\n"+sum+"\n"), 0o644)
}

// discardFCStampIfTarballRejected drops the extract stamp when the cached
// tarball is missing or is not the pinned release. download then replaces it.
func discardFCStampIfTarballRejected(tgz, stamp, want string) {
	if err := requireSHA256(tgz, want); err != nil {
		_ = os.Remove(stamp)
	}
}

func EnsureStrongAssets(root string) (firecracker, kernel string, err error) {
	tgz, err := cacheFile(root, "firecracker-"+fcVersion+"-x86_64.tgz")
	if err != nil {
		return "", "", err
	}
	firecracker, err = cacheFile(root, "firecracker")
	if err != nil {
		return "", "", err
	}
	kernel, err = cacheFile(root, kernelFile)
	if err != nil {
		return "", "", err
	}
	stamp, err := cacheFile(root, fcStampName)
	if err != nil {
		return "", "", err
	}
	discardFCStampIfTarballRejected(tgz, stamp, fcPin)
	if err := download(fcURL, tgz, fcPin); err != nil {
		return "", "", err
	}
	current, err := fcBinaryCurrent(firecracker, stamp)
	if err != nil {
		return "", "", err
	}
	if !current {
		// Drop the cached binary before the copy. xargs exits 0 when find
		// prints nothing, so chmod would succeed on the old file and the
		// stamp would record it as this tarball. An empty path must fail
		// the shell, and the stamp is written only after that succeeds.
		_, err = Docker(2*time.Minute, "run", "--rm",
			"-v", CacheDir(root)+":/out",
			AlpineImage, "sh", "-c",
			"apk add --no-cache tar >/dev/null && rm -f /out/firecracker && tar -xzf /out/"+filepath.Base(tgz)+" -C /tmp && bin=$(find /tmp -name 'firecracker-*x86_64' ! -name '*debug*' | head -1) && test -n \"$bin\" && cp \"$bin\" /out/firecracker && chmod +x /out/firecracker && test -f /out/firecracker",
		)
		if err != nil {
			return "", "", err
		}
		if err := writeFCStamp(firecracker, stamp); err != nil {
			return "", "", err
		}
	}
	if err := download(kernelURL, kernel, kernelSHA256); err != nil {
		return "", "", err
	}
	return firecracker, kernel, nil
}

func CreateStrong(root, id string, opts CreateOpts) (_ *StrongInstance, err error) {
	defer func() { takeGateReady(id) }()
	// A bad root file fails before any habitat directory or container.
	if opts.Rootfs != "" {
		if err := ext4Regular(opts.Rootfs); err != nil {
			return nil, err
		}
	}
	// Return before the rootfs copy. A live Firecracker container still has
	// that file open, and a second copy would replace it. The previous
	// release named it htm-fc-<id>.
	fcContainer := ContainerName(id, "fc")
	if err := refuseLiveFirecracker(root, id); err != nil {
		return nil, err
	}
	firecracker, kernel, err := EnsureStrongAssets(root)
	if err != nil {
		return nil, err
	}
	// --rootfs is the root disk. Do not pack --image into a new one.
	var cached string
	if opts.Rootfs == "" {
		cached, err = EnsureImageRootfs(root, opts.Image, diskMB(opts))
		if err != nil {
			return nil, err
		}
	}
	dir, conf, https, err := prepareHabitat(root, id, opts)
	if err != nil {
		return nil, err
	}
	src := cached
	if opts.Rootfs != "" {
		src = opts.Rootfs
	}
	if _, err := habitatRootfs(dir, src); err != nil {
		return nil, err
	}
	// The copy is in place before docker run. On a later error, remove it
	// only when htm-<id>-fc does not exist. An existing Firecracker container
	// keeps its disk.
	defer func() {
		if err != nil {
			dropUnmountedRootfs(id, dir)
			if anyContainerExists(firecrackerNames(id, dir)...) == nil {
				_ = os.RemoveAll(attachKeyDir(root, id))
			}
		}
	}()
	logPath := filepath.Join(dir, "io.jsonl")
	serialPath := filepath.Join(dir, "serial.log")
	scripts, err := fcScriptsDir(root)
	if err != nil {
		return nil, err
	}
	entrypoint := filepath.Join(scripts, "entrypoint.sh")
	// Public half only. The private key stays outside the instance directory.
	pubArg, err := writeAttachKey(root, id)
	if err != nil {
		return nil, err
	}
	plan := StrongPlan()
	bootArgs := "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/fc-init " + pubArg + " " + plan.KernelArgs()
	if https {
		bootArgs += " hermetarium.ca=" + plan.CAURL()
	}
	vm := map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": "/opt/vmlinux.bin",
			"boot_args":         bootArgs,
		},
		"drives": []map[string]any{{
			"drive_id":       "rootfs",
			"path_on_host":   "/opt/rootfs.ext4",
			"is_root_device": true,
			"is_read_only":   false,
		}},
		// No rate limit. An empty object is the virtio-rng device, so getrandom
		// does not depend on the host CPU exposing RDRAND to the guest.
		"entropy":        map[string]any{},
		"machine-config": map[string]any{"vcpu_count": cpuCount(opts), "mem_size_mib": memMiB(opts)},
		// The guest keeps mem_size_mib. It reports pages it has freed, and the
		// host kernel drops those pages. amount_mib 0 does not take memory away.
		"balloon": freePageBalloon(),
		"network-interfaces": []map[string]any{{
			"iface_id":      "eth0",
			"guest_mac":     "AA:FC:00:00:00:01",
			"host_dev_name": "tap0",
		}},
	}
	raw, err := json.MarshalIndent(vm, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "vm.json"), raw, 0o644); err != nil {
		return nil, err
	}

	fcArgs := []string{
		"run", "-d", "--name", fcContainer,
		"--device", "/dev/kvm",
		"--device", "/dev/net/tun",
		"--cap-add", "NET_ADMIN",
		// /proc/sys is read-only in this container. default/ is copied onto
		// tap0 when it appears, and the kernel uses max(all, interface).
		"--sysctl", "net.ipv4.ip_forward=1",
		"--sysctl", "net.ipv4.conf.all.rp_filter=0",
		"--sysctl", "net.ipv4.conf.default.rp_filter=0",
		"--entrypoint", "sh",
	}
	fcArgs = append(fcArgs, inboundPublish(opts.NoInbound)...)
	// The entrypoint accepts tcp/18081 from the bridge gateway unless
	// this is set. The gate file is copied unchanged and may still listen there.
	fcArgs = append(fcArgs, "-e", "GATE_IP="+plan.Gate, "-e", "GUEST_IP="+plan.Guest, "-e", "GATE_CIDR="+plan.CIDR, "-e", "GATE_PREFIX="+strconv.Itoa(plan.Prefix))
	if https {
		fcArgs = append(fcArgs, "-e", "GATE_HTTPS=1", "-e", "GATE_CA_PORT="+strconv.Itoa(GateCAPort))
	}
	if opts.NoInbound {
		fcArgs = append(fcArgs, "-e", "NO_INBOUND=1")
	}
	fcArgs = append(fcArgs,
		"-v", firecracker+":/opt/firecracker:ro",
		"-v", kernel+":/opt/vmlinux.bin:ro",
		"-v", strongRootfsVolume(dir),
	)
	fcArgs = append(fcArgs,
		"-v", entrypoint+":/entrypoint.sh:ro",
		"-v", dir+":/log",
	)
	fcArgs = append(fcArgs, TestLabelArgs(opts.TestLabel)...)
	fcArgs = append(fcArgs, NetToolsImage, "/entrypoint.sh")
	if _, err := Docker(30*time.Second, fcArgs...); err != nil {
		return nil, err
	}
	if err := waitGateRules(dir, fcContainer); err != nil {
		endStrong(id, dir)
		return nil, err
	}

	gate := ContainerName(id, "gate")
	if err := runSquid(root, gate, dir, opts.TestLabel, []string{"--network", "container:" + fcContainer}); err != nil {
		endStrong(id, dir)
		return nil, err
	}
	// Firecracker starts only after squid.pid and dns.ready. DNS stays down
	// until httpd has accepted, so the guest cannot fetch the CA before it
	// is served. A container that has already exited hides the boot error.
	inboundTimeout := 40 * time.Second
	if !containerRunning(fcContainer) {
		err := waitGuestUname(dir, inboundTimeout, func() (string, bool) {
			return containerLogs(fcContainer), containerRunning(fcContainer)
		})
		_ = DestroyStrong(id)
		return nil, err
	}
	if err := WaitSquid(dir, gate, 20*time.Second); err != nil {
		endStrong(id, dir)
		return nil, err
	}
	if https {
		if err := waitGateCA(dir, fcContainer); err != nil {
			endStrong(id, dir)
			return nil, err
		}
	}
	// Squid is listening. DNS is what lets the entrypoint boot Firecracker,
	// so a sidecar started here shares a netns where the gate is up and the
	// guest has not started.
	if err := runGateReady(root, id, fcContainer); err != nil {
		endStrong(id, dir)
		return nil, err
	}
	if err := startGateDNS(root, id, fcContainer, gate, plan.Gate, opts.TestLabel, conf); err != nil {
		if !containerRunning(fcContainer) {
			err = waitGuestUname(dir, inboundTimeout, func() (string, bool) {
				return containerLogs(fcContainer), containerRunning(fcContainer)
			})
		}
		_ = DestroyStrong(id)
		return nil, err
	}
	// No inbound port, so the serial line is the signal the guest reached
	// user space. fc-init prints it only after the certificate install, so
	// a failed install cannot look like a booted habitat.
	if opts.NoInbound {
		if err := waitGuestUname(dir, inboundTimeout, func() (string, bool) {
			return containerLogs(fcContainer), containerRunning(fcContainer)
		}); err != nil {
			_ = DestroyStrong(id)
			return nil, err
		}
	}
	url, err := finishInbound(fcContainer, opts.NoInbound, inboundTimeout)
	if err != nil {
		endStrong(id, dir)
		return nil, err
	}
	inst := &StrongInstance{
		ID: id, Wall: "strong", FirecrackerContainer: fcContainer, Gate: gate,
		GateIP: plan.Gate, BoxIP: plan.Guest,
		InboundURL: url, NoInbound: opts.NoInbound,
		LogPath: logPath, SerialPath: serialPath, Dir: dir,
	}
	if err := writeInstance(inst); err != nil {
		endStrong(id, dir)
		return nil, err
	}
	return inst, nil
}

func containerLogs(name string) string {
	stdout, stderr, _ := DockerExec(10*time.Second, "logs", name)
	return stdout + stderr
}

func containerRunning(name string) bool {
	out, err := Docker(5*time.Second, "inspect", "-f", "{{.State.Running}}", name)
	return err == nil && strings.TrimSpace(out) == "true"
}

// containerFree is nil when no container has this name. Any other docker
// error is returned so a dead daemon is not treated as absence.
func containerFree(name string) error {
	_, err := Docker(5*time.Second, "inspect", "-f", "{{.Id}}", name)
	if err == nil {
		return fmt.Errorf("%s already exists", name)
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such object") {
		return nil
	}
	return err
}

// strongRootfsVolume is the Firecracker container bind. The host path is this
// habitat's copy, var/<id>/rootfs.ext4, not the shared cache.
func strongRootfsVolume(dir string) string {
	return habitatRootfsPath(dir) + ":/opt/rootfs.ext4"
}

// endStrong removes the private rootfs only after every Firecracker container
// for this id is gone, including the pre-rename name. DestroyStrong takes
// only an id, and tests pass a different root, so the caller supplies the
// instance directory. The cache is not in that directory.
func endStrong(id, dir string) {
	_ = DestroyStrong(id)
	dropUnmountedRootfs(id, dir)
	if dir == "" || anyContainerExists(firecrackerNames(id, dir)...) != nil {
		return
	}
	_ = os.RemoveAll(attachKeyDir(filepath.Dir(filepath.Dir(dir)), id))
}

// firecrackerNames is this habitat's Firecracker container only.
// A pre-rename name is included when the instance file stored it, or when
// that container's /log is this habitat. htm-gate-y-fc is habitat gate-y's
// container, not the legacy gate of y-fc.
func firecrackerNames(id, dir string) []string {
	stored := ""
	if dir != "" {
		if inst, err := readInstanceFile(dir); err == nil {
			stored = inst.FirecrackerContainer
		}
	}
	return habitatContainerNames(id, "fc", stored, dir)
}

// habitatContainerNames is the current name and the pre-rename name, each
// kept only when it belongs to this habitat. The two schemes are each
// collision-free; mixed, legacyContainerName("y-fc", "gate") is
// ContainerName("gate-y", "fc").
func habitatContainerNames(id, part, stored, dir string) []string {
	var names []string
	for _, name := range []string{ContainerName(id, part), legacyContainerName(id, part), stored} {
		if name == "" || containsName(names, name) {
			continue
		}
		if containerBelongs(name, stored, dir) {
			names = append(names, name)
		}
	}
	return names
}

func containsName(names []string, name string) bool {
	for _, have := range names {
		if have == name {
			return true
		}
	}
	return false
}

// containerBelongs is true when this instance recorded the name, or the
// container's /log mount is this habitat's directory.
func containerBelongs(name, stored, dir string) bool {
	if name == "" {
		return false
	}
	if stored != "" && name == stored {
		return true
	}
	return logDirIs(name, dir)
}

// logDirIs reports whether name's /log mount is dir. A missing container
// is not a match. This is the same mount check as omakase/backup.sh.
func logDirIs(name, dir string) bool {
	if name == "" || dir == "" {
		return false
	}
	out, err := Docker(5*time.Second, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Destination "/log"}}{{.Source}}{{end}}{{end}}`, name)
	if err != nil {
		return false
	}
	got := strings.TrimSpace(out)
	if got == "" {
		return false
	}
	return samePath(got, dir)
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	if x, err := filepath.EvalSymlinks(aa); err == nil {
		aa = x
	}
	if x, err := filepath.EvalSymlinks(bb); err == nil {
		bb = x
	}
	return aa == bb
}

// refuseLiveFirecracker fails create when this habitat's Firecracker
// container is still running. The instance file may still name htm-fc-<id>.
func refuseLiveFirecracker(root, id string) error {
	dir := filepath.Join(VarDir(root), id)
	stored := ""
	if inst, err := loadInstance(root, id); err == nil {
		stored = inst.FirecrackerContainer
		if inst.Dir != "" {
			dir = inst.Dir
		}
	}
	return anyContainerExists(habitatContainerNames(id, "fc", stored, dir)...)
}

// anyContainerExists is nil when every named container is absent. An empty
// name is skipped. A docker failure is returned, so a dead daemon is not
// treated as absence.
func anyContainerExists(names ...string) error {
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := containerFree(name); err != nil {
			return err
		}
	}
	return nil
}

// dropUnmountedRootfs removes the private rootfs when no Firecracker container
// has this id. The cache is not in dir.
func dropUnmountedRootfs(id, dir string) {
	if anyContainerExists(firecrackerNames(id, dir)...) != nil {
		return
	}
	removePrivateRootfs(dir)
}

func removePrivateRootfs(dir string) {
	path := habitatRootfsPath(dir)
	_ = os.Remove(path)
	// A killed copy leaves the partial. Remove does not follow a symlink.
	_ = os.Remove(path + ".partial")
}

func WaitStrongSerial(inst *StrongInstance, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stdout := containerLogs(inst.FirecrackerContainer)
		_ = os.WriteFile(inst.SerialPath, []byte(stdout), 0o644)
		leak := strings.Contains(stdout, "LEAK_OK") || strings.Contains(stdout, "LEAK_FAIL")
		if leak {
			return stdout, nil
		}
		if !containerRunning(inst.FirecrackerContainer) {
			return stdout + extraFC(inst), fmt.Errorf("Firecracker container exited before leak check finished")
		}
		time.Sleep(500 * time.Millisecond)
	}
	stdout := containerLogs(inst.FirecrackerContainer) + extraFC(inst)
	_ = os.WriteFile(inst.SerialPath, []byte(stdout), 0o644)
	return stdout, fmt.Errorf("timeout waiting for strong-wall serial")
}

func extraFC(inst *StrongInstance) string {
	p := filepath.Join(inst.Dir, "fc.log")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return "\n--- fc.log ---\n" + string(b)
}

// waitGuestUname blocks until fc-init prints GUEST_UNAME= on the Firecracker
// container serial console. A dead or missing container fails immediately.
func waitGuestUname(dir string, timeout time.Duration, console func() (string, bool)) error {
	deadline := time.Now().Add(timeout)
	var text string
	var running bool
	for {
		text, running = console()
		if strings.Contains(text, "GUEST_UNAME=") {
			return nil
		}
		if !running {
			// The marker can be written as the Firecracker container exits, after the read above.
			text, _ = console()
			if strings.Contains(text, "GUEST_UNAME=") {
				return nil
			}
		}
		if !running || !time.Now().Before(deadline) {
			parts := make([]string, 0, 2)
			if fc := strings.TrimSpace(tailHostFile(filepath.Join(dir, "fc.log"))); fc != "" {
				parts = append(parts, fc)
			}
			serial := strings.TrimSpace(tailHostFile(filepath.Join(dir, "serial.log")))
			if serial == "" {
				if len(text) > 4000 {
					text = text[len(text)-4000:]
				}
				serial = strings.TrimSpace(text)
			}
			if serial != "" {
				parts = append(parts, serial)
			}
			return fmt.Errorf("guest did not boot: %s", strings.Join(parts, "\n"))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func DestroyStrong(id string) error {
	DockerIgnore("rm", "-f", "htm-probe-"+id)
	DockerIgnore("rm", "-f", "htm-mock-"+id)
	stopGateDNS(id)
	removeGate(id)
	stored, dir := "", ""
	if root, err := Root(); err == nil {
		if inst, err := loadInstance(root, id); err == nil {
			stored = inst.FirecrackerContainer
			dir = inst.Dir
		}
	}
	names := habitatContainerNames(id, "fc", stored, dir)
	dockerRemove("-f", names...)
	if root, err := Root(); err == nil && anyContainerExists(names...) == nil {
		_ = os.RemoveAll(attachKeyDir(root, id))
	}
	return nil
}
