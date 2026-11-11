# Nghiệm thu P6 — Tiến hoá tri thức L1

> **Bản ghi lịch sử:** Biên bản dưới đây phản ánh trạng thái tại commit được kiểm tra. Tổng hợp hiện trạng và các phát hiện đã đóng / còn mở tại `HEAD` xem ở [docs/status.md](status.md) và [docs/evals/README.md](evals/README.md).

Ngày: 2026-11-10. Commit kiểm tra: **`dff6570896298b7b921fcf124eec8cd26b84530f`**.

Chuẩn đối chiếu: P6 tại mục 16, điều kiện dữ liệu mục 10.6, vòng Propose–Probe–Compose, kiểm chứng độc lập và các ranh giới mục 15 của `bint-plan.md`. Source hiện có P7; biên bản này chỉ nghiệm thu P6 và các thành phần dùng chung liên quan.

## Kết luận

**Chưa đủ điều kiện nghiệm thu P6.** Luồng chức năng đã được triển khai và các test chuẩn qua, nhưng kiểm tra bổ sung tái hiện năm vấn đề, trong đó ba vấn đề mức cao. Ngoài lỗi mã nguồn, chưa có bằng chứng được cung cấp trong các artifact của commit này rằng một đề xuất L1 thật cải thiện kết quả trên tập kiểm tra độc lập.

| Hạng mục P6 | Đánh giá |
|---|---|
| Kiểm tra điều kiện tiên quyết về dữ liệu | Có; scan kiểm nhãn, validation/test, cỡ mẫu và ngân sách, công bố trạng thái chưa kiểm chứng |
| Phát hiện nhóm tác vụ/khám phá lặp lại | Có ở mức MVP theo module và tệp được chạm; chưa có bằng chứng chất lượng nhóm trên dữ liệu thực tế |
| Agent evolver/proposal schema có phiên bản gốc | Có schema và base hash/version; bảo đảm chỉ đọc và provenance còn lỗi P6-02/P6-04 |
| Đánh giá loại bỏ/đo tác động | Có base/candidate, kiểm phạm vi và bộ lọc nguồn gốc; tests dùng agent giả, chưa chứng minh tác động thật |
| Thêm/sửa/gộp/loại bỏ tri thức | Có; merge có thể bỏ nội dung khác biệt khỏi tập active (P6-05) |
| Phê duyệt/quay lui/phát hiện base thay đổi | Luồng thông thường qua; khoá và xử lý rollback lỗi chưa đạt (P6-01/P6-03) |
| Tiêu chí nghiệm thu: lợi ích trên tập kiểm tra độc lập | **Chưa chứng minh**; không lấy kết quả fixture hoặc apply --unverified làm bằng chứng lợi ích |

## Kiểm tra đã thực hiện

Môi trường: Linux amd64, Go 1.25.1, người dùng không phải root. Binary probe: `/tmp/bint-acceptance-p6-dff6570`, build từ source nêu trên.

| Lệnh/kịch bản | Kết quả |
|---|---|
| `go test -count=1 ./...` | PASS, exit 0 |
| `go test -race -count=1 ./internal/evolve ./internal/cli ./internal/workflow` | PASS, exit 0; phạm vi race là ba package này |
| `go vet ./...` | PASS, exit 0 |
| `CGO_ENABLED=0 go build -o /tmp/bint-acceptance-p6-dff6570 ./cmd/bint` | PASS, exit 0 |
| CLI scan → propose --no-agent → apply bị chặn khi chưa có bằng chứng | PASS |
| CLI apply --unverified → history verified=false → rollback | PASS; khôi phục chính xác cả hai tệp |
| Năm probe bổ sung qua Go overlay | **FAIL năm assertion**, xác nhận các vấn đề dưới đây |
| Probe khoá chạy thêm `-count=2` | FAIL cả hai lần; có 1854/1884 và 1758/1806 lượt lấy khoá bị chồng với chủ khác |

