---
type: context-contract
id: access-matrix
area: collaboration
status: active
human_owner: maintainer
domain_owner: coordination
visibility: public-example
created: 2026-08-17
updated: 2026-08-17
---

# Access matrix

These permissions are collaboration rules, not an operating-system sandbox. A task manifest may narrow them; expanding them requires an explicit reason and human approval.

| Content | Human reviewer | Lead Agent | Specialist Agent | Reviewer Agent |
|---|---|---|---|---|
| Full personal notes | explicit approval only | normally no | no | no |
| Project context | read and approve | read/write within task | read assigned slice | read assigned slice |
| Other projects | explicit approval only | task manifest only | task manifest only | task manifest only |
| Task events and deliverables | read/write | read/write assigned scope | write own result | read/write review result |
| Knowledge candidates | review and promote | propose | propose | validate |
| Stable knowledge hub | approve promotion | read approved entry points | read assigned entry points | read assigned entry points |

## Required rules

1. `allowed_read_paths` and `allowed_write_paths` use repository-relative paths; unlisted paths are not implicitly authorized.
2. Permission to read does not imply permission to repeat or publish; outputs disclose only what the task requires.
3. Domain agents update only their assigned project or task scope.
4. Agents must not write credentials, sessions, runtime memory, pairing data, or device-local state to the repository.
5. If a task requests out-of-scope access, stop that portion and create a `blocked` event for human review.
