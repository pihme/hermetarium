# Test spec

What the suite checks in this tree today. Product rules for tests stay in [SPEC.md §11](SPEC.md#11-tests). This file is the inventory of the running tests.

## Commands

| Command | Packages | Gate |
| --- | --- | --- |
| `make test` | `./supervisor` `./supervisor/gatedns` `./firecracker-scripts` `./examples/...` `./porter` `./tests/...` | Merge gate. CI job `test` on push to `main`, pull request, and `workflow_dispatch`. |
| `make test-live` | `./tests/agentd` `./tests/inhabitants` with `-tags live` | Optional. CI job `test-live` on `workflow_dispatch` only. |

Both use `go test -count=1 -p 1 -timeout 15m -v`. Live files are `//go:build live`, so `make test` does not compile them. `make test-live` also runs the untagged tests in those two packages (agentd mock on both walls, official-CLI smoke).

Needs Docker and Go 1.24+. Strong-wall cases need `/dev/kvm` and x86_64. Data root is `HERMETARIUM_ROOT`, else this checkout.

Habitats are created through the supervisor Go API. Chat and echo are HTTP POST via `supervisor.Call` to the inbound URL. On the weak wall, `ExecWeak` runs the leak ping, the probe curl, `printenv`, and the official CLI version command. On the strong wall, `WaitStrongSerial` supplies `GUEST_UNAME` and `LEAK_FAIL`.

## Fixtures

`tests/harness` builds images and fills a temp ACL before `create`.

- Echo and agentd: `hermetarium-echo:local` and `hermetarium-agentd:local`, 128 MiB / 256 MB, ACL from `examples/<name>/squid.conf`.
- Official CLIs: `hermetarium-in-claude:local`, `hermetarium-in-grok:local`, and `hermetarium-in-opencode:local`, 2048 MiB / 2048 MB, ACL from `inhabitants/<name>/squid.conf`. Porter is copied into the image build.
- Mock runs replace the habitat `cache_peer` with `127.0.0.1 parent 18082` and `__VENDOR_KEY__` with `htm-test-key`. Live runs keep the real vendor peer and substitute `HERMETARIUM_ANTHROPIC_API_KEY` or `HERMETARIUM_XAI_API_KEY`. The supervisor does not read those variables. The OpenCode mock fills `__MODEL_PEER__`, `__MODEL_HOST__`, `__MODEL_PREFIX__` (empty), and `__MODEL_KEY__` (`htm-test-key`) instead. There is no OpenCode live case.
- Echo creates start a probe httpd on the gate (`tests/probe`, port 18080, body `hermetarium-ok`). Mock creates start `tests/vendormock` on the gate. The mock requires `x-api-key` or `Authorization: Bearer` equal to `htm-test-key`. `/v1/messages` and `/v1/chat/completions` return a bash tool call `id -u`, then the text `0` after a tool result.

## `make test`

### Supervisor, Firecracker scripts, porter, examples

In-process. No Docker habitat.

| Test | Checks |
| --- | --- |
| `TestResolveCreate` | `create` requires `--image` and `--acl`. Both flags parse into the exec struct. Defaults are 512 MiB and 1024 MB. `--mem 2048 --disk 4096` parses. `--mem 0` and a non-integer `--disk` fail. `--cpus` is unset unless the flag is set. `--cpus 4` parses. `--cpus 0` fails. `--no-inbound` is off unless the flag is set. |
| `TestCPUs` | Needs Docker and KVM. `--cpus 4` is a CPU quota of 4 in the Docker box (`cpu.max`) and four processors in the strong-wall guest. |
| `TestSparseRootDisk` | Needs Docker. A 50 GB root disk is sparse (little allocated space) and is not noticeably slower to create than a 1 GB disk. |
| `TestHabitatRootfsIsPrivate` | Two strong-habitat rootfs files copied from one cache, and a third copied afterwards, are different paths. A write in the first is not in the others or in the cache. The copy stays sparse. The Firecracker container's bind source is `var/<id>/rootfs.ext4`, not the cache path. |
| `TestHabitatRootfsReplacesSymlink` | A symlink at `var/<id>/rootfs.ext4` is replaced by the copy. The cache bytes stay. The result is a regular file mode `0600`, and no `rootfs.ext4.partial` is left. |
| `TestHabitatRootfsFailureKeepsDest` | When the rename onto `rootfs.ext4` fails, only `rootfs.ext4.partial` is removed. A `rootfs.ext4.partial` symlink is not opened, so the cache is not truncated. |
| `TestHabitatRootfsStalePartial` | A regular file at `rootfs.ext4.partial` makes the copy fail with `os.ErrExist` and text containing `run hermetarium destroy`. The cache file is unchanged. |
| `TestRemovePrivateRootfs` | Removing the private disk deletes `var/<id>/rootfs.ext4`, `rootfs.ext4.partial` (a regular file or a symlink, without following it), and a symlink at `rootfs.ext4`. It leaves a neighboring data file, `rootfs-<image>-<size>.ext4`, and an unsuffixed `rootfs-<id>.ext4`. |
| `TestDropUnmountedRootfs` | Needs Docker, not KVM. When no `htm-fc-<id>` exists, the failed-create cleanup removes `var/<id>/rootfs.ext4` and `rootfs.ext4.partial`. When that container exists, both stay. The cache, an unsuffixed `rootfs-<id>.ext4`, and a neighboring data file stay either way. |
| `TestNoInboundURLMessage` | `noInbound: true` is the sentence `this habitat has no inbound address`. A stored URL is returned. An empty URL without `noInbound` is an error. |
| `TestNoInbound` | Needs Docker and KVM. An image that only sleeps starts on both walls with `--no-inbound` and a gate file that has no inhabitant peer and no inbound port. No host port is published. `url` prints that sentence. |
| `TestNoInboundGatePort` | Needs Docker and KVM. A gate file that still listens on 18081, with `--no-inbound`, does not accept that port from the bridge gateway on either wall. Squid is listening on 127.0.0.1:18081. Without the flag, 18081 is published and the bridge gateway can connect. |
| `TestModelGate` | Needs Docker and KVM. On both walls the box calls `http://model.hermetarium.test/v1/chat/completions` with a placeholder key. The mock sees exactly one `Authorization` value, the gate file's key, plus that file's `Host` and path prefix. Streamed lines arrive with a gap, not in one piece. The key is not in the box environment or filesystem. The box cannot reach the mock on the gate address. |
| `TestWaitGuestUname` | A serial log containing `GUEST_UNAME=` makes the strong-wall boot wait return nil. A log without that line and a dead Firecracker container returns immediately and includes the `fc.log` tail. |
| `TestNoInboundRejectedMachineConfig` | Needs Docker and KVM. The gate-ready hook sets `vcpu_count` to 0 after Squid is listening and before Firecracker reads `vm.json`. Strong-wall `create --no-inbound` returns an error that includes the `fc.log` tail, and no container for that id remains. |
| `TestOpenWeb` | Needs Docker and KVM. On both walls the shipped gate file keeps the bumped connection Squid already opened to the origin and checks that certificate with the system CA. The test replaces only that CA path with the mock CA. The gate token replaces two box `Authorization` values on `api.github.com`. A `DELETE`, a contents write, `POST /graphql`, `PATCH`, and a Git refs write are denied and do not reach the origin. A POST whose path contains `..` or `%` is denied and does not reach the origin. Cleartext `api.github.com` is denied, and that request does not carry the gate token. Creating a repository passes. A pass-through host shows its own certificate in the box and its name in the log as `passthrough`. The token is not in the box. |
| `TestACLOpensWeb` | An `http_access allow` for inbound, vendor, or probe does not open the web. `container_out` and `container_tls` do. |
| `TestGateDNS` | Needs Docker and KVM. On both walls, with a closed gate file, `example.com` does not resolve and the log says `NXDOMAIN`, while `probe.hermetarium.test` resolves to the gate and is logged. With the web open, `example.com` resolves and the log has the address. A query to `1.1.1.1` gets no answer, and on the weak wall `127.0.0.11` cannot resolve an outside name. `localhost` is `127.0.0.1`. |
| `TestAttachKeyOutsideGate` | Each habitat gets its own key pair. The private key is mode `0600` under `var/.attach/<id>/`, outside the instance directory the gate mounts. The boot argument is `hermetarium.pub=` plus that public key. The Firecracker container does not mount the private key, and `attach` does not publish a port. |
| `TestSSHRemoteCommand` | The strong-wall command is one quoted `exec` of the operator's argv, including a newline and a single quote. |
| `TestAttachRefusedNoSSHD` | No habitat and no Docker daemon. A strong-wall connect whose client log says `Connection refused` exits 1 and says nothing listens on port 22: no SSH server at `/usr/sbin/sshd`, or it did not start, and names `USAGE`. A timeout stays the client's text. The same refusal on the weak wall stays on stderr with no `/usr/sbin/sshd`. |
| `TestAttachTTY` | Needs Docker and KVM. `attach` on both walls gives a command a pty and the window size set on the operator terminal, follows a resize, and a later attach still sees a detached tmux session. The I/O log stays empty. The strong wall uses SSH. A walk of the instance directory finds no private key material, and neither the Firecracker container nor the gate publishes a port. |
| `TestAttachPrivateKeyLeak` | `privateKeyLeak` walks an instance directory. It reports the seed, the seed in base64, the decoded PEM block, each PEM body line after the first, a PEM with newlines removed, hex of the key file and of that stripped PEM, a nested `id_ed25519`, and `OPENSSH PRIVATE KEY` in a small file. The shared first PEM body line is not a leak, nor is that line plus the header in a 1 MiB `rootfs.ext4`. |
| `TestAttachDestroyRemovesKey` | Needs Docker, not KVM. No habitat is created. A strong `instance.json` makes `destroy` remove `var/.attach/<id>/` after that directory's `id_ed25519` was written. |
| `TestDestroyStaysOnWall` | Needs Docker, not KVM. Destroy of a strong record with an empty `bridge.name` succeeds and leaves that file. Destroy of a weak record leaves a planted `rootfs.ext4`. |
| `TestAttachExitCode` | Needs Docker and KVM. On both walls, `attach` of `sh -c 'exit 7'` returns 7. The test calls `attach` again on the strong wall while the status is non-zero and not 7, for up to 90 seconds, and fails immediately on status 0 or on any other weak-wall status. |
| `TestAttachConcurrentWeakTTY` | Needs Docker, not KVM. Two weak-wall TTY attaches at the same time both print `CONCURRENT_OK`. |
| `TestResizeGone` | `resizeGone` is true for HTTP 404 and for `cannot resize a stopped container`. It is false for a nil error, a 500 that does not say the container stopped, a Docker socket dial error, a skipped resize, and a rejected `DOCKER_HOST` of `tcp://127.0.0.1:2375`. |
| `TestResizeDockerExec` | Needs Docker, not KVM. `resizeDockerExec` sets a running `docker exec` to 17 rows by 91 columns. The operator pty was 40 by 100, and no `SIGWINCH` is sent to Docker. `stty size` prints `17 91`. After the exec exits, the same call reports the session gone. |
| `TestWeakAttachResize` | Needs Docker, not KVM. The weak-wall attach callback resizes a short-lived container's exec. The test does not forward SIGWINCH to the docker CLI, so the CLI's own resize cannot hide a callback that returns without resizing. After the initial window of 40 100, `stty size` is 17 91. |
| `TestWeakAttachIgnoresForeignExec` | Needs Docker, not KVM. A foreign TTY exec that appears before this weak-wall attach is not resized. This session's `stty size` becomes 17 91. |
| `TestAttachGuestStderr` | Needs Docker and KVM. A non-TTY `attach ID -- CMD` on the strong wall returns status 0 when the command prints `Connection refused` on stderr. That output is not an attach error, and the command is not run again. |
| `TestDebianSshdPrivsep` | Needs Docker. Debian OpenSSH refuses to start when `/run/sshd` is missing. `fc-init` creates that directory mode `0755` before `sshd`, and `sshd -t` then accepts the guest config. |
| `TestChooseID` | With no `--id`, `create` still makes a 12-character id. `--id claude` is kept. A blank, a space, a slash, a leading `-` or `.`, and a name longer than 63 characters are rejected. `claude` and `grok` do not share a subnet. |
| `TestCreateRejectsSizeFlagsOnWeakWall` | `create --wall weak` with `--mem` exits 2. |
| `TestCreateRejectsCPUsAboveWallLimit` | `--cpus` above 32 on the strong wall, or above `runtime.NumCPU` on the weak wall, exits 2 and names the limit. No habitat is started. The check still allows 32, the host CPU count, and an omitted `--cpus`. |
| `TestCreateRejectsRootfsOnWeakWall` | `create --wall weak` with `--rootfs` exits 2, says the Docker wall rejects it, and creates no file. |
| `TestExt4Regular` | A root file is accepted only when it is a regular file with ext4 magic `0xEF53` at offset `0x438`. A directory, a symlink, a missing path, and a non-ext4 file are rejected. The check does not change an accepted file. |
| `TestCreateRejectsDataDisk` | `create --data` and `create --data-size` exit 2 on both walls, create no file, and say the strong wall has no data disk. |
| `TestVersionCommand` | `version` exits 0 and prints `supervisor.Version`. |
| `TestAlpineImageFromDockerfile` | `AlpineImage` is `alpine:3.24`, the same tag parsed from the build image Dockerfile (`supervisor/build-image/Dockerfile`). |
| `TestSquidDockerfile` | `SquidImage` is `hermetarium-squid:local`. The embedded Dockerfile starts `FROM ubuntu/squid:` and installs `squid-openssl`, and it requires `/usr/lib/squid/security_file_certgen`. |
| `TestEnsureNetToolsStamp` | An existing nettools image is rebuilt when the Dockerfile stamp differs. A failed build restores the previous stamp, or removes it when there was none. A matching stamp skips the build. |
| `TestGateHasNoAnonymousVolumes` | Needs Docker. The gate mounts tmpfs on `/var/log/squid` and `/var/spool/squid`, so it has no anonymous volumes. `DestroyWeak` and `DestroyStrong` remove the gate container. |
| `TestDestroyRemovesBoxVolumes` | Needs Docker. A weak-wall image with `VOLUME /data` has those box volumes removed by `DestroyWeak`. The test checks only that box's volumes, not the machine-wide dangling-volume list. |
| `TestNewTestLabelFormat` | Two labels differ. Each has prefix `htm`, this process's pid, and a uuid. |
| `TestDockerArgsWithTestLabel` | `docker run` and `docker create` get `--label hermetarium.test=...` after the verb. `inspect` stays unlabeled. A blank label and a label already present are left unchanged. |
| `TestParseTestLabelPID` | A well-formed `htm-<pid>-<uuid>` yields the pid. A wrong prefix, a non-numeric or zero pid, or a missing uuid do not parse. |
| `TestPidRunning` | This process is running. A pid that has already exited is not. |
| `TestCleanupRemovesOnlyOwnLabel` | Needs Docker. Removing containers with one test label leaves a container that has a different label. |
| `TestCleanupDetectsLeftoverLabel` | Needs Docker. A container with the test label is listed before cleanup and gone after `RemoveContainersByTestLabel`. |
| `TestSweepLeavesRunningPidContainers` | Needs Docker. A sweep removes a container whose label pid is not running and leaves one whose label pid is this process. |
| `TestCreateDoesNotSetTestLabel` | Needs Docker. `CreateWeak` without `TestLabel` sets no `hermetarium.test` label on the gate, box, or DNS container. |
| `TestRootfsExportRemovesImageVolumes` | Needs Docker. `docker rm -fv` of a temporary `docker create` from an image that declares `VOLUME` removes that volume. `EnsureImageRootfs` of the same image leaves no container from that image. |
| `TestWeakBridgeForwardAccept` | Needs Docker. An internal bridge on `SubnetForID`'s CIDR gets `FORWARD` accept for that bridge in both `iptables-nft` and `iptables-legacy`. A missing legacy binary fails the test. After removal, both rules are gone. CI's Docker uses the iptables backend, so this job does not reproduce the nftables FORWARD DROP from issue #111. The test still checks that both rules are installed and removed. |
| `TestDestroyWeakMissingNetTools` | Needs Docker. A weak habitat on `SubnetForID`'s CIDR has both FORWARD accepts. After `hermetarium-nettools:local` is removed, destroy rebuilds that image, exits 0, and the box, the network, and both FORWARD accepts are gone. `hermetarium-sleep:local` is removed. |
| `TestDestroyWeakForgetError` | Needs Docker. An empty `bridge.name` makes destroy exit non-zero. `htm-box-<id>` and `htm-<id>` are gone, and `bridge.name` is still there. `hermetarium-sleep:local` is removed. |
| `TestBridgeIPv4` | `bridgeIPv4` keeps a printed gateway (`10.20.0.9`). An empty gateway on `10.41.0.0/24` is the first address, `10.41.0.1`. An empty inspect line is not an address. |
| `TestNftFilterTableDrop` | A fake `nft` records the commands. An owned table that is only iptables-nft's FORWARD, INPUT, and OUTPUT chains, with no rules and policy accept, is removed with `nft delete table ip filter`. A table that was not owned is not touched. An owned table that still has a rule, set, map, counter, flowtable, drop policy, user chain, quota, limit, synproxy, a dormant or persist table flag, a chain comment, or a nat prerouting chain is not deleted. A delete that finds the table already gone still prints `dropped`. `delete loses to content` is artificial: the fake returns EBUSY, which real `nft delete table` does not do for a non-empty table, and the marker stays. Two overlapping destroys do not error and do not leave the marker set. A failed marker write still passes `OWNED=1` and does not delete a table that has a rule. Replacing the marker during destroy leaves the new file. The accept script prints `owned` only when the table was missing. |
| `TestNftFilterRulesBeforeTable` | A fake iptables and nft record commands for the whole forget script. `nft delete table ip filter` runs only after both FORWARD accepts are deleted. |
| `TestNftFilterTableNetns` | Needs Docker. In a container network namespace, real nftables matches: the empty table created by the accept script is gone after destroy, a pre-existing table stays, a table that still has a rule stays, and `phase-set` keeps a set. `phase-policy` keeps a FORWARD drop policy, `phase-user` keeps a user chain, and `phase-objects` keeps named quota, limit, and synproxy objects. `phase-builtins` removes INPUT, OUTPUT, and FORWARD when each has policy accept and no rules. The host firewall is not used. |
| `TestChecksumMismatchIsRefused` | A file whose SHA-256 is not the pinned kernel checksum is deleted and the check returns an error. Matching bytes are kept. |
| `TestFirecrackerStamp` | The extract stamp is the pinned tarball checksum plus the binary's SHA-256. A missing stamp, a stamp without that hash, or a swapped binary is not a cache hit. A rejected tarball deletes the stamp. A matching tarball keeps it. |
| `TestEnsureStrongAssetsRejectsTarballWithoutBinary` | Needs Docker. A tarball with no `firecracker-*x86_64` binary, and an existing cached binary, makes `EnsureStrongAssets` return an error and does not write the extract stamp. |
| `TestGateFilesHardened` | Shipped gate files do not set `DONT_VERIFY_PEER` or `dns_v4_first`, and no active line uses `myport`. |
| `TestDockerRunNotPrivileged` | No non-test `.go` file under `supervisor/` contains `--privileged`. |
| `TestSquidStartupQuiet` | Needs Docker. Squid started with the echo gate file does not log the `myport` or `dns_v4_first` warnings. |
| `TestWaitSquidRetriesMissingLogUntilDeadline` | With the pid present but cache.log missing, WaitSquid keeps retrying until the deadline, runs no chmod, and then fails. |
| `TestWaitSquidReturnsChmodError` | A Docker error from the chmod is retried and, once the deadline passes, returned as the create error. |
| `TestWaitSquidChmodSucceedsOnlyWhenBothLogsExist` | The chmod runs once, after both access.log and cache.log exist, and WaitSquid then succeeds. |
| `TestUntrustedVendorGetsNoKey` | Needs Docker. A vendor certificate the gate does not trust gets no request and no key. The same certificate trusted via `tls-cafile` does. |
| `TestInstallACL` | An empty ACL path fails. A file is copied to `<instance>/squid.conf`. |
| `TestInstallACLMissingFile` | A missing ACL path fails with `read ACL`. |
| `TestRootEnvWins` | `HERMETARIUM_ROOT` is the data root. Cache is `<root>/.cache`. |
| `TestRootCheckoutFromCwd` | With the env unset, a checkout working directory is the root. Cache is `<root>/.cache`. |
| `TestRootXDGWhenNoCheckout` | Outside a checkout, root is `$XDG_DATA_HOME/hermetarium` and cache is `$XDG_CACHE_HOME/hermetarium`. |
| `TestCacheDirExplicitRoot` | `CacheDir` follows the root argument. |
| `TestParseSquidLine` | An allow line for `probe.hermetarium.test` parses method, bytes, and destination. A `TCP_DENIED` line is not allowed. An inbound POST on port 18081 has direction `in`. An `https://` URL is protocol `https`, port 443, and keeps the path. A bumped line is decrypted and keeps the path. A CONNECT line is `https` on the authority port. A bumped CONNECT to an IP is named from SNI and keeps that IP in `upstream`. A spliced line is passthrough, takes the destination from SNI, and keeps the upstream address from the peer or, when the peer is `-`, from the IP authority. A `NONE_NONE_ABORTED` CONNECT is not a policy denial, has `aborted` set, and takes the destination from SNI. A `TCP_TUNNEL_ABORTED/200` CONNECT that already transferred bytes does not have `aborted` set. |
| `TestACLWantsHTTPS` | A commented `https_port` line leaves interception off. An active `https_port` with `ssl-bump` turns it on. |
| `TestGateRulesScriptOneList` | The allow-list is one script. Each habitat port appears once. The strong-wall entrypoint runs that file and does not carry its own ports or `https_port` check. It serves the certificate with `httpd` only when `GATE_HTTPS=1`, and only accepts `dns.ready` after that port has answered. |
| `TestGateRulesMatchAcrossWalls` | Needs Docker and KVM. After create, the filter and nat rules in the weak gate and the Firecracker container are the same once the habitat interface and subnet are ignored. Docker's embedded-DNS nat chains exist only on the weak bridge and are not part of that compare. |
| `TestGateCAAcceptsBeforeDNS` | Needs Docker and KVM. The Firecracker container's `httpd` answers `ca.pem` before `dns.ready` exists. No `htm-ca-<id>` container is started. |
| `TestAddressPlanOneList` | The four stand-in names are one list. The weak plan stays on `10.x` and the strong plan stays on `172.16.0.0/24`. `entrypoint.sh`, `pack-oci.sh`, and `examples/echo/squid.conf` do not carry a second copy of the strong-wall gate address. |
| `TestPrepareMITM` | `PrepareMITM` writes a CA certificate and keeps the private key out of the public directory. |
| `TestPrepareMITMAfterGateChown` | Needs Docker. After the gate user owns `mitm/`, `PrepareMITM` can write a new CA. |
| `TestInhabitantHTTPSStaysCommented` | The inhabitant gate files ship `OPEN-HTTPS` commented out. |
| `TestFcScriptsDirExtractsWithoutTree` | `entrypoint.sh`, `pack-oci.sh`, and `gate-cert.sh` extract as executables under the data root, and the extract does not create a `firecracker-scripts/` directory there. |
| `TestExtract` | The same three scripts extract executable. The entrypoint contains the marker `fc-scripts` (a string in the Firecracker scripts, not a name for the container). The pack script contains `rootfs.ext4` and builds it with `mke2fs -d` (no mount). fc-init starts `sshd` after the guest address and `/run/sshd` (mode 0755) and before the inhabitant, and only when `[ -x /usr/sbin/sshd ]`. It sources `gate-cert.sh` instead of installing the certificate itself, and prints `GUEST_UNAME=` only after that install succeeds. The scripts do not build a vsock terminal. A second extract leaves the entrypoint mtime unchanged. |
| `TestGateCAFallsThroughToCurl` | The shared gate CA install runs against a fake busybox without wget: curl is tried next under timeout 3 and writes the CA to both paths. With no curl, the install exits non-zero and the only output is `fc-init: could not fetch the gate CA`. |
| `TestEcho` | The echo handler returns the POST body with status 200. |
| `TestHealth` | agentd `GET /` returns 200 and a body containing `ok`. |
| `TestToolCommand` | A tool-use payload `{"command":"id -u"}` yields the command `id -u`. |
| `TestCmdFirstTurn` | Porter argv for a first turn: `claude -p --output-format text --dangerously-skip-permissions`, `grok --always-approve -p`, and `opencode run --auto --`. Claude's environment includes `CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS=1`. OpenCode's environment includes `OPENCODE_DISABLE_AUTOUPDATE=1` and `OPENCODE_DISABLE_MODELS_FETCH=1`. OpenCode's `-p` is not the prompt. `--` keeps a prompt that looks like a flag as the message. The model comes from `opencode.json`, not `-m`. |
| `TestCmdContinuesSession` | After a turn has run, `claude`, `grok`, and `opencode` gain `--continue`. Claude's flag stays in front of `-p`. Grok's follows the prompt, because `-p` is `--single`. OpenCode's stays in front of `--`, before the positional prompt. |
| `TestCmdUnknownHarness` | An unknown `HARNESS` value returns an error. |
| `TestReleaseScript` | `tests/harness` runs `python3 -m unittest` on `.github/scripts/test_release.py`. In a temp git repo, `diff_note_lines()` keeps an added `// Release-Notes:` or `# Release-Notes:` line under an artifact path, indented or not, and drops a note that the commit moves or only renames, a note in `*_test.go`, `*.md`, `testdata/`, `fixtures/` or `vendor/`, a note outside the artifact's paths, a `Release-Notes:` that is not a whole line, and a repeated note text. |

Porter tests compare argv. They do not exec `claude`, `grok`, or `opencode`.

### Hello-world

`tests/hello-world` `TestHelloWorld`. Image `examples/echo`.

**Weak.** `ping -c 1 -W 2 1.1.1.1` exits non-zero in the box, while a TCP connect to `1.1.1.1:443` from the gate succeeds (the gate is the only way out). `harness.AssertNeighborBlocked`: a `busybox` container on Docker's default bridge cannot connect to the gate on ports 3128, 3129, 18081 or 80. `curl` of `http://probe.hermetarium.test/hello` returns `hermetarium-ok`. The I/O log contains destination `probe.hermetarium.test` or the gate IP. Then the echo exercise below.

**Strong.** Guest serial shows `GUEST_UNAME` starting with `6.1.188`, different from the host kernel in `/proc/sys/kernel/osrelease`, and contains `LEAK_FAIL`. `GUEST_RNG` contains `virtio_rng` (guest `/sys/class/misc/hw_random/rng_current`); that is the entropy device. `GUEST_GETRANDOM_OK` means a 16-byte `getrandom` returned within 2 seconds. It does not show the device is present: this kernel can seed itself without virtio-rng. An image with no `/bin/busybox` prints `GUEST_GETRANDOM_SKIPPED` instead of failing the call. The Firecracker container's `HostConfig.Privileged` is false. `harness.AssertNeighborBlocked` on the Firecracker container, as on the weak wall. Then the echo exercise. The strong guest does not fetch the probe.

**Image.** The same echo image passed as `CreateOpts.Image` with `examples/echo/squid.conf` and no probe sidecar. Echo exercise only.

**Echo exercise** (`tests/echo`). Two POSTs to the same inbound URL: body `ping` returns `ping`, body `pong` returns `pong`. After a log sync, the I/O log has direction `in`.

### Terminal

`TestAttachTTY`. Image `hermetarium-attach:local` (Alpine with `openssh` and tmux, command `sleep`). `--no-inbound` on both walls. The strong wall connects with SSH. The private key stays on the host.

A command run through `attach` prints a pty name under `/dev/pts` and `stty size` of 40 rows by 100 columns, the size set on the operator's pty before the call. The operator pty is then resized to 24 by 80 and the process receives `SIGWINCH`. A later `stty size` in that same command prints 24 by 80.

That command exits. A second attach runs `tmux new-session -d` with a command that writes `alive` and keeps sleeping. A third attach runs `tmux has-session` and prints that file. `var/<id>/io.jsonl` is still empty.

### Backup

`tests/backup` `TestBackupWhileWriting`. Needs Docker and `/dev/kvm`. `restic` comes from `PATH`. When it is missing, the test downloads the pinned official linux-amd64 binary into `.cache` and does not commit it. Strong wall, `--no-inbound`. The guest keeps rewriting `/churn` on its root filesystem after writing `/known`. `vm.json` has one drive, the root disk, and the guest command line has no `hermetarium.data=1`. `omakase/backup.sh` pauses `htm-fc-<id>`, then Restic reads `var/<id>/rootfs.ext4`. A restore contains `/known` as `known-v1`, plus `io.jsonl`, `access.log`, and the journal. A fake model key that exists only in the gate file, and a fake gate private key planted at `mitm/ca.key`, are absent from the snapshot listing and from the restored tree. A second backup, while the guest has changed the root file only a little, adds no new files and under half of what the first snapshot added. An ext4 journal of a few MiB still passes. A full recopy does not. The guest is still writing after the script returns, so the Firecracker container was unpaused. `destroy` deletes `var/<id>/rootfs.ext4` and leaves the restored file. `create --wall strong --rootfs` boots that file: the guest prints the known text and does not take the first-boot path. `destroy` of that habitat deletes the copy and leaves the restored file.

`tests/backup` `TestSecretRefusal`. Runs `omakase/backup.sh` with stand-in `restic`, `docker`, and `hermetarium` binaries. No Docker daemon and no KVM. The journal is `squid.conf`, a symlink to `squid.conf`, a `..` path that reaches `squid.conf`, `mitm/ca.key`, `var/<id>/`, `var/<id>`, `/`, another habitat's `squid.conf`, the directory that holds it, the data directory that contains `var/`, a hard link to `squid.conf`, a directory that contains only a hard link to `squid.conf`, when the filesystem allows a hard link, or a directory outside `var/` that reaches the key only through a bind mount of `mitm/`, when that mount is permitted. The script exits with the refusal and does not run `restic backup`.

`tests/backup` `TestFirecrackerContainerNotThisRoot`. No Docker daemon and no KVM. The stand-in `docker inspect` reports a `/log` mount that is not `var/<id>` under `HERMETARIUM_ROOT`. The script refuses that Firecracker container and does not run `restic backup`.

`tests/backup` `TestPauseBound`. No Docker daemon and no KVM. A stand-in `restic backup` sleeps. `HERMETARIUM_PAUSE_SECONDS=1` stops it. The stand-in `docker` records `pause` then `unpause`.

`tests/backup` `TestBackupUnpauseOnSignal`. No Docker daemon and no KVM. A stand-in `restic backup` sleeps, and the pause bound is 30 seconds. `SIGTERM` to the shell alone unpauses within a few seconds. The stand-in `docker` records `pause` then `unpause`.

`tests/backup` `TestBackupUnpauseOnSignalDuringPause`. No Docker daemon and no KVM. The stand-in `docker pause` records `pause` and sleeps. `SIGTERM` to that process group unpauses. The log is `pause` then `unpause`.

`tests/backup` `TestPauseSecondsTooLarge`. No Docker daemon and no KVM. `HERMETARIUM_PAUSE_SECONDS` of more than 9 digits, or above 21600, is refused. 21600 is accepted. The same checks run under `bash` when it is installed.

`tests/backup` `TestBackupUnpauseOnResticFailure`. Needs Docker and `/dev/kvm`. Same writing habitat as `TestBackupWhileWriting`. A `restic` ahead of the real binary on `PATH` exits non-zero for `backup` after the pause. The script fails, the Firecracker container is running again, and the guest writes a later `WRITE_n`.

### Denied-request mail

`tests/digest` `TestDigestDeniedThenQuiet`. No Docker daemon and no KVM. `omakase/digest.sh` runs a stand-in `hermetarium logs` and a stand-in mail program. One denied request is one mail that names that destination. An allowed request and a denied inbound request are not in it. The next run, with nothing new, does not send mail.

`tests/digest` `TestDigestManyAreOneMail`. No Docker daemon and no KVM. A first run with nothing denied sends nothing. Many denied requests, and one TLS handshake the client closed, then produce one mail. That mail names each destination and its count, including the pass-through host. It does not contain the request path, a key planted in that path, or a request body.

`tests/digest` `TestDigestFailedMailDoesNotAdvance`. No Docker daemon and no KVM. When the mail program fails, the script exits non-zero and does not remember the denial as sent. The next run sends that one mail.

`tests/digest` `TestDigestSpanOrdersFractionalSeconds`. No Docker daemon and no KVM. Three denials in one second use the `RFC3339Nano` forms of .000, .100, and .120 (`...01Z`, `...01.1Z`, `...01.12Z`). The span runs from the .000 instant to the .120 instant. A denied DNS lookup is counted by name. Its answer is not in the mail.

### Guest Docker

`tests/guest-docker` `TestGuestDocker`. Needs Docker and `/dev/kvm`. Image `docker:29-dind` plus a saved `busybox:1.37`, built as `hermetarium-guest-docker:local`. Strong wall, 2048 MiB memory, 2048 MB disk, echo gate file. The guest starts Docker with `DOCKER_INSECURE_NO_IPTABLES_RAW=1` and the serial log contains `hermetarium-docker-ok` and `DOCKER_INNER_OK`.

### Agentd mock

`tests/agentd` `TestAgentd`, subtests `weak` and `strong`. Image `examples/agentd`. Mock vendor. Vendor host in the log is `claude.hermetarium.test`.

**Weak, before the exercise.** `printenv` does not contain `htm-test-key`.

**Both, before the exercise.** `harness.AssertInboundNoVendor`: a POST to `/v1/messages` on the inbound URL with `Host: claude.hermetarium.test` is refused (non-2xx) and does not return the mock vendor's tool call. The inbound port never forwards to the vendor and never carries the key.

**Exercise** (`tests/harness`). POST `hello`, then POST `root`, on the same inbound URL. The second reply contains `0`. The I/O log has direction `in` and destination `claude.hermetarium.test`.

### Official CLI smoke

`tests/inhabitants` `TestOfficialCLIs`. Weak wall only. One subtest per inhabitant.

| Inhabitant | Command that must exit 0 | Log host if the mock chat succeeds |
| --- | --- | --- |
| `claude-code` | `/usr/local/bin/claude --version` | `claude.hermetarium.test` |
| `grok-build` | `/usr/local/bin/grok --version` | `grok.hermetarium.test` |
| `opencode` | `/usr/local/bin/opencode --version` | `model.hermetarium.test` |

OpenCode is optional. It does not replace echo or agentd. Its gate file is the generic model stand-in, filled with the mock peer and `htm-test-key`. A failed mock chat still passes the subtest. There is no live OpenCode case.

`printenv` does not contain `htm-test-key`. `harness.AssertInboundNoVendor` with the inhabitant's vendor host, as in the agentd mock. A POST `Run id -u and reply with only the number.` is attempted with a 20s timeout. A failed call ends the subtest successfully. A successful call continues into the agentd exercise (two further POSTs, reply contains `0`, inbound and vendor host in the I/O log).

### OpenCode terminal setup

`tests/opencode-tui`.

`TestOpenCodeTUIFiles`. No Docker. The setup README names `inhabitants/opencode`, `--image opencode`, the strong wall, `--no-inbound`, Porter, `htm-fc-<id>`, and `htm-gate-<id>`. It does not call the inhabitant an assistant. `omakase/opencode-tui/` has no Dockerfile. The OpenCode Dockerfile pins OpenCode 1.19.5 and installs `git`, `rg`, `tmux`, `openssh-server`, and `build-essential`, with the update check and the model-list download off, and a placeholder `OPENCODE_API_KEY`. The gate file still has its five placeholders, decrypts HTTPS, resolves outside names, has no inbound port, and does not set `DONT_VERIFY_PEER` or `dns_v4_first`.

`TestOpenCodeTUI`. Needs Docker and KVM. On both walls, `--no-inbound`, the probe runs as root (`BOX_ID=0`). The shipped gate file is filled with the pretend model peer, `htm-model-key`, and `htm-github-token`. The test replaces only the outgoing CA path with the mock CA, and adds `hosts_file` so the intercepted names resolve to the gate. `url` prints `this habitat has no inbound address`. A DNS query to `1.1.1.1` gets no answer. `ping` to `1.1.1.1` fails. The box cannot open the model peer's port on the gate. The model call is `http://model.hermetarium.test/v1/chat/completions` with a placeholder key. The mock sees one `Authorization`, the gate key, plus that file's `Host` and `/openai` prefix. Streamed lines arrive with a gap. `GET https://other.example/hello` returns 200, and the I/O log has that host, the path `/hello`, and `decrypted`. The page keeps the box `Authorization`. `POST https://api.github.com/user/repos` carries the gate token and not the box token. That token is on no origin line except `path=/user/repos`. `DELETE`, a contents `PUT`, `POST /graphql`, `PATCH`, a Git refs write, a POST whose path contains `..` or `%`, and cleartext `GET http://api.github.com/user` are 403 and do not reach the origin. `pinned.example` shows its own certificate and is `passthrough` in the log. Neither secret is in the box environment, the box filesystem, the guest rootfs, or `io.jsonl`. Push Guard checks are not in this test.

## `make test-live`

Real vendor peer. A case whose key variable is empty calls `t.Skip`.

**Agentd** (`TestAgentdLive`). Weak wall. Requires `HERMETARIUM_ANTHROPIC_API_KEY`. One POST: `Run the shell command id -u and reply with only the number.` The reply contains `0`.

**Official CLIs** (`TestOfficialCLILive`). Weak wall. One subtest each:

| Inhabitant | Key |
| --- | --- |
| `claude-code` | `HERMETARIUM_ANTHROPIC_API_KEY` |
| `grok-build` | `HERMETARIUM_XAI_API_KEY` |

Turn one is the same `id -u` prompt; the reply contains `0`. Turn two POSTs `What number did you just report?` to the same URL and logs the body.

## CI

Job `test` (ubuntu-24.04, 40 minutes) opens `/dev/kvm`, caches `.cache`, runs `make build`, then `make test`.

Job `test-live` (ubuntu-24.04, 30 minutes) exports `HERMETARIUM_ANTHROPIC_API_KEY`, and `HERMETARIUM_XAI_API_KEY` from repository secrets. If both are empty, the job exits 0 without calling `make test-live`.

Job `release` runs after `test` on push to `main`. It does not run the suite.

## Named in SPEC §11 and not asserted here

- Strong-wall fetch of the probe, strong-wall official CLIs, and strong-wall live runs. Grok Build and Claude Code were run on the strong wall by hand and answered; no automated test covers it.
- A second agentd live turn, and the text of the official-CLI live second turn.
- The vendor key absent from the inhabitant filesystem. The weak-wall check is `printenv` only.
- A direct request from the box to the vendor origin failing.
- The I/O log allowed bit. Habitat tests match destination or direction.
- Installing a package inside the habitat.
- The `hermetarium` CLI as a subprocess (`create`, `url`, `destroy`). `destroy` in habitat tests is cleanup.
- A paid OpenCode upstream, and a git push. The OpenCode smoke does not require a paid key. Its README shows the model HTTP log line, and says a push is absent until the gate file allows a git host.
