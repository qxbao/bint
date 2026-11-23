---
name: verify-bint
description: Dùng khi cần chứng minh một hành vi người dùng thấy của CLI bint (init, sync, skill, task, knowledge, doctor…) trên binary thật dựng từ cây hiện tại, trong repo nháp cô lập, kèm bằng chứng — không chỉ unit test. Cũng dùng khi tiêu chí chấp nhận của phiếu nói về đầu ra CLI.
---

# verify-bint — lái bint thật trong môi trường cô lập

Mọi lệnh chạy từ **gốc repo Bint**, qua `bash` (Git Bash trên Windows; không cần tmux). Helper:
`bash .bint/skills/verify-bint/scripts/vb.sh <launch|doctor|run|cleanup>` (xem đầu tệp
[scripts/vb.sh](scripts/vb.sh)). Một lượt kiểm = một thư mục `RUN` trong thư mục tạm, cô lập hoàn toàn: `BINT_STATE_DIR`,
`BINT_CONFIG_HOME`, `BINT_ISO_HOME`, `HOME`/`USERPROFILE` riêng, `GIT_CONFIG_NOSYSTEM=1`. Không đụng trạng thái, cấu hình hay
repo thật của người dùng.

## Launch

```sh
bash .bint/skills/verify-bint/scripts/vb.sh launch        # in dòng RUN=<thư mục>; ghi nhớ nó
```

Build `bint` từ cây hiện tại vào `<RUN>/bin`, dựng repo Go nháp `<RUN>/work` (một commit `add.go`, `go.mod`), chạy
`bint init --harness claude,codex --no-hooks`. **Sẵn sàng** khi lệnh in `RUN=` và thoát 0. Lượt kiểm song song: mỗi lượt một
`launch` riêng (thư mục khác nhau, không chung gì).

## Doctor

```sh
bash .bint/skills/verify-bint/scripts/vb.sh doctor <RUN>
```

Kiểm binary có trong `<RUN>/bin` (in `--version`: commit nguồn, `sửa` khi cây có thay đổi chưa commit), repo nháp đã init, và
`bint doctor` không có mục ✗. Chạy trước lần lái đầu và sau mọi lần lái hỏng.

## Drive

```sh
bash .bint/skills/verify-bint/scripts/vb.sh run <RUN> <tên-bằng-chứng> -- <đối số bint…>
```

Chạy `bint <đối số>` trong `<RUN>/work` với môi trường cô lập; in và ghi bản chép. Thao tác trên repo nháp (sửa tệp, `git`) làm
trực tiếp trong `<RUN>/work`. Ưu tiên `--json` khi cần khẳng định chính xác; so khớp theo khoá JSON hay câu thông báo, không theo vị
trí cột. Biến đánh dấu agent (`CLAUDECODE`…) được giữ: thao tác chỉ-dành-cho-người (`task approve`, `knowledge approve`) bị chặn
như thật — ghi là `verified-unreachable: cần người` thay vì lách.

Bản đồ tính năng: [features/README.md](features/README.md).

## Evidence

Mỗi `run` ghi `<RUN>/evidence/<tên>.txt`: dòng lệnh, stdout+stderr, `[exit N]`; `launch` ghi `launch-init.txt` và `source-rev.txt`.
Chuẩn chứng minh: đi đúng đường người dùng (lệnh CLI công khai), ghi cả hành động lẫn trạng thái sau (tệp trên đĩa, `--json`), kiểm
tác dụng phụ (tệp `.claude/skills/…`, mục `.bint/knowledge/items/…`, `core.lock`). Trích bằng chứng vào tiêu chí:
`bint task criteria <id> <n> met --evidence "<lệnh vb.sh> → <dòng then chốt>; <RUN>/evidence/<tên>.txt"`.

## Cleanup

```sh
bash .bint/skills/verify-bint/scripts/vb.sh cleanup <RUN>
```

Xoá repo nháp, trạng thái, cấu hình và binary của lượt; **giữ** `<RUN>/evidence/`. Chỉ xoá thư mục tên `verify-bint-*`. Không
dừng tiến trình theo tên.

## Gotchas

- TUI (`bint` không đối số) không lái được qua helper này: hành vi TUI kiểm bằng bộ test mô hình sẵn có
  (`go test ./internal/tui/app -run <Test…>`, golden trong `internal/tui/app/testdata/golden`).
- Harness thật (`claude`, `codex`) không được gọi; luồng `bint run` cần agent nên nằm ngoài phạm vi skill này.
- Windows: đường dẫn trong lệnh viết bằng `/` (sh nuốt `\`); `RUN` có dạng `/tmp/verify-bint-…` trong Git Bash.
