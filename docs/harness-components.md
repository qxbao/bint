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

**Guardrail dựng sẵn `bint-readonly` (driver session, G3b).** Ngoài luật `.bint/harness`, hook của Claude Code có sẵn một
guardrail không cần khai báo: `PreToolUse` của subagent `bint-planner` / `bint-reviewer` (nhận biết bằng `agent_type`) trả `deny`
cho Edit/Write/MultiEdit/NotebookEdit và cho lệnh Bash không khớp `workflow.agent.readonly_tools`, kèm lý do và sự kiện
`guardrail.triggered` (`guardrail: bint-readonly`). Khác biệt giữa harness nằm ở `hookcore.Dialect.SubagentReadonly` (Claude: có;
Codex, Antigravity: không). Hook cài thêm `SubagentStop` (ghi `subagent.stopped` với `agent_id` và `agent_transcript_path`, và tệp
`runtime/subagents/<agent_id>.json`) để tính chi phí subagent; repo cài hook bản cũ chạy lại `bint init`. Chi tiết khớp lệnh và giới
hạn: [workflows.md](workflows.md#driver-session).

**Guardrail dựng sẵn `bint-worktree` (driver session, G4).** Guardrail dựng sẵn thứ hai, cũng không cần khai báo: `PreToolUse` của
subagent `bint-implementer` thuộc bước của tác vụ `worktree` trả `deny` cho Edit/Write/MultiEdit/NotebookEdit có đường dẫn ngoài
worktree và cho lệnh Bash không bắt đầu bằng `cd <worktree> &&`, kèm lý do và `guardrail.triggered` (`guardrail: bint-worktree`,
`outcome: fired|unmapped`). Hook ánh xạ `agent_id` ⇒ worktree bằng dòng đầu transcript của subagent (`hookcore.Dialect.SubagentPrompt`;
Claude: có; Codex, Antigravity: không, guardrail không áp) và tệp `runtime/session-steps/<run_id>.json` do `bint run next` ghi; hook
chỉ đọc tệp. Không ánh xạ được thì fail-closed có giới hạn (chỉ khi còn bước worktree đang chạy). Chi tiết:
[isolation-subagent-plan.md](isolation-subagent-plan.md) mục 12.14.

**Hậu kiểm `bint-worktree` ở `bint run complete`.** Vì hook fail-open, `complete` của bước `bint-implementer` (tác vụ `worktree`) còn
quét transcript của subagent: lời gọi vi phạm đã thực sự chạy mà hook không chặn thì tác vụ `blocked` và spool có
`guardrail.triggered` với `outcome: missed_by_hook`, `on: post_step` (do workflow ghi, không phải hook). Chi tiết và giới hạn:
[workflows.md](workflows.md#driver-session).

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

### Ghi vết hook (`BINT_HOOK_TRACE`)

Chẩn đoán "hook có được gọi không, nhận gì, trả gì". Đặt `BINT_HOOK_TRACE=1` **trước khi mở harness** (hook kế thừa môi trường
của harness); mỗi lần `bint hook <harness>` chạy ghi một cặp tệp dưới `<thư mục trạng thái>/runtime/hook-trace/`:

- `<thời điểm UTC>-<harness>-<sự kiện>.in`: đầu vào nguyên văn (cắt ở 1 MiB kèm dòng đánh dấu), ghi **trước** khi xử lý;
- `<cùng tên>.out`: JSON `output` (thứ in ra stdout cho harness), `duration_ms`, `skipped`, `written`, `spool_file`, `error`.

Có `.in` mà không có `.out` nghĩa là hook chết hoặc quá hạn giữa chừng; không có `.in` nghĩa là harness không gọi hook. Chỉ giữ 500
tệp mới nhất (cũ nhất bị xoá). Không đặt biến (hoặc giá trị khác `1`) thì không ghi gì. Lỗi ghi vết bị bỏ qua: hook không bao giờ
chặn agent, không đổi đầu ra, không mở SQLite. **Cảnh báo:** `.in` chứa nguyên văn lệnh, đường dẫn và nội dung tệp của agent (tệp
0600, trong thư mục trạng thái ngoài repository); tắt biến và xoá thư mục khi chẩn đoán xong.
