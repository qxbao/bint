# Codex evolution — điều tra vòng sửa 5

> Đây là ghi chép vòng 5. [Vòng 6](codex-restricted-adapter-round6.md) đã tìm và kiểm chứng
> metadata model mở công cụ, bổ sung adapter có probe và model live; giới hạn model cấu hình được ghi riêng.

Phát hiện `.18` vẫn mở tại thời điểm vòng 5. Không kết luận Codex không thể hỗ trợ no-tools chỉ từ `exec --help`.
Chưa có chứng cứ đủ để mở các factory evolver/reflector/harness proposer cho dữ liệu eval.

Đã đọc [config reference](https://developers.openai.com/codex/config-reference/),
[config schema](https://developers.openai.com/codex/config-schema.json) và
[app-server](https://developers.openai.com/codex/app-server/) chính thức ngày 12/11/2026.
Schema có các cờ tắt nhóm công cụ (`features.shell_tool`, app/plugin, web search…) và
`tools.update_plan`; cấu hình ToolRegistry không cung cấp allowlist toàn cục.
Tắt shell riêng không đủ chứng minh tắt các bề mặt công cụ còn lại.

## Phép thử thực tế và kết quả

Các phép thử dưới đây dùng binary Codex thật `0.167.2`, fixture và `CODEX_HOME` mới dưới `/tmp`;
không sao chép auth/instructions/plugins/hooks, không trust/approve hoặc đổi cấu hình máy.
Model `gpt-6.1-sol` khớp model reflector hiện tại. Không chạy `bint run` lồng và không gọi eval/evolve.

1. Chạy `codex app-server generate-json-schema --experimental --out <tmp>/schema`.
   [Trích schema kèm SHA-256](evidence/codex-appserver-schema-round5.json) thuộc binary này:
   `ThreadStartParams.environments=[]` tắt environment access mặc định; `TurnStartParams` có thể
   ghi đè environments. `dynamicTools` là khai báo công cụ động, không phải cổng tắt công cụ native.
2. Tạo [candidate config](evidence/codex-no-tools-candidate-round5.toml), tắt shell/unified exec,
   view image, app/plugin/hook, multi-agent, image generation, browser/computer use, goal/sleep/skill
   search và update_plan; web search disabled. `codex app-server --strict-config
   --print-startup-config` nhận cấu hình, [exit 0](evidence/codex-candidate-strict-config-round5.txt).
   Nhận cấu hình không chứng minh công cụ bị khóa khi gọi model.
3. Mở `codex app-server --stdio`, handshake `initialize` với `experimentalApi=true`, sau đó
   `thread/start` với `ephemeral=true`, `environments=[]`, `approvalPolicy=never`, `cwd=<fixture>`.
   [Phản hồi đã bỏ metadata máy/session](evidence/codex-appserver-empty-environments-round5.json)
   xác nhận environments, runtime roots và instruction sources rỗng, sandbox readOnly. Đây là
   bằng chứng runtime khởi tạo thread, chưa phải bằng chứng model hay tập công cụ no-tools.
4. Thử endpoint Responses nội bộ để bắt request từ CLI thật và kiểm tra mọi tool surface trước
   khi gửi dữ liệu. Python `HTTPServer(('127.0.0.1', 0), ...)` thất bại ngay tại `socket()`:
   `PermissionError: [Errno 1] Operation not permitted`. Không có request nào tới endpoint này.
5. Thử `codex debug prompt-input` trên profile cô lập: đầu ra có
   [định danh công cụ của host](evidence/codex-debug-tool-names-round5.json) dù đã loại biến session.
   Diagnostic không chỉ rõ tập công cụ của thread tạo ở bước 3; không dùng nó để chứng nhận no-tools.
6. Thử `codex --no-daemon exec --json --sandbox read-only --ephemeral --skip-git-repo-check
   --cd <fixture> -` với prompt chỉ yêu cầu OK và candidate config. [Log vòng 5](evidence/codex-live-candidate-round5.txt)
   ghi kết nối bị chặn trước model response. Đây là live attempt chưa hoàn tất; không phải agent giả
   hay bằng chứng model đã chạy. Profile không có auth người dùng.

## Hướng tiếp tục và cổng còn thiếu

Adapter app-server với `environments=[]` cùng profile không integrations là hướng khả dĩ cho
reflector/proposer. Cần chứng minh tập tools thực tế trên model request (gồm direct, deferred,
Code Mode, MCP và hosted tools), rồi thử canary đọc/ghi ngoài fixture và yêu cầu gọi công cụ.
Candidate config phải được kiểm tra với transport/model đang dùng; không dựa vào một lần model
tự nói rằng nó không có công cụ. Evolver cần thêm allowlist đọc tương đương hoặc cơ chế cung cấp
đầu vào đã giới hạn mà vẫn đáp ứng nghiệp vụ.

Sandbox hiện tại cấm cả endpoint nội bộ lẫn kết nối model, nên chưa thực hiện được phép chứng minh
đó. Giữ fail-closed, không bật evolution máy. AC-2 chưa đạt; lỗi tương thích `.18` chưa sửa xong.
