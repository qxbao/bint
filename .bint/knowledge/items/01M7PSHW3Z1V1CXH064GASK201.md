---
schema: 1
id: 01M7PSHW3Z1V1CXH064GASK201
type: warning
status: candidate
title: Đổi đề bài do Bint dựng cho b-req/b-feat (prompt() trong run.go, kể cả khối sliceGuideBlock) làm TestFlowGoldenRunnerBe…
scope:
  paths:
    - internal/workflow/run.go
    - internal/workflow/testdata/**
evidence:
  - path: internal/workflow/flow_golden_test.go
    lines: "611"
    fingerprint: lines:fb2611cddbc3817c
origin:
  kind: agent
  session: 849a4ec1-1273-4746-8442-8e9387a7c494
  commit: 6ed58aa00e99644a3f1804730dea2b0fc2fdd2e6
author: Duong Quan Bao <dquanbao@gmail.com>
version: 1
created_at: "2026-11-18T07:39:45Z"
---

Đổi đề bài do Bint dựng cho b-req/b-feat (prompt() trong run.go, kể cả khối sliceGuideBlock) làm TestFlowGoldenRunnerBehaviour đỏ vì golden ghim độ dài và băm đề bài: sinh lại bằng BINT_UPDATE_GOLDEN=1 go test -run TestFlowGoldenRunnerBehaviour ./internal/workflow rồi xem git diff chỉ đổi dòng băm của bước tương ứng.
