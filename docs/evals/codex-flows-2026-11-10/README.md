# Kiểm chứng các flow Codex — 2026-11-10

**Trạng thái:** đã chạy 15 session Codex thật, sửa các lỗi citation, path hook eval và tiếp tục sau khi bị chặn. `b-do` đủ ba bước, đường dẫn bảo vệ → duyệt → resume, và eval strict ba nhánh đã qua trên corpus nhỏ. **Chưa nghiệm thu toàn bộ mọi tình huống F1–F7**: các phần chưa chạy thật được liệt kê riêng bên dưới. Không suy rộng từ tác vụ cộng/nhân số nguyên sang hiệu quả trên dự án lớn.

## Thiết lập và ranh giới bằng chứng

| Mục | Giá trị |
|---|---|
| Codex | `codex-cli 0.167.0`, model `gpt-6.1-sol`, effort low, đăng nhập thuê bao; chỉ có token, không có USD |
| Bint ban đầu | `2764e8fc95f1715d77e596a8c3701e0de12aa020` (`origin/main` sau fetch) |
| Nhánh sửa | `fix/codex-flows`; mỗi lỗi một commit; không merge/push nhánh này |
| Merge trước khi khảo sát | Đã merge `origin/main` vào `duc`, commit `a8434a9`, không xung đột; nhánh `duc` được giữ nguyên |
| Repo thử | Hai Git repo tạo mới: `/probe/repo` cho hook/workflow, `/probe/eval-project` cho eval; project Go nhỏ, không phải repo Bint |
| Sandbox | `workspace-write`, approval policy `never`; không dùng cờ bỏ sandbox/approval |
| Hook tự động | Chỉ ở các lượt đã kiểm tra hook, dùng `--dangerously-bypass-hook-trust`; đây là lựa chọn của phép thử, adapter không tự thêm cờ |
| Quota | 15 session có `thread.started` và `turn.completed`; xem [sessions.json](evidence/sessions.json). Lỗi khởi động và `debug prompt-input` không gọi model |

Nhãn **[đã kiểm chứng thật]** chỉ dùng cho hành vi quan sát trực tiếp ở CLI/payload/lịch sử thật. **[suy luận từ code]** gồm kiểm tra mã, kiểm thử đơn vị hoặc đối chiếu tài liệu; không coi agent giả là bằng chứng Codex. **[không kiểm được + lý do]** ghi rõ giới hạn của đợt này.

Các lượt dùng bản build từ nguồn đang sửa, sau đó được commit thành các SHA ở bảng sửa lỗi. Hai sửa biên từ review (bảng TOML trùng và quote đường dẫn khác thường) chỉ được kiểm chứng bằng test cục bộ; không gọi thêm model sau session 15. Không dùng `--agent-command` cho bất kỳ kết luận runtime Codex nào.

Các kiểm thử `multiharness_test.go` gọi `HandleHook` bằng map payload, không khởi chạy Codex. Workflow end-to-end trong `internal/cli/workflow_e2e_test.go` dùng `--agent-command`/script giả. Artifact [multiharness-smoke.json](../reacceptance-p0-p5-137dbf9/multiharness-smoke.json) với `0s` không có transcript/chương trình thực thi kèm theo để chứng minh là Codex thật: **[không kiểm được + thiếu provenance]**, không dùng nó để nghiệm thu Codex. Pilot trước có agent thật nhưng citation fail; đợt này tái hiện chính xác lỗi đó.

## Kết quả F1–F7

