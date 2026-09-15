#!/usr/bin/env python3
"""Bump hermetarium and porter from conventional commits + path filters.

Tags: hermetarium/vX.Y.Z and porter/vX.Y.Z
Commit type (feat/fix/BREAKING) chooses major/minor/patch.
Only commits that touch a component's paths count for that component.
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
            "squid/",
            "firecracker-helper/",
            "go.mod",
            "Makefile",
        ],
        "asset": "hermetarium-linux-amd64",
        "build": [
            "go",
            "build",
            "-ldflags",
            "-s -w -X hermetarium/supervisor.Version={ver}",
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
    r = subprocess.run(args, check=check, capture_output=True, text=True)
    return r.stdout.strip()


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
        if not kind:
            touched = run(["git", "rev-list", "-1", "HEAD", "--"] + c["paths"], check=False)
            if not prev and touched:
                kind = "minor"
                kinds_subj = [("minor", "initial release")]
            else:
                print(f"{c['name']}: no releasable commits since {prev or 'start'}")
                continue
        base = parse_semver(prev, prefix) if prev else (0, 0, 0)
        nxt = fmt_ver(bump(base, kind))
        tag = prefix + nxt
        notes = "\n".join(f"- {s}" for _, s in kinds_subj) or f"{c['name']} {nxt}"
        print(f"{c['name']}: {prev or '0.0.0'} -> {tag} ({kind})")
        if dry:
            continue
        build = [a.format(ver=nxt) if "{ver}" in a else a for a in c["build"]]
        run(build)
        run(["git", "tag", "-a", tag, "-m", f"{c['name']} {nxt}"])
        run(["git", "push", "origin", tag])
        run(
            [
                "gh",
                "release",
                "create",
                tag,
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
