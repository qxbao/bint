# CI/CD của Bint

Bint là một CLI Go thuần (`module bint`, Go theo `go.mod`, không CGO khi build), một tệp thực
thi duy nhất `./cmd/bint`. CI/CD dùng GitHub Actions, gồm hai workflow tách biệt:

| Workflow | Tệp | Trạng thái | Trigger |
|---|---|---|---|
| CI | `.github/workflows/ci.yml` | **Đang hoạt động** | PR vào `main`, push lên `main`, chạy tay |
| Release (CD) | `.github/workflows/release.yml` | **Bản nháp, bị khoá** | chỉ chạy tay (`workflow_dispatch`), mọi job bị bỏ qua khi chưa bật |

Không workflow nào publish artifact ra ngoài, tạo tag hay deploy ở trạng thái mặc định khi chưa bật `BINT_CD_ENABLED=true`.

## CI (`ci.yml`)

### Trigger và chống chạy trùng

- `pull_request` nhắm vào `main` và `push` lên `main`. Push lên nhánh khác không chạy CI (nhánh
  đó được kiểm khi mở PR), nên một commit trong PR không bị chạy hai lần.
- `workflow_dispatch`: chạy lại thủ công trên nhánh bất kỳ từ tab Actions.
- `concurrency` theo `ref`: commit mới trong cùng PR huỷ lần chạy cũ; push lên `main` không bị
  huỷ để commit nào trên `main` cũng có kết quả.
- Không dùng `paths-ignore`: nếu đặt các job CI là required check, PR chỉ sửa tài liệu vẫn có
  kết quả, không bị kẹt ở trạng thái "chờ".

### Các job

Ba job chạy song song trên `ubuntu-24.04`, mỗi job có `timeout-minutes`. Quyền của toàn
workflow: `contents: read`; checkout với `persist-credentials: false`. Không dùng secret nào.

**Lint** (10 phút)

| Bước | Lệnh | Bắt lỗi gì |
|---|---|---|
| gofmt | `gofmt -l .` (có tệp nào in ra là fail) | định dạng |
| go.mod/go.sum gọn | `go mod tidy -diff` | phụ thuộc thừa/thiếu |
| go mod verify | `go mod verify` | module trong cache bị sửa |
| go vet | `go vet ./...` và `GOOS=windows go vet ./...` | lỗi phổ biến; bản Windows kiểm cả `proc_windows.go` |
| staticcheck | `staticcheck ./...` cho linux và windows, bản cố định `v0.7.0` | phân tích tĩnh, mã chết, lỗi API |

staticcheck chạy với bộ quy tắc mặc định, không có tệp cấu hình riêng. Không dùng golangci-lint
để tránh thêm một lớp cấu hình; khi cần nhiều linter hơn có thể chuyển sang sau.

**Test (race)** (25 phút)

`CGO_ENABLED=1 go test -race -count=1 -timeout 20m ./...`

- Hạn 20 phút áp dụng cho từng package; job có thêm 5 phút cho khởi tạo và biên dịch.
  Bộ test TUI từng mất hơn 14 phút trên runner nên hạn 15 phút không đủ dư địa.
- Chạy mọi package; package không có tệp test vẫn được biên dịch (go test báo
  `[no test files]`), không package nào bị loại.
- `-race` cần CGO; gcc có sẵn trên runner Ubuntu. Đây là lựa chọn có chủ đích vì Bint có khoá
  ghi, spool và worktree song song.
- `-count=1` vì `actions/setup-go` khôi phục cache build (gồm cache kết quả test); kiểm thử
  đầu-cuối gọi `git` thật nên không nên dùng lại kết quả cũ.
- Bước "Môi trường" in phiên bản Go/git và đặt `user.name`/`user.email` toàn cục cho git
  (Bint yêu cầu git >= 2.31; runner Ubuntu 24.04 có bản mới hơn).
- Bất kỳ test nào fail → `go test` thoát khác 0 → job fail.

**Build (đa nền tảng)** (15 phút)

Biên dịch chéo với `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X bint/internal/cli.Version=…"`
cho `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}` — đúng các nền tảng
README nói là hỗ trợ. Chuỗi phiên bản ở CI là `0.0.0-ci+<12 ký tự SHA>`. Sau đó chạy
`bint --version` và `bint --help` trên bản `linux/amd64`. Binary **không** được upload.

Cờ build trùng với job đóng gói của `release.yml`, nên CI xanh nghĩa là bản release build được.

### Cache

`actions/setup-go` với `go-version-file: go.mod` tự cache module và build cache theo `go.sum`.
Không có cấu hình cache thủ công.

### Pin phiên bản