| Flow | Kết quả và nhãn | Bằng chứng |
|---|---|---|
| F1 — init | **[đã kiểm chứng thật]** tạo `.codex/hooks.json`, `.codex/config.toml`, đủ 5 skill `.agents/skills/`; chạy lại giữ nguyên byte cấu hình. `local` thêm hook vào ignore, `project` không thêm; gỡ giữ hook/khoá khác. Init trả mã 1 do cảnh báo trust, dù đã tạo xong tệp. | [init-first](evidence/init-first.txt), [init-second](evidence/init-second.txt), [scopes](evidence/install-scopes.json) |
| F1 — doctor/trust | **[đã kiểm chứng thật]** ban đầu báo hook chưa duyệt và project chưa trust. Sau sửa, cấu hình citation cũ bị cảnh báo. **[không kiểm được + không thao tác UI trust]** duyệt từng hook bằng `/hooks`, thu hồi trust và hash cũ sau đổi nội dung. Không thử cài hook cấp user vào home thật. | [doctor trước](evidence/doctor-before.txt), [citation trước](evidence/doctor-citation-before.txt), [doctor sau](evidence/doctor-after.txt) |
| F2 — vòng đời/tool | **[đã kiểm chứng thật]** `SessionStart`, `SessionEnd`, `PreToolUse`, `PostToolUse`; shell là `Bash`, patch là `apply_patch`; `tool_input.command` là chuỗi. Có `session_id`, `cwd`, `tool_use_id`, `transcript_path`, `model`; `tool_response` là chuỗi. | [fixture hook thật](../../../internal/harness/codex/testdata/codex-0.167/hooks.json) |
| F2 — sửa tệp/ghi nhận | **[đã kiểm chứng thật]** add/update/delete/move; Bint bắt đủ cả tên cũ lẫn mới khi move. Session 05 ghi 4 đường dẫn, 5 sự kiện sửa (extra.txt được thêm rồi xoá), 1 test qua, kết thúc completed. Replay event dùng ID ổn định ở test; không coi hai thao tác khác nhau trên cùng tệp là trùng. | [run 05](evidence/run-05.jsonl), fixture hook, các log workflow |
| F2 — context/deny/MCP | **[đã kiểm chứng thật]** hook trả additionalContext, transcript có message role `developer`; agent không nhắc lại marker nên chưa chứng minh nó áp dụng. Deny chặn migration, agent không tạo file hoặc lách qua shell. MCP hook có tiền tố thật `mcp__bint__`. | [developer message](evidence/injected-developer-context.json), [deny stderr](evidence/run-05.stderr), [run 06](evidence/run-06.jsonl) |
| F2 — độ trễ/lỗi | **[đã kiểm chứng thật]** 125 lần hook được recorder thu, median 12,165 ms, p95 143,492 ms, max 1515,652 ms; có chi phí snapshot Git. **[không kiểm được + hết ngân sách model]** chủ động làm hook lỗi/timeout để đo fail-open. Không suy kết luận fail-open từ việc hook bình thường thoát 0. | [latency](evidence/hook-latency.json) |
| F3 — MCP | **[đã kiểm chứng thật]** cả context_for_task, context_for_paths chạy; cite bị policy never chặn trước sửa, chạy thành công sau per-tool `approval_mode = "approve"`. Citation có cả ở lượt trực tiếp và eval; không tự duyệt tool tạo/sửa tri thức. **[không kiểm được + chưa chạy cặp đối chứng]** tỷ lệ agent tự gọi chỉ dựa vào AGENTS/skill so với nhắc tường minh. | [run 05](evidence/run-05.jsonl), [run 06](evidence/run-06.jsonl), [eval context](evidence/eval-c7f6ba68ccf02-bint-1-context.jsonl) |
| F4 — skill | **[đã kiểm chứng thật, CLI không gọi model]** prompt-input liệt kê đủ b-do/b-req/b-feat/b-review/b-bootstrap; thêm `agents/openai.yaml` với `allow_implicit_invocation: false` loại b-do khỏi danh sách tự kích hoạt. **[không kiểm được + quota]** model gọi riêng từng `$skill`, tự chọn theo description và gọi tường minh khi policy=false. Workflow Bint truyền sẵn nội dung skill nên không thay thế phép thử discovery/invocation. | [catalogue](evidence/context-catalogue.json), [policy](evidence/skill-policy-summary.json) |
| F4 — ngôn ngữ/event | **[suy luận từ code]** grep core không thấy yêu cầu dùng Read/Edit/Write/Grep/Task/TodoWrite/AskUserQuestion, plan mode hay `.claude/`; các chữ “Task ID” là danh từ, không phải công cụ Task. Có `$ARGUMENTS` và tên gọi `/b-*`; runner thay placeholder khi điều phối. Không có event Skill tương ứng được thu; adapter không tự tạo `skill.activated`. | `internal/skills/core/`, `internal/workflow/run.go`, `Classify`, bảng khả năng |
| F5 — b-do thật | **[đã kiểm chứng thật]** lần đầu failed vì `bint` trên PATH cũ không hiểu cấu hình; sau chỉ định đúng executable, resume chạy b-req 39 s → b-feat 38 s → b-review 36 s, task done; ghi token, parent/child và task ID, cost null. `max_cost_usd`/`max_turns` có cảnh báo không áp được. B-req/review của tác vụ này không sửa mã nguồn. Cảnh báo scope về AGENTS.md được truy về bước người thử thêm binding executable sau khi tạo task. | [lần đầu](evidence/workflow.json), [resume](evidence/workflow-resume.json), log từng bước và [sessions](evidence/sessions.json) |
| F5 — bảo vệ/resume | **[đã kiểm chứng thật]** deny trước khi có diff → task blocked; approve cũ chỉ duyệt verify, không duyệt path. Sau sửa: `task approve <id> --path migrations/001.sql` → resume chạy lại b-feat 33 s → tạo file, verify pass, done. | [blocked](evidence/protected.json), [approve cũ](evidence/protected-approve.txt), [approve path](evidence/protected-approve-path.txt), [resume](evidence/protected-resume.json) |
| F5 — giới hạn/lỗi | **[đã kiểm chứng thật]** thiếu binary và Codex CLI thoát 2 do cờ không hợp lệ đều cho task failed với lỗi dùng được, không gọi model. **[suy luận từ code + test]** readonly giữ bằng workspace-write + snapshot hậu kiểm, không phải sandbox read-only; JSONL chịu event lạ/thiếu field. **[không kiểm được + quota]** cố ý vi phạm readonly, Ctrl-C/resume khi đang sinh token, timeout, hết đăng nhập, role/mixed Claude↔Codex, các workflow đơn lẻ và b-bootstrap chạy thật. | [missing binary](evidence/missing-binary.json), [invalid flag](evidence/invalid-flag.json), test hồi quy và log |
| F6 — eval | **[đã kiểm chứng thật]** 1 case × 1 repeat × none/minimal/bint, strict: 3/3 passed; đăng nhập thuê bao còn dùng được. Bint arm ghi mcp:task, mcp:paths, cite và hook:path. Không có remote Git hoặc commit lời giải trong 3 workdir. Đây là smoke hạ tầng, không phải số liệu hiệu quả/gate. | [mine fail→pass](evidence/eval-mine.json), [đợt eval](evidence/eval-run.txt), [report](evidence/eval-report.json), [isolation](evidence/eval-isolation.json) |
| F7 — AGENTS | **[đã kiểm chứng thật, prompt-input]** AGENTS thử dài 869 byte có đủ khối bint và hướng dẫn cuối trong model input. Tệp 40 KiB: mặc định thấy block 0–31 (32 KiB); đặt 4096 chỉ thấy block 0–3. Không có chỉ dẫn riêng Claude trong khối bint đã sinh. | [catalogue](evidence/context-catalogue.json), [giới hạn](evidence/agents-limit.json) |

