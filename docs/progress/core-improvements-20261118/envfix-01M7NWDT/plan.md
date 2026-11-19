# Kế hoạch

## Hướng triển khai
Nguyên nhân (log verify-2.log): base `C:\Users\admin\AppData\Local\bint\state\bint-8d262468937e\tmp\verify-1877255678\` dài ~81
ký tự, %TEMP% thường ~34; đường dẫn git trong test eval (`…\<TestName><n>\001\repo-<12>\eval-work\bases\<sha40>.git.tmp\hooks\applypatch-msg.sample`)
lên ~264 > MAX_PATH 260. TestVerifyUsesPrivateTempDir hỏng vì test in `$TMPDIR` từ sh: runtime MSYS đổi biến TMPDIR sang
`/c/...` cho tiến trình MSYS, nhưng đổi lại dạng Windows khi gọi chương trình gốc (go.exe) — nên lỗi (2) là lỗi của test.

- `internal/workflow/ops.go`: thêm `verifyTempBase(tasksDir string) string` rẽ theo `runtime.GOOS` (không gọi syscall riêng nên
  không cần cặp _unix/_windows [k:01M746NASVKV9XMRHA4QNHJ3R8]): Windows → `filepath.Join(os.TempDir(), "bv")`; còn lại →
  `state.TmpDirFor(tasksDir)`. Mẫu MkdirTemp: Windows `""` (tên số ~10 ký tự), khác giữ `verify-`. `verifyTempDir` dùng hai thứ
  này; đường lỗi giữ nguyên (trả "", TMPDIR kế thừa). `verifyTempEnv` giữ nguyên (đã dạng Windows). Runner, Env.Verify và
  goalfreeze cùng đi qua `verifyTempDir` nên được sửa một chỗ.
- Test: helper `nativePath(s)` đổi `/x/rest` → `X:\rest` trên Windows; TestVerifyUsesPrivateTempDir/…FailureDoesNotBreakVerify
  dùng `verifyTempBase`. Test FailureDoesNotBreakVerify chiếm chỗ base bằng tệp: trên Windows base là `%TEMP%\bv` — với
  testutil.Isolate thì TMP/TEMP có thể là thư mục của test; b-feat kiểm Isolate có đặt TMP/TEMP riêng không, nếu không thì
  dùng t.Setenv("TMP"/"TEMP", t.TempDir()) trong test để không chạm %TEMP% thật.
- Kiểm tra mục tiêu: test mới gọi lại binary test làm tiến trình Go con, đo `os.TempDir()` thật.

## Cỡ kế hoạch
Kết luận: nhỏ
- `internal/workflow/ops.go` — verifyTempBase, mẫu tên ngắn, chú thích
- `internal/workflow/bench_frictions_test.go` — sửa 2 test, thêm TestVerifyChildProcessTempDir + helper
- `docs/workflows.md` — mục kiểm chứng trên Windows

## Bề mặt bị ảnh hưởng
- Tạo thư mục tạm kiểm chứng (Runner sau b-feat, `bint task verify`, goalfreeze) → AC-1.
- Biến môi trường TMPDIR/TMP/TEMP truyền qua sh tới tiến trình con (go test, git) → AC-3.
- Test hồi quy của chính cơ chế (bench_frictions_test.go) → AC-2.
- Bộ test đầy đủ chạy dưới verify trên Windows (eval/evolve/tui/cli) → AC-4.
- Dọn thư mục mồ côi khi bint chết giữa lần chạy → Ngoài phạm vi: hiện cũng không có GC cho `<state>/tmp`; thư mục `bv\…` sót lại trong %TEMP% để Windows/Disk Cleanup dọn.

## Giả định thay người dùng
- Windows dùng `%TEMP%\bv\<số>` thay vì thư mục trong state dir: lý do tách TMPDIR (sandbox Codex để `/tmp/.git`) chỉ có ở
  Linux; đổi lại bằng cách sửa `verifyTempBase`. Linux/macOS không đổi.
- Không bật `core.longpaths` (thay đổi cấu hình git của mọi tiến trình con, sh/công cụ khác vẫn có thể vướng MAX_PATH).

## Giả định và mâu thuẫn (deep)
Yêu cầu gợi ý "đường dẫn dưới thư mục trạng thái"; tiêu chí test hiện tại ràng buộc cha = `state.TmpDirFor`. Rút trong state dir
(`tmp\v<số>`) chỉ bớt ~6 ký tự → ~258, sát ngưỡng; nên đổi base trên Windows và cập nhật test. Tên thư mục test của Go
(`<TestName><số>`) dài là ngoài tầm kiểm soát.

## Lựa chọn và lý do (deep)
1. Rút tên trong state dir (`tmp\v…`): ít đổi, nhưng còn ~2–6 ký tự dư — loại.
2. `%TEMP%\bv\<số>` trên Windows: ~47 ký tự, bớt ~34 so với hiện tại, gần bằng chạy tay — chọn.
3. Bật core.longpaths qua GIT_CONFIG_*: chỉ cứu git, có thể xung đột GIT_CONFIG_COUNT của test — loại.

## Vùng ảnh hưởng (deep)
`Env.verifyTempDir` (gọi từ ops.go Verify ~818, goalfreeze.go ~221); `verifyTempEnv` không đổi. Không đổi API, cấu hình hay dữ liệu lưu.

## Rủi ro và rollback (deep)
- %TEMP% của tiến trình bint lại là TMPDIR riêng lồng nhau (bint chạy trong verify): vẫn ngắn hơn hiện tại; chấp nhận.
- Defender quét %TEMP% — trước đây state dir cũng bị quét, không tệ hơn [k:01M7CJ92ZB2DRYYHGFBQABYM9D].
- Rollback: revert một hàm trong ops.go.

## Chiến lược kiểm thử (deep)
`go test ./internal/workflow -run 'TestVerify'` trên Windows và Linux; `GOOS=windows go vet ./...`; cuối cùng chạy verify.command
đầy đủ qua `bint task verify` trên Windows và đối chiếu danh sách FAIL với verify-2.log. Không kiểm được tự động: ngưỡng MAX_PATH
ở máy có tên người dùng dài hơn.

## Checklist nhất quán (deep)
AC-1←TSK-1,TSK-3; AC-2←TSK-2; AC-3←TSK-3; AC-4←TSK-4. Mọi TSK có "xong khi" đo được; `--allow` gồm internal/workflow/** và docs/** đủ cho 3 tệp.
