Bạn đang chạy **binary cũ** trong PATH. Bản mới tôi vừa build đã nhận lệnh `gc`.

Trong thư mục mã nguồn hiện tại, chạy để cập nhật đúng binary:

```sh
rtk proxy go build -o /home/baodq/bench-bint-v3-20261117/bin/bint ./cmd/bint
rtk proxy bint gc --dry-run
```

Sandbox không cho tôi ghi đè đường dẫn binary đó, nên bước cập nhật này cần bạn chạy trên máy.