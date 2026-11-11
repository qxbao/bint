# Thành phần quy trình của agent (P8 — kế hoạch H)

Hướng dẫn sử dụng cho phần mã H1–H4 (đã có mã và kiểm thử không tốn token, **phần đo H5 chưa chạy**).
Thiết kế gốc và danh sách hạng mục xem [p8-harness-evolution-plan.md](p8-harness-evolution-plan.md);
vòng tiến hoá chung xem [evolution.md](evolution.md).

Ngoài tri thức và skill, Bint quản lý **thành phần quy trình** — chỉ dẫn dùng công cụ, guardrail, script
hỗ trợ, chiến lược chọn ngữ cảnh — như tệp có phiên bản trong `.bint/harness/` (commit được, như tri thức):

```
.bint/harness/
  guidance/<id>.md      chỉ dẫn (frontmatter YAML + Markdown); guidance chung vào khối Bint của AGENTS.md,
                        guidance riêng (scope domain:<glob>) được chèn qua hook một lần mỗi phiên khi đường dẫn khớp
  guardrails/<id>.yml   luật: on (pre_edit | pre_bash | stop) · when · unless · action (remind | ask | deny) · message
  scripts/<id>.sh       script hỗ trợ + <id>.yml mô tả; chỉ được liệt kê trong chỉ dẫn, Bint KHÔNG BAO GIỜ tự chạy
  strategy.yml          ghi đè tập con context.* (max_tokens, path_max_items, lessons, interfaces.mcp|path_hooks)
```

Mỗi thành phần có `id`, `scope` (`global` | `domain:<glob>`), `status` (`candidate` → `verified` →
`deprecated`), `origin` (`human` | `agent`), `version`. **Chỉ `verified` có hiệu lực ở phiên thật.**

```sh
bint harness component add --template test-before-stop     # luôn vào ở candidate (origin=agent nếu gọi từ agent)
bint harness component add --kind guardrail --file g.yml   # (script: thêm --script-file hoặc g.sh cùng tên)
bint harness component list | show <id>
bint harness component approve <id>        # chỉ người; ghi người duyệt. deprecate: tương tự
bint harness render [--harness codex] [--paths a.go,b.go]   # agent sẽ nhận gì; guardrail bị hạ mức thế nào
```

(`bint harness list|show <tên>` vẫn là bảng adapter.) Ví dụ guardrail:

```yaml
id: test-before-stop
scope: global
status: candidate
origin: human
version: 1
on: stop
when: { edited: "src/**" }   # glob tệp đã sửa trong phiên (paths: tệp của lần sửa; command: regexp lệnh)
unless: { ran: test }        # đã chạy lệnh kiểm thử SAU lần sửa cuối (hoặc regexp lệnh)
action: remind
message: "Chưa chạy test cho phần đã sửa; chạy test trước khi kết thúc."
```

**Thực thi:** hook (không mở SQLite, không gọi mô hình, không bao giờ trả lỗi) đọc luật từ
`.bint/harness`, theo dõi lệnh và tệp đã sửa của phiên trong tệp JSONL nối thêm ở `runtime/harness/`, và
ghi `guardrail.triggered` vào spool. `remind` chèn ngữ cảnh (ở `stop`: chặn kết thúc **một lần** mỗi phiên);
`deny` ở `stop` chặn tối đa 3 lần rồi cho qua (`bypass`) — Bint không nhốt agent. Harness không hỗ trợ một mức
thì hành động bị hạ theo thứ tự deny → ask → remind → bỏ (xem bảng năng lực tại [workflows.md](workflows.md#multi-harness-p5)). **Hook `Stop` mới:** repo
đã cài hook bản cũ cần chạy lại `bint init` (`bint doctor` báo thiếu hook cho tới lúc đó).

**Đo trong eval:** nhánh `bint` dùng tập thành phần đang hiệu lực; guidance dựng sẵn `bint-mcp-tools`
thay đoạn ghi chú MCP cũ **đúng từng byte** (repo không có `.bint/harness` thì nhánh `bint` y hệt trước).
Biến thể `bint@harness=off` (chỉ bản dựng sẵn) | `on` (mặc định: verified) | `all` (thêm candidate), ví dụ
`bint eval run --arms bint,bint@harness=all`. Báo cáo có bảng số liệu từng guardrail theo nhánh: số lượt có
guardrail hiệu lực, đủ điều kiện, kích hoạt, làm theo, bị chặn, vượt trần (bảng `eval_guardrail_usage`, migration 0009).

**Tiến hoá:** cơ hội `harness` (nhóm lượt thất bại validation theo dấu hiệu hành vi: `no_test`/`edit_outside` →
guardrail, `repeat_cmd`/`timeout` → chỉ dẫn, `ignored_ctx` → chiến lược; và `harness:mcp_unused`) sinh đề xuất
`target_type: harness` (create/update/deprecate) do agent theo `roles.evolver` viết — **không công cụ, thư mục
rỗng, chỉ claude**; rủi ro do Bint tính (guidance/`remind` thấp; `ask`/strategy trung bình; `deny`/script cao).
Mỗi cơ hội cho tối đa **hai đề xuất anh em, chung và riêng**: bản chung đo trên mẫu toàn validation, bản riêng chỉ
trên tác vụ khớp vùng; `bint evolve cycle` giữ một nửa theo `non_inferiority_margin` và **không bao giờ gộp** hai
bản. `bint evolve evaluate` buộc `--delivery bint` cho đề xuất quy trình. Tự áp dụng chỉ với `create` rủi ro thấp,
và thành phần vẫn vào ở `candidate` chờ người duyệt. Phần đo (tốn tiền) chưa chạy: xem [p8-harness-evolution-plan.md](p8-harness-evolution-plan.md).
