# Context layers

Context is a shared, versioned work surface—not a transcript dump. Load only the smallest slice that lets an agent complete its assigned task.

| Layer | Canonical location | Holds | Does not hold |
|---|---|---|---|
| Project Context | `context/` | Cross-demand facts, principles, constraints, architecture and glossary | Demand-local work or runtime state |
| Demand Context | `需求/<demand>/context/` | A demand's objective, decisions, tasks, artifact index and writeback candidates | Unreviewed chat as fact |
| Task Context | Task manifest | One task's objective, acceptance, access, writeback and expiry | Long-lived project facts |
| Runtime / Personal Context | Memory, Session, Profile, SOUL and runtime state | Continuity and execution environment | Shared project facts by default |

Runtime and personal context may contribute a specifically authorized slice. They are not copied into a project or a public repository by default.

## Retrieval order

1. Current task manifest and the user's explicit instructions.
2. Project rules and the selected demand entry point.
3. The demand's `context/` sidecar and named live artifacts.
4. Relevant project `context/` files.
5. Only the runtime/personal slice explicitly authorized for the task.

If the package or an authorized path is missing, mark the task blocked rather than searching the whole workspace.
