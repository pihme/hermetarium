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

Other team members (coordination, project managers, design, research) make no repo changes. Design hands finished files to Maintainer and never approves its own work. An approval the owner gives to Design counts; Design makes the preview and the approval visible in the project room before Maintainer deploys.

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
- **Missing labels:** a new PR without its handover label gets it checked and set (exactly one `ready-for-*`, per the table below). Report only if a question remains.

| When | Who | Sets |
| --- | --- | --- |
| Issue triaged: website, docs, smaller change / larger code work | owner or Maintainer | `ready-for-maintainer` / `ready-for-development` |
| PR ready, CI green | Maintainer or Developer | `ready-for-qa` (Maintainer first checks author and file list) |
| Problems found, including a rebase or conflict fix | QA | back to the role responsible for the ticket: Developer `ready-for-development`, Maintainer `ready-for-maintainer` (see Handing work back) |
| Dependabot PR needs a rebase | QA | comment `@dependabot rebase`, keep `ready-for-qa`; `ready-for-maintainer` only if Dependabot cannot rebase |
| Review passes | QA | merges, closes linked issues, removes `ready-for-qa` |
| Release PR from 1.0.0 on clean (chain done, CI green, release checks done) | Maintainer | `ready-for-human` (never `ready-for-qa`), then informs the owner |
| Dependabot opens a PR | Dependabot (`labels:` in `.github/dependabot.yml`) | `ready-for-maintainer` |
| Dependabot PR content checked, CI green (not during a release) | Maintainer | `ready-for-qa` (removes `ready-for-maintainer`), see Dependabot PRs; also for a Nix bump that changes a release input |
| Dependabot PR with `ready-for-qa` | QA | reviews and merges (conflicts: see the rebase row) |
| Dependabot PR needs a decision | Maintainer | `ready-for-human` (removes `ready-for-maintainer`) |
| Issue already done, nothing to change | Maintainer or Developer | `ready-for-qa` on the issue; QA verifies and closes |
| Decision or input needed | any role | `needs-input` or `ready-for-human` |

- **Handing work back:** QA returns work to the role responsible for the ticket: Developer → `ready-for-development`, Maintainer → `ready-for-maintainer`. The recipient is never chosen by defect type or size; a rebase or a conflict does not change who is responsible.
- Work only on your own label. Issues without a pickup label stay untouched, even clear bugs; triage belongs to the owner.
- All bots write as the same GitHub account; the label history, not the author, shows who did what.
- Act on a pickup label only if the owner's account set it (check the issue's label events). If any other account set it, do not start work: set `needs-input` and leave it to the owner. Exception: `ready-for-maintainer` that Dependabot (`dependabot[bot]`) sets on its own PR when it opens it, through `labels:` in `.github/dependabot.yml`.

### Dependabot PRs

1. Dependabot PRs arrive with `ready-for-maintainer` (set by `.github/dependabot.yml`, next to `dependencies`).
2. Maintainer checks the content: the diff touches only what the title says, CI is green, no side effects on release inputs or lock files beyond the update, and the repo's glibc and release guards pass where they apply. Before `ready-for-qa`, Maintainer also checks the author and the file list (no stray files such as build output, correct Git identity).
3. If it is fine, Maintainer sets `ready-for-qa` and removes `ready-for-maintainer`. QA then reviews and merges on its own.
4. If it needs a decision (e.g. a breaking major update), Maintainer sets `ready-for-human` instead and informs the owner. A routine Nix lock bump that changes a release input needs no decision: it follows step 3 (`ready-for-qa`, QA merges); only the release PR that follows (where the release tool opens one) is merged by the owner (see Releases).
5. During a release Dependabot PRs stay open and keep `ready-for-maintainer` (see Releases). Release PRs from 1.0.0 on are still merged only by the owner.
6. **One release per Dependabot round:** the releasing (`fix(deps)`) PRs of one Dependabot round (the weekly Saturday run) go out together in one release, not one release per bump. Where every merge to `main` cuts a release, Maintainer consolidates them into one PR (see Labels and handover); where a release PR collects the changes, that release PR covers the whole round.
7. **All lock files of a bump:** when a bump is bundled, Maintainer first searches the whole repo for every lock file and workspace in which the package appears (e.g. `fuzz/`, `bench/`, `nix/`) and takes all of them into the same PR, not only those Dependabot opened a PR for.
8. **Versions already rejected:** a dependency version the owner already rejected is not ignored permanently in `.github/dependabot.yml`. The decisions already made are kept in an internal list (outside the repos). If Dependabot opens the same rejected version again, its PR is closed with a reference to that decision; a later version is evaluated anew and may change the decision.


### Approvals and completion

- **Assigned content PRs:** a PR whose content the owner assigned is opened without asking the owner again. Required approvals for rule-file changes stay as they are, and safety checks are never bypassed.
- **One approval question:** QA formulates an approval question for the owner once, in its binding wording; the coordination role presents it unchanged. No second approval round for the same question in slightly different words.
- **An approval names the goal:** the question and the approval name what is to be achieved (e.g. "merge if no test actually fails"), not only a single case, and the approval is applied to that goal.
- **Forwarded orders:** an order from the owner that the coordination role (or the responsible PM) forwards counts as the owner's explicit order, without an extra approval card. Safety checks are never bypassed, and approvals that need the owner's own words still count only when the owner gave them.
- **Rollout approvals:** an owner approval for a rollout also covers the follow-up PRs of that rollout whose content stays within what was approved. It does not cover unrelated changes.
- **Verified completion only:** report only verified results. Local commits and finished subtasks are not the whole assignment. A publication is reported as live only after the live result was checked.
- **Owner tasks only when actionable:** present a task to the owner as ready to act on only after its prerequisites are verified (e.g. CI green and review done for a PR; a sitemap deployed and its live URL checked before it is offered for submission). Prepared tasks are not presented as immediately actionable.