Mọi action pin theo commit SHA, kèm comment tag:

| Action | Tag | SHA |
|---|---|---|
| `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `actions/upload-artifact` (chỉ release) | v7.0.2 | `cf430e030ddbb5b0abf93d22962f4752f3646cd9` |
| `actions/download-artifact` (chỉ release) | v8.0.2 | `9000827ccba6bdab643e8b6fd33ac0654aef8333` |

Go: theo dòng `go` trong `go.mod` (hiện 1.25.1). staticcheck: biến `STATICCHECK_VERSION` trong
`ci.yml`. Nâng cấp: đổi SHA + comment cùng lúc; lấy SHA bằng
`git ls-remote https://github.com/actions/checkout refs/tags/vX.Y.Z`.

## Chạy CI trên local

Cùng lệnh với CI (cần Go đúng bản trong `go.mod`, git >= 2.31; `-race` cần trình biên dịch C):

```sh
# Lint
gofmt -l .                       # không in gì là đạt
go mod tidy -diff
go mod verify
go vet ./... && GOOS=windows go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
# staticcheck cho windows: cài binary rồi đặt GOOS (go run với GOOS khác sẽ build sai nền tảng)
go install honnef.co/go/tools/cmd/staticcheck@v0.7.0 && GOOS=windows "$(go env GOPATH)/bin/staticcheck" ./...

# Test
CGO_ENABLED=1 go test -race -count=1 -timeout 20m ./...

# Build một nền tảng
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/bint ./cmd/bint
```

Kiểm tra cú pháp workflow (tuỳ chọn, không bắt buộc trong CI):
`go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` (cài thêm `shellcheck` để kiểm cả
các khối `run:`).

> **Chạy `go test` từ phiên agent:** `app.InAgentSession` coi tiến trình là agent khi có biến môi
> trường nhận diện (`BINT_AGENT`, `CLAUDECODE`, `AI_AGENT`, `CODEX_SANDBOX`,
> `CODEX_SANDBOX_NETWORK_DISABLED`, `GEMINI_CLI`) và chặn các thao tác chỉ dành cho người
> (`approve`, `apply`, `reject`). `testutil.Isolate` không tự xoá các biến này, nên kiểm thử đóng
> vai người dùng phải gọi `asHuman(t)` (hoặc đặt rỗng từng biến) để không lỗi `ErrAgentForbidden`
> khi chạy trong phiên agent.
>
> **macOS:** chạy test với `$TMPDIR` mặc định (`/var/folders/…`, qua symlink tới
> `/private/var/…`). So sánh đường dẫn phải nhận diện cùng đối tượng qua symlink,
> kể cả tệp chưa tạo hoặc đã xóa; không cần đổi TMPDIR để làm xanh test.
> Xem [báo cáo tương thích macOS](macos-compatibility.md) để biết kết quả và giới hạn.
> Suite dùng cổng loopback động qua `httptest`; sandbox cấm bind sẽ làm suite lỗi.

## Xem kết quả CI trên GitHub

- PR: mục **Checks** ở cuối PR hiện ba check `CI / Lint`, `CI / Test (race)`,
  `CI / Build (đa nền tảng)`.
- Tab **Actions** → workflow **CI** → chọn lần chạy → từng job/bước có log; lỗi gofmt được
  đánh dấu bằng annotation `::error::`.
- CLI: `gh run list --workflow ci.yml`, `gh run view <id> --log-failed`,
  chạy tay: `gh workflow run ci.yml --ref <nhánh>`.
- Khuyến nghị: Settings → Branches → bảo vệ `main`, bật "Require status checks" với ba check
  trên (cần ít nhất một lần chạy để GitHub biết tên check).

## CD (`release.yml`) — đã chuẩn bị, chưa kích hoạt

### Quy ước phiên bản và tag

- Chưa có tag nào trong repository. Quy ước đề xuất theo comment trong
  `internal/cli/root.go`: SemVer có tiền tố `v` — `vX.Y.Z`, bản thử `vX.Y.Z-rc.N`.
- Phiên bản được gắn khi build qua `-ldflags "-X bint/internal/cli.Version=vX.Y.Z"`
  (mặc định trong mã: `0.1.0-dev`).
- Tag do người bảo trì tạo và push bằng tay; workflow **không** tạo tag.

### Luồng dự kiến

