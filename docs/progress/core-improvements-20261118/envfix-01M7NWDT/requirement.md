# Yêu cầu
## Mục tiêu và phạm vi
`bint task verify` (và kiểm chứng của Runner, goalfreeze) trên Windows chạy `verify.command` với TMPDIR riêng quá dài
(`<state>/tmp/verify-NNNNNNNNN`, ~81 ký tự so với ~34 của %TEMP%), làm git trong test vượt MAX_PATH ("Filename too long")
và 8 test eval/evolve/tui hỏng dù không liên quan; còn TestVerifyUsesPrivateTempDir hỏng vì sh.exe (MSYS) đổi TMPDIR sang
`/c/Users/...` khi in ra. Sửa để kiểm chứng trên Windows không còn các lỗi này, giữ nguyên hành vi Linux/macOS.

## Yêu cầu chức năng
1. Trên Windows, thư mục tạm của mỗi lần kiểm chứng nằm dưới `os.TempDir()/bv` với tên ngắn (MkdirTemp mẫu ngắn), vẫn riêng
   cho từng lần và được dọn sau khi chạy. Linux/macOS giữ `state.TmpDirFor(...)/verify-*` (lý do /tmp/.git của sandbox Codex).
2. TMPDIR/TMP/TEMP truyền dạng Windows (như hiện tại); tiến trình Go con của sh nhận `os.TempDir()` đúng thư mục riêng.
3. Test TestVerifyUsesPrivateTempDir chấp nhận dạng MSYS do sh in ra (đổi về dạng Windows trước khi so).

## Tiêu chí chấp nhận
- [ ] AC-1: Trên Windows thư mục tạm của lệnh kiểm chứng nằm dưới os.TempDir()/bv với tên ngắn (độ dài ≤ len(os.TempDir())+16 ký tự); trên Linux/macOS vẫn là thư mục con verify-* của state.TmpDirFor
- [ ] AC-2: TestVerifyUsesPrivateTempDir xanh trên Windows: so TMPDIR sau khi đổi dạng MSYS /c/... về dạng Windows, thư mục được dọn sau khi chạy
- [ ] AC-3: kiểm tra mục tiêu TestVerifyChildProcessTempDir xanh
- [ ] AC-4: bint task verify với go test -timeout 30m ./... trên Windows không còn lỗi Filename too long hay lượt lỗi hạ tầng ở 8 test eval/evolve/tui đã nêu

## Kiểm tra mục tiêu
- Chuẩn bị: harness workflow (newHarness) với `eval.test_command` gọi lại chính binary test (`os.Args[0]`, đường dẫn qua
  `filepath.ToSlash`) bằng `-test.run=^TestHelperPrintTempDir$` và biến môi trường chỉ tệp đầu ra; helper ghi `os.TempDir()`
  của tiến trình Go con (bỏ qua khi biến không đặt).
- Hành động: chạy kiểm chứng qua Runner (finishFeat) và qua `Env.Verify` (bint task verify).
- Quét/đo: đọc tệp đầu ra: giá trị `os.TempDir()` mà tiến trình Go con thấy; kiểm thư mục đã bị dọn.
- Điều kiện đạt: đường dẫn tuyệt đối dạng gốc OS (Windows: có ổ đĩa, không bắt đầu `/c/`), cha là base của nền tảng
  (Windows `os.TempDir()/bv`), độ dài trên Windows ≤ len(os.TempDir())+16; mỗi lần một thư mục mới; đã dọn.
- Tên test và tệp dự kiến: `TestVerifyChildProcessTempDir` trong `internal/workflow/bench_frictions_test.go` (chạy trong
  `go test ./...` của verify.command).

## Ngoài phạm vi
- TestMilestoneTaskToDone (tui/app, "file being used by another process") trong cùng log: lỗi khoá tệp, không do đường dẫn
  tạm; người dùng có thể vẫn thấy test này đỏ chập chờn — cần phiếu riêng nếu lặp lại.
- Bật `core.longpaths` cho git, hay rút ngắn đường dẫn trong chính các test eval (`eval-work/bases/<sha40>.git.tmp`).
- Dọn thư mục `bv` mồ côi khi bint bị giết giữa chừng (vốn cũng không có GC cho `<state>/tmp`).

## Câu hỏi mở
Không có.
