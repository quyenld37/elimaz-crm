# Bộ kiểm từ ngoài (external-probe)

Kế hoạch M0 bản 5, Task 19; spec 04 mục 8 ("Kiểm từ ngoài"), ADR-38. **Chưa cài ở task này**: chọn dịch vụ và cài cần
chủ shop OK (spec 04 mục 8: "Dịch vụ nào: chọn ở M0, cần chủ shop OK").

## Việc nó làm

Mỗi lần chạy (lịch: mỗi phút một lần):

1. `GET HOOKS_HEALTH_URL` (ví dụ `https://hooks.elimaz.com.vn/health`) và `GET DEEP_HEALTH_URL`
   (`https://crm.elimaz.com.vn/health/deep`, header `Authorization: Bearer <DEEP_HEALTH_TOKEN>`). Đạt là mã 200.
2. Đích nào hỏng: chờ 20 s, gọi lại cả hai. Đích hỏng ở **cả hai lượt** → nhắn Telegram, nêu tên đích (`hooks`,
   `deep`). Cùng nội dung không nhắn lại trong 10 phút (nhớ qua `PROBE_STATE_FILE`).
3. Gửi nhịp sống của chính nó: `GET PROBE_HEARTBEAT_URL` (dịch vụ nhận nhịp sống tự báo khi bộ kiểm ngừng chạy).

Tệp chạy chỉ dùng thư viện chuẩn của Go (kèm gói `internal/platform/urlerr` của CRM, cũng chỉ thư viện chuẩn), không đọc database, không có dữ liệu khách. Mã thoát: 0 đạt; 1 không nhắn
được Telegram, không ghi được tệp trạng thái hay không gửi được nhịp sống; 2 thiếu biến.

## Biến môi trường

| Biến | Bắt buộc | Ý nghĩa |
|---|---|---|
| `HOOKS_HEALTH_URL` | có | `/health` của vai trò ingest |
| `DEEP_HEALTH_URL` | có | `/health/deep` của vai trò api |
| `DEEP_HEALTH_TOKEN` | có | bằng `HEALTH_TOKEN` của api |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | có | bot và kênh **gấp** |
| `PROBE_HEARTBEAT_URL` | không | URL nhận nhịp sống của bộ kiểm |
| `PROBE_STATE_FILE` | nên đặt | đường dẫn **bền** giữa các lần chạy (mặc định `$TMPDIR/crm-external-probe.json`); nơi chạy xóa tệp giữa hai lần thì chống lặp 10 phút mất tác dụng, tin nhắn lại mỗi phút |
| `TELEGRAM_API` | không | mặc định `https://api.telegram.org` (test đặt máy giả) |

## Dựng và cài

```sh
# Từ apps/crm: tệp chạy tĩnh cho Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o external-probe ./scripts/external-probe
```

Cài ở **một** dịch vụ chạy theo lịch không thuộc Cloudflare và ngoài VPS, chạy được tệp chạy Linux mỗi phút (lịch
thưa hơn mỗi phút làm chậm cảnh báo), hạn mỗi lần 60 s. Đặt biến bằng kho bí mật của dịch vụ
đó, không ghi vào mã. Một phương án chạy thử tại chỗ:

```sh
HOOKS_HEALTH_URL=... DEEP_HEALTH_URL=... DEEP_HEALTH_TOKEN=... TELEGRAM_BOT_TOKEN=... TELEGRAM_CHAT_ID=... ./external-probe
```

Test: `go test ./scripts/external-probe/`.