Các test hiện có đã chạy gồm `TestScanProposeApplyRollback`, `TestAgentProposalAndRisk`, `TestValidateChangesBoundaries`, `TestComputeVerdict`, `TestEvaluateCreateProposal`, `TestDerivedFromWindowAndKind`, `TestEvolveEndToEnd` và các test deferred. Source P7 cũng được thực thi trong suite toàn bộ, nhưng việc đó không tự nghiệm thu P7.

Artifact: [checks.json](evals/acceptance-p6-dff6570/checks.json), [CLI lifecycle](evals/acceptance-p6-dff6570/cli-lifecycle.json), [đầu ra probe](evals/acceptance-p6-dff6570/probe-results.log).

## P6-01 — Cao: khoá tiến hoá không bảo đảm độc quyền

Vị trí: `internal/evolve/apply.go:32`, tạo/ghi khoá tại dòng 39–42, thu hồi tại dòng 48–57 và Release tại dòng 63.

AcquireLock tạo tệp evolve.lock bằng O_EXCL rồi mới ghi PID. Một bên khác có thể đọc tệp còn rỗng, xem như khoá bỏ lại và xoá nó trước khi chủ đầu tiên ghi xong. Chủ khác tiếp tục lấy khoá; Release chỉ xoá theo tên đường dẫn nên chủ cũ có thể gỡ khoá của chủ mới.

Probe gọi hàm thật với 40 goroutine ×60 lần thử, giữ vùng làm việc 200 µs, dùng bộ đếm atomic để phát hiện nhiều chủ cùng hoạt động. Một lần ghi nhận 1639/1853 lượt lấy khoá chồng lấn; hai lần chạy bổ sung ghi nhận 1854/1884 và 1758/1806. Các lần chạy fixture toàn bộ cũng tái hiện lỗi.

Ảnh hưởng: apply, rollback và các thao tác dùng cùng evolve.lock không được bảo đảm tuần tự. Kiểm tra base hash trước khi ghi không đủ khi hai thao tác cùng qua kiểm tra rồi cùng sửa tri thức/lịch sử.

Đây là **khoá tiến hoá riêng**, không phải AcquireWriteLock trong workflow đã được nghiệm thu ở các biên bản trước. Bộ race qua không chứng minh giao thức khoá trên filesystem đúng.

Điều kiện đóng: công bố/lấy/thu hồi khoá theo giao thức độc quyền nhất quán, kiểm quyền sở hữu khi trả khoá; kiểm thử cả goroutine, nhiều tiến trình và thu hồi khoá chết. Không sửa bằng cách chỉ tăng thời gian chờ trong test.

## P6-02 — Cao: evolver sửa tệp đang dirty nhưng kết quả vẫn được chấp nhận

Vị trí: `internal/evolve/generate.go:387` và `:394`.

Bint so sánh chuỗi `git status --porcelain --untracked-files=all` trước/sau agent, không so sánh nội dung. Tệp đã có trạng thái M vẫn là M sau khi bị sửa thêm; chuỗi status không đổi.

Probe chuẩn bị một source file có chỉnh sửa của chủ dự án trước khi gọi Propose. Agent giả thêm một dòng chú thích vào chính tệp đó rồi trả JSON proposal hợp lệ. Kết quả: nội dung tệp đã đổi nhưng proposal vẫn được tạo, trường skipped rỗng; không có lỗi vi phạm chỉ đọc.

Phát hiện ở lớp hậu kiểm Bint khi harness/command có khả năng ghi, không phải tuyên bố vượt qua một OS sandbox chỉ đọc thật sự. Thuộc tính ReadOnly của request và lời nhắc agent không thay thế được kiểm tra nội dung như Bint đã công bố.

Điều kiện đóng: kiểm tra fingerprint/nội dung/trạng thái đầy đủ, gồm tệp đã dirty và untracked; hoặc chạy agent trong môi trường chỉ đọc được xác minh. Lỗi quan sát Git cũng phải được xử lý thay vì bỏ qua rồi chấp nhận kết quả.

## P6-03 — Cao: rollback lỗi giữa chừng để lại trạng thái tệp một phần

Vị trí: `internal/evolve/apply.go:448`–460; phần undo chỉ được định nghĩa sau vòng ghi tại dòng 471.

