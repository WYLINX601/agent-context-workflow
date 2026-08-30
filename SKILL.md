---
name: agent-context-workflow
description: Load the minimum verified Project, Demand and Task Context needed to continue work across agents and tools; preserve live artifacts, respect path permissions, and write reviewed results back to the correct layer.
---

# Agent Context Workflow

Use this skill when creating, repairing or continuing a project Context workflow; when a task changes agent/tool; or when an Agent must write results back without treating chat or runtime memory as project truth.

## Authority

Follow this order:

1. The user's explicit instruction and the selected live artifact.
2. A valid Task Context manifest.
3. Approved Demand Context and decisions.
4. Project Context.
5. Runtime/Personal Context, drafts, outputs and prior sessions.

If same-level sources conflict, preserve both and request human review. Do not resolve a conflict by file timestamp alone.

## Load Context minimally

1. Read the task objective, acceptance criteria, expiry and access scope.
2. Read the selected demand's `README.md` and only the named files in `需求/<demand>/context/`.
3. Read only the project `context/` files required by the task.
4. Load a personal/runtime slice only when explicitly authorized.
5. Before acting, confirm the goal, trusted facts, allowed paths, writeback destination and human decision points.

Never inject an entire repository, personal Memory or a conversation transcript merely to “add context.”

## Access and safety

- Treat `access.read` and `access.write` as allowlists. Unlisted paths are denied.
- Use repository-relative paths only.
- Do not write or publish credentials, tokens, `.env`, Sessions, Memory, Profile data, runtime state or `.obsidian`.
- Read access does not imply permission to repeat or publish information.
- If work needs broader access, stop that part and create a blocked escalation for human review.

See [`contracts/context/access.md`](contracts/context/access.md).

## Writeback gate

Agent output is a candidate until reviewed:

1. Place user-facing work in `需求/<demand>/输出/`.
2. Record demand-local facts, decisions and task status in `需求/<demand>/context/`.
3. Promote only reviewed cross-demand facts to project `context/`.
4. Keep reusable methods as separately reviewed capability candidates.

Do not write full chat logs, hidden reasoning or raw tool logs into project facts. Completion records name the task, result, output paths, verification, risk and next step.

See [`contracts/context/writeback.md`](contracts/context/writeback.md).

## Exceptions

Preserve first. When an artifact is ambiguous, malformed or conflicts with a higher-authority source:

1. Do not overwrite, move or promote it.
2. Record the path, observed conflict, impact and required reviewer.
3. Continue only safe work that does not depend on the uncertainty.

Use [`contracts/context/freshness.md`](contracts/context/freshness.md) for stale or disputed Context.

## Reference assets

- [Context contracts](contracts/context/)
- [Task manifest schema and example](contracts/task-context/)
- [Project and demand templates](templates/)
- [Minimal validated example](examples/minimal-project/)
