# Bộ thử tải M0 (kế hoạch M0 Task 22, ADR-42)

Đo đường webhook → việc nền → báo hiệu và đường đọc API của CRM dưới tải, chấm theo ngưỡng của spec 04 mục 1.
Kết quả mỗi lần chạy ghi vào `docs/do-luong/thu-tai-m0.md` để so giữa các mốc.

## Chạy

```sh
make db-up                      # PostgreSQL, SeaweedFS, Jaeger của dự án compose
loadtest/run.sh normal          # 1×: 10 sự kiện/giây, 30 phút
loadtest/run.sh heavy           # 10×: 100 sự kiện/giây, 15 phút
loadtest/run.sh chaos           # heavy + chaos.sh: giết crm-api, giết crm-worker, ngắt PostgreSQL 60 s, cách nhau 3 phút
loadtest/run.sh soak            # 1× trong 8 giờ
DURATION=1m loadtest/run.sh normal   # chạy khói
```

Nhiều phiên trên một máy: đặt `COMPOSE_PROJECT`, `CRM_PG_PORT`, `CRM_API_PORT`, `CRM_INGEST_PORT`,
`LOADTEST_ACCESS_PORT` riêng (xem đầu `run.sh`). Trên Linux (máy thử) đặt `LOADTEST_ACCESS_LISTEN` là địa chỉ bridge của Docker: container tới máy qua `host-gateway`, không tới được 127.0.0.1. Image riêng `crm:loadtest` (`CRM_IMAGE`), dựng lần đầu hoặc khi
`LOADTEST_BUILD=1`. Kết quả ở `loadtest/out/<bài>-<giờ>/` (bỏ khỏi git; `tokens.json` bị xóa khi bài xong): `report.md` là bảng chấm.
`run.sh` thoát 1 khi có tiêu chí KHÔNG ĐẠT.

## Tải gồm gì

| Phần | Ở đâu | Làm gì |
|---|---|---|
| Webhook | `k6/common.js` `webhook` | `POST /hooks/selftest` có chữ ký HMAC, mỗi lượt một ping, `eventId = lt.<run>.<ms gửi>.<VU>.<lượt>` |
| Đọc API | `k6/common.js` `read` | 50 nhân viên giả, `GET /api/v1/me` và `GET /api/v1/platform/jobs` xen kẽ, chia đều cho 50 thẻ; mỗi nhân viên dưới 5 lượt/giây (giới hạn 300 / 60 s của api) |
| 50 client SSE | `loadkit sse` | Mỗi client một nhân viên, giữ `GET /api/v1/stream`, tự nối lại; đếm pong của lượt chạy, trùng, độ trễ từ lúc gửi |
| Thẻ Access | `loadkit access` | CA, khóa ký mới mỗi lượt; api kiểm bằng bộ kiểm Cloudflare Access thật, trỏ tới máy chủ khóa giả qua `compose.loadtest.yml` |
| Dữ liệu thử | `loadkit seed` | Tenant "Thử tải", `nv01…nv50@loadtest.invalid` vai trò owner, định tuyến selftest `loadtest` |
| Hỏng giữa tải | `k6/chaos.sh` | Gây hỏng, khởi động lại ngay, đo thời gian tới khi ba vai trò healthy và không còn việc pending quá 2 s; thêm 2 lệnh `command.selftest` mỗi giây |
| Lấy mẫu | `run.sh` | `docker stats` mỗi 15 s; `loadkit stats` (việc của lượt chạy, histogram `job_pickup_ms`, dòng chết và kích thước `platform.jobs`) mỗi 5 phút |
| Chấm | `loadkit report` | Bảng tiêu chí → ngưỡng → số đo → ĐẠT / KHÔNG ĐẠT / CHƯA ĐO |

## Ngưỡng và cách đo

| Tiêu chí | Ngưỡng | Đo bằng |
|---|---|---|
| Nhận webhook | p95 < 300 ms | k6 `webhook_ms` (thời gian trả lời của ingest) |
| Webhook tới báo hiệu | p95 < 2 s, p99 < 5 s | `loadkit sse`: lúc nhận pong − lúc gửi trong `eventId`, lần nhận đầu ở mỗi client |
| Việc nền từ lúc ghi tới lúc nhận | p95 < 1 s, p99 < 3 s | histogram `job_pickup_ms` trong `platform.metrics_minute`; chỉ biết cận ngăn (…, 1000, 2000, 5000 ms), chấm "cận ngăn ≤ ngưỡng" |
| API đọc | p95 < 200 ms, p99 < 500 ms | k6 `read_ms` |
| Webhook khác 200 | 0 | k6 `webhook_bad` |
| Mỗi eventId tới mỗi client đúng một lần | thiếu 0, trùng 0 | `loadkit sse` so với số webhook 200 (bài hỏng giữa tải chỉ đòi trùng 0: client mất luồng lúc api chết nhận `resync`) |
| Không mất sự kiện | mọi webhook 200 có việc `inbound.selftest` done | `loadkit stats` sau khi hàng đợi cạn |
| Không lệnh nào gửi hai lần | 0 | việc `command.selftest` của lượt chạy có `attempts > 1` |
| Tự trở lại sau mỗi lần hỏng | ≤ 2 phút | `chaos.sh` (trạng thái healthy của Docker lấy mẫu mỗi 5 s, nên sai số tới 5 s) |
| Ngâm: bộ nhớ | mẫu cuối ≤ mẫu đầu từ giờ thứ nhất + 20% | `docker stats` của `crm-ingest`, `crm-api`, `crm-worker` |
| Ngâm: dòng chết | `n_dead_tup` của `platform.jobs` < 50.000 | `pg_stat_user_tables` |
| Ngâm: kích thước bảng jobs | cuối ≤ lúc giờ thứ hai | `pg_total_relation_size` |

Bài ngâm chạy ngắn hơn hai giờ thì hai tiêu chí cuối là CHƯA ĐO, không phải ĐẠT.

## Giới hạn

- Số đo trên máy dev là để tham khảo; tiêu chí đạt chính thức đo trên máy thử (kế hoạch M0 Task 23 bước 7.9).
- Tải thường là tốc độ đều 10 sự kiện/giây; phát lại một ca live thật khi có Page thử đủ dữ liệu [P].
- Không dùng với database có dữ liệu thật: `chaos.sh` thêm việc thẳng vào `platform.jobs` bằng chủ bảng.
