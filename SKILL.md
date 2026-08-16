---
name: project-context-workflow
description: Govern project-local context setup, repair, scoped reading, artifact-safe editing, PM/product context architecture, exception quarantine, and migrations from arbitrary/legacy layouts into the target `context/` + `需求/` + optional `agent/` model. Use when Codex starts or repairs project context/memory; converts AGENTS.md, .context-scope.yml, context/, baseline, requirements/workstreams, versions, outputs, drafts, tools, or ad-hoc folders into target structure; preserves live artifacts; handles uncertain ownership, authority conflicts, partial writes, and blocked migrations; manages demand context sidecars, versions, outputs, drafts, agent assets, writeback, archive, and handoff.
---

# Project Context Workflow

This workflow governs project-local context. Project-local files and current user artifacts remain the source of truth; global memory and historical sessions are weak hints only.

## Routing

Use this workflow for:

- Setting up, repairing, simplifying, or migrating project context/memory.
- Converting arbitrary or legacy context structures into the target `context/` + `需求/` + optional `agent/` structure.
- Updating `AGENTS.md`, `.context-scope.yml`, `context/`, `agent/`, `需求/`, `requirements/`, `workstreams/`, `版本/`, `versions/`, `outputs/`, `drafts/`, or baseline snapshots.
- Choosing the minimum relevant context to read before product/project work.
- Preserving user-edited artifacts while using context as constraints.
- Deciding between no record, live artifact patch, demand context update, draft, output, demand version, or cross-demand project package.
- Creating or updating PM/product context architecture: project-level context plus demand-level sidecars.
- Reviewing writeback candidates and promoting only confirmed cross-demand facts.
- Preparing archive, rollback, or handoff material.
- Handling unexpected structure, authority, schema, write, or migration conditions without silently losing or promoting information.

## Authority Model

When the user names, opens, or selects a concrete artifact, use this authority order:

1. Current user instruction and explicit constraints.
2. Current target artifact content: `@file`, `<current_note>`, `<editor_selection>`, PRD/spec/table/whiteboard/code file.
3. Project boundary rules: `AGENTS.md`, `.context-scope.yml`.
4. Demand index and demand home: `需求/README.md`, `需求/<name>/README.md` or aliases.
5. Demand context sidecar: `需求/<name>/context/`.
6. Stable project context: `context/` as global constraints and coordination context.
7. Active demand version package if named or clearly relevant.
8. `agent/` operation assets only when templates/tools are needed; never as product facts.
9. Drafts, outputs, historical sessions, and legacy memory as weak evidence only.

Rules:

- Read the current target artifact before drafting changes to it.
- If context conflicts with the artifact, preserve the artifact and flag the conflict.
- Treat files named `current-state.md` or `last-reviewed-state.md` as snapshots, not guaranteed current truth.
- Do not regenerate or replace a user-edited artifact from context unless the user explicitly asks for a full rewrite.
- Default to narrow patches. Save broad alternatives as drafts.
- Ask before destructive edits, whole-file replacement, moving/renaming user artifacts, or deleting context unless the user explicitly asked for structure repair/migration.
- When ownership, authority, or safety is uncertain, preserve the original, quarantine the affected item, register the exception, and do not guess.

## Context Layers

Use the lightest layer that fits:

| Layer | Purpose | Examples | Write rule |
| --- | --- | --- | --- |
| Live artifact | User-facing current work product | PRD, strategy doc, prompt table, whiteboard, code file | Highest authority; patch minimally |
| Demand workspace | User-facing container for one requirement/task/workstream | `需求/<name>/README.md`, work docs, source files | User works here directly |
| Demand context | Agent-maintained PM sidecar for one demand | `需求/<name>/context/REQUIREMENT_CONTEXT.md`, `decision-ledger.md`, `tasks.md` | Store local detail here, not project context |
| Stable project context | Cross-demand facts, constraints, principles, glossary, architecture, index | `context/PROJECT_CONTEXT.md`, `context/principles.md`, `context/constraints.md` | Promote only reviewed project-level facts; keep lean |
| Output | Generated deliverables | `需求/<name>/输出/*.xlsx`, screenshots, previews | Do not treat as context unless indexed |
| Draft | Agent alternatives, backups, speculative rewrites | `需求/<name>/草稿/` or `drafts/` | Weak evidence only |
| Agent asset | Operation templates/tools for agents | `agent/templates/`, `agent/tools/`, scaffolds | Use only for execution; never as product fact |
| Version package | Managed iteration of a demand or project release | `需求/<name>/版本/<version>/` | Use only for version-worthy work |
| Historical material | Old sessions, legacy memory, old snapshots | `~/.codex/sessions`, archived sessions | Weak hints; verify before use |

