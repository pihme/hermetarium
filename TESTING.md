# Test spec

What the suite checks in this tree today. Product rules for tests stay in [SPEC.md §11](SPEC.md#11-tests). This file is the inventory of the running tests.

## Commands

| Command | Packages | Gate |
| --- | --- | --- |
| `make test` | `./supervisor` `./firecracker-helper` `./examples/...` `./porter` `./tests/...` | Merge gate. CI job `test` on push to `main`, pull request, and `workflow_dispatch`. |
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
| `TestNoInboundURLMessage` | `noInbound: true` is the sentence `this habitat has no inbound address`. A stored URL is returned. An empty URL without `noInbound` is an error. |
| `TestNoInbound` | Needs Docker and KVM. An image that only sleeps starts on both walls with `--no-inbound` and a gate file that has no inhabitant peer and no inbound port. No host port is published. `url` prints that sentence. |
| `TestChooseID` | With no `--id`, `create` still makes a 12-character id. `--id claude` is kept. A blank, a space, a slash, a leading `-` or `.`, and a name longer than 63 characters are rejected. `claude` and `grok` do not share a subnet. |
| `TestCreateRejectsSizeFlagsOnWeakWall` | `create --wall weak` with `--mem` exits 2. |
| `TestCreateRejectsDataOnWeakWall` | `create --wall weak` with `--data` exits 2. |
| `TestCreateRejectsBadDataFlag` | On the strong wall, `--data` with no file, or a value that starts with `-`, exits 2 and creates no file. A `--data` path containing `:` exits 2 and creates no file. |
| `TestEnsureDataDisk` | An existing `--data` file is accepted only when it is a regular file with ext4 magic `0xEF53` at offset `0x438`. A directory, a non-ext4 file, and a path containing `:` are rejected. The colon path is not created. An accepted file is not resized. |
| `TestDataDiskLock` | Taking the data-disk lock fails with "in use" when another flock already holds the lock file. It also fails with "in use" in a second process while the first call still holds the lock. |
| `TestVersionCommand` | `version` exits 0 and prints `supervisor.Version`. |
| `TestAlpineImageFromDockerfile` | `AlpineImage` is `alpine:3.24`, the same tag parsed from the embedded helper Dockerfile. |
| `TestSquidDockerfile` | `SquidImage` is `hermetarium-squid:local`. The embedded Dockerfile starts `FROM ubuntu/squid:` and installs `squid-openssl`, and it requires `/usr/lib/squid/security_file_certgen`. |
| `TestDestroyRemovesGateVolumes` | Needs Docker. Starting the gate creates the image's anonymous volumes. `DestroyWeak` and `DestroyStrong` remove those volumes. |
| `TestChecksumMismatchIsRefused` | A file whose SHA-256 is not the pinned kernel checksum is deleted and the check returns an error. Matching bytes are kept. |
| `TestFirecrackerStamp` | The extract stamp is the pinned tarball checksum plus the binary's SHA-256. A missing stamp, a stamp without that hash, or a swapped binary is not a cache hit. A rejected tarball deletes the stamp. A matching tarball keeps it. |
| `TestGateFilesHardened` | Shipped gate files do not set `DONT_VERIFY_PEER` or `dns_v4_first`, and no active line uses `myport`. |
| `TestSquidStartupQuiet` | Needs Docker. Squid started with the echo gate file does not log the `myport` or `dns_v4_first` warnings. |
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
| `TestExtract` | The same two scripts extract executable. The entrypoint contains `fc-helper`. The pack script contains `rootfs.ext4`. A second extract leaves the entrypoint mtime unchanged. |
| `TestEcho` | The echo handler returns the POST body with status 200. |
| `TestHealth` | agentd `GET /` returns 200 and a body containing `ok`. |
| `TestToolCommand` | A tool-use payload `{"command":"id -u"}` yields the command `id -u`. |
| `TestCmdFirstTurn` | Porter argv for a first turn: `claude -p --output-format text --dangerously-skip-permissions` and `grok --always-approve -p`. Claude's environment includes `CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS=1`. |
| `TestCmdContinuesSession` | After a turn has run, `claude` and `grok` gain `--continue`. Claude's flag stays in front of `-p`. Grok's follows the prompt, because `-p` is `--single`. |
| `TestCmdUnknownHarness` | An unknown `HARNESS` value returns an error. |

Porter tests compare argv. They do not exec `claude` or `grok`.

### Hello-world

`tests/hello-world` `TestHelloWorld`. Image `examples/echo`.

**Weak.** `ping -c 1 -W 2 1.1.1.1` exits non-zero in the box, while a TCP connect to `1.1.1.1:443` from the gate succeeds (the gate is the only way out). `harness.AssertNeighborBlocked`: a `busybox` container on Docker's default bridge cannot connect to the gate on ports 3128, 3129, 18081 or 80. `curl` of `http://probe.hermetarium.test/hello` returns `hermetarium-ok`. The I/O log contains destination `probe.hermetarium.test` or the gate IP. Then the echo exercise below.

**Strong.** Guest serial shows `GUEST_UNAME` starting with `6.1.188`, different from the host kernel in `/proc/sys/kernel/osrelease`, and contains `LEAK_FAIL`. The helper container's `HostConfig.Privileged` is false. `harness.AssertNeighborBlocked` on the helper container, as on the weak wall. Then the echo exercise. The strong guest does not fetch the probe.

**Image.** The same echo image passed as `CreateOpts.Image` with `examples/echo/squid.conf` and no probe sidecar. Echo exercise only.

**Echo exercise** (`tests/echo`). Two POSTs to the same inbound URL: body `ping` returns `ping`, body `pong` returns `pong`. After a log sync, the I/O log has direction `in`.

### Data disk

`tests/data-disk` `TestDataDiskSurvivesDestroy`. Needs Docker and `/dev/kvm`. Strong wall, `--no-inbound`, a 32 MiB sparse ext4 file. The first boot writes `/data/marker`. A second `create` with that file while the first habitat is running is refused. After `destroy` the file is still there, mode `0600`. `e2fsck -p` then a read-only mount with `noexec,nosuid,nodev` shows the marker and refuses to run a program from the disk. A new `create --data` of the same file sees the marker.

`TestDataDiskMountFailure`. Needs Docker and `/dev/kvm`. A file with ext4 magic `0xEF53` at offset `0x438` and no valid superblock makes create fail. No container for that id is left. The inhabitant does not write `/data/marker` on the root disk.

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
