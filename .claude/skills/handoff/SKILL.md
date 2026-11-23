---
name: handoff
description: Compact the current conversation into a handoff document for another agent to pick up.
argument-hint: "What will the next session be used for?"
disable-model-invocation: true
---

Next session: $ARGUMENTS

Write a handoff document summarising the current conversation so a fresh agent can continue the work. Save it to the OS temp directory
(`$TMPDIR`, else `/tmp`; `%TEMP%` on Windows) as `handoff-<timestamp>.md`, not in the workspace, and tell the user the absolute path.

Point, don't copy: reference what already exists by path, ID or commit instead of restating it:

- Bint tickets: task ID, status, step it stopped at (`bint task show <id>`), its `requirement.md`/`plan.md`/`tasks.md`, open review
  findings, recorded errors and open questions (`Q-N`);
- plans and ADRs in `docs/`, `GLOSSARY.md`, knowledge item IDs (`[k:<id>]`) that mattered;
- commits, branches, worktrees (`bint run status <id>`), uncommitted changes (`git status`).

Write what only this conversation knows: decisions made and why, approaches tried and ruled out, the user's preferences, the exact next
step. Include a **Suggested skills** section naming which skills the next agent should call (e.g. `/b-do`, `/grill-with-docs`, `tdd`,
`diagnosing-bugs`).

Redact secrets and personal data (API keys, passwords, emails, tokens): write `<REDACTED>`.

If the user passed arguments, treat them as what the next session will focus on and tailor the document to that.
