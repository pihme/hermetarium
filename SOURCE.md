# gh-pages source

This branch holds the static GitHub Pages site for `pihme/hermetarium`.

Generated on 2026-09-26 from `main` at commit `f9542a68796afddfeb71f018fb509e8b8c45eb6f`.
Self-contained `index.html`, `chronik/index.html` (Chronicles) and `jigsaw/index.html` (family page, from `family.json`), all with inline CSS, no build step, no trackers; plus favicons (`favicon.svg`, `favicon.png`, `apple-touch-icon.png`) and `.nojekyll`.
Generator: `gen.py` (layout B, sidebar handbook) + per-site content script + `chronik-<repo>.json` chronicle data + `family.json` project-family registry + `status-<repo>.json` (Current status; open issues and releases pulled live via `gh` at build time).
Regenerate when `main` changes; do not merge this branch into `main`.
