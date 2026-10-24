# Triage labels

The engineering skills (`triage` and friends) speak in canonical roles. This table maps them to the label strings in this repo's tracker (created by `scripts/sync-labels.sh` in presets from `labels.json`).

| Role | Label in this tracker | Meaning |
| --- | --- | --- |
| category: bug | `bug` | Something is broken |
| category: enhancement | `enhancement` | New feature or improvement |
| `needs-triage` | `needs-triage` | Owner needs to evaluate this issue |
| `needs-info` | `needs-info` | Waiting on the reporter for more information |
| `ready-for-agent` | `ready-for-development` | Larger feature or code work, fully specified, for Developer |
| (team) | `ready-for-maintainer` | Website, docs fix or smaller change, for Maintainer |
| `ready-for-qa` | `ready-for-qa` | Ready for QA review and independent testing |
| `ready-for-human` | `ready-for-human` | Needs the owner to implement or decide |
| (team) | `needs-input` | Waiting for input or a decision from the owner |
| `wontfix` | `wontfix` | Will not be actioned |

Pickup labels: `ready-for-maintainer` is for Maintainer (website, docs fixes, smaller changes), `ready-for-development` is for Developer (larger feature and code work), and `ready-for-qa` is for QA. Colors and owners: `team.md`.

