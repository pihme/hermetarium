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
A "Release-Notes:" trailer on that same commit is copied into the GitHub release
notes (one bullet per trailer line). Without it, the notes fall back to
"version set by a Release-As footer in <sha>".

Release notes of a normal release: one bullet per fix/feat/perf commit subject,
plus one bullet per Release-Notes line that a commit adds to the artifact's
source (a squash merge drops the commit body, so a source line is the only
note that reaches main). Such a line counts exactly when:
  - it is a whole line "Release-Notes: <text>", optionally indented and
    optionally behind a // or # comment marker, e.g.
        // Release-Notes: <text>
            # Release-Notes: <text>
  - it is added in a file under that artifact's paths (the table above), and
    the file is not a test or doc: *_test.go, *.md and anything under a
    testdata/, fixtures/ or vendor/ directory never count;
  - the same commit does not also remove a note with the same text (a moved
    note is not new). Renamed files only count their changed lines.
Notes elsewhere (tests/, docs/, .github/, examples/, inhabitants/) never count.
The same note text appears once per release.
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
            "nix/reference-env/flake.lock",
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
        "paths": ["porter/", "nix/reference-env/flake.lock"],
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


# A Release-Notes line in a -U0 diff: + (added) or - (removed), optional
# indentation, optional // or # comment marker.
_DIFF_NOTE = re.compile(r"^([+-])[ \t]*(?:(?://|#)[ \t]*)?Release-Notes:[ \t]+(\S(?:.*\S)?)\s*$")

# Never a source of release notes, even under an artifact's paths.
NOTE_EXCLUDES = [
    ":(exclude,glob)**/*_test.go",
    ":(exclude,glob)**/*.md",
    ":(exclude,glob)**/testdata/**",
    ":(exclude,glob)**/fixtures/**",
    ":(exclude,glob)**/vendor/**",
]


def diff_note_lines(sha: str, paths: List[str]) -> List[str]:
    """Release-Notes lines this commit adds under paths (see module docstring).

    A squash merge onto main keeps the subject and drops the body, so a
    trailer in the message never reaches the published notes. An added
    source line still does. The diff is limited to paths minus NOTE_EXCLUDES,
    so a large vendored or generated change elsewhere is never read.
    """
    r = subprocess.run(
        ["git", "--no-pager", "show", "-U0", "-M", "--format=", "--no-color", "--no-ext-diff", sha, "--"]
        + paths
        + NOTE_EXCLUDES,
        capture_output=True,
        text=True,
    )
    if r.returncode != 0:
        err = (r.stderr or "").rstrip()
        raise SystemExit(f"command failed ({r.returncode}): git show {sha}\n{err}")
    added: List[str] = []
    removed: List[str] = []
    for line in (r.stdout or "").splitlines():
        if line.startswith("+++ ") or line.startswith("--- "):
            continue
        m = _DIFF_NOTE.match(line)
        if m:
            text = " ".join(m.group(2).split())
            (added if m.group(1) == "+" else removed).append(text)
    found = []
    for text in added:
        if text in removed:
            removed.remove(text)  # moved, not new
        elif text not in found:
            found.append(text)
    return found


def log_kinds(since: Optional[str], paths: List[str]) -> List[Tuple[str, str]]:
    rng = f"{since}..HEAD" if since else "HEAD"
    fmt = "%H%x1f%s%x1f%b%x1e"
    args = ["git", "log", rng, "--format=" + fmt, "--"] + paths
    out = run(args, check=False)
    rows: List[Tuple[str, str]] = []
    seen_notes = set()
    for rec in out.split("\x1e"):
        rec = rec.strip()
        if not rec:
            continue
        sha, _, rest = rec.partition("\x1f")
        subj, _, body = rest.partition("\x1f")
        subj = subj.strip()
        sha = sha.strip()
        kind = commit_kind(subj, body)
        if kind:
            rows.append((kind, subj))
        for line in diff_note_lines(sha, paths):
            if line not in seen_notes:
                rows.append(("", line))
                seen_notes.add(line)
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


def release_as_note_lines(sha: str) -> List[str]:
    """Release-Notes trailers on the Release-As commit; otherwise a fallback line."""
    out = subprocess.run(
        ["git", "log", "-1", "--format=%(trailers:key=Release-Notes,valueonly,separator=%x1d)", sha],
        capture_output=True,
        text=True,
    ).stdout.strip()
    lines = [v.strip() for v in out.split("\x1d") if v.strip()]
    return lines or [f"version set by a Release-As footer in {sha}"]


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


def build_checked(c: dict, ver: str) -> None:
    """Build a static (CGO_ENABLED=0) binary and fail if it could not load on older distros."""
    # gatedns.bin is gitignored. go:embed needs it on disk before the host build.
    if c["name"] == "hermetarium":
        run(["make", "gatedns"])
    build = [a.format(ver=ver) if "{ver}" in a else a for a in c["build"]]
    os.environ["CGO_ENABLED"] = "0"
    run(build)
    run(["bash", os.path.join(os.path.dirname(__file__), "check-glibc-baseline.sh"), os.path.join("dist", c["asset"])])


def main() -> int:
    if "--check-build" in sys.argv:
        os.makedirs("dist", exist_ok=True)
        for c in COMPONENTS:
            build_checked(c, "0.0.0-check")
        return 0
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
            have = {s for _, s in kinds_subj}
            for line in release_as_note_lines(forced[1]):
                if line not in have:
                    kinds_subj.append(("", line))
                    have.add(line)
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
        build_checked(c, nxt)
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
                "LICENSE",
                "THIRD_PARTY_NOTICES.md",
            ]
        )
        released = True
    return 0 if (released or dry) else 0


if __name__ == "__main__":
    sys.exit(main())
