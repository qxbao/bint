# Tri thức: thêm mục và quét tổng quát

## Sub-features

- `bint knowledge add` ghi mục (agent → `candidate`), kèm `--enforced-by <loại>:<tệp>[#ghi chú]` (lặp lại được).
- `bint knowledge audit --all --json`: tín hiệu `never_used`, `unused`, `review_old`, `warning`, `problem`, `unenforced` (rule/warning chưa có
  `enforced_by` còn tệp).

## How to get to it (user POV)

`bint knowledge add --type rule --evidence <tệp>:<dòng> "<nội dung>"`; rà soát bằng `/b-knowledge-audit` hoặc `bint knowledge audit --all`.

## Driving it with vb.sh

1. `vb.sh run <RUN> k-enforced -- knowledge add --type rule --title "Cộng bằng Add" --evidence add.go:3 --enforced-by "test:add.go#TestAdd" "Mọi
   phép cộng đi qua demo.Add."` → `Đã ghi <ID> [rule/candidate, origin agent]`; tệp mục có khoá `enforced_by`.
2. `vb.sh run <RUN> k-bare -- knowledge add --type warning --title "Không đổi chữ ký Add" --evidence add.go:3 "Chữ ký demo.Add dùng ở nhiều nơi gọi."`
3. `vb.sh run <RUN> audit -- knowledge audit --all --json` → mục ở bước 2 có `"unenforced"` trong `signals`, mục ở bước 1 thì không.
4. `vb.sh run <RUN> k-bad -- knowledge add --type rule --evidence add.go:3 --enforced-by add.go "x"` → lỗi cách dùng nêu dạng `<loại>:<tệp>`.

## Gotchas

- Duyệt mục (`bint knowledge approve`) là thao tác chỉ-dành-cho-người: `verified-unreachable: cần người` trong lượt kiểm của agent.
