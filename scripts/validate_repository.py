#!/usr/bin/env python3
"""Validate public Context Workflow assets without contacting external services."""

from __future__ import annotations

import re
import sys
from datetime import datetime
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator


ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "PUBLIC_ASSET_MANIFEST.yaml"
TASK = ROOT / "examples/minimal-project/task-context.yaml"
TASK_SCHEMA = ROOT / "contracts/task-context/task-context.schema.yaml"
REQUIRED_TASK_FIELDS = {
    "schema_version", "task_id", "project_id", "owner", "status", "objective",
    "acceptance_criteria", "context", "access", "created_at", "expires_at", "writeback",
}
FORBIDDEN_TEXT = ("03_context/", "02_projects/", "context-contract/", "configs/task-context/", "/Users/")
SECRET_PATTERNS = (
    re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    re.compile(r"\bghp_[A-Za-z0-9]{30,}\b"),
    re.compile(r"\bsk-[A-Za-z0-9]{20,}\b"),
    re.compile(r"(?i)(?:api[_-]?key|password)\s*[:=]\s*['\"]?[A-Za-z0-9_/-]{12,}"),
)


def fail(errors: list[str], message: str) -> None:
    errors.append(message)


def is_relative(path: str) -> bool:
    candidate = Path(path)
    return not candidate.is_absolute() and ".." not in candidate.parts and not re.match(r"^[A-Za-z]:", path)


def validate_task(errors: list[str]) -> None:
    task = yaml.safe_load(TASK.read_text())
    schema = yaml.safe_load(TASK_SCHEMA.read_text())
    for error in Draft202012Validator(schema).iter_errors(task):
        location = ".".join(str(part) for part in error.path) or "task manifest"
        fail(errors, f"schema error at {location}: {error.message}")
    missing = REQUIRED_TASK_FIELDS - task.keys()
    if missing:
        fail(errors, f"task manifest missing fields: {', '.join(sorted(missing))}")
    if task.get("schema_version") != 1:
        fail(errors, "task manifest schema_version must be 1")
    if not re.fullmatch(r"[A-Z][A-Z0-9_-]+", str(task.get("task_id", ""))):
        fail(errors, "task_id must be uppercase with underscores or hyphens")
    if not isinstance(task.get("acceptance_criteria"), list) or not task["acceptance_criteria"]:
        fail(errors, "acceptance_criteria must be a non-empty list")
    for entry in task.get("context", []):
        if not isinstance(entry, dict) or not is_relative(str(entry.get("path", ""))):
            fail(errors, "every context path must be repository-relative")
    access = task.get("access", {})
    for section in ("read", "write"):
        if not isinstance(access.get(section), list) or not all(is_relative(str(p)) for p in access.get(section, [])):
            fail(errors, f"access.{section} must contain repository-relative paths")
    deny = access.get("deny", [])
    for required in (".env", "session", "memory", ".obsidian"):
        if not any(required in str(item) for item in deny):
            fail(errors, f"access.deny must include {required}")
    for field in ("created_at", "expires_at"):
        try:
            datetime.fromisoformat(str(task[field]).replace("Z", "+00:00"))
        except (KeyError, ValueError):
            fail(errors, f"{field} must be an ISO-8601 timestamp")
    event_path = task.get("writeback", {}).get("event_path", "")
    if not event_path.startswith("需求/") or not event_path.endswith(".md"):
        fail(errors, "writeback.event_path must point to a demand context event")
    if not (TASK.parent / event_path).is_file():
        fail(errors, "task writeback event does not exist in the minimal example")


def validate_allowlist(errors: list[str]) -> None:
    data = yaml.safe_load(MANIFEST.read_text())
    if data.get("policy") != "allowlist":
        fail(errors, "public asset manifest must use allowlist policy")
    targets = set()
    for asset in data.get("assets", []):
        target = asset.get("target")
        if not target or not is_relative(str(target)):
            fail(errors, "each public asset target must be repository-relative")
            continue
        targets.add(target)
        if asset.get("review_status") != "approved":
            fail(errors, f"public asset is not approved: {target}")
        if not (ROOT / target).exists():
            fail(errors, f"allowlisted asset does not exist: {target}")
    for required in ("README.md", "SKILL.md", "contracts/context", "contracts/task-context", "templates", "examples/minimal-project", "scripts"):
        if required not in targets:
            fail(errors, f"allowlist is missing core asset: {required}")


def validate_markdown_links(errors: list[str]) -> None:
    link_re = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
    for markdown in ROOT.rglob("*.md"):
        if ".git" in markdown.parts:
            continue
        for destination in link_re.findall(markdown.read_text()):
            destination = destination.strip("<>").split("#", 1)[0]
            if not destination or re.match(r"(?:https?://|mailto:|data:)", destination):
                continue
            if not (markdown.parent / destination).exists():
                fail(errors, f"broken Markdown link: {markdown.relative_to(ROOT)} -> {destination}")


def scan_public_text(errors: list[str]) -> None:
    for path in list(ROOT.rglob("*.md")) + list(ROOT.rglob("*.yaml")) + list(ROOT.rglob("*.yml")):
        if ".git" in path.parts:
            continue
        content = path.read_text()
        for forbidden in FORBIDDEN_TEXT:
            if forbidden in content:
                fail(errors, f"forbidden internal or legacy reference in {path.relative_to(ROOT)}: {forbidden}")
        for pattern in SECRET_PATTERNS:
            if pattern.search(content):
                fail(errors, f"possible secret in {path.relative_to(ROOT)}")


def main() -> int:
    errors: list[str] = []
    validate_task(errors)
    validate_allowlist(errors)
    validate_markdown_links(errors)
    scan_public_text(errors)
    if errors:
        print("validation failed:")
        print("\n".join(f"- {error}" for error in errors))
        return 1
    print("validation passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
