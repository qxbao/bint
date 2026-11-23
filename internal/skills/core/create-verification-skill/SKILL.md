---
name: create-verification-skill
description: Generate a project-local verification skill (.bint/skills/verify-<app>/) that drives the app the way a user does and captures evidence — any language, framework or platform. Use for /create-verification-skill or when a project has no scripted way to prove UI/CLI/service behaviour.
argument-hint: "Optional: app name or surface to target"
disable-model-invocation: true
---

# Create a verification skill

Đích: $ARGUMENTS

Every serious project needs a scripted way to drive the real app and prove behaviour: launch it, exercise a feature the way a user
would, and capture evidence. This skill generates that as a **Bint custom skill** `.bint/skills/verify-<app>/`, published by `bint sync`
to every harness. You write it for the next agent, not for a human: it will be read cold, mid-task (often by b-feat or b-goal), by an
agent that has never seen the app.

## 1. Interview the repo, not the user

Answer these from the codebase and ask the user only what you cannot observe:

- **Surface:** what does a user touch? Web UI, CLI/TUI, desktop app, API, mobile app, library? Pick the primary one, note the rest.
- **Run:** how does the app start locally? Prefer the repo's own documented command (package scripts, Makefile, README). Note ports,
  env vars, seed data, auth.
- **Drive:** how can an agent interact with it programmatically? Existing harnesses first (Playwright/Cypress specs, expect scripts,
  PTY helpers in the test suite, curl-able endpoints, a debug port). Then a generic recipe: browser/CDP for web and Electron, plain HTTP
  for services, a PTY harness for CLI/TUI. **Windows has no native tmux**: for CLI/TUI use the repo's own PTY test helpers, a ConPTY-based
  driver (e.g. a small Go/Python program over a pseudo-console), or plain non-interactive invocations with flags and stdin — say which in
  the skill and keep one recipe that runs on every OS the project supports.
- **Observe:** what evidence can be captured? Screenshots, terminal transcripts, response bodies, logs, exit codes, files, DB state.
- **Isolate:** can two instances run side by side (ports, data dirs, profiles, `HOME`)? If not, say so: refusing to double-drive a shared
  instance beats corrupting the user's session.

If the checkout doesn't build or start as-is, fix that first (or report it precisely) before generating; a skill written against a broken
base teaches wrong steps.

## 2. Generate the skill

Run `bint skill new verify-<app>` for the skeleton, then write `.bint/skills/verify-<app>/SKILL.md`. Frontmatter: `name: verify-<app>`
and a `description` opening with "Dùng khi …"/"Use when …" that names the app, the surface and when to reach for it (only `name`,
`description`, `disable-model-invocation`, `argument-hint` are allowed). Sections, each grounded in what the interview found, no
placeholders left:

- **Launch:** the exact command that starts the app for verification and how to tell it's ready (a log line, a prompt). Launch reaches a
  usable state from an empty data dir, including migrations and seed data; ready means usable, not just that a process answers. Include
  teardown. For a short-lived CLI or TUI: build the binary once, then start each drive in its own isolated session/data dir.
- **Doctor:** one read-only check answering "is this instance worth driving?": process up, right version/build, port owned by us, auth
  valid. Run it first whenever anything looks off.
- **Drive:** the harness recipe with real selectors/commands from this repo. Prefer stable handles (ARIA labels, data attributes, prompt
  strings, route paths, CLI flags) over coordinates and tab order.
- **Evidence:** what to capture and where it goes. Proof standards: exercise the real user path, not internal setters or test-only
  endpoints; capture the action and the resulting state; verify side effects (files written, rows inserted, messages sent) alongside
  what's visible; mocks only where a production boundary already isolates the external system. A dry-run/test mode is verified by
  observing what it skips (files, network, git refs), not by trusting its name.
- **Cleanup:** tear down what this run started (never kill by process name). Cleanup removes instances and scratch state, never the
  evidence: proof artifacts survive in a location the skill names.
- **Helpers:** scripts live in the skill folder (they're published as aux files), are executable, and their invocation is in the body.

## 3. Seed the feature map

Create `.bint/skills/verify-<app>/features/README.md` plus one file per user-facing feature (top 3–5 to start, from routes, commands,
menus or docs), following [references/feature-map-example/](references/feature-map-example/README.md): a README index, then per feature
the four H2s `Sub-features`, `How to get to it (user POV)`, `Driving it with <harness>`, `Gotchas`. If seed data lacks a journey's
precondition, the journey reports failure and the reason goes in `Gotchas`; never weaken the journey to make it pass.

## 4. Prove it before handing it over

`bint skill lint verify-<app>` must pass. Then run the skill's own instructions end to end once: launch, doctor, drive ONE mapped feature,
capture evidence, clean up, and confirm the evidence still exists at the named location. Fix what fails, running the generated cleanup
after every failed iteration. A generated skill that was never executed is a draft. If you proved it in a stand-in for the real target,
list every setup step the stand-in did that the target will not.

Then `bint sync` publishes it. Optionally add pressure scenarios (`tests/*.yml`) and `bint skill test verify-<app>` as `/b-add-skill`
describes.

## 5. Hand over

Tell the user: the skill path, the feature files, the evidence of the proving run. Point at `/maintain-verification-skill` for keeping
the map honest as the app changes. Under Bint, b-feat and b-goal use `verify-<app>` when a criterion is about user-visible behaviour;
Bint still runs the ticket's `verify.command` as the gate.
