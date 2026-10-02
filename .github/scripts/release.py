#!/usr/bin/env python3
"""Bump hermetarium and porter from conventional commits + path filters.

Tags: hermetarium/vX.Y.Z and porter/vX.Y.Z
Commit type (feat/fix/BREAKING) chooses major/minor/patch.
Only commits that touch a component's paths count for that component.

While the version is 0.x a breaking change bumps the minor version instead
(SemVer: anything may change before 1.0.0; 1.0.0 is a deliberate decision).

Release-As: a "Release-As: <artifact>@X.Y.Z" commit footer (git trailer) since the
artifact's last tag sets exactly that version, from any commit and any path, so an
empty commit triggers it (one footer line per artifact):
  git commit --allow-empty -m "chore: release 1.0" -m "Release-As: hermetarium@1.0.0"
The bare form "Release-As: X.Y.Z" is ignored with a warning: it names no artifact.
Upwards only: a version at or below the current one is ignored with a warning.
Several footers for one artifact: the highest wins.
"""
from __future__ import annotations

import os
import re
import subprocess
import sys
from typing import List, Optional, Tuple

COMPONENTS = [
    {
        "name": "hermetarium",
        "tag_prefix": "hermetarium/v",
        "paths": [
            "supervisor/",
            "firecracker-helper/",
            "go.mod",
            "Makefile",
        ],
        "asset": "hermetarium-linux-amd64",
        "build": [
            "go",
            "build",
            "-ldflags",
            "-s -w -X github.com/pihme/hermetarium/supervisor.Version={ver}",
            "-o",
            "dist/hermetarium-linux-amd64",
            "./supervisor/cmd/hermetarium",
        ],
    },
    {
        "name": "porter",
        "tag_prefix": "porter/v",
        "paths": ["porter/"],
        "asset": "porter-linux-amd64",
        "build": [
            "go",
            "build",
            "-ldflags",
            "-s -w -X main.Version={ver}",
            "-o",
            "dist/porter-linux-amd64",
            "./porter",
        ],
    },
]


def run(args: List[str], check: bool = True) -> str:
    r = subprocess.run(args, capture_output=True, text=True)
    if r.stdout.strip():
        print(r.stdout.rstrip())
    if r.returncode != 0:
        err = (r.stderr or r.stdout or "").rstrip()
        if err:
            print(err, file=sys.stderr)
        if check:
            raise SystemExit(f"command failed ({r.returncode}): {' '.join(args)}")
    return (r.stdout or "").strip()


def last_tag(prefix: str) -> Optional[str]:
    out = run(["git", "tag", "-l", prefix + "*", "--sort=-v:refname"], check=False)
    tags = [t for t in out.splitlines() if t]
    return tags[0] if tags else None


def parse_semver(tag: str, prefix: str) -> Tuple[int, int, int]:
    raw = tag[len(prefix) :] if tag.startswith(prefix) else tag
    m = re.match(r"^(\d+)\.(\d+)\.(\d+)", raw)
    if not m:
        return (0, 0, 0)
    return int(m.group(1)), int(m.group(2)), int(m.group(3))


def bump(ver: Tuple[int, int, int], kind: str) -> Tuple[int, int, int]:
    major, minor, patch = ver
    if kind == "major" and major == 0:
        kind = "minor"  # pre-1.0: breaking changes bump the minor version
    if kind == "major":
        return (major + 1, 0, 0)
    if kind == "minor":
        return (major, minor + 1, 0)
    return (major, minor, patch + 1)


def commit_kind(subject: str, body: str) -> Optional[str]:
    text = subject + "\n" + body
    if re.search(r"^BREAKING CHANGE:", text, re.M) or re.match(
        r"^\w+(\([^)]+\))?!:", subject
    ):
        return "major"
    m = re.match(r"^(\w+)(\([^)]+\))?:", subject)
    if not m:
        return None
    t = m.group(1)
    if t == "feat":
        return "minor"
    if t in ("fix", "perf"):
        return "patch"
    return None


def log_kinds(since: Optional[str], paths: List[str]) -> List[Tuple[str, str]]:
    rng = f"{since}..HEAD" if since else "HEAD"
    fmt = "%s%x1f%b%x1e"
    args = ["git", "log", rng, "--format=" + fmt, "--"] + paths
    out = run(args, check=False)
    rows = []
    for rec in out.split("\x1e"):
        rec = rec.strip()
        if not rec:
            continue
        subj, _, body = rec.partition("\x1f")
        kind = commit_kind(subj.strip(), body)
        if kind:
            rows.append((kind, subj.strip()))
    return rows