## User-facing Demand Workspace

Prefer a simple user-facing structure in Obsidian/PM projects:

```text
project/
  README.md
  AGENTS.md
  .context-scope.yml
  context/                         # project-level context only
    PROJECT_CONTEXT.md
    principles.md
    constraints.md
    glossary.md
    architecture.md
    exception-ledger.md           # unresolved structure/authority/write exceptions, not project facts
    writeback-inbox.md
  agent/                            # optional operation assets; not context
    templates/
    tools/
  需求/                              # user workspace; alias: requirements/ or workstreams/
    README.md                       # demand list / status / entry points
    <需求名>/
      README.md                     # demand home for the user
      live artifacts...             # PRD, strategy docs, tables, boards
      context/                      # agent-maintained sidecar
        REQUIREMENT_CONTEXT.md
        user-scenarios.md
        solution-thinking.md
        decision-ledger.md
        tasks.md
        artifacts.md
        writeback.md
      版本/                          # alias: versions/
        <version>/
          README.md
          PRD.md or spec.md
          context/
            VERSION_CONTEXT.md
            assumptions.md
            discussion.md
            tasks.md
            writeback.md
      输出/                          # alias: outputs/
      草稿/                          # alias: drafts/
      资料/ or 工作文件/               # optional source/work files
```

Use Chinese folder names when the project is Chinese/user-facing and the user prefers them. Accept existing `requirements/`, `versions/`, `outputs/`, and `drafts/` as aliases, but prefer one convention per project and document it in `.context-scope.yml`.

Rules:

- The user should be able to enter `需求/<需求名>/` and work without reading context internals.
- Demand-level context should be inside `需求/<需求名>/context/`, not scattered at the demand root.
- Generated files belong in `输出/`; agent drafts/backups belong in `草稿/`.
- Root-level `versions/`, `output(s)/`, `baseline/`, and old `requirements/` should be migrated or documented as legacy when simplifying structure.
- Keep project root visually small: project entry files, `context/`, `需求/`, and optional `agent/` are usually enough.



## Target Structure Migration Scenario

Use this scenario when the user asks to retrofit another project, vault folder, or legacy context layout into the standard structure.

Target model:

```text
project/
  README.md
  AGENTS.md
  .context-scope.yml
  context/                 # project facts only
  需求/                     # user-facing workspaces
    README.md
    <demand>/
      README.md
      context/             # demand sidecar
      版本/                 # demand versions
      输出/                 # generated deliverables
      草稿/                 # drafts/backups
      资料/ or 工作文件/      # optional sources/work files
  agent/                   # optional operation assets, not facts
```

Migration triggers:

- The project has old `requirements/`, `workstreams/`, root `versions/`, root `output(s)/`, `baseline/`, scattered `context/facts/`, `context/philosophy/`, `skills/`, `tools/`, or ad-hoc agent folders.
- The user says the project tree is too complex or asks to standardize context across projects.
- Context contains live artifacts, generated files, tool templates, or version packages that should be separated.

Migration process:

1. **Inventory first**: list root folders, project context files, demand-like folders, versions, outputs, drafts/backups, tools/templates, and scripts. Do not move anything before the inventory is clear.
2. **Classify by role**:
   - Live artifacts -> owning `需求/<demand>/` root.
   - Project facts/principles/constraints/glossary/architecture -> `context/`.
   - Demand decisions/scenarios/tasks/artifact index/writeback -> `需求/<demand>/context/`.
   - Demand iterations/PRDs/evaluation rounds -> `需求/<demand>/版本/<version>/`.
   - Generated deliverables -> `需求/<demand>/输出/`.
   - Agent drafts/backups -> `需求/<demand>/草稿/`.
   - Source material or temporary work files -> `需求/<demand>/资料/` or `工作文件/`.
   - Templates/tools/scaffolds for agents -> `agent/`.