Ba session đầu dùng `--ignore-user-config` không thu được hook; không dùng chúng làm bằng chứng F2. Các session sau nạp cấu hình thông thường, truyền trust dự án qua `-c projects."<repo-tạm>".trust_level="trusted"`, và bật bypass hook trust cho hook đã kiểm tra. Cấu hình/prompt từng lượt đầu nằm trong `run-*-command.json`.

## Những lệch và cách sửa

| Mã | Giả định ban đầu → hành vi quan sát | Mức độ | Sửa và bằng chứng |
|---|---|---|---|
| C1 | Mọi hook timeout 10 s → Codex báo `clamping SessionEnd hook timeout to 3s ...` | Khó chịu/cấu hình sai | `f4e402d`: chỉ SessionEnd dùng 3 s; fixture cảnh báo thật, test đỏ→xanh; session 06 không còn cảnh báo |
| C2 | MCP đăng ký là citation chạy được → `MCP tool call requires approval, but approval policy is never` | Chặn flow, mất citation | `7448cfc`: per-tool cite_knowledge approval ở init + executor eval; doctor cảnh báo cấu hình cũ; session 06 và eval cite thành công |
| C3 | Codex eval thiếu path hook là giới hạn harness → executor bỏ qua Settings, trong khi F2 chứng minh Codex có hook apply_patch | Chặn khả năng nhánh bint/sai phép so sánh | `9fb3064`: sinh inline PreToolUse dialect Codex từ executable MCP; không chạy hook Claude, không tự bỏ trust; eval có hook:path thật |
| C4 | task approve sau deny sẽ duyệt path → deny xảy ra trước file/diff nên không có PendingPaths; resume cũ không quay lại thực thi | Chặn flow | `c96c599`: người dùng chọn `--path <tệp-cụ-thể>` (không glob/ngoài repo), resume chạy lại b-feat; chỉ lựa chọn mới có hành vi này. Lệnh approve không cờ và flow Claude hiện có giữ nguyên. Fixture deny + unit/CLI test + session 15 thật |
| C5 | Cập nhật managed MCP block không ảnh hưởng bảng con thủ công → có thể sinh hai header cite_knowledge giống nhau | Chặn khởi động, phát hiện bằng review/test | `52b61a9`: phát hiện cả namespace bảng con ngoài block, báo conflict và giữ nguyên tệp thay vì phá cấu hình. Test nâng cấp có manifest cũ tái hiện lỗi; chưa gọi model cho trường hợp biên |
| C6 | Bọc executable trong dấu nháy kép là đủ → shell diễn giải `$()`, `$HOME`, dấu nháy/backslash trong tên | Sai executable, phát hiện bằng review/test | `f42463b`: quote POSIX cho hook eval; test thực thi đường dẫn literal. Windows giữ nhánh quote riêng, chưa thử Windows. Không thay bộ sinh lệnh hook Claude |
| O1 | `bint` gọi trong shell là bản build đang khảo sát → PATH tìm bản cũ, từ chối nhiều field config | Chặn flow do thiết lập máy | Repo thử hướng dẫn dùng executable tuyệt đối; không thay binary đã cài của người dùng. Log lỗi giữ nguyên; không đổi schema/test để né lỗi |
| O2 | Mô hình nhớ marker là bằng chứng chèn context → hook trả và transcript developer có marker nhưng model nói không thấy | Giới hạn diễn giải | Dùng transcript/payload làm bằng chứng truyền context; không nâng thành “agent đã áp dụng” |
| O3 | Codex không có lựa chọn chỉ gọi tường minh → policy=false thực sự loại skill khỏi catalogue | Khai báo thiếu | Cập nhật bảng khả năng thành partial, ghi rõ Bint chưa sinh policy; chưa thay kiến trúc xuất skill |

