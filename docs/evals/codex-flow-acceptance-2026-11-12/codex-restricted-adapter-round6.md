# Codex evolution — bản sửa vòng 6

Factory của evolver, reflector và harness proposer đã chuyển sang
`eval.CodexRestrictedAgent`. Đây là đường thực thi thật thay cho việc từ chối mọi
Codex ở vòng 5. Không đổi harness/model hoặc bật evolution trong cấu hình máy.
Chưa kết luận toàn bộ flow tương thích hoàn toàn: review vòng 6 đã chạy thật ba
factory theo model cấu hình mới ([log](evidence/factory-live-review-round6.txt)),
nhưng workflow/hook trusted và chu kỳ evolution đầy đủ chưa chạy lại. Tên cũ
`gpt-6.1-astra` bị từ chối trước inference là lịch sử; người dùng đã chọn
`gpt-6-astra`. Review độc lập cuối do Bint Core thực hiện.

## Cơ chế đã kiểm

Đối chiếu [config reference](https://developers.openai.com/codex/config-reference/),
[config schema](https://developers.openai.com/codex/config-schema.json) và
[app-server](https://developers.openai.com/codex/app-server/) chính thức, cùng schema
và `codex debug models` của binary `0.167.2`.

`features.code_mode=false`, `features.multi_agent=false` và `environments=[]`
**chưa đủ**. Metadata model `tool_mode=code_mode_only` và
`multi_agent_version=v2` vẫn mở `functions.exec`, `functions.wait` và namespace
`collaboration`. Bỏ hai metadata này vẫn còn `apply_patch` do
`apply_patch_tool_type=freeform`. [Ba request từ CLI thật](evidence/codex-tool-surface-round6.json)
cho thấy từng bề mặt còn lại; đây là endpoint Responses fixture, không phải model live.
Trường `tools` cấp cao nhất trống không chứng minh `input.additional_tools` trống.

Adapter thực hiện:

1. Tạo cwd rỗng, `CODEX_HOME` và `XDG_STATE_HOME` tạm. Chỉ sao chép auth mode 0600
   cùng cấu hình inference/provider cần thiết theo helper eval; tắt skill người
   dùng/admin/bundled, không sao chép instructions/plugins/hooks. Xoá profile bằng
   defer trên mọi nhánh, không gán lại `HOME` hoặc thay auth gốc.
2. Lấy catalog từ chính CLI, chọn **đúng slug model được yêu cầu**. Bỏ metadata
   Code Mode, multi-agent, apply_patch và experimental tools; tắt các feature/tool
   liên quan trong profile tạm. Không áp `extra_args` có thể mở lại công cụ.
   Model không có trong catalog bị từ chối trước inference; không đổi sang model khác.
3. Mở app-server mới, `--strict-config`, thread ephemeral, `environments=[]`,
   runtime roots rỗng, sandbox read-only và approval never. Xác nhận phản hồi giữ
   đúng model, không có instruction source từ environment. `turn/start` tiếp tục
   truyền environments rỗng.
4. **Trước mỗi lần gửi dữ liệu thật**, chạy CLI với catalog/features/model và tên
   provider như trên vào endpoint loopback tạm, chỉ gửi prompt vô hại. Đọc request
   Responses thật, kiểm cả `tools`, namespace và `input.additional_tools`; từ chối
   native/hosted/deferred tools, reader thiếu/thừa/trùng, input type hoặc trường
   cấp cao mới chưa kiểm. Probe thất bại thì không khởi chạy inference thật.
5. Reflector/proposer không đăng ký công cụ nào. Evolver chỉ có dynamic tools
   `Read`, `Grep`, `Glob`, do Bint xử lý qua JSON-RPC; không có shell. Các handler
   dùng `os.Root`, chỉ đọc tệp thường trong repository, chặn đường dẫn tuyệt đối,
   traversal/symlink và `.git`, `.bint/evals`, `.bint/runtime` (cả biến thể hoa/thường),
   giới hạn kích thước/kết quả. Request server ngoài allowlist làm huỷ cả tiến trình.

Probe không phải sandbox thay thế. Bề mặt công cụ được đóng trước khi gửi prompt
nhạy; cwd/profile rỗng ngăn nạp tài liệu test qua discovery. Phòng vệ chống rò rỉ
`Cases`/`RepoDir`, `CheckLesson`, `EvalCases`, ngân sách eval/evolve evaluate và cổng
chỉ người duyệt được giữ nguyên. Không đổi hook fail-open. [k:01M746NPTW1B304MRQQPBCAFAM]
[k:01M74D57EZ2HZSQMTB9YP56F3D]

## Bằng chứng

- `TestCodexEvolutionFactories`: cả ba factory nhận Codex, giữ binary/model role,
  evolver có reader, reflector/proposer không có reader. CLI deterministic propose
  với reflector Codex không còn phát cảnh báo từ chối harness; không gọi model ở
  ca deterministic. `TestRunWorkflowAgentCommandIgnoresUnusedHarness` sửa `.24`:
  command tùy chọn không bị harness không sử dụng chặn và không thay snapshot.
- `TestRestrictedCodexToolSurface`, `TestRestrictedCodexCatalogPreservesModel`,
  `TestCodexReadersConfineFilesAndBoundOutput` kiểm metadata, các tool surface,
  model không được thay thế, đọc ngoài root/symlink/eval/binary/giới hạn output.
  `TestRestrictedRPCProfileCleanupAndRejectsUnknownTools` kiểm thành công,
  unexpected tool, unsafe probe và timeout đều dọn profile. Đây là test fixture,
  không dùng làm bằng chứng model live.
- [Model live `gpt-6.1-sol`](evidence/codex-restricted-live-round6.txt): hai chế độ
  trên repository/profile cô lập, không công cụ báo đọc/ghi unavailable; chế độ
  reader lấy đúng `BINT_SOURCE_CANARY_7193`, yêu cầu đọc đường dẫn ngoài root nhận
  `path outside readable source scope`; secret ngoài fixture không bị đọc ra hoặc
  ghi đè. Log còn ghi proof allowlist từ CLI thật và profile_removed sau cleanup.
- [Model evolver theo cấu hình máy](evidence/codex-configured-evolver-round6.txt):
  `gpt-6.1-astra` bị từ chối trước inference vì thiếu trong catalog. GET `/models`
  có auth tới provider hiện tại cũng không liệt kê tên này;
  [evidence chỉ giữ danh sách ID](evidence/provider-model-ids-round6.json), không
  xuất credential/URL provider. Chưa thử inference trực tiếp để kết luận provider có chấp nhận alias ngoài danh sách hay không; adapter hiện chặn tên thiếu metadata trước inference, không tự thay model.
- Chưa chạy lại toàn bộ b-req → b-feat → b-review/finalize hay deny → human approve
  → resume với model/hook trusted trên thay đổi mới. Các hàng tương ứng trong ma
  trận giữ nhãn test/lịch sử. Không `bint run` lồng, không trust/approve, không gọi
  eval/evolve evaluate hoặc tự bật evolution máy.

Lệnh live adapter tái lập (model này không thay cho bằng chứng factory theo cấu hình):

```sh
BINT_CODEX_RESTRICTED_LIVE=1 BINT_CODEX_RESTRICTED_MODEL=gpt-6.1-sol \
  GOCACHE=/tmp/bint-gocache go test ./internal/eval \
  -run '^TestCodexRestrictedLive$' -v -count=1
```

Phép thử dùng auth đã có qua bản sao cô lập và xoá bản sao sau chạy. Không thay
model cấu hình của role bằng biến của phép thử. Windows được kiểm compile/vet và
staticcheck; chưa có runtime Codex Windows/macOS. Binary/model catalog/provider
khác phải qua probe; khả năng app-server experimental không được suy thành tương
thích vô điều kiện.