3. **Design the mapping**: create a concise old-path -> new-path map. If many files will move, write a migration log such as `context/structure-migration-YYYYMMDD.md`.
4. **Preserve artifacts**: never bury user-facing live artifacts in `context/`; move them to the demand workspace and update links.
5. **Normalize project context**: flatten or simplify `context/` so it contains only project-level facts and snapshots. Rename `current-state.md` to `last-reviewed-state.md` or mark it non-authoritative.
6. **Normalize demand context**: put demand sidecars under `需求/<demand>/context/`; do not scatter PM sidecars at the demand root.
7. **Normalize versions**: put local versions under `需求/<demand>/版本/`; put version context under `版本/<version>/context/` when simplifying.
8. **Normalize agent assets**: move root `skills/`, `tools/`, template folders, and scaffolds to `agent/`, not `context/`.
9. **Update rules and references**: update `AGENTS.md`, `.context-scope.yml`, root `README.md`, `需求/README.md`, demand `README.md`, `artifacts.md`, scripts, and wikilinks/path references.
10. **Validate**: parse YAML, compile/check scripts when possible, scan for stale old paths, scan non-backup wikilinks, confirm root-level legacy folders are gone or documented, and confirm every unresolved item is registered in the exception ledger or migration log.

Decision rules:

- Ask before deleting content; moving as part of an explicitly requested migration is acceptable, but keep a move map.
- Do not create a new version package solely to record the migration; use a migration log unless the user asks for a formal version/release.
- Keep backups in `草稿/backups/` or an archive folder, but exclude them from current-reference validation.
- Prefer readable Chinese folder names for Chinese Obsidian projects; accept English aliases when already established.
- If a folder cannot be confidently classified, leave it in place, register it in the exception ledger or migration log, and report it as unresolved rather than guessing.

## Exception Handling and Quarantine

Use this policy whenever an artifact, authority source, route, schema, write, or migration does not behave as expected.

Default to **preserve, isolate, surface, recover**:

1. Detect and classify the exception before editing the affected item.
2. Preserve the original artifact and current state; do not overwrite, move, rename, or delete on low confidence.
3. Register the exception in `context/exception-ledger.md`, or in the migration log when the project has not yet established a `context/` directory. Use the documented project equivalent if one already exists.
4. Mark quarantined items as excluded from current facts, active routing, writeback promotion, and validation unless they are explicitly indexed as evidence.
5. Continue only independent, reversible, safe work. Stop the affected scope when continuing would require a guess or an irreversible action.
6. Close the exception only with new evidence, explicit user confirmation, a deterministic rule, or a documented decision to accept it as legacy.

Use these severity levels:

| Level | Examples | Default action |
| --- | --- | --- |
| P0 | Data loss, secret exposure, irreversible deletion, unrecoverable write | Stop related writes and preserve the scene |
| P1 | Live artifact/context conflict, active route unknown, destructive migration boundary | Block the affected scope; continue only unrelated safety checks |
| P2 | Unknown ownership, legacy layout, malformed optional sidecar, unclassified output/tool artifact | Quarantine and continue the safe subset |
| P3 | Non-critical broken link, stale snapshot, cosmetic or reporting issue | Repair or report while continuing |

Record at least: `id`, `detected_at`, `path`, `severity`, `status`, `classification`, `observed`, `preserved_action`, `excluded_from`, `blocking`, `blocking_scope`, `next_action`, `review_condition`, and `source`.

Use these default classifications:

- Unknown owner: quarantine; do not move it until the owning demand/version is known.
- Possible live artifact: preserve in place and stop only the move or rewrite that would affect it.
- Authority conflict: preserve the higher-authority artifact, record the conflict, and do not silently merge or promote either side.
- Tool logs, screenshots, and generated outputs: treat them as process artifacts, not facts; preserve them according to the project’s retention rule.
- Missing or malformed sidecar: create a minimal structured record only when scope is clear; otherwise leave the source untouched and record the gap.
- Partial write or interrupted command: re-read the filesystem, compare before retrying, verify the result, and never claim completion from an unverified partial state.
- Unavailable external source: record the source as unavailable or the claim as an assumption; do not convert absence of evidence into a fact.

When reporting an exception, state what was detected, what was preserved or isolated, whether the main task can continue, what scope is blocked, and the exact condition for resuming. Do not describe a task as fully converged while an open exception still affects the claimed scope.

## Agent Operation Assets

Use `agent/` for reusable operation assets that help Codex execute project workflows. These assets are optional and are not project facts.

Examples:

- `agent/templates/version-package/`: scaffold for demand version packages.
- `agent/tools/create-version.md`: instructions for creating a version package.
- `agent/tools/context-writeback.md`: instructions for writeback mechanics.
- `agent/scripts/`: deterministic project-specific helper scripts when needed.