Rollback kiểm tra xung đột, rồi khôi phục từng tệp. Nếu ghi/xoá một tệp lỗi, hàm trả ngay mà không phục hồi các tệp đã khôi phục ở những vòng trước. Cơ chế undo có trong source chỉ áp dụng cho lỗi ghi kiểm toán sau khi đã đi qua vòng này.

Probe tạo/áp dụng một merge thật trong fixture, có hai tệp trong bản kiểm toán. Giữ nội dung tệp thứ hai đúng trạng thái sau apply nhưng đặt quyền 0400. Rollback đọc kiểm tra được cả hai tệp; khôi phục tệp đầu thành công, ghi tệp thứ hai bị EACCES.

**Kết quả:** hàm trả lỗi, tệp đầu đã đổi, tệp sau còn trạng thái sau apply; proposal trong DB vẫn `applied`. Đây không phải kết quả rollback thành công và không phải khôi phục nguyên trạng khi thất bại. Quyền tệp được trả lại trong cleanup của fixture.

Điều kiện đóng: lưu/khôi phục trạng thái trước khi rollback cho mọi nhánh lỗi filesystem, kể cả tệp đang ghi lỗi; báo rõ khi quá trình bù trừ cũng thất bại. Thêm test lỗi ở tệp thứ n và lỗi DB, không chỉ rollback thành công/xung đột nội dung.

## P6-04 — Trung bình: update bỏ đầu vào mới từ bên thứ ba khỏi provenance

Vị trí: `internal/evolve/generate.go:300`–335, đặc biệt nhánh sao chép base tại 308–313 và vòng kiểm đầu vào tại 332.

DraftItem dùng Draft.Inputs khi tạo mục mới. Với update, nó giữ Origin của base mà không gộp Inputs mới; cờ ThirdParty sau đó chỉ được tính từ Inputs cũ và đường dẫn evidence. Đầu vào bên ngoài được ghi rõ trong draft có thể bị mất khi áp dụng.

Probe đi qua đường thực tế Propose bằng agent giả → Apply trong vai người duyệt của fixture, có `--unverified`/xác nhận rủi ro ở tầng API. Draft sửa một rule stale nêu input `https://example.invalid/external-guidance`, evidence vẫn là mã local hợp lệ. URL chỉ là dữ liệu fixture; không truy cập mạng.

**Kết quả:** proposal giữ input đó, nhưng mục được áp dụng thành verified có `origin.inputs=[]`, `third_party=false`; Context Engine trả nó với `label=rule`. Có người áp dụng trong fixture; lỗi là mất provenance/cờ bên thứ ba, không phải agent tự nâng rule mà không có người.

Điều kiện đóng: giữ/gộp đầu vào mới và chuỗi nguồn gốc khi update, tính ThirdParty trên toàn bộ đầu vào đã dùng. Kiểm cả create/update và metadata sau khi commit/chia sẻ, theo mục 15.5.

## P6-05 — Trung bình: merge không bảo toàn nội dung khác biệt trong tri thức active

Vị trí: `internal/evolve/scan.go:473`–483 và `internal/evolve/generate.go:230`–247.

Hai mục được coi là trùng theo type/scope và độ giống tiêu đề ≥0,6. Merge lấy body của mục giữ lại, chỉ hợp phạm vi/thẻ/bằng chứng; mục còn lại bị deprecated. Nội dung khác biệt của body không được hợp hoặc kiểm tra.

Probe tạo hai fact cùng tiêu đề/phạm vi nhưng body khác nhau: một fact yêu cầu truyền cancellation context khi Open, fact kia yêu cầu dừng worker trước Close. Đề xuất merge được tạo; sau apply trong vai người duyệt của fixture với --unverified, tập active chỉ còn body thứ nhất.

Mục thứ hai vẫn tồn tại ở deprecated và có bản kiểm toán để quay lui; không kết luận mất vĩnh viễn bytes lịch sử. Tuy nhiên, hành động mang tên merge đã làm mất một thông tin khác biệt khỏi tập tri thức được truy xuất bình thường.

