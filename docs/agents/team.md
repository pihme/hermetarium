# Team rules for the repos

How the team works on GitHub in this repo. Same in every repo of the family; change it in presets first. Repo-specific rules stay in `AGENTS.md`. "The owner" is the human who owns these repos.

Read **For everyone** first, then the section for your role.

## For everyone

### Roles

| Role | Owns | Picks up |
| --- | --- | --- |
| **Maintainer** | Website, docs fixes, smaller changes; repo settings and labels; Dependabot PR review; release owner | `ready-for-maintainer` |
| **Developer** (a team bot or an external coding agent) | Larger feature and code work, as PRs | `ready-for-development` |
| **QA** | Review, testing, all merges (release PRs: see Releases), closing issues | `ready-for-qa` |

Other team members (coordination, design, research) make no repo changes. Design hands finished files to Maintainer and never approves its own work.

### Work order

**All roles: finish before you start.** Take what is closest to being merged first: a PR before an issue, a rebase before real work, a sent-back PR before new work.

Independent issues and PRs each move on their own. Hand one on as soon as your part is done and its CI is green, and keep working the others. An item that depends on another waits until that one has landed.

### Labels and handover

| Label | Meaning | Owner | Color |
| --- | --- | --- | --- |
| `ready-for-maintainer` | Website, docs fix or smaller change; new Dependabot PR | Maintainer | green `0E8A16` |
| `ready-for-development` | Larger feature or code work, fully specified | Developer | green `0E8A16` |
| `ready-for-qa` | Ready for review and testing | QA | yellow `FBCA04` |
| `needs-input` | Waiting for input or a decision from the owner | owner | orange `D93F0B` |
| `ready-for-human` | Needs the owner to implement or decide | owner | orange `D93F0B` |

**The label is the handover.** When your part of an issue or PR is done, remove your label on that item and set the next one. Each issue and each PR carries exactly one `ready-for-*` label.

- **Only the label decides whether a PR is merged**, never messages between team members. If a PR must not be merged (e.g. a release PR that is not ready yet), Maintainer removes its `ready-for-qa` or `ready-for-human` label.
- Setting a `ready-for-*` label removes every other `ready-for-*` label on that item in the same step; QA treats an item with two as not handed over.
- When Dependabot PRs are consolidated into one PR, the maintainer removes the `ready-for-*` label from the individual Dependabot PRs and closes them with a comment linking the consolidated PR.

| When | Who | Sets |
| --- | --- | --- |
| Issue triaged: website, docs, smaller change / larger code work | owner or Maintainer | `ready-for-maintainer` / `ready-for-development` |
| PR ready, CI green | Maintainer or Developer | `ready-for-qa` |
| Problems found | QA | back to the issue's previous label |
| Only a rebase or trivial conflict fix needed | QA | `ready-for-maintainer` (also for Developer's PRs) (Dependabot PRs: comment `@dependabot rebase` instead, keep `ready-for-qa`; Maintainer only if Dependabot cannot rebase) |
| Review passes | QA | merges, closes linked issues, removes `ready-for-qa` |
| Release PR from 1.0.0 on clean (chain done, CI green, release checks done) | Maintainer | `ready-for-human` (never `ready-for-qa`), then informs the owner |
| Dependabot opens a PR | Dependabot (`labels:` in `.github/dependabot.yml`) | `ready-for-maintainer` |
| Dependabot PR content checked, CI green (not during a release) | Maintainer | `ready-for-qa` (removes `ready-for-maintainer`), see Dependabot PRs |
| Dependabot PR with `ready-for-qa` | QA | reviews and merges (conflicts: see the rebase row) |
| Dependabot PR needs a decision | Maintainer | `ready-for-human` (removes `ready-for-maintainer`) |
| Issue already done, nothing to change | Maintainer or Developer | `ready-for-qa` on the issue; QA verifies and closes |
| Decision or input needed | any role | `needs-input` or `ready-for-human` |

- Work only on your own label. Issues without a pickup label stay untouched, even clear bugs; triage belongs to the owner.
- All bots write as the same GitHub account; the label history, not the author, shows who did what.
- Act on a pickup label only if the owner's account set it (check the issue's label events). If any other account set it, do not start work: set `needs-input` and leave it to the owner. Exception: `ready-for-maintainer` that Dependabot (`dependabot[bot]`) sets on its own PR when it opens it, through `labels:` in `.github/dependabot.yml`.

