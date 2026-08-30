# Architecture

The architecture separates a durable work surface from transient execution:

```text
Human request
  → Project Context + Demand Context
  → Task manifest (goal, acceptance, access, expiry, writeback)
  → Personal Agent or Agent Squad
  → reviewed output / decision / candidate
  → Demand or Project Context
```

Project Context holds only cross-demand facts. Demand Context is the local sidecar for a piece of work. The task manifest grants the smallest executable slice. Runtime and personal context help an agent continue work but do not become public project facts by default.

## Components

| Component | Responsibility |
|---|---|
| Human | Goal, scope, approval, high-risk decisions and promotion |
| Project Context | Stable constraints, architecture, principles and glossary |
| Demand Context | Local objective, decisions, tasks, artifact index and writeback candidates |
| Task Context | Bounded objective, acceptance, access, expiry and completion route |
| Personal Agent | Direct co-creation within a bounded task |
| Agent Squad | Lead-coordinated specialist work with sliced Context |
| Runtime adapter | Optional protocol/runtime integration; never the source of facts |

The [Multica/Hermes bridge](../plugins/multica-hermes-bridge/) is one reference adapter. It is optional and does not change the Context contract.
