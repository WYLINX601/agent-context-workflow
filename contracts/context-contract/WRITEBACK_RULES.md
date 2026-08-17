---
type: context-contract
id: writeback-rules
area: collaboration
status: active
human_owner: maintainer
domain_owner: coordination
visibility: public-example
created: 2026-08-17
updated: 2026-08-17
---

# Writeback rules

Conversation, sessions and runtime memory are process caches. A task becomes a team fact only after its state and result are written to the project workspace.

## Routing

| Result | Writeback location | Owner |
|---|---|---|
| Progress, blocked state, completion | project `events/` | current executor |
| Requirement status and acceptance | current requirement | task owner |
| Project goal, status and next steps | project `HOME.md` and `CONTEXT.md` | domain owner |
| Approved or rejected decisions | project `decisions/` | domain owner with human approval |
| Reusable experience | `knowledge-candidates/` | discoverer; human reviewer promotes |
| Stable long-term knowledge | knowledge hub | human maintainer |

## Concurrency and integrity

1. Each event is an independent file; do not append to or overwrite another executor's active event.
2. Automated writes should validate required fields and frontmatter before replacing a target atomically.
3. If the current version is uncertain, compare before editing; do not blindly overwrite.
4. On a write conflict, preserve both versions and create a `blocked` event with the paths, differences and proposed decision owner.
5. Keep source links and `updated` metadata when changing shared summaries; separate facts, analysis and proposals.

## Minimum completion record

Every completed task should record the task ID, status, completed work, output paths, verification, remaining risk, next step, executor and update time. Do not write full chat, hidden reasoning or raw terminal logs into the fact source.
