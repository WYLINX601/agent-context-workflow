# Workflow

## 1. Create the work surface

Start a project from [`templates/project/`](../templates/project/) and create one user-facing demand from [`templates/demand/`](../templates/demand/). Record only confirmed cross-demand material in `context/`; keep demand-local work in `需求/<demand>/`.

## 2. Define the task

Copy [`task-context.example.yaml`](../contracts/task-context/task-context.example.yaml). A manifest must state its goal, acceptance criteria, Context paths, read/write allowlists, deny list, expiry and writeback location. Validate it with `python3 scripts/validate_repository.py`.

## 3. Choose an execution mode

Use a Personal Agent when a person needs to work directly with one agent and real artifacts. Use an Agent Squad when a Lead must coordinate specialist roles and handoffs. Both modes operate on the same demand and Task Context.

## 4. Review and write back

Results stay candidates until a human reviews them. Put user-facing results in `输出/`; keep local facts in the demand sidecar; promote only reviewed cross-demand facts to project `context/`. Treat reusable methods as separate capability candidates.

## 5. Continue safely

When a task resumes in another tool or session, reload the manifest, its named demand files and only the relevant project Context. If a fact is stale, unauthorized or disputed, preserve it and escalate instead of guessing.
