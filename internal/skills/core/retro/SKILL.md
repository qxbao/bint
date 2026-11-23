---
name: retro
description: Retrospective on Bint runs and agent sessions. Find the mistakes agents keep repeating in this repo and make each one impossible — architecture first, then types, then a lint whose error names the fix, then a test, knowledge last.
argument-hint: "Optional: task IDs, a time range, or the correction you just made"
disable-model-invocation: true
---

# Retro

Phạm vi: $ARGUMENTS

You are improving the coding agents' **environment** so future runs go better: the repo, its checks, its knowledge — not this session's
code. Assume every contributor is an agent that sees only the files it opened, copies the nearest example, and takes the shortest
path that compiles. Make a change that looks right from one file right for the whole repo.

Call the Skill tool with "writing-for-agents" for the writing style guide before writing any rule, skill or knowledge text.

## 1. Gather evidence

Default scope: the last ~2 weeks of tasks, or the task IDs / correction the user named.

- Bint runs: `bint task list --all --json`, then per task `bint task show <id> --json` — step errors
  (`bint task error` records), review findings and their severity, fix rounds, verify failures, questions asked to the human.
- Git: recent commits, reverts, `fix(...)` follow-ups touching the same files.
- Knowledge: `bint knowledge audit --all` signals (stale, unused, duplicated), pending candidates.
- Steering files: `AGENTS.md`/`CLAUDE.md`, skills, overlays.
- Ask: **where did an agent need a human again?** Each such hand-back is a mistake class (missing capability, missing information).

## 2. Group into mistake classes

A class counts once it happened **twice** (two tasks, two findings, two corrections). Write each class as one sentence plus its
evidence (task IDs, finding IDs, commits, `file:line`). One-off mistakes are not classes.

## 3. Fix each class at the highest level that works

1. **Architecture**: one owner per piece of state, one supported way per task; hide internals so the wrong import fails; replace
   hand-synced lists with one source of truth; delete old ways an agent would copy.
2. **Types**: make the bad state unwritable.
3. **Lint / CI check** whose error names the file, type or function to use instead. If the pattern is already common, fail only when
   a change adds more. Mechanical violations (fixed pattern, banned API, import shape, file location) always land here or higher.
4. **Test** the behaviour; fix or delete tests that pass when behaviour breaks or fail when only internals change.
5. **Knowledge** last, only for judgement calls: `bint knowledge add --type rule|warning|pattern --paths '<glob>' --evidence <file>:<line>
   "…"` (or `report_observation`). It is a candidate until a human approves it.

**Prove each new check fails on a real past mistake** (re-introduce it on a scratch copy or `git stash` it in, run the check, see red).
A check that has never been red is a guess.

Other candidate categories (from the original retro): **navigation** pointers where agents searched long; **tool economy** (expensive
calls, token-heavy tooling); **no-ops** in steering files; **information access** (logs, read-only access the agent lacked).

## 4. Report, then act only on approval

Present classes in order of frequency × severity: each with its evidence, the level you picked, and **why a higher level didn't work**.
Ask which to implement. Approved fixes run as Bint tickets (`/b-do`, one ticket per class) or, if the user prefers, directly in this
session — one commit per class. You never approve knowledge (`bint knowledge approve` is human-only) and never mark tasks done.

Knowledge items you keep should name what enforces them (lint, test, hook, type + path) in their body, so a later retro can drop a rule
once its mistake can no longer happen.
