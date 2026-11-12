---
schema: 1
id: 01M768KJGVZ4K2ZQ3JRKW01C6H
type: warning
status: verified
title: Chân phím trong khung phải đi qua frame.Box.BottomHints (footer.FitStyle tự rút nhãn rồi bỏ phím theo độ rộng khung), đ…
scope:
  paths:
    - internal/tui/**
evidence:
  - path: internal/tui/components/frame/frame.go
    lines: "39"
    fingerprint: lines:72e975c229fedd07
origin:
  kind: agent
  session: f8f803a0-c2b2-494a-b7fd-4b87dab90987
  commit: 38c13c96e378836a570ab91677974d35accc1f41
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-12T02:17:20Z"
version: 1
created_at: "2026-11-11T21:35:44Z"
updated_at: "2026-11-12T02:17:20Z"
verified_at: "2026-11-12T02:17:20Z"
status_reason: người duyệt chấp nhận
---

Chân phím trong khung phải đi qua frame.Box.BottomHints (footer.FitStyle tự rút nhãn rồi bỏ phím theo độ rộng khung), đừng tự ghép chuỗi Bottom: edge() sẽ cắt giữa một phím. Phần tử bấm chuột bọc bằng zone.Mark (chỉ có tác dụng trong Model.Render, nơi dấu được gỡ); danh sách mới phải vẽ bằng list.Render để hàng bấm được.
