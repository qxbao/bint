# Phiếu tác vụ

## Sub-features

- `bint task new "<yêu cầu>" --workflow b-do --plan --criteria … --allow … --json`: lập phiếu, phân loại, chế độ cô lập.
- `bint task show <id> --json`, `bint task lint <id>` (hợp đồng artifact v1).

## How to get to it (user POV)

`bint task new` (hoặc `/b-do`, `/grill-with-docs` in bản nháp lệnh), rồi `bint task show`.

## Driving it with vb.sh

1. `vb.sh run <RUN> task-new -- task new "Thêm hàm Sub vào demo" --workflow b-do --plan --criteria "Sub(5,3) trả 2" --clear-scope --allow 'add.go' --json`
   → JSON có `ticket.id`, `classification.plan: true`.
2. `vb.sh run <RUN> task-show -- task show <id> --json` → `criteria[0]` đúng chuỗi đã nhập, `scope.allow` là `["add.go"]`.
3. `vb.sh run <RUN> task-lint -- task lint <id>` → chưa có tài liệu b-req nên báo lỗi thiếu tài liệu, thoát khác 0 (đúng hành vi).

## Gotchas

- `bint run` cần harness thật (claude/codex) nên không lái ở đây.
- Lệnh kiểm chứng chỉ chạy sau `bint task approve` (chỉ người): `verified-unreachable: cần người`.