```
người bảo trì: git tag -a v0.1.0 -m "Bint v0.1.0" && git push origin v0.1.0
        │
        ▼  Actions → "Release (draft, disabled)" → Run workflow (tag=v0.1.0, dry_run)
guard   — bỏ qua toàn bộ nếu vars.BINT_CD_ENABLED != 'true' hoặc không chạy từ nhánh main
        — tag đúng dạng vX.Y.Z[-pre], đã tồn tại, nằm trên lịch sử main
build   — checkout đúng tag, không dùng cache; go test -race
        — build 6 nền tảng, đóng gói bint_<ver>_<os>_<arch>.tar.gz / .zip (kèm README.md)
        — SHA256SUMS; kiểm `bint --version` khớp tag
        — upload artifact của lần chạy (giữ 7 ngày, không công khai ra Release)
publish — chỉ khi dry_run = false
        — environment "release" (chờ người duyệt)
        — gh release create <tag> --verify-tag --draft [--prerelease] --generate-notes
        → người bảo trì xem lại draft trên GitHub rồi bấm Publish
```

### Các lớp khoá hiện tại

1. **Không có trigger tự động**: chỉ `workflow_dispatch`; không có `push: tags`, `release`,
   `schedule`.
2. **Biến repository `BINT_CD_ENABLED` chưa đặt (hoặc khác `true`)** → job `guard` bị bỏ qua,
   `build` và `publish` phụ thuộc vào nó nên cũng bị bỏ qua. Bấm "Run workflow" lúc này chỉ tạo
   một lần chạy với mọi job ở trạng thái *skipped*.
3. `dry_run` mặc định `true` → kể cả khi đã bật, mặc định chỉ build.
4. Job `publish` dùng environment `release`; environment chưa tạo thì GitHub tự tạo khi chạy lần
   đầu mà không có người duyệt, nên **phải tạo và cấu hình người duyệt trước khi bật** (bước 2
   dưới đây).
5. Release được tạo ở trạng thái **draft**, không công khai cho tới khi có người bấm Publish.
6. Quyền: mặc định `permissions: {}`; chỉ `publish` có `contents: write`. Chỉ dùng
   `GITHUB_TOKEN` tự cấp, không có secret.

### Kích hoạt CD

1. Thêm tệp `LICENSE` (repository chưa có giấy phép; không nên phát hành binary khi chưa rõ
   giấy phép).
2. Settings → Environments → **New environment** `release`: thêm *Required reviewers*, giới hạn
   *Deployment branches and tags* cho nhánh `main` (lần chạy `workflow_dispatch` mang ref của
   nhánh được chọn, không phải ref của tag) và tag `v*` (nếu sau này thêm trigger push tag).
3. Settings → Actions → General → *Workflow permissions*: để mặc định *Read*; workflow tự xin
   `contents: write` ở job `publish`.
4. (Khuyến nghị) Settings → Rules → tag ruleset cho `v*`: chỉ người bảo trì được tạo/xoá tag.
5. Settings → Secrets and variables → Actions → tab **Variables** → thêm
   `BINT_CD_ENABLED = true`.
6. Đổi `name:` trong `release.yml` thành `Release`, sửa comment đầu tệp.
7. Thử: tạo tag `v0.1.0-rc.1`, chạy workflow với `dry_run = true`, tải artifact kiểm tra; sau đó
   chạy lại với `dry_run = false`, duyệt environment, kiểm tra draft release rồi mới Publish.
8. (Tuỳ chọn, sau khi đã ổn) thêm trigger tự động khi push tag:

   ```yaml
   on:
     push:
       tags: ["v*.*.*"]
   ```

   và thay `inputs.tag` bằng `github.ref_name`, `inputs.dry_run` bằng `false` cho trường hợp
   `push`. Environment `release` vẫn giữ bước duyệt tay.

Tắt lại CD: xoá biến `BINT_CD_ENABLED` (hoặc đặt khác `true`), hoặc Actions → workflow → **Disable
workflow**.

### Secrets và quyền cần khi triển khai thực tế

| Mục | Hiện tại | Khi kích hoạt |
|---|---|---|
| `GITHUB_TOKEN` | không dùng quyền ghi | `contents: write` chỉ ở job `publish` (đã khai báo) |
| Biến `BINT_CD_ENABLED` | chưa đặt | `true` |
| Environment `release` | chưa tạo | có required reviewers, chỉ nhánh `main` và tag `v*` |
| Secret khác | không có | không cần cho GitHub Release. Chỉ cần nếu mở rộng: ký/notarize macOS (chứng chỉ Apple), ký Windows, Homebrew tap (token có quyền ghi repo tap) |

Mở rộng có thể cân nhắc sau (chưa làm để tránh over-engineering): attestation nguồn gốc build
(`actions/attest-build-provenance`, cần `id-token: write`, `attestations: write`), Homebrew tap,
GoReleaser nếu số kênh phát hành tăng.
