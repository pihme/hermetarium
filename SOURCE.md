# gh-pages source

This branch holds the static GitHub Pages site for `pihme/hermetarium`.

Generated on 2026-09-26 from `main` at commit `e19e1b39aff410de65750e8f8d4099ad7d689511`.
Self-contained `index.html`, `chronik/index.html` (Chronicles) and `jigsaw/index.html` (family page, from `family.json`), all with inline CSS, no build step, no trackers; plus favicons (`favicon.svg`, `favicon.png`, `apple-touch-icon.png`) and `.nojekyll`.
Generator: `gen.py` (layout B, sidebar handbook) + per-site content script + `chronik-<repo>.json` chronicle data + `family.json` project-family registry + `status-<repo>.json` (Current status; open issues and releases pulled live via `gh` at build time).
Regenerate when `main` changes; do not merge this branch into `main`.
