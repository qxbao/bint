---
name: principles
description: Engineering principles index. Use when planning, implementing, debugging, reviewing or measuring and a choice needs a principle — diff size, sequencing, domain modelling, boundaries, idempotency, shared state, tests, numbers, proof, repeated failed fixes, root causes, recurring corrections.
---

# Principles

One line per principle; open the leaf file when its trigger fits. Each leaf is short: read it whole before applying it.

| Principle | Apply when |
|---|---|
| [laziness-protocol](laziness-protocol.md) | refactoring, judging diff size, tempted to add abstractions or layers: bias to deletion and the smallest change that solves the problem |
| [subtract-before-you-add](subtract-before-you-add.md) | sequencing an addition, refactor or rewrite: remove dead code and redundant checks first, then build on the simpler base |
| [model-the-domain](model-the-domain.md) | writing stateful logic, or code that branches a lot or repeats a shape assumption across files |
| [boundary-discipline](boundary-discipline.md) | wiring validation, error handling or adapters: guard at system boundaries, trust internal types |
| [make-operations-idempotent](make-operations-idempotent.md) | commands, lifecycle steps or loops that run amid crashes, restarts and retries: converge to the same end state |
| [separate-before-serializing-shared-state](separate-before-serializing-shared-state.md) | concurrent actors might write the same file, branch, key or state: remove the sharing before adding locks |
| [sequence-verifiable-units](sequence-verifiable-units.md) | multi-step work and commit/PR stacking: small units, each ending in a verifiable state, checked before the next |
| [test-behavior-not-implementation](test-behavior-not-implementation.md) | writing, changing or keeping a test: assert what users of the code observe |
| [explain-the-number](explain-the-number.md) | before measuring, and before trusting or reporting a measured number: find what limits it, rule out measuring the wrong thing |
| [prove-it-works](prove-it-works.md) | before declaring done: verify against the real artifact, not a proxy, self-report or "it compiles" |
| [attack-the-premise](attack-the-premise.md) | two or more fixes sharing one premise failed the same gate: write the premise down and test it first |
| [fix-root-causes](fix-root-causes.md) | debugging: fix the first place the system went wrong; a guard is a fix only when the state it handles is legitimate |
| [encode-lessons-in-structure](encode-lessons-in-structure.md) | writing the same instruction a second time, or a recurring correction: encode it as a check, not more text |

## Khi chạy dưới Bint

- Nguyên tắc không thắng cổng của Bint: phạm vi sửa (`scope.allow`), lệnh kiểm chứng, duyệt người và ngân sách luôn đứng trên.
- Trong `bint run`: không sinh agent con để áp nguyên tắc (ví dụ thử nhiều phương án song song); làm tuần tự trong phiên.
- `prove-it-works` ở Bint nghĩa là bằng chứng chạy được: `bint task criteria … --evidence "<lệnh + kết quả>"`, test mục tiêu xanh, hoặc
  skill `verify-<app>` của dự án khi có.
- `encode-lessons-in-structure` đi cùng `/retro`: lỗi lặp lại thành check/test; tri thức (`bint knowledge add`) chỉ cho phán đoán.
