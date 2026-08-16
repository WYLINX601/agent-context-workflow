# project-context-workflow

A Codex skill for setting up, repairing, and governing project-local context.

It helps Codex work safely with project context and memory, including:

- project and demand context architecture;
- scoped context reading and authority handling;
- artifact-safe editing and conflict preservation;
- migrations from legacy layouts into `context/`, `需求/`, and optional `agent/`;
- drafts, outputs, versions, handoffs, archives, and exception quarantine.

## Installation

Copy this directory into your Codex skills directory:

```bash
cp -R project-context-workflow ~/.codex/skills/
```

The skill is provided through `SKILL.md` and follows the standard Codex skill format.

## License

MIT
