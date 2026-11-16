"""Unit tests for release.py: python3 -m unittest discover -s .github/scripts

CI runs them through tests/harness/release_script_test.go (unit leg).
"""
from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import release  # noqa: E402

PATHS = release.COMPONENTS[0]["paths"]  # hermetarium: supervisor/, ...


class DiffNoteLinesTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.repo = self._tmp.name
        self._cwd = os.getcwd()
        os.chdir(self.repo)
        self.git("init", "-q", "-b", "main")
        self.write("supervisor/main.go", "package supervisor\n")
        self.commit("chore: init")

    def tearDown(self) -> None:
        os.chdir(self._cwd)
        self._tmp.cleanup()

    def git(self, *args: str) -> str:
        env = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
        return subprocess.run(
            ["git", "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", *args],
            check=True, capture_output=True, text=True, env=env,
        ).stdout.strip()

    def write(self, path: str, text: str) -> None:
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        with open(path, "w") as f:
            f.write(text)

    def commit(self, msg: str) -> str:
        self.git("add", "-A")
        self.git("commit", "-q", "-m", msg)
        return self.git("rev-parse", "HEAD")

    def notes(self, sha: str):
        return release.diff_note_lines(sha, PATHS)

    def test_plain_comment_note_kept(self) -> None:
        self.write("supervisor/a.go", "package supervisor\n\n// Release-Notes: plain note\n")
        self.assertEqual(self.notes(self.commit("fix: a")), ["plain note"])

    def test_hash_note_in_makefile_kept(self) -> None:
        self.write("Makefile", "# Release-Notes: make note\nall:\n")
        self.assertEqual(self.notes(self.commit("fix: make")), ["make note"])

    def test_indented_note_kept(self) -> None:
        self.write("supervisor/a.go", "package supervisor\n\nfunc f() {\n\t// Release-Notes: tab note\n}\n")
        self.write("firecracker-scripts/x.sh", "f() {\n    #  Release-Notes:   spaced   note \n}\n")
        self.assertEqual(sorted(self.notes(self.commit("fix: b"))), ["spaced note", "tab note"])

    def test_moved_note_ignored(self) -> None:
        self.write("supervisor/a.go", "package supervisor\n\n// Release-Notes: old note\nvar a = 1\n")
        self.commit("fix: c")
        self.write("supervisor/a.go", "package supervisor\n\nvar a = 1\n")
        self.write("supervisor/b.go", "package supervisor\n\n    // Release-Notes: old note\n")
        self.assertEqual(self.notes(self.commit("refactor: move")), [])

    def test_renamed_file_ignored(self) -> None:
        body = "package supervisor\n\n// Release-Notes: renamed note\n" + "var x = 1\n" * 20
        self.write("supervisor/old.go", body)
        self.commit("fix: d")
        os.rename("supervisor/old.go", "supervisor/new.go")
        self.assertEqual(self.notes(self.commit("refactor: rename")), [])

    def test_excluded_paths_ignored(self) -> None:
        note = "// Release-Notes: stray\n"
        self.write("supervisor/a_test.go", note)
        self.write("supervisor/testdata/x.go", note)
        self.write("supervisor/fixtures/x.go", note)
        self.write("supervisor/vendor/x/x.go", note)
        self.write("supervisor/README.md", "Release-Notes: stray\n")
        self.write("tests/foo/foo.go", note)
        self.write("docs/notes.md", "Release-Notes: stray\n")
        self.write(".github/scripts/x.py", "# Release-Notes: stray\n")
        self.assertEqual(self.notes(self.commit("fix: e")), [])

    def test_not_a_whole_line_ignored(self) -> None:
        self.write("supervisor/a.go", 'package supervisor\n\nvar s = "Release-Notes: in a string"\n// see Release-Notes: here\n')
        self.assertEqual(self.notes(self.commit("fix: f")), [])

    def test_duplicate_text_once(self) -> None:
        self.write("supervisor/a.go", "// Release-Notes: twice\n")
        self.write("porter/b.go", "// Release-Notes: twice\n")
        self.write("supervisor/c.go", "// Release-Notes: twice\n")
        self.assertEqual(self.notes(self.commit("fix: g")), ["twice"])


if __name__ == "__main__":
    unittest.main()
