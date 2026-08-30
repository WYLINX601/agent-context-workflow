# Access

Permissions in a task manifest are collaboration rules. They are not a substitute for operating-system or service-level security.

- Use repository-relative paths in `access.read` and `access.write`.
- Unlisted paths are denied by default.
- Read permission does not grant permission to repeat, publish or promote information.
- Project Context is shared only within the assigned scope; other demands need explicit task authorization.
- Credentials, tokens, `.env`, personal memory, sessions, runtime state and `.obsidian` are always denied unless a separate, private operating procedure explicitly authorizes them.
- An out-of-scope request becomes a blocked task or escalation; do not silently widen access.
