# Backup

A host script and a systemd user timer. They back up one running habitat with [Restic](https://restic.net/). The habitat keeps running. The script freezes `/data` through `hermetarium attach` for the read, then unfreezes it.

Restic reads a disk file over more than one write. A read of the live file is not one moment, so the script does not do that. The habitat image needs `fsfreeze` (util-linux). Debian images already have it. An Alpine image needs `apk add util-linux`.

## What is backed up

- `var/<id>/io.jsonl`. The script runs `hermetarium logs` first, so this file matches the access log.
- `var/<id>/access.log` (Squid's access log)
- Git Warden's journal for that habitat
- the data disk (`create --data`)

`var/<id>/` is under `HERMETARIUM_ROOT`, else this checkout, else `$XDG_DATA_HOME/hermetarium` or `~/.local/share/hermetarium`.

## Left out

- `var/<id>/squid.conf`, the gate file. It holds the model key.
- `var/<id>/mitm/ca.key`, the gate's private key.

The script refuses a data disk or a journal path that is one of those files, or a directory that contains them.

## Settings

Where the repository lives and who holds its password is your choice. The script does not pick a repository and it does not create a password.

`~/.config/hermetarium/backup.env` is the file the timer reads.

```
RESTIC_REPOSITORY=/srv/hermetarium-restic
RESTIC_PASSWORD_FILE=/home/you/.config/hermetarium/restic-password
HERMETARIUM_BACKUP_SCRIPT=/path/to/hermetarium/omakase/backup.sh
PATH=/usr/local/bin:/usr/bin:/bin
HERMETARIUM_ID=opencode
HERMETARIUM_DATA_DISK=/srv/opencode.ext4
GIT_WARDEN_JOURNAL=/srv/opencode.journal
```

| Setting | Meaning |
| --- | --- |
| `RESTIC_REPOSITORY` | Restic repository. Required. |
| `RESTIC_PASSWORD_FILE` | File whose contents are the repository password. Required unless `RESTIC_PASSWORD` is set. |
| `RESTIC_PASSWORD` | Password in the environment. Prefer the file. |
| `HERMETARIUM_BACKUP_SCRIPT` | Absolute path of this script. Required. The timer runs this and nothing else. |
| `PATH` | Command search path for the timer. Required to contain `restic`. The user manager does not use a login PATH. |
| `HERMETARIUM_ID` | Habitat id. Required. The habitat must be running. |
| `HERMETARIUM_DATA_DISK` | Host path passed to `create --data`. Required. It must be the disk that habitat has mounted. |
| `GIT_WARDEN_JOURNAL` | File or directory of Git Warden's journal for this habitat. Required. |
| `HERMETARIUM_ROOT` | Data directory. Optional. Same rule as the `hermetarium` command. Not the script path. |
| `HERMETARIUM` | `hermetarium` binary. Optional. Otherwise `PATH`, then `$HERMETARIUM_ROOT/bin/hermetarium`. |

This repository does not install Git Warden and does not define a journal format. Point `GIT_WARDEN_JOURNAL` at the journal you keep for that habitat.

Initialize the repository once, with those settings:

```bash
restic init
```

`backup.env` sets `HERMETARIUM_BACKUP_SCRIPT` and a `PATH` that contains `restic`. The systemd user manager does not use a login `PATH`. An empty `HERMETARIUM_ROOT` is not a script path.

## Timer

```bash
mkdir -p ~/.config/systemd/user ~/.config/hermetarium
cp omakase/hermetarium-backup.service omakase/hermetarium-backup.timer ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now hermetarium-backup.timer
```

`loginctl enable-linger` keeps the timer running when you are logged out.

If the script is killed while `/data` is frozen, unfreeze it:

```bash
hermetarium attach YOUR_ID -- fsfreeze -u /data
```

## Restore

`restic restore` writes under `--target` and keeps each file's original path.

```bash
restic restore latest --target /tmp/restore
```

Check the data disk before a habitat attaches it again. `e2fsck -fn` must finish clean. Do not attach a disk that fails.

```bash
DISK=/tmp/restore/path/to/disk.ext4
e2fsck -fn "$DISK"
```

A clean check exits 0. If it reports errors, repair, then check again. Exit status 1 from `e2fsck -p` means it changed the filesystem.

```bash
e2fsck -p "$DISK" || test $? -le 1
e2fsck -fn "$DISK"
```

Only a disk that passes `e2fsck -fn` goes back to `create`:

```bash
hermetarium create --wall strong --data "$DISK" --image IMAGE --acl ACL
```

Unmount the file first if you mounted it to read it. Do not check or mount the copy a running habitat still has open. `destroy` does not delete the data disk.
