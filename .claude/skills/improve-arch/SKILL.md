---
name: improve-arch
description: Scan a codebase for deepening opportunities, present them as a visual HTML report, then grill through whichever one you pick and hand it to /grill-with-docs.
argument-hint: "Optional direction: a module, subsystem or pain point"
disable-model-invocation: true
---

# Improve architecture

Hướng: $ARGUMENTS

Surface architectural friction and propose **deepening opportunities**: refactors that turn shallow modules into deep ones. The aim is
testability and AI-navigability. Phiên tương tác, chỉ đọc mã cho tới khi người chọn ứng viên; không sửa mã trong skill này.

- Call the Skill tool with "codebase-design" for the architecture vocabulary (**module**, **interface**, **depth**, **seam**, **adapter**,
  **leverage**, **locality**) and its principles (the deletion test, "the interface is the test surface", "one adapter = hypothetical
  seam, two = real"). Use these terms exactly in every suggestion.
- The domain language in `GLOSSARY.md` gives names to good seams; ADRs in `docs/adr/` and Bint knowledge of type `decision` record
  decisions this command should not re-litigate.

## 1. Explore

**Scope before you scan: YAGNI.** Deepening pays off by making future changes easier, so weight the parts that change.

- The user named a direction → take it and skip the inference below.
- Otherwise find hot spots: `git log --since=3.months --name-only --format= | sort | uniq -c | sort -rn | head -40` (files that keep
  changing), plus Bint's own signal: tasks that failed or needed many fix rounds (`bint task list --all --json`, `bint task show <id> --json`:
  touched files, review findings, recorded errors).

Read `GLOSSARY.md` and the ADRs in the area first. Then walk the code with the code graph instead of reading files blindly:
`cg_explore` for an area, `cg_callers`/`cg_callees` for coupling, `cg_impact` for blast radius. Note friction:

- understanding one concept needs bouncing between many small modules;
- **shallow** modules whose interface is nearly as complex as the implementation;
- pure functions extracted just for testability while the bugs hide in how they're called (no **locality**);
- tightly-coupled modules leaking across their seams;
- untested or hard-to-test parts through their current interface.

Apply the **deletion test** to anything you suspect is shallow. Sub-agents are allowed here (interactive session); keep exploration
results as conclusions, not file dumps.

## 2. Present candidates as an HTML report

Write a self-contained HTML file to the OS temp directory (`$TMPDIR`, else `/tmp`; `%TEMP%` on Windows):
`<tmpdir>/architecture-review-<timestamp>.html`. Open it (`xdg-open` / `open` / `start ""`) and tell the user the absolute path.
Follow [HTML-REPORT.md](HTML-REPORT.md) for the scaffold, diagram patterns and styling.

Each candidate card: **Files**, **Problem**, **Solution** (plain English), **Benefits** (locality, leverage, how tests improve),
**Before / After diagram**, **Recommendation strength** (`Strong` | `Worth exploring` | `Speculative`). End with **Top recommendation**.
A candidate contradicting an ADR or a `decision` knowledge item appears only when the friction warrants reopening it, marked clearly.

Propose no interfaces yet. Ask: "Which of these would you like to explore?"

## 3. Grilling loop

Once the user picks a candidate, call the Skill tool with "grilling" to walk the decision tree: constraints, dependencies, the shape of
the deepened module, what sits behind the seam, what tests survive. Call the Skill tool with "domain-modeling" to keep `GLOSSARY.md`
and ADRs current as decisions crystallise (a rejection with a load-bearing reason → offer an ADR so future reviews don't re-suggest it).

**Done when** the user confirms the deepened design. Then hand off to `/grill-with-docs` (or draft the `bint task new` command the same
way it does) so the refactor runs as a Bint ticket with criteria, scope and a test seam. Large refactors → expand–contract phases as
parent/child tickets.
