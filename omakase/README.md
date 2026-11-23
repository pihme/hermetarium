# Backup

A host script and a systemd user timer. They back up one running habitat with [Restic](https://restic.net/). The script pauses the Firecracker container `htm-<id>-fc` and the guest for the restic read, then unpauses that same container. The habitat continues without a restart. A restart kills Firecracker. While the container is paused, `url` and `attach` to that habitat do not answer. The pause bound defaults to 900 seconds (`HERMETARIUM_PAUSE_SECONDS`, at most 21600).

## What is backed up

- `var/<id>/rootfs.ext4`, the root file the guest is running
- `var/<id>/io.jsonl`. The script runs `hermetarium logs` first, so this file matches the access log.
- `var/<id>/access.log` (Squid's access log)
- Git Warden's journal for that habitat

`var/<id>/` is under `HERMETARIUM_ROOT`, else this checkout, else `$XDG_DATA_HOME/hermetarium` or `~/.local/share/hermetarium`.

## Left out

- `var/<id>/squid.conf`, the gate file. It holds the model key.
- `var/<id>/mitm/ca.key`, the gate's private key.

The script refuses a journal path that is one of those files, a hard link to one of them, or a directory that contains one of them, including by a hard link or a nested mount. That includes another habitat's `var/<id>/squid.conf` and `var/<id>/mitm/ca.key` under the same data directory. It also refuses `/` and any directory that contains `var/`. The root file is under `var/<id>/` and is not one of those two secrets.

The running Firecracker container must be this habitat. `htm-<id>-fc` is used only when its `/log` mount is `var/<id>` under `HERMETARIUM_ROOT`. A habitat created before that name is `htm-fc-<id>`. The script pauses that container when `htm-<id>-fc` is absent. A different root that also has `var/<id>/squid.conf` is not this habitat.

## Settings

Where the repository lives and who holds its password is your choice. The script does not pick a repository and it does not create a password.

`~/.config/hermetarium/backup.env` is the file the timer reads.

```
RESTIC_REPOSITORY=/srv/hermetarium-restic
RESTIC_PASSWORD_FILE=/home/you/.config/hermetarium/restic-password
HERMETARIUM_BACKUP_SCRIPT=/path/to/hermetarium/omakase/backup.sh
PATH=/usr/local/bin:/usr/bin:/bin
HERMETARIUM_ID=opencode
GIT_WARDEN_JOURNAL=/srv/opencode.journal
```

| Setting | Meaning |
| --- | --- |
| `RESTIC_REPOSITORY` | Restic repository. Required. |
| `RESTIC_PASSWORD_FILE` | File whose contents are the repository password. Required unless `RESTIC_PASSWORD` is set. |
| `RESTIC_PASSWORD` | Password in the environment. Prefer the file. |
| `HERMETARIUM_BACKUP_SCRIPT` | Absolute path of this script. Required. The timer runs this and nothing else. |
| `PATH` | Command search path for the timer. Required to contain `restic` and `timeout`. The user manager does not use a login PATH. |
| `HERMETARIUM_ID` | Habitat id. Required. The habitat must be running. |
| `GIT_WARDEN_JOURNAL` | File or directory of Git Warden's journal for this habitat. Required. |
| `HERMETARIUM_ROOT` | Data directory. Optional. Same rule as the `hermetarium` command. Not the script path. |
| `HERMETARIUM` | `hermetarium` binary. Optional. Otherwise `PATH`, then `$HERMETARIUM_ROOT/bin/hermetarium`. |
| `HERMETARIUM_PAUSE_SECONDS` | Longest `restic backup` may keep the Firecracker container paused. Optional. Default 900. At most 21600 (6 hours). |

This repository does not install Git Warden and does not define a journal format. Point `GIT_WARDEN_JOURNAL` at the journal you keep for that habitat.

Initialize the repository once, with those settings:

```bash
restic init
```

`backup.env` sets `HERMETARIUM_BACKUP_SCRIPT` and a `PATH` that contains `restic` and `timeout`. The systemd user manager does not use a login `PATH`. An empty `HERMETARIUM_ROOT` is not a script path.

`chmod 600` the restic password file and `backup.env`. `backup.env` contains the password when `RESTIC_PASSWORD` is set.

## Timer

```bash
mkdir -p ~/.config/systemd/user ~/.config/hermetarium
cp omakase/hermetarium-backup.service omakase/hermetarium-backup.timer ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now hermetarium-backup.timer
```

`loginctl enable-linger` keeps the timer running when you are logged out.

`restic backup` is stopped after `HERMETARIUM_PAUSE_SECONDS` (default 900, at most 6 hours). The script unpauses `htm-<id>-fc` then. If `restic` hits that limit or exits non-zero, the script unpauses and exits non-zero. The shell EXIT trap unpauses on a failure the shell can catch. SIGKILL skips the trap. If that happens, unpause the Firecracker container yourself:

```bash
docker unpause htm-YOUR_ID-fc
```

## Restore

`restic restore` writes the root file under `--target` and keeps each file's original path.

```bash
restic restore latest --target /tmp/restore
```

The snapshot is the host root file at the pause instant, without a guest freeze. In-flight guest writes may be missing. The restored ext4 may need journal replay. Run `e2fsck -p` on the restored file before `create --rootfs`. Exit status 1 means `e2fsck` changed the filesystem.

`create --wall strong --rootfs FILE` copies that ext4 file, keeping holes, to `var/<id>/rootfs.ext4` and boots the copy. With `--rootfs` set, Hermetarium does not pack `--image` into the root disk. `--image` and `--acl` stay required. The file must be a regular file with ext4 magic. The Docker wall rejects `--rootfs`. `destroy` deletes the copy and leaves `FILE`.

```bash
ROOT=/tmp/restore/path/to/var/YOUR_ID/rootfs.ext4
e2fsck -p "$ROOT" || test $? -le 1
hermetarium create --wall strong --rootfs "$ROOT" --image IMAGE --acl ACL
```

Do not pass the root file a running habitat still has open.

# Denied requests

The mail digest of denied requests (`digest.sh`, `hermetarium-digest.service`, `hermetarium-digest.timer`) and `mail.sh`, the wall's one mail configuration that the Push Guard's mail for a red push uses too, are in Git Warden: [omakase/README.md](https://github.com/pihme/git-warden/blob/main/omakase/README.md) in [pihme/git-warden](https://github.com/pihme/git-warden). Hermetarium does not send mail. Squid only writes the log, and the digest reads it with `hermetarium logs`. Each refused record there has `allowed` false and a `reason`. Besides `not-allowlisted`, that now includes `ca-untrusted` (the box did not trust the gate certificate) and `no-such-host` (the name did not resolve), so the digest reports those records too.

The settings files (`~/.config/hermetarium/digest.env`, `~/.config/hermetarium/mail.env`) and `var/<id>/digest.state` stay the same. On a host that runs the timer from this checkout, point `HERMETARIUM_DIGEST_SCRIPT` in `digest.env` at `omakase/digest.sh` in a Git Warden checkout: [Moving from Hermetarium](https://github.com/pihme/git-warden/blob/main/omakase/README.md#moving-from-hermetarium).
