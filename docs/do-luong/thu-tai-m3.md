# Thử tải M3 — webhook hãng vận chuyển (kế hoạch M3 Task M3-48, ADR-42)

Bộ thử: `loadtest/carrier_run.sh`, `loadtest/k6/carrier_webhooks.js`, `tools/loadkit carrierseed` (cách chạy, ngưỡng,
cách đo ở `loadtest/README.md`, mục "Bộ thử tải M3"). Mỗi lần chạy thêm một mục, không xóa mục cũ.

**Số đo tạm: đo lại trên server thử trước khi chạy thật** (kế hoạch M3 Task 140, Điểm dừng C).

## Lần 1 — 28/11/2026, bể GitHub (máy thử tham khảo)

**Máy:** runner `ubuntu-24.04` của GitHub (4 lõi), một máy riêng cho bài, load average lúc bắt đầu 1,23. Ba vai trò CRM
(ingest, api, worker) dựng bằng `go build ./cmd/crm` từ mã của nhánh task-139 (gốc main 83c88921), chạy như tiến trình
trên máy, `GOMEMLIMIT` như compose dev; PostgreSQL 18, SeaweedFS của compose dev trong cùng máy; hãng giả
`tools/fakecarrier` (mọi lần tra trả sau 200 ms) và k6 v2.0.0 cũng trên máy đó.

Tải: 15 phút, 20 tin hãng/giây (10× ca hiện tại: 2.000 đơn/ngày × khoảng 6 tin mỗi vận đơn, dồn đỉnh), 3.000 vận đơn
dựng qua API (lên đơn, gửi hãng; lệnh tạo của worker gửi hãng giả thật), mỗi vận đơn 6 tin: lấy hàng → đang chuyển →
đang giao → giao thất bại → đang giao → thành công. Mỗi tin là một webhook có chữ ký tới `/hooks/fake/<route>`; CRM
tra lại hãng giả (lệnh `fulfillment.refresh_status`) rồi áp.

Chạy khói trước đó (run 46107485650, 1 phút, 200 vận đơn): mọi tiêu chí ĐẠT. Lần chạy khói đầu (run 46106926321) đỏ
trước khi có tải: vai trò api không đọc `FAKECARRIER_URL` nên "Gửi hãng" qua API với hãng giả trả 409
`carrier_account_unavailable` — đã sửa trong Task 139 (`config.Load`, test `TestLoadAPIReadsFakeCarrier`).

### Tải 10× (15 phút, run 46107770766)

Lượt c20261128043004: tải từ 2026-11-28T04:33:16Z tới 2026-11-28T04:48:16Z (UTC); k6 mã 0. Máy lúc kết thúc: load
average 1,14 / 1,19 / 0,91.

| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Nhận webhook p95 | < 300 ms | 1,7 ms (18.000 tin trả 200) | ĐẠT |
| Không webhook nào khác 200 | 0 | 0 (trang quản trị hãng giả lỗi: 0) | ĐẠT |
| Webhook tới vận đơn đổi trạng thái p95 (gồm lần tra 200 ms) | < 5.000 ms | 212,2 ms (p99 216,5 ms) | ĐẠT |
| Mỗi tin hãng được áp | đã áp = đã gửi | gửi 18.000, áp 18.000 | ĐẠT |
| `pg_stat_database.deadlocks` không tăng | cuối − đầu = 0 | 0 → 0 | ĐẠT |
| `tx_retry_exhausted` | 0 | 0 | ĐẠT |
| Việc dead mới | 0 | 0 → 0 | ĐẠT |

Sau lần chạy 15 phút, bộ dựng dữ liệu (`loadkit carrierseed`) đổi sang public API của module và trigger số dư COD đổi
sang `INSERT … ON CONFLICT`; chạy khói lại (run 46112220852, 1 phút, 200 vận đơn, mã cuối của Task 139): mọi tiêu chí
ĐẠT, nhận p95 1,7 ms, webhook → đổi trạng thái p95 211,5 ms (p99 214,1 ms), gửi 1.200 áp 1.200, deadlocks 0 → 0.

### Đọc số đo

- Độ trễ "webhook tới vận đơn đổi trạng thái" gần như toàn bộ là 200 ms chờ của hãng giả khi tra; phần của CRM (ghi bản
  nhận, tách gói, lệnh tra, áp) khoảng 12–17 ms ở p95–p99.
- Đồng hồ: `sentAt` do k6 ghi và `shipment_events.recorded_at` do PostgreSQL ghi trên cùng máy.
- Bài không gồm hỏng giữa tải (giết vai trò, ngắt database); các ca hỏng của M3 là test e2e `TestM3Chaos_*` (viết sẵn,
  chạy ở lượt gộp mốc) và test cấp gói (xem `docs/tasks/task-139.md`).

### Chưa đo

- `heavy.js` của M0 (đường webhook selftest, đọc API, 50 client SSE): chưa chạy lại ở M3 — bài đó dựng image Docker và
  compose riêng, không có ở bể từ xa. M3 không đổi đường selftest; chờ server thử của hệ mới.
- Số đo chính thức: chờ server thử của hệ mới.