### Comments

- **Comments carry results only:** review findings, what was done. No status comments ("starting review", "looking at it").
- **Sign every comment with your role first.** All roles post from the owner's account, so every PR/issue comment, review and description starts with a first line naming the role, e.g. `*Maintainer on behalf of the user*` or `*QA on behalf of the user*`, then the content.

### Initiative

Wanted: spot duplicate or redundant PRs, red CI on `main`, docs that no longer match the code; fix trivial things per `AGENTS.md`; update `AGENTS.md` and docs in the same PR as the change; bundle related small changes into one PR (in content repos with a high PR rate, bundle smaller changes where sensible, to avoid stacked PRs); one topic per PR.

Not wanted: scope extensions or new work during a release; design decisions (design proposes, the owner decides); editing published releases or release notes; reopening issues the owner closed.

Before handing anything over, check for duplicate PRs and open work on the same files.

### Long-running operations

- Never wait in the foreground for longer than 2 minutes.
- Start long operations (CI runs, Docker builds, renders, long test runs) in the background and check on them only briefly, at most every 1–2 minutes, so that new messages are noticed in between.

### Website repos

Applies only to repos explicitly classified as websites without a Nix environment (the classification is kept in presets).

- **Native toolchain:** use the project's declared native toolchain. A general instruction to work inside Nix (e.g. the reference environment in `AGENTS.md`) does not impose a Nix requirement on these repos. This does not waive QA, required CI or safety rules and does not extend to other repo categories.
- **Documented build path:** the repo documents its actual non-Nix runtime/toolchain, dependency installation, build, validation and deployment commands, and output location in its existing README or `AGENTS.md`. Document only the versions or constraints the project actually requires; there is no new version-pinning policy. Maintainer keeps these instructions consistent with the toolchain in use. QA follows the documented non-Nix path and reports missing or contradictory prerequisites as findings instead of inventing commands or introducing Nix. This does not authorize toolchain migrations. Website CI and the documented deploy path are authorized separately (see **CI and deploy** below).
- **CI and deploy:** website repos get GitHub Actions. On every pull request the site is built with the documented native (non-Nix) toolchain; any documented checks (for example a link check) run as well. After merge or push to `main` the same build path runs, then the site is published automatically via the documented deploy path to `gh-pages`. Concrete commands stay those documented in each website repo; presets invents none. QA before merge and a live check after publish remain.
- **Change-scoped QA:** for content changes, QA runs the documented site build and checks the affected generated pages and links. For generator or theme changes, QA additionally checks links, HTML and the sitemap across the entire generated site. Relevant existing checks, required CI, handoff labels, protected-file approvals and safety rules stay.
- **Roles:** the responsible PM commissions and coordinates content; Maintainer implements and deploys; QA reviews. Publication is reported only after the live result was checked (for a sitemap: the sitemap file itself).
- No version, tag, release or branch naming policy follows from this section; existing project-specific requirements stay.

### Releases

- Exactly one open release PR.
- During a release no new PRs are started and Dependabot PRs stay open.
- Versions only via the release PR or a `Release-As:` footer. Never tag, bump or edit published releases by hand.
- **Release PRs from 1.0.0 on are approved and merged only by the owner** (the release tool's PR, e.g. `chore: release vX.Y.Z`). Neither QA nor Maintainer merges them. When the chain, CI and the release checks are clean, Maintainer always sets `ready-for-human` on the release PR (removing every other `ready-for-*` label) and informs the owner. A release PR never gets `ready-for-qa`; QA may at most check it beforehand. To hold a release PR back, Maintainer removes its label (see Labels and handover). Repos still below 1.0.0 follow this rule from their 1.0.0 release on.

## Maintainer

- Owns the website (GitHub Pages), docs fixes and smaller changes; repo settings and labels.
- Ensures "Automatically delete head branches" is on in every repo, so merged branches are deleted automatically.
- Before the first PR in a new repo, sets up the standard labels and both wake workflows (`label-wake`, `pr-result-wake`). Existing repos that lack the standard labels or a wake workflow are retrofitted as well.
- Before setting `ready-for-qa`, checks the PR's author and file list.
- No larger code work: label it `ready-for-development`.
- Checks every Dependabot PR and hands it to QA or the owner (see Dependabot PRs).
- Release owner: prepares the release PR and checks for duplicate release PRs before and after every merge.
- Never merges.
- One quick fix is fine; if it does not solve the problem, report it to the coordination role first (not the owner) or open a PR with the root-cause fix, never try the next fix silently.

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
- Does not block a documentation-only PR on tests unrelated to the changed documentation. Relevant link checks and documentation lint stay required.
- **The only role that merges:** all PRs (release PRs: see Releases), squash, after green CI and review. Closes the linked issues.
- Squash-merge titles are Conventional Commits: a change that must ship needs `fix:` or `feat:`. With `release.py`, `chore:`/`docs:`/`ci:` do not release; a repo whose release tool also counts other types on a release path says so in `AGENTS.md` ("Versions and releases").
- **Stacked PRs:** before squash-merging the lower PR of a stack, retarget the PRs above it onto `main`. Handing them back still follows the ticket's ownership.
- Dependabot PRs keep their title and prefix from `.github/dependabot.yml`; never turn an update into `feat!:`.

**Pre-release check (before the release PR is merged):**
- Release PR content and version bump are correct.
- Changelog entries match the merged PRs.
- CI is green on the release PR.
- No duplicate release PRs.
- No open PRs or issues blocking the release.
- Dependabot PRs are held until the release is out.
