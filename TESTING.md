# Test spec

What the suite checks in this tree today. Product rules for tests stay in [SPEC.md §11](SPEC.md#11-tests). This file is the inventory of the running tests.

## Commands

| Command | Packages | Gate |
| --- | --- | --- |
| `make test` | `./supervisor` `./supervisor/gatedns` `./firecracker-helper` `./examples/...` `./porter` `./tests/...` | Merge gate. CI job `test` on push to `main`, pull request, and `workflow_dispatch`. |
| `make test-live` | `./tests/agentd` `./tests/inhabitants` with `-tags live` | Optional. CI job `test-live` on `workflow_dispatch` only. |

Both use `go test -count=1 -p 1 -timeout 15m -v`. Live files are `//go:build live`, so `make test` does not compile them. `make test-live` also runs the untagged tests in those two packages (agentd mock on both walls, official-CLI smoke).

Needs Docker and Go 1.24+. Strong-wall cases need `/dev/kvm` and x86_64. Data root is `HERMETARIUM_ROOT`, else this checkout.

Habitats are created through the supervisor Go API. Chat and echo are HTTP POST via `supervisor.Call` to the inbound URL. On the weak wall, `ExecWeak` runs the leak ping, the probe curl, `printenv`, and the official CLI version command. On the strong wall, `WaitStrongSerial` supplies `GUEST_UNAME` and `LEAK_FAIL`.

## Fixtures

`tests/harness` builds images and fills a temp ACL before `create`.

- Echo and agentd: `hermetarium-echo:local` and `hermetarium-agentd:local`, 128 MiB / 256 MB, ACL from `examples/<name>/squid.conf`.
- Official CLIs: `hermetarium-in-claude:local`, and `hermetarium-in-grok:local`, 2048 MiB / 2048 MB, ACL from `inhabitants/<name>/squid.conf`. Porter is copied into the image build.
- Mock runs replace the habitat `cache_peer` with `127.0.0.1 parent 18082` and `__VENDOR_KEY__` with `htm-test-key`. Live runs keep the real vendor peer and substitute `HERMETARIUM_ANTHROPIC_API_KEY` or `HERMETARIUM_XAI_API_KEY`. The supervisor does not read those variables.
- Echo creates start a probe httpd on the gate (`tests/probe`, port 18080, body `hermetarium-ok`). Mock creates start `tests/vendormock` on the gate. The mock requires `x-api-key` or `Authorization: Bearer` equal to `htm-test-key`. `/v1/messages` and `/v1/chat/completions` return a bash tool call `id -u`, then the text `0` after a tool result.

## `make test`

### Supervisor, helper, porter, examples

In-process. No Docker habitat.

| Test | Checks |
| --- | --- |
| `TestResolveCreate` | `create` requires `--image` and `--acl`. Both flags parse into the exec struct. Defaults are 512 MiB and 1024 MB. `--mem 2048 --disk 4096` parses. `--mem 0` and a non-integer `--disk` fail. `--cpus` is unset unless the flag is set. `--cpus 4` parses. `--cpus 0` fails. `--no-inbound` is off unless the flag is set. |
| `TestCPUs` | Needs Docker and KVM. `--cpus 4` is a CPU quota of 4 in the Docker box (`cpu.max`) and four processors in the strong-wall guest. |
| `TestSparseRootDisk` | Needs Docker. A 50 GB root disk is sparse (little allocated space) and is not noticeably slower to create than a 1 GB disk. |
| `TestHabitatRootfsIsPrivate` | Two strong-habitat rootfs files copied from one cache, and a third copied afterwards, are different paths. A write in the first is not in the others or in the cache. The copy stays sparse. The helper bind source is `var/<id>/rootfs.ext4`, not the cache path. |
| `TestHabitatRootfsReplacesSymlink` | A symlink at `var/<id>/rootfs.ext4` is replaced by the copy. The cache bytes stay. The result is a regular file, and no `rootfs.ext4.partial` is left. |
| `TestHabitatRootfsFailureKeepsDest` | When the rename onto `rootfs.ext4` fails, only `rootfs.ext4.partial` is removed. A `rootfs.ext4.partial` symlink is not opened, so the cache is not truncated. |
| `TestHabitatRootfsStalePartial` | A regular file at `rootfs.ext4.partial` makes the copy fail with `os.ErrExist` and text containing `run hermetarium destroy`. The cache file is unchanged. |
| `TestRemovePrivateRootfs` | Removing the private disk deletes `var/<id>/rootfs.ext4`, `rootfs.ext4.partial` (a regular file or a symlink, without following it), and a symlink at `rootfs.ext4`. It leaves a neighboring data file, `rootfs-<image>-<size>.ext4`, and an unsuffixed `rootfs-<id>.ext4`. |
| `TestDropUnmountedRootfs` | Needs Docker, not KVM. When no `htm-fc-<id>` exists, the failed-create cleanup removes `var/<id>/rootfs.ext4` and `rootfs.ext4.partial`. When that container exists, both stay. The cache, an unsuffixed `rootfs-<id>.ext4`, and a neighboring data file stay either way. |
| `TestNoInboundURLMessage` | `noInbound: true` is the sentence `this habitat has no inbound address`. A stored URL is returned. An empty URL without `noInbound` is an error. |
| `TestNoInbound` | Needs Docker and KVM. An image that only sleeps starts on both walls with `--no-inbound` and a gate file that has no inhabitant peer and no inbound port. No host port is published. `url` prints that sentence. |
| `TestNoInboundGatePort` | Needs Docker and KVM. A gate file that still listens on 18081, with `--no-inbound`, does not accept that port from the bridge gateway on either wall. Squid is listening on 127.0.0.1:18081. Without the flag, 18081 is published and the bridge gateway can connect. |
| `TestModelGate` | Needs Docker and KVM. On both walls the box calls `http://model.hermetarium.test/v1/chat/completions` with a placeholder key. The mock sees exactly one `Authorization` value, the gate file's key, plus that file's `Host` and path prefix. Streamed lines arrive with a gap, not in one piece. The key is not in the box environment or filesystem. The box cannot reach the mock on the gate address. |
| `TestWaitGuestUname` | A serial log containing `GUEST_UNAME=` makes the strong-wall boot wait return nil. A log without that line and a dead helper returns immediately and includes the `fc.log` tail. `DATA_MOUNT_FAIL` is still "did not mount", not a boot timeout. |
| `TestNoInboundRejectedMachineConfig` | Needs Docker and KVM. `AfterGate` sets `vcpu_count` to 0, which Firecracker rejects. Strong-wall `create --no-inbound` returns an error that includes the `fc.log` tail, and no container for that id remains. |
| `TestACLOpensWeb` | An `http_access allow` for inbound, vendor, or probe does not open the web. `container_out` and `container_tls` do. |
| `TestGateDNS` | Needs Docker and KVM. On both walls, with a closed gate file, `example.com` does not resolve and the log says `NXDOMAIN`, while `probe.hermetarium.test` resolves to the gate and is logged. With the web open, `example.com` resolves and the log has the address. A query to `1.1.1.1` gets no answer, and on the weak wall `127.0.0.11` cannot resolve an outside name. `localhost` is `127.0.0.1`. |
| `TestAttachProgramEmbedded` | The release binary embeds an ELF `fc-attach` for the strong-wall guest. |
| `TestAttachTTY` | Needs Docker and KVM. `attach` on both walls gives a command a pty and the window size set on the operator terminal, follows a resize, and a later attach still sees a detached tmux session. The I/O log stays empty. |
| `TestChooseID` | With no `--id`, `create` still makes a 12-character id. `--id claude` is kept. A blank, a space, a slash, a leading `-` or `.`, and a name longer than 63 characters are rejected. `claude` and `grok` do not share a subnet. |
| `TestCreateRejectsSizeFlagsOnWeakWall` | `create --wall weak` with `--mem` exits 2. |
| `TestCreateRejectsCPUsAboveWallLimit` | `--cpus` above 32 on the strong wall, or above `runtime.NumCPU` on the weak wall, exits 2 and names the limit. No habitat is started. The check still allows 32, the host CPU count, and an omitted `--cpus`. |
| `TestCreateRejectsDataOnWeakWall` | `create --wall weak` with `--data` exits 2. |
| `TestCreateRejectsBadDataFlag` | On the strong wall, `--data` with no file, or a value that starts with `-`, exits 2 and creates no file. A `--data` path containing `:` exits 2 and creates no file. |
| `TestEnsureDataDisk` | An existing `--data` file is accepted only when it is a regular file with ext4 magic `0xEF53` at offset `0x438`. A directory, a non-ext4 file, and a path containing `:` are rejected. The colon path is not created. An accepted file is not resized. |
| `TestDataDiskLock` | Taking the data-disk lock fails with "in use" when another flock already holds the lock file. It also fails with "in use" in a second process while the first call still holds the lock. |
| `TestRefusedDataDiskLeavesNoLock` | A non-ext4 file or a directory passed as `--data` is refused before the lock is taken: no `FILE.lock` is created and the file is unchanged. A valid ext4 file still gets its lock. |
| `TestDataMountFailureIncludesGuestReason` | The data-mount failure error includes the tail of the guest serial log (the mount error and `DATA_MOUNT_FAIL`), not the whole log. |
| `TestVersionCommand` | `version` exits 0 and prints `supervisor.Version`. |
| `TestAlpineImageFromDockerfile` | `AlpineImage` is `alpine:3.24`, the same tag parsed from the embedded helper Dockerfile. |
| `TestSquidDockerfile` | `SquidImage` is `hermetarium-squid:local`. The embedded Dockerfile starts `FROM ubuntu/squid:` and installs `squid-openssl`, and it requires `/usr/lib/squid/security_file_certgen`. |
| `TestDestroyRemovesGateVolumes` | Needs Docker. Starting the gate creates the image's anonymous volumes. `DestroyWeak` and `DestroyStrong` remove those volumes. |
| `TestDestroyRemovesBoxVolumes` | Needs Docker. A weak-wall image with `VOLUME /data` leaves the dangling-volume set unchanged after `DestroyWeak`. |
| `TestWeakBridgeForwardAccept` | Needs Docker. An internal bridge on `SubnetForID`'s CIDR gets `FORWARD` accept for that bridge in both `iptables-nft` and `iptables-legacy`. A missing legacy binary fails the test. After removal, both rules are gone. CI's Docker uses the iptables backend, so this job does not reproduce the nftables FORWARD DROP from issue #111. The test still checks that both rules are installed and removed. |
| `TestBridgeIPv4` | `bridgeIPv4` keeps a printed gateway (`10.20.0.9`). An empty gateway on `10.41.0.0/24` is the first address, `10.41.0.1`. An empty inspect line is not an address. |
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
| `TestParseSquidLine` | An allow line for `probe.hermetarium.test` parses method, bytes, and destination. A `TCP_DENIED` line is not allowed. An inbound POST on port 18081 has direction `in`. An `https://` URL is protocol `https`, port 443, and keeps the path. |
| `TestACLWantsHTTPS` | A commented `https_port` line leaves interception off. An active `https_port` with `ssl-bump` turns it on. |
| `TestPrepareMITM` | `PrepareMITM` writes a CA certificate and keeps the private key out of the public directory. |
| `TestPrepareMITMAfterGateChown` | Needs Docker. After the gate user owns `mitm/`, `PrepareMITM` can write a new CA. |
| `TestInhabitantHTTPSStaysCommented` | The two inhabitant gate files ship `OPEN-HTTPS` commented out. |
| `TestFcHelperDirExtractsWithoutTree` | `entrypoint.sh` and `pack-oci.sh` extract as executables under the data root, and the extract does not create a `firecracker-helper/` directory there. |
| `TestExtract` | The same two scripts extract executable. The entrypoint contains `fc-helper`. The pack script contains `rootfs.ext4` and builds it with `mke2fs -d` (no mount). A second extract leaves the entrypoint mtime unchanged. |
| `TestGateCAFallsThroughToCurl` | The fc-init gate CA fetch runs against a fake busybox without wget: curl is tried next under timeout 3 and writes the CA to both paths. With no curl, the only output is `fc-init: could not fetch the gate CA`. |
| `TestEcho` | The echo handler returns the POST body with status 200. |
| `TestHealth` | agentd `GET /` returns 200 and a body containing `ok`. |
| `TestToolCommand` | A tool-use payload `{"command":"id -u"}` yields the command `id -u`. |
| `TestCmdFirstTurn` | Porter argv for a first turn: `claude -p --output-format text --dangerously-skip-permissions` and `grok --always-approve -p`. Claude's environment includes `CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS=1`. |
| `TestCmdContinuesSession` | After a turn has run, `claude` and `grok` gain `--continue`. Claude's flag stays in front of `-p`. Grok's follows the prompt, because `-p` is `--single`. |
| `TestCmdUnknownHarness` | An unknown `HARNESS` value returns an error. |
| `TestReleaseScript` | `tests/harness` runs `python3 -m unittest` on `.github/scripts/test_release.py`. In a temp git repo, `diff_note_lines()` keeps an added `// Release-Notes:` or `# Release-Notes:` line under an artifact path, indented or not, and drops a note that the commit moves or only renames, a note in `*_test.go`, `*.md`, `testdata/`, `fixtures/` or `vendor/`, a note outside the artifact's paths, a `Release-Notes:` that is not a whole line, and a repeated note text. |

Porter tests compare argv. They do not exec `claude` or `grok`.

### Hello-world

`tests/hello-world` `TestHelloWorld`. Image `examples/echo`.

**Weak.** `ping -c 1 -W 2 1.1.1.1` exits non-zero in the box, while a TCP connect to `1.1.1.1:443` from the gate succeeds (the gate is the only way out). `harness.AssertNeighborBlocked`: a `busybox` container on Docker's default bridge cannot connect to the gate on ports 3128, 3129, 18081 or 80. `curl` of `http://probe.hermetarium.test/hello` returns `hermetarium-ok`. The I/O log contains destination `probe.hermetarium.test` or the gate IP. Then the echo exercise below.

**Strong.** Guest serial shows `GUEST_UNAME` starting with `6.1.188`, different from the host kernel in `/proc/sys/kernel/osrelease`, and contains `LEAK_FAIL`. `GUEST_RNG` contains `virtio_rng` (guest `/sys/class/misc/hw_random/rng_current`); that is the entropy device. `GUEST_GETRANDOM_OK` means a 16-byte `getrandom` returned within 2 seconds. It does not show the device is present: this kernel can seed itself without virtio-rng. An image with no `/bin/busybox` prints `GUEST_GETRANDOM_SKIPPED` instead of failing the call. The helper container's `HostConfig.Privileged` is false. `harness.AssertNeighborBlocked` on the helper container, as on the weak wall. Then the echo exercise. The strong guest does not fetch the probe.

**Image.** The same echo image passed as `CreateOpts.Image` with `examples/echo/squid.conf` and no probe sidecar. Echo exercise only.

**Echo exercise** (`tests/echo`). Two POSTs to the same inbound URL: body `ping` returns `ping`, body `pong` returns `pong`. After a log sync, the I/O log has direction `in`.

### Terminal

`TestAttachTTY`. Image `hermetarium-attach:local` (Alpine with tmux, command `sleep`). `--no-inbound` on both walls.

A command run through `attach` prints a pty name under `/dev/pts` and `stty size` of 40 rows by 100 columns, the size set on the operator's pty before the call. The operator pty is then resized to 24 by 80 and the process receives `SIGWINCH`. A later `stty size` in that same command prints 24 by 80.

That command exits. A second attach runs `tmux new-session -d` with a command that writes `alive` and keeps sleeping. A third attach runs `tmux has-session` and prints that file. `var/<id>/io.jsonl` is still empty.

### Data disk

`tests/data-disk` `TestDataDiskSurvivesDestroy`. Needs Docker and `/dev/kvm`. Strong wall, `--no-inbound`, a 32 MiB sparse ext4 file. The first boot writes `/data/marker`. A second `create` with that file while the first habitat is running is refused. After `destroy` the file is still there, mode `0600`. `e2fsck -p` then a read-only mount with `noexec,nosuid,nodev` shows the marker and refuses to run a program from the disk. A new `create --data` of the same file sees the marker.

`TestDataDiskMountFailure`. Needs Docker and `/dev/kvm`. A file with ext4 magic `0xEF53` at offset `0x438` and no valid superblock makes create fail. The create error contains `DATA_MOUNT_FAIL` and the guest's `/dev/vdb` mount error. No container for that id is left. The inhabitant does not write `/data/marker` on the root disk.

### Backup

`tests/backup` `TestBackupWhileWriting`. Needs Docker and `/dev/kvm`. `restic` comes from `PATH`. When it is missing, the test downloads the pinned official linux-amd64 binary into `.cache` and does not commit it. Strong wall, `--no-inbound`, a 32 MiB data disk. The guest keeps rewriting `/data/churn` after writing `/data/known`. `omakase/backup.sh` freezes `/data` with `fsfreeze` through `hermetarium attach`, then Restic reads the host file. A restore to another directory passes `e2fsck -fn`, and `/data/known` is `known-v1`. A fake model key that exists only in the gate file, and a fake gate private key planted at `mitm/ca.key`, are absent from the snapshot listing and from the restored tree. A second backup, while the guest has changed the disk only a little, adds little data. The guest is still writing after the script returns.

`tests/backup` `TestSecretRefusal`. Runs `omakase/backup.sh` with stand-in `restic`, `docker`, and `hermetarium` binaries. No Docker daemon and no KVM. The disk or the journal is `squid.conf`, a symlink to `squid.conf`, a `..` path that reaches `squid.conf`, `mitm/ca.key`, `var/<id>/`, or `var/<id>`. The script exits with the refusal and does not run `restic backup`.

`tests/backup` `TestBackupThawOnResticFailure`. Needs Docker and `/dev/kvm`. Same writing habitat as `TestBackupWhileWriting`. A `restic` ahead of the real binary on `PATH` exits non-zero for `backup` after the guest is frozen. The script fails, and the guest writes a later `WRITE_n`, so the EXIT trap thawed `/data`.

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

`printenv` does not contain `htm-test-key`. `harness.AssertInboundNoVendor` with the inhabitant's vendor host, as in the agentd mock. A POST `Run id -u and reply with only the number.` is attempted with a 20s timeout. A failed call ends the subtest successfully. A successful call continues into the agentd exercise (two further POSTs, reply contains `0`, inbound and vendor host in the I/O log).

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
