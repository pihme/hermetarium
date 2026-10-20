# Team rules for the repos

How the team works on GitHub in this repo. Same in every repo of the family; change it in presets first. Repo-specific rules stay in `AGENTS.md`. "The owner" is the human who owns these repos.

Read **For everyone** first, then the section for your role.

## For everyone

### Roles

| Role | Owns | Picks up |
| --- | --- | --- |
| **Maintainer** | Website, docs fixes, smaller changes; repo settings and labels; release owner | `ready-for-maintainer` |
| **Developer** (a team bot or an external coding agent) | Larger feature and code work, as PRs | `ready-for-development` |
| **QA** | Review, testing, **all merges**, closing issues | `ready-for-qa` |

Other team members (coordination, design, research) make no repo changes. Design hands finished files to Maintainer and never approves its own work.

### Work order

**All roles: finish before you start.** Take what is closest to being merged first: a PR before an issue, a rebase before real work, a sent-back PR before new work.

### Labels and handover

| Label | Meaning | Owner | Color |
| --- | --- | --- | --- |
| `ready-for-maintainer` | Website, docs fix or smaller change | Maintainer | green `0E8A16` |
| `ready-for-development` | Larger feature or code work, fully specified | Developer | green `0E8A16` |
| `ready-for-qa` | Ready for review and testing | QA | yellow `FBCA04` |
| `needs-input` | Waiting for input or a decision from the owner | owner | orange `D93F0B` |
| `ready-for-human` | Needs the owner to implement or decide | owner | orange `D93F0B` |

**The label is the handover.** When your part is done, remove your label and set the next one. Exactly one `ready-for-*` label at a time.

| When | Who | Sets |
| --- | --- | --- |
| Issue triaged: website, docs, smaller change / larger code work | owner or Maintainer | `ready-for-maintainer` / `ready-for-development` |
| PR ready, CI green | Maintainer or Developer | `ready-for-qa` |
| Problems found | QA | back to the issue's previous label |
| Only a rebase or trivial conflict fix needed | QA | `ready-for-maintainer` (also for Developer's PRs) |
| Review passes | QA | merges, closes linked issues, removes `ready-for-qa` |
| Dependabot PR, CI green (not during a release) | QA | reviews and merges; conflicts: comment `@dependabot rebase` |
| Issue already done, nothing to change | Maintainer or Developer | `ready-for-qa` on the issue; QA verifies and closes |
| Decision or input needed | any role | `needs-input` or `ready-for-human` |

- Work only on your own label. Issues without a pickup label stay untouched, even clear bugs; triage belongs to the owner.
- All bots write as the same GitHub account; the label history, not the author, shows who did what.
- Act on a pickup label only if the owner's account set it (check the issue's label events). If any other account set it, do not start work: set `needs-input` and leave it to the owner.

- Build and test run in the repo's Nix devShell (`nix develop -c …`), the same as CI. A result from outside it does not count.

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

## Maintainer

- Owns the website (GitHub Pages), docs fixes and smaller changes; repo settings and labels.
- Ensures "Automatically delete head branches" is on in every repo, so merged branches are deleted automatically.
- No larger code work: label it `ready-for-development`.
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
- **The only role that merges:** all PRs incl. release PRs, squash, after green CI and review. Closes the linked issues.
- Squash-merge titles are Conventional Commits: a change that must ship needs `fix:` or `feat:`; `chore:`/`docs:`/`ci:` do not release.
- Dependabot PRs keep their title and prefix from `.github/dependabot.yml`; never turn an update into `feat!:`.

**Pre-release check (before merging the release PR):**
- Release PR content and version bump are correct.
- Changelog entries match the merged PRs.
- CI is green on the release PR.
- No duplicate release PRs.
- No open PRs or issues blocking the release.
- Dependabot PRs are held until the release is out.
