---
type: context-contract
id: context-layers
area: collaboration
status: active
human_owner: maintainer
domain_owner: coordination
visibility: public-example
created: 2026-08-17
updated: 2026-08-17
---

# Context layers

The runtime assembles context; the project workspace and knowledge hub hold facts and reviewed knowledge. Do not build a second always-on context concatenator or inject the whole repository into every session by default.

| Layer | Fact location | Purpose | Default maintainer |
|---|---|---|---|
| Stable context | knowledge hub | Long-term goals, stable preferences, reviewed knowledge | human maintainer |
| Rolling context | `03_context/` | Recent priorities, open items and decisions | coordination agent |
| Project context | `02_projects/<project_id>/` | Project goals, requirements, decisions and deliverables | domain owner |
| Task context | current task manifest | Objective, acceptance, access scope and writeback location for one task | task owner |

## Runtime files

- `SOUL.md`: stable role identity and boundaries; it does not store dynamic project facts.
- Root `AGENTS.md`: public or organization-wide collaboration rules.
- Project `AGENTS.md`: local rules loaded when entering a project.
- `MEMORY.md` and `USER.md`: runtime continuity and experience cache, not project facts.
- Session: conversation process, not the completion record.
- Task manifest: explicitly loaded for a task, not permanently injected.

## Retrieval order

`SOUL` → root `AGENTS.md` → role definition → task manifest → project rules and context → current requirement and decisions → manifest-listed evidence.

When a task package, project entry point or authorized path is missing, create a blocked event instead of guessing through a full-repository scan.