Không sửa `docs/gate-*.md`, không đổi thước đo/ngưỡng hồi tố và không nới sandbox toàn cục. Corpus tri thức verified trong repo thử là dữ liệu tổng hợp dựng cho phép thử, không phải thao tác agent tự duyệt tri thức của dự án Bint.

## Còn mở và giới hạn thực tế

- **[suy luận từ code] Doctor trust chưa xác minh hash hiện hành.** `HooksTrusted` chỉ tìm dấu vết ít nhất một hook của file trong config user; chưa đủ để khẳng định mọi hook mới/đã đổi đều được duyệt. Phép thử xác minh cảnh báo ban đầu, không nghiệm thu trường hợp trust một phần/hash cũ. Cần thu luồng `/hooks` thật trước khi thay thuật toán hash, hoặc hạ thông báo thành “chưa xác minh”.
- **[đã kiểm chứng thật] Strict chưa cô lập hết ngữ cảnh cấp người dùng.** Dù Git workdir không có lời giải/remote, cả ba nhánh vẫn đọc skill plugin ở home Codex. [isolation](evidence/eval-isolation.json) giữ các lệnh chứng minh, bỏ nội dung riêng của plugin. Không gọi nhánh none là “không có mọi ngữ cảnh người dùng”; không dùng smoke này để nghiệm thu chống rò rỉ toàn môi trường. Cần thiết kế profile tự động cô lập skill/config nhưng giữ auth; không sao chép auth hoặc sửa home thật trong đợt này.
- **[không kiểm được + ngân sách 15 session]** F2 fail-open khi lỗi/timeout; F4 gọi riêng đủ năm skill và description matching; F5 cố ý sửa ở bước chỉ đọc, Ctrl-C/timeout/hết đăng nhập, role/mixed và các workflow đơn lẻ. Các test dùng binary/agent giả chỉ kiểm hợp đồng của Bint, không đổi nhãn thành Codex thật.
- **[không kiểm được + tránh sửa cấu hình user]** cài/gỡ `--hooks-scope user` và thao tác duyệt trust tương tác. `local`/`project` đã thử thật; source `HooksPath(user)` trỏ Codex home nhưng chưa nghiệm thu runtime user scope.
- **[suy luận từ code]** output init vẫn gợi ý `/b-bootstrap trong Claude Code` ngay cả khi chỉ bật Codex. Skill core chủ yếu trung tính, nhưng tên `/b-*`/`$ARGUMENTS` cần trình bày theo harness. Đề xuất thêm metadata trình bày/policy ở lớp xuất `skills.Target` và dùng body gốc + overlay hiện có; không nhân đôi bộ skill. Chưa triển khai thay đổi kiến trúc này.
- **[suy luận từ code]** metadata `allow_implicit_invocation` có thể bổ sung qua `agents/openai.yaml` nhưng chưa được Bint xuất tự động. Không diễn giải nó là ẩn khỏi menu người dùng. Chưa thu được sự kiện native Skill để bật CapSkillEvents trên Codex.
- **[đã kiểm chứng thật]** Codex chỉ báo token; cost/num_turns là null, không tự suy ra tiền hay số lượt model từ token/turn.started. Các giới hạn max_cost/max_turns chưa áp được ở tầng Codex và Bint đã nói rõ điều đó.
- **[đã kiểm chứng thật]** shell Codex của máy này tìm Go 1.22.2, trong khi host là 1.25.1; thử go.mod 1.24 từng bị lỗi cache read-only khi tự tải toolchain. Corpus sau dùng Go 1.22 và cache trong repo; không sửa test để ép qua. `minimal`/`bint` agent có lúc không tự chạy được test do cache, nhưng bộ chấm host áp test ẩn sau agent và đều qua; log hai tầng được giữ riêng.