Điều kiện đóng: chỉ merge xác định khi nội dung thực sự tương đương, hoặc bảo toàn/gộp nội dung và xử lý mâu thuẫn rõ ràng. Tiêu đề giống nhau không đủ chứng minh body trùng. Thêm regression test cho hai thông tin bổ sung và hai thông tin mâu thuẫn.

## Những phần đã có bằng chứng hoạt động

- Scan lọc `unlabeled`, gộp lượt con về lượt gốc, nhóm theo module, tính bằng chứng mới và tránh đề xuất đang chờ cùng cơ hội.
- Kiểm điều kiện label/corpus/ước tính chi phí; CLI fixture thiếu dữ liệu có quantitative=false.
- Proposal có schema, risk do chương trình tính, kế hoạch đánh giá chốt khi tạo và base hash/version; source cũ đổi thì apply từ chối.
- Agent draft chỉ có các trường nội dung; candidate không hợp lệ bị loại. Mục mới do agent tạo giữ candidate sau apply.
- Cặp base/candidate hỗ trợ đánh giá loại bỏ; lọc evidence/lời giải và tác vụ liên quan supporting run; kiểm tái sử dụng test giữa đề xuất cùng đối tượng; có phân biệt retrospective/prospective.
- Test fixture cho Finalize/VerifiedBy và apply supported trên test qua; đây là hợp đồng với agent giả, không phải kết quả cải thiện thực tế.
- Người/agent, --unverified và xác nhận rủi ro được kiểm tra trong test. CLI fixture chặn apply mặc định khi không có đánh giá, ghi verified=false khi chấp nhận chưa kiểm chứng, rồi rollback thành công khôi phục đúng cả hai tệp.
- History có before/after, mô hình/harness, chi phí riêng, cảnh báo nhãn thay đổi và kiểm tra hồi quy sau áp dụng. Deferred/manual được kiểm thử cục bộ; không tự coi là tiến hoá tự động P8.

## Bằng chứng hiệu quả độc lập còn thiếu

Tiêu chí P6 yêu cầu đề xuất được chấp nhận có lợi trên tập kiểm tra độc lập. Các test positive của source dùng fixture và agent giả. README cũng ghi chưa có số liệu thật cho tiêu chí này.

Pilot Bflow đã công bố trước đây là phép đo các nhánh none/minimal/bint trên hai tác vụ validation, không phải đánh giá một đề xuất L1 trên tập test độc lập. Không dùng pilot đó, một lần apply --unverified hoặc lịch sử verified=false của fixture để đóng tiêu chí P6.

Trước nghiệm thu hiệu quả: cần corpus đã duyệt đủ cỡ mẫu, supporting runs có nhãn đáng tin, đề xuất/kế hoạch đóng băng, loại tác vụ sinh ra chính đề xuất và các test đã dùng để điều chỉnh cùng đối tượng; lưu report độc lập, harness/model, chi phí/ngân sách và quyết định áp dụng. Kết quả hồi cứu phải giữ cảnh báo rò rỉ còn lại; muốn kết luận mạnh hơn cần đối chiếu tiến cứu phù hợp.

Đợt này không khởi thêm benchmark mô hình trả phí hoặc dùng dữ liệu sản xuất. Quyết định nghiệm thu vẫn là chưa đạt do lỗi đã tái hiện và thiếu bằng chứng hiệu quả độc lập.

## Tái hiện và điều kiện đóng

Các fixture được lưu trong repository, không chỉ ở `/tmp`. Chạy trên Linux bằng người dùng không phải root:

```sh
python3 docs/evals/acceptance-p6-dff6570/run-probes.py
```

Script thêm test qua Go overlay tạm, giữ nguyên source package. Trên commit đang kiểm tra, exit 1 là năm assertion thất bại đã mô tả; sau bản sửa đúng, chúng phải qua. Có thể chọn riêng bằng `--run '^TestAcceptanceP6RollbackFailurePreservesAppliedState$'`.

Đóng năm vấn đề, giữ suite thường/race phù hợp qua, rồi bổ sung phép đo lợi ích độc lập mới có thể ký nghiệm thu P6. Biên bản này không tự chốt P7 hoặc các lỗi P0–P5 ở phiên bản khác.