Rules:

- Do not store agent tools or templates in project-level `context/`.
- Do not read `agent/` during normal product work unless a task requires templates, tools, scaffolding, validation, or structure repair.
- Never use `agent/` as a product fact source or to override a live artifact.
- If an operation rule affects future collaboration, summarize the rule in `AGENTS.md`, `.context-scope.yml`, or `context/architecture.md`; keep the full tool/template in `agent/`.
- Generated outputs still belong in `需求/<name>/输出/`; drafts/backups still belong in `需求/<name>/草稿/`.

## PM Context Architecture

For product-management projects, context is a sidecar to product work, not the user's main workspace.

Project-level context stores only cross-demand material:

- Product positioning and current project goal.
- Product principles, non-goals, and decision rules.
- Stable constraints, glossary, permissions, governance rules.
- Architecture/context model and demand index pointers.
- Writeback inbox for candidates that may affect multiple demands.

Demand-level context stores local PM thinking:

- User scenarios, JTBD, triggers, pain, success state.
- Solution options, tradeoffs, product value, principles, non-goals.
- Decisions, assumptions, evidence, risks in `decision-ledger.md`.
- Tasks, artifacts, versions, outputs, and writeback candidates.

User collaboration loop:

1. The user works in live artifacts or natural conversation.
2. The agent reads the live artifact first and preserves its structure.
3. The agent updates demand context only after material product judgment emerges.
4. The agent reports what was recorded, what is only a draft/output, and what is a project-level writeback candidate.
5. The user confirms only high-impact decisions, strong constraints, or project-level promotions.

Do not make context maintenance the user's primary task.

## Empty Context File Rule

Context files and sections may be intentionally empty.

- Empty means no confirmed or useful material has been recorded yet.
- Do not fill empty slots just because a template has them.
- Prefer a short marker such as `暂无已确认内容` when helpful.
- Never treat empty `evidence.md`, `risks.md`, `user-scenarios.md`, `decision-ledger.md`, `writeback.md`, or `版本/` as a failure by itself.
- Context preserves formed judgment; it should not manufacture completeness.

## Demand and Version Selection

When work is more than a trivial one-file edit:

1. Check whether the user named a demand/workstream.
2. If not, inspect `需求/README.md`, `requirements/README.md`, or `workstreams/README.md`.
3. Reuse an existing demand when the task clearly belongs there.
4. Create a new demand only when the task has durable local scope: repeated discussion, multiple artifacts, local decisions, tasks, outputs, or future versions.
5. If the task is a small artifact patch with no durable local scope, skip demand creation and report that it was deliberately skipped.

Demand read order:

1. `AGENTS.md` and `.context-scope.yml`.
2. Current live artifact if named/selected.
3. `需求/README.md` and `需求/<name>/README.md`.
4. `需求/<name>/context/REQUIREMENT_CONTEXT.md`, `artifacts.md`, `decision-ledger.md`, `tasks.md` as needed.
5. `需求/<name>/版本/<version>/context/VERSION_CONTEXT.md` if a version is named or clearly active.
6. Relevant project-level `context/` files.
7. `agent/` only if templates/tools/scaffolding are needed.

## Version Package Decision

Version packages are for managed iterations, not for every agent edit.

Create a demand version under `需求/<name>/版本/<version>/` only when the work has most of these properties:

- A distinct PRD, demo, evaluation round, review package, implementation slice, or delivery package.
- Clear goal, scope, tasks, acceptance criteria, and status.
- Multiple artifacts or multiple rounds likely need coordination.
- Future agents need a handoff/evidence trail.
- The user asks for a new version, release, PRD package, demo package, or workstream.

Create a cross-demand project package only when the work spans multiple demands and the user asks for a project-level release/review.

Do not create a new version for:

- Local edits to a current note/table/spec.
- Copy, wording, or field adjustments.
- One-off analysis or discussion.
- Agent drafts or alternative structures.
- Context writeback candidates that can be stored lightly.

If work is not version-worthy, prefer in this order:

1. Patch the live artifact directly.
2. Save alternatives in `需求/<name>/草稿/`.
3. Put durable local PM context in `需求/<name>/context/`.
4. Update `REQUIREMENT_CONTEXT.md` or `decision-ledger.md` only for material product judgment.
5. Put generated deliverables in `需求/<name>/输出/`.
6. Report project-level writeback candidates instead of creating a heavy package solely to store them.