## Các bước người dùng cần làm

1. Dùng đúng bản Bint đã build/cài trên PATH (đối chiếu executable trong cấu hình MCP và lệnh skill). `bint init --harness codex --hooks --mcp --agents-md` tạo cấu hình/skill nhưng không thay Codex trust cho người dùng.
2. Mở Codex tại repo, tin cậy project, rồi vào `/hooks` đọc và duyệt các định nghĩa hook. Đây là hai lớp trust; không coi cảnh báo init là thao tác thất bại toàn bộ.
3. Giữ policy riêng `cite_knowledge` do init tạo. Nếu có bảng `mcp_servers.bint...` tự viết ngoài managed block, xử lý conflict theo thông báo; Bint không ghi đè lựa chọn của người dùng.
4. Nếu hook chặn trước khi có diff: đọc đúng đường dẫn/lý do, chạy `bint task approve <task-id> --path <đường-dẫn-tương-đối>` rồi `bint run resume <task-id>`. `--path` là tên file, không glob; agent không được tự chạy approve.
5. Chỉ khi làm automation với hook đã kiểm tra mới chủ động dùng bypass hook trust từng invocation. Không bật mặc định: cờ này bỏ kiểm tra trust cho **mọi** hook được bật trong lượt, dù sandbox vẫn giữ nguyên.