RELEASE_AS = re.compile(r"^v?(\d+)\.(\d+)\.(\d+)$")
_warned = set()


def warn(msg: str) -> None:
    if msg in _warned:
        return
    _warned.add(msg)
    print(("::warning::" if os.environ.get("GITHUB_ACTIONS") else "warning: ") + msg, file=sys.stderr)


def release_as(since: Optional[str], name: str) -> Optional[Tuple[Tuple[int, int, int], str]]:
    """Highest Release-As footer for this artifact since its last tag: (version, short sha)."""
    names = [c["name"] for c in COMPONENTS]
    rng = f"{since}..HEAD" if since else "HEAD"
    out = subprocess.run(["git", "log", rng, "--format=%h%x1f%(trailers:key=Release-As,valueonly,separator=%x1d)%x1e"],
                         capture_output=True, text=True).stdout
    best = None
    for rec in out.split("\x1e"):
        sha, _, vals = rec.strip().partition("\x1f")
        for raw in (v.strip() for v in vals.split("\x1d")):
            if not raw:
                continue
            target, _, ver = raw.rpartition("@")
            if not target and len(names) > 1:
                warn(f"{sha}: 'Release-As: {raw}' ignored: name the artifact, e.g. 'Release-As: {names[0]}@{raw}' ({', '.join(names)})")
                continue
            if target and target not in names:
                warn(f"{sha}: 'Release-As: {raw}' ignored: unknown artifact {target!r} ({', '.join(names)})")
                continue
            if target and target != name:
                continue
            m = RELEASE_AS.match(ver)
            if not m:
                warn(f"{sha}: 'Release-As: {raw}' ignored: not X.Y.Z")
                continue
            v = (int(m.group(1)), int(m.group(2)), int(m.group(3)))
            if best is None or v > best[0]:
                best = (v, sha)
    return best


def strongest(kinds: List[str]) -> Optional[str]:
    if "major" in kinds:
        return "major"
    if "minor" in kinds:
        return "minor"
    if "patch" in kinds:
        return "patch"
    return None


def fmt_ver(v: Tuple[int, int, int]) -> str:
    return f"{v[0]}.{v[1]}.{v[2]}"


def main() -> int:
    dry = "--dry-run" in sys.argv or os.environ.get("RELEASE_DRY_RUN") == "1"
    os.makedirs("dist", exist_ok=True)
    released = False
    for c in COMPONENTS:
        prefix = c["tag_prefix"]
        prev = last_tag(prefix)
        kinds_subj = log_kinds(prev, c["paths"])
        kind = strongest([k for k, _ in kinds_subj])
        base = parse_semver(prev, prefix) if prev else (0, 0, 0)
        forced = release_as(prev, c["name"])
        if forced and forced[0] <= base:
            warn(f"{forced[1]}: 'Release-As' {c['name']} {fmt_ver(forced[0])} ignored: not above {fmt_ver(base)}")
            forced = None
        if forced:
            kind = "Release-As " + forced[1]
            kinds_subj = kinds_subj + [("", f"version set by a Release-As footer in {forced[1]}")]
        if not kind:
            touched = run(["git", "rev-list", "-1", "HEAD", "--"] + c["paths"], check=False)
            if not prev and touched:
                kind = "minor"
                kinds_subj = [("minor", "initial release")]
            else:
                print(f"{c['name']}: no releasable commits since {prev or 'start'}")
                continue
        nxt = fmt_ver(forced[0]) if forced else fmt_ver(bump(base, kind))
        tag = prefix + nxt
        notes = "\n".join(f"- {s}" for _, s in kinds_subj) or f"{c['name']} {nxt}"
        print(f"{c['name']}: {prev or '0.0.0'} -> {tag} ({kind})")
        if dry:
            continue
        build = [a.format(ver=nxt) if "{ver}" in a else a for a in c["build"]]
        run(build)
        sha = os.environ.get("GITHUB_SHA") or run(["git", "rev-parse", "HEAD"])
        # Let GitHub mint the tag (no local git identity needed for git tag -a).
        run(
            [
                "gh",
                "release",
                "create",
                tag,
                "--target",
                sha,
                "--title",
                f"{c['name']} {nxt}",
                "--notes",
                notes,
                os.path.join("dist", c["asset"]),
            ]
        )
        released = True
    return 0 if (released or dry) else 0


if __name__ == "__main__":
    sys.exit(main())