## Outputs, Drafts, and Source Files

Keep these separate:

- Live artifact: user-facing current truth.
- Context: agent sidecar and stable constraints.
- Output: generated deliverables such as Excel, preview images, exports, rendered docs.
- Draft: speculative rewrite, backup, or alternative proposal.
- Source/work files: raw PDFs, extracted images, temporary scripts, inspection logs.

Rules:

- Do not put generated outputs into project-level `context/`.
- Do not use outputs as the source of truth unless the output is the live artifact the user is editing.
- Link outputs from `artifacts.md` rather than copying their contents into context.
- Move root `output/` and `outputs/` into the owning demand when repairing structure. Move root tool/template folders into `agent/`, not `context/`.

## Writeback Gate

Use a three-step gate for durable context:

1. Candidate: record durable items in demand `context/writeback.md`, version `context/writeback.md`, project `context/writeback-inbox.md`, or final response.
2. Review: classify each candidate as mechanical fact, confirmed decision, assumption, stale/risky note, or sensitive/temporary material.
3. Promotion: write only reviewed project-level items into stable `context/`; leave demand-local conclusions in the demand unless they affect multiple demands.

High-impact product positioning, scope changes, governance rules, permission rules, and safety rules require explicit user confirmation before promotion. Mechanical facts with local evidence may be promoted when scope is clear.

Strength labels:

- **strong constraint**: safety, permissions, non-goals, editing rules, confirmed project-wide decisions.
- **architecture supplement**: reusable model, context map, capability relationship.
- **weak learning**: pattern or heuristic that should not force future outputs.

## Repair and Migration Rules

Use the Target Structure Migration Scenario for full-project conversions. When fixing residual context structure:

- First inventory existing root folders, demand folders, context files, agent assets, versions, outputs, drafts, and references.
- Preserve live artifacts; move them into the owning demand rather than burying them in context.
- Move old demand sidecars into `需求/<name>/context/`.
- Move old demand versions into `需求/<name>/版本/`; place version context files under `版本/<version>/context/` when simplifying.
- Move generated files from root `output(s)/` into `需求/<name>/输出/`.
- Flatten or simplify project `context/` so it contains only cross-demand context; move agent operation assets to `agent/`.
- Rename `current-state.md` to a last-reviewed snapshot or clearly mark it as non-authoritative.
- Update `AGENTS.md`, `.context-scope.yml`, root `README.md`, demand `README.md`, `artifacts.md`, scripts, and path references.
- Record a move map or rollback hint when many files are moved.
- Register unclassified, conflicting, or blocked paths in `context/exception-ledger.md` before closing the repair.
- Validate that old root folders are gone or explicitly documented as legacy; do not claim full conformance while a claimed scope has open P0/P1 exceptions.

## Process

Unless the user asks only to discuss, explain, summarize, or review:

1. Locate the actual project directory and target artifact.
2. Read boundary files: `AGENTS.md`, `.context-scope.yml`.
3. Read the current artifact first when named/selected.
4. Read the demand index and relevant demand context.
5. Read only relevant project-level context, including the exception ledger for structure repair, migration, or known unresolved paths.
6. Choose the work container: no record, live patch, demand context, draft, output, version, agent asset, or project package.
7. Execute safely: preserve structure, patch narrowly, keep assumptions separate, and apply exception handling when any step deviates from the expected state.
8. Close: verify changed files, references, scripts, context writeback status, and exception-ledger status.

Prefer scoped reads over broad scans. When similar projects exist, require explicit source paths before using cross-project facts.

## Verification

Report:

- Files/folders created, moved, updated, or deliberately left in place.
- Whether a demand was reused, created, migrated, or skipped.
- Whether a demand version or project version package was used, created, migrated, or skipped.
- Which PM sidecars were updated, skipped, left empty, or left for user confirmation.
- Which outputs, drafts, and agent assets were moved or indexed.
- Which exceptions were detected, their severity/status, what was preserved or quarantined, what scope remains blocked, and when they should be revisited.
- Which writeback candidates were added, promoted, pending, local, or deliberately not recorded.
- Remaining legacy structure, if any, and why it remains.
- Target-structure conformance: root is minimal, project context is lean, demands own their artifacts/context/versions/outputs/drafts, and agent assets are outside context.
- Rollback paths or move maps for structure changes.
- Validation results, including YAML parse, script compile, broken path/reference scan, and skill validation when the skill itself was edited.
