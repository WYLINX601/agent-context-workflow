# Writeback

Agent output is not automatically project truth. Use a three-step gate:

1. **Candidate** — record a result in the demand's `context/` sidecar or its output folder.
2. **Review** — distinguish verified facts, decisions, assumptions, risks and reusable methods.
3. **Promotion** — only reviewed cross-demand material enters project `context/`; reusable methods become a separately reviewed capability candidate.

| Result | Default destination |
|---|---|
| User-facing deliverable | `需求/<demand>/输出/` |
| Demand task status and local decision | `需求/<demand>/context/` |
| Cross-demand fact | `context/` after review |
| Reusable method | A reviewed skill/workflow/context candidate |

Completion records should include the task ID, work completed, output paths, verification, remaining risk and next step. Never write full chat logs or hidden reasoning into project facts.