### Dependabot PRs

1. Dependabot PRs arrive with `ready-for-maintainer` (set by `.github/dependabot.yml`, next to `dependencies`).
2. Maintainer checks the content: the diff touches only what the title says, CI is green, no side effects on release inputs or lock files beyond the update, and the repo's glibc and release guards pass where they apply.
3. If it is fine, Maintainer sets `ready-for-qa` and removes `ready-for-maintainer`. QA then reviews and merges on its own.
4. If it needs a decision (e.g. a breaking major update or a change to a release input), Maintainer sets `ready-for-human` instead and informs the owner.
5. During a release Dependabot PRs stay open and keep `ready-for-maintainer` (see Releases). Release PRs from 1.0.0 on are still merged only by the owner.


### Comments

- **Comments carry results only:** review findings, what was done. No status comments ("starting review", "looking at it").
- **Sign every comment with your role first.** All roles post from the owner's account, so every PR/issue comment, review and description starts with a first line naming the role, e.g. `*Maintainer on behalf of the user*` or `*QA on behalf of the user*`, then the content.

### Initiative

Wanted: spot duplicate or redundant PRs, red CI on `main`, docs that no longer match the code; fix trivial things per `AGENTS.md`; update `AGENTS.md` and docs in the same PR as the change; bundle related small changes into one PR; one topic per PR.

Not wanted: scope extensions or new work during a release; design decisions (design proposes, the owner decides); editing published releases or release notes; reopening issues the owner closed.

Before handing anything over, check for duplicate PRs and open work on the same files.

### Releases

- Exactly one open release PR.
- During a release no new PRs are started and Dependabot PRs stay open.
- Versions only via the release PR or a `Release-As:` footer. Never tag, bump or edit published releases by hand.
- **Release PRs from 1.0.0 on are approved and merged only by the owner** (the release tool's PR, e.g. `chore: release vX.Y.Z`). Neither QA nor Maintainer merges them. When the chain, CI and the release checks are clean, Maintainer sets `ready-for-human` on the release PR (removing every other `ready-for-*` label) and informs the owner. A release PR never gets `ready-for-qa`; QA may at most check it beforehand. To hold a release PR back, Maintainer removes its label (see Labels and handover). Repos still below 1.0.0 follow this rule from their 1.0.0 release on.

## Maintainer

- Owns the website (GitHub Pages), docs fixes and smaller changes; repo settings and labels.
- Ensures "Automatically delete head branches" is on in every repo, so merged branches are deleted automatically.
- No larger code work: label it `ready-for-development`.
- Checks every Dependabot PR and hands it to QA or the owner (see Dependabot PRs).
- Release owner: prepares the release PR and checks for duplicate release PRs before and after every merge.
- Never merges.

**Post-release check:**
- Tag exists and matches the released version.
- Release assets are present and downloadable.
- Release notes are published and correct. Never edit published notes; report problems instead.
- Package registry or site updated, if applicable.
- The release PR was merged once; no duplicate release PR left open.

**Build pipeline sanity check:**
- Workflows run green on `main`.
- No workflow persistently failing, skipped or disabled.
- Permissions and tokens work.
- CI minutes stay reasonable.

## Developer

- A team bot or an external coding agent.
- Does larger feature and code work as PRs.

## QA

- Reviews and tests PRs. Pushes no code.
- Works on several PRs in parallel, e.g. reviews other PRs while waiting for a test.
- Never re-runs tests CI already ran; tests only what CI does not cover.
- **The only role that merges:** all PRs (release PRs: see Releases), squash, after green CI and review. Closes the linked issues.
- Squash-merge titles are Conventional Commits: a change that must ship needs `fix:` or `feat:`; `chore:`/`docs:`/`ci:` do not release.
- Dependabot PRs keep their title and prefix from `.github/dependabot.yml`; never turn an update into `feat!:`.

**Pre-release check (before the release PR is merged):**
- Release PR content and version bump are correct.
- Changelog entries match the merged PRs.
- CI is green on the release PR.
- No duplicate release PRs.
- No open PRs or issues blocking the release.
- Dependabot PRs are held until the release is out.