Các lượt CLI đã để lại 4 mục project trust tạm trong config user. Đã gỡ đúng các mục này, parse/đối chiếu TOML để xác nhận không đổi phần khác; không đổi auth, không logout, không sửa hook người dùng. Lệnh gây phát sinh trust được lưu trong command/log; [biên bản hoàn nguyên](evidence/trust-cleanup.json). Các cấu hình thử nằm ở repo tạm; state Bint và transcript do CLI tự ghi được giữ cục bộ để đối chiếu, không đưa auth hoặc config user đầy đủ vào Git.

## Kiểm thử và artifact

Fixture thực dùng trong test: [hook stdin/stdout](../../../internal/harness/codex/testdata/codex-0.167/hooks.json), [cảnh báo timeout](../../../internal/harness/codex/testdata/codex-0.167/session-end-timeout.json), [citation denied](../../../internal/eval/testdata/codex-0.167/citation-denied.jsonl), [citation approved JSONL](../../../internal/eval/testdata/codex-0.167/citation-approved.jsonl). Test kiểm tra Normalizer/Classify, đủ đường dẫn patch, event lạ/thiếu field và nil cost/turn; CLI/unit test bảo vệ người duyệt và resume. Các test giả chỉ dùng kiểm hợp đồng cục bộ.

Các test đỏ có chủ đích trước sửa đã báo: `SessionEnd timeout=10: Codex chỉ cho phép tối đa 3s`; `citation vẫn cần duyệt dưới policy never`; `nhánh bint thiếu path hook Codex`; `unknown flag: --path`; cập nhật MCP trả `Outcome:updated` thay vì Conflict; executable literal bị shell diễn giải. Đây là bước hồi quy đỏ→xanh, không phải lỗi còn tồn tại trong bộ test cuối.

Kết quả các lệnh yêu cầu, mã thoát đều **0**:

| Lệnh | Đầu ra nguyên văn |
|---|---|
| `gofmt -l .` | [gofmt.txt](evidence/gofmt.txt) — rỗng |
| `go vet ./...` | [vet.txt](evidence/vet.txt) — rỗng |
| `go test ./...` | [go-test.txt](evidence/go-test.txt) |
| `go test -race ./internal/harness/... ./internal/app/... ./internal/eval/...` | [go-test-race.txt](evidence/go-test-race.txt) |
| Kiểm lại package harness sau cập nhật bảng khả năng | [harness-final.txt](evidence/harness-final.txt) |

Review độc lập không phát hiện lỗi critical hoặc thay đổi hành vi Claude mặc định; hai lỗi P2 nêu trên đã tái hiện và sửa. [Kế hoạch/ledger](PLAN.md), [quy ước bằng chứng](evidence/README.md) và [manifest session](evidence/sessions.json) cho phép truy ngược từng kết luận. Không có assertion mới về hiệu quả hoặc nghiệm thu gate.

Tài liệu OpenAI đã đối chiếu: [Hooks](https://learn.chatgpt.com/docs/hooks), [MCP config](https://learn.chatgpt.com/docs/config-file/config-reference), [Build skills](https://learn.chatgpt.com/docs/build-skills), [AGENTS.md](https://learn.chatgpt.com/docs/agent-configuration/agents-md). Tài liệu chỉ giúp đặt giả thuyết; nhãn runtime ở trên dựa vào Codex 0.167 thực tế.
