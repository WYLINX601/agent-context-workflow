---
type: context-contract
id: freshness-policy
area: collaboration
status: active
human_owner: maintainer
domain_owner: coordination
visibility: public-example
created: 2026-08-17
updated: 2026-08-17
---

# Freshness and conflict policy

## Check points

Check context freshness before accepting a task, resuming an interrupted task, writing shared facts, and declaring completion.

## Validity order

1. The task manifest has not passed `expires_at`, and its objective and acceptance criteria are still clear.
2. Referenced requirements, project context and decisions have no unexplained material changes since task creation.
3. Recent decisions have not superseded the current approach.
4. Dependencies, open questions and external conditions still hold.

When validity cannot be demonstrated, mark the item `stale` and do not silently reuse it.

## Conflict priority

1. Explicit human approval or the latest approved decision.
2. The current requirement and valid task manifest.
3. Project `HOME.md` and `CONTEXT.md` or their documented equivalents.
4. Rolling context summaries.
5. Runtime memory and sessions.

Do not choose between same-level conflicts by file timestamp. Preserve both versions, link evidence, and escalate to the responsible maintainer.

## Escalation record

An escalation should include the conflicting or stale object, paths, update times, factual summaries, impact on the current task, reversible temporary handling, required decision owner, and deadline.

While waiting, continue only work that does not depend on the disputed fact and does not expand risk.
