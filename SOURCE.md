# gh-pages source

This branch holds the static GitHub Pages site for `pihme/hermetarium`.

Generated on 2026-10-03 from `main` at commit `0e5e288d570a74cc03a537a485fde79ef04f8020`.
Self-contained `index.html`, `chronik/index.html` (Chronicles) `jigsaw/index.html` (family page, from `family.json`) and, where present, `namesake/index.html` (from `namesake-<repo>.json`), all with inline CSS, no build step, no trackers; plus favicons (`favicon.svg`, `favicon.png`, `apple-touch-icon.png`) and `.nojekyll`.
Generator: `gen.py` (layout B, sidebar handbook) + per-site content script + `chronik-<repo>.json` chronicle data + `family.json` project-family registry + `status-<repo>.json` (Current status; open issues and releases pulled live via `gh` at build time).
Regenerate when `main` changes; do not merge this branch into `main`.
