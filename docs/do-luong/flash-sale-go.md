# Phép đo đường mua flash sale bằng Go (Task 1, phép thử ngắn số 5)

Ngày đo: 18/11/2026. Mã: `spikes/flashsale` (module Go riêng). Kế hoạch: M0 bản 5, Task 1 (bản đã sửa theo ADR-46
ngày 18/11). Ngưỡng: ADR-44 "Phép đo đầu tiên của M0".

> **Chỉ để tham khảo.** k6, máy chủ Go và PostgreSQL (trong máy ảo của Docker Desktop) chạy chung một laptop
> của chủ shop, cùng lúc với container của dự án khác và của phiên Task 2 (phiên đó chạy thử khôi phục PostgreSQL
> trong lúc đo, xem lần "tắt dấu vết 1"). Số đo chính thức là lần chạy lại trên máy thử 13.140.64.56 (Step 6),
> **chưa làm**: bị giữ lại vì cần OK riêng của chủ shop để vào máy chủ.

## Cấu hình máy

| Mục | Giá trị |
|---|---|
| Máy | Apple M5, 10 nhân (4 hiệu năng cao + 6 tiết kiệm), RAM 16 GB, macOS 26.6.2 |
| Docker Desktop | máy ảo Linux 10 CPU, 7,65 GiB RAM, dùng chung với container khác |
| PostgreSQL | `postgres:18` (18.7) ghim digest, cấu hình mặc định của image (`max_connections` 100, `shared_buffers` 128 MB, `synchronous_commit` on), `shm_size` 256 MB |
| Jaeger | `jaegertracing/jaeger:2.23.0` ghim digest, lưu trong bộ nhớ, cùng máy; khởi động lại trước mỗi lần bật dấu vết |
| Máy chủ đo | Go 1.27.4, chạy thẳng trên macOS (không container); pool 20 kết nối (ngân sách `api` của ADR-42), `-max-inflight 64`, role `crm_app` (RLS bật và FORCE) |
| Giới hạn thời gian | Kết nối: `statement_timeout` 30 s, `lock_timeout` 5 s, `idle_in_transaction_session_timeout` 60 s (ADR-26). Mỗi transaction: `lock_timeout` 200 ms, `statement_timeout` 2 s (giá trị gợi ý của ADR-46 cho lượt mua) |
| Chạy lại transaction | ADR-46: `40P01`, `55P03`, `40001` → chạy lại tối đa 3 lần, chờ 20/40/80 ms × [0,5; 1,5]; hết lượt → 503 "thử lại", không lưu |
| k6 | v2.0.0, cùng máy |

## Đường đi đo được

Mỗi lượt bấm: bước 1 (ghi khóa chống trùng `running`, commit riêng) rồi bước 2 (một transaction: một câu
`set_config` đặt `lock_timeout`, `statement_timeout`, tenant; ghi giới hạn khách có điều kiện; lấy đơn vị
`FOR UPDATE SKIP LOCKED`; tạo đơn nháp + dòng đơn + giữ hàng + đánh dấu đơn vị trong một câu CTE; chuyển khóa sang
`done`). Số lượt đi về database mỗi lượt bấm: 11 khi mua được (4 + 7), 12 khi hết suất (4 + 8). Chưa gom lô pgx để
bớt lượt đi về: để dành nếu Step 6 cần.

Độ trễ phía máy chủ đo từ lúc nhận yêu cầu tới lúc handler ghi xong câu trả lời. Phía k6: `http_req_duration` (mỗi
yêu cầu, kể cả lần thử lại) và `click_to_final` (từ lượt bấm tới kết quả cuối, gồm các lần chờ thử lại). CPU tính
theo % một nhân (100% = một nhân). CPU tiến trình Go lấy bằng `ps` mỗi giây (macOS tính trung bình trượt, số gần
đúng); CPU PostgreSQL, Jaeger bằng `docker stats` mỗi 1–2 giây. CPU của k6 không đo. Số deadlock lấy từ
`pg_stat_database.deadlocks` của database `crm` trước và sau mỗi lần chạy.

## Kết quả

### `spike.js`: 500 suất, 20.000 khách, tới 1.000 lượt bấm/giây trong 60 s, `-limit 1`

k6 tăng từ 0 lên 1.000 lượt/giây trong 10 s rồi giữ 50 s. Không dựng 20.000 VU cùng lúc (tệp task, "Quyết định thay
chủ shop" 7). Lần "bắt đầu ở đỉnh" là lần thêm để tham khảo: bắt đầu ngay ở 1.000 lượt/giây, để 500 suất bị tranh ở
tốc độ đỉnh.

| | Tắt dấu vết 1 (bị nhiễu) | Tắt dấu vết 2 | Tắt dấu vết 3 | Bật `-otel` 1 | Bật `-otel` 2 | Tắt, bắt đầu ở đỉnh |
|---|---|---|---|---|---|---|
| bought / sold_out / over_limit | 500 / 50.916 / 1.000 | 500 / 53.497 / 1.000 | 500 / 53.496 / 1.000 | 500 / 53.494 / 1.000 | 500 / 53.497 / 1.000 | 500 / 58.497 / 1.000 |
| 503 "thử lại" / lượt bấm k6 bỏ vì thiếu VU | 22.257 / 2.581 | 0 / 0 | 255 / 0 | 981 / 0 | 615 / 0 | 0 / 0 |
| Máy chủ p50 / p95 / p99 | 6,5 / 75,1 / 192,1 ms | 5,6 / 9,4 / 17,5 ms | 5,6 / 8,8 / 36,1 ms | 6,8 / 39,5 / 92,7 ms | 6,5 / 22,9 / 63,7 ms | 5,8 / 9,9 / 22,8 ms |
| k6 `http_req_duration` p50 / p95 / p99 | 6,8 / 76,5 / 195,7 ms | 5,7 / 9,5 / 17,6 ms | 5,7 / 8,9 / 36,1 ms | 6,9 / 39,6 / 93,4 ms | 6,6 / 23,0 / 63,7 ms | 5,8 / 10,0 / 22,9 ms |
| k6 bấm → kết quả cuối p95 / p99 | 1.492 / 4.367 ms | 10 / 18 ms | 9 / 56 ms | 52 / 508 ms | 26 / 428 ms | 10 / 23 ms |
| Yêu cầu/giây (cả 60 s, gồm 10 s tăng dần) | 1.236 | 917 | 921 | 925 | 927 | 1.000 |
| Chạy lại transaction (theo SQLSTATE) / hết lượt | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| Deadlock trước → sau | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 |
| CPU Go trung bình / cao nhất; RSS cao nhất | 47% / 81%; 71 MB | 48% / 55%; 44 MB | 47% / 58%; 44 MB | 69% / 88%; 79 MB | 69% / 101%; 83 MB | 52% / 58%; 44 MB |
| CPU PostgreSQL trung bình / cao nhất | 91% / 174% | 53% / 81% | 54% / 147% | 81% / 132% | 75% / 155% | 59% / 114% |
| CPU Jaeger trung bình / cao nhất | 0% / 5% | 3% / 98% | 0% / 0% | 26% / 286% | 22% / 274% | 0% / 0% |
| Kết nối của máy chủ đo (mở / đang chạy, cao nhất) | 20 / 13 | 20 / 4 | 20 / 6 | 20 / 20 | 20 / 7 | 20 / 5 |
| Bên chạm trần trước | Giới hạn 64 lượt đang xử lý, vì máy bận việc khác | Không bên nào: tốc độ do k6 đặt | Không bên nào (255 lượt 503 do giới hạn 64 trong một đợt chậm ngắn) | Giới hạn 64 lượt đang xử lý | Giới hạn 64 lượt đang xử lý | Không bên nào |
| `verify` | ĐẠT | ĐẠT | ĐẠT | ĐẠT | ĐẠT | ĐẠT |

Lần "tắt dấu vết 1" trùng lúc phiên Task 2 chạy thử khôi phục PostgreSQL trong cùng máy ảo Docker (container
`crm-t02-pg-restore-*`); từ giây thứ 20 độ trễ tăng vọt dù không bật gì thêm. Không dùng lần này để đánh giá; lần 3
đo bù. Các lần sau cũng có thể chịu một phần nhiễu này (container của phiên Task 2 còn chạy tới hết buổi đo).

Trước khi kế hoạch thêm ADR-46 (cùng ngày, chưa có `lock_timeout` 200 ms theo transaction và chưa chạy lại), một
vòng đo đầy đủ cho số tương tự: tắt dấu vết p95 máy chủ 9,3–10,8 ms; bật dấu vết 28,6–49,1 ms; bắt đầu ở đỉnh 12,3 ms;
sustained p95 7,8 ms; mọi `verify` đạt. Số trong tệp này là của vòng sau, đúng cấu hình ADR-46.

### `per_customer.js`: một khách, 100 yêu cầu song song, mỗi yêu cầu một khóa, `-limit 2`

| Mục | Kết quả |
|---|---|
| bought / over_limit | 2 / 98 |
| 503 "thử lại" (quá 64 lượt đang xử lý, gửi lại cùng khóa) | 36 |
| Máy chủ p50 / p95 / p99 | 42,3 / 91,8 / 94,7 ms (các lượt cùng xếp hàng trên một dòng `flash_sale_claims` của khách) |
| k6 p50 / p95 / p99 | 43,1 / 92,7 / 96,2 ms |
| Chạy lại transaction / hết lượt; deadlock | 0 / 0; 0 → 0 |
| `verify -limit 2` | ĐẠT: 2 đơn, 2 đơn vị `held`, tổng giới hạn của khách 2 |

Lần chạy dưới 1 giây nên bộ lấy mẫu CPU, kết nối không có số đáng tin.

### `sustained.js`: 50 lượt mua/giây trong 5 phút, 15.000 suất, mỗi lượt một khách mới

| Mục | Kết quả |
|---|---|
| bought / sold_out | 15.000 / 1 (lượt cuối, khi đã hết 15.000 suất) |
| Máy chủ p50 / p95 / p99 / cao nhất | 5,4 / 8,3 / 12,6 / 182,1 ms |
| k6 p50 / p95 / p99 | 5,6 / 8,5 / 13,0 ms |
| Yêu cầu/giây | 50 |
| Chạy lại transaction / hết lượt; deadlock | 0 / 0; 0 → 0 |
| CPU Go trung bình / cao nhất; RSS | 5% / 8%; 25 MB |
| CPU PostgreSQL trung bình / cao nhất | 9% / 26% |
| `verify` | ĐẠT: 15.000 đơn, 15.000 đơn vị `held`, 15.000 khách |

## Đối chiếu ngưỡng ADR-44 (tham khảo, chưa chính thức)

| Ngưỡng | Trên máy này |
|---|---|
| p95 phía máy chủ < 300 ms ở 1.000 lượt/giây | Đạt: 8,8–9,9 ms khi tắt dấu vết (không tính lần bị nhiễu); 22,9–39,5 ms khi bật |
| 500 suất ra đúng 500 đơn | Đạt cả 6 lần |
| Không khách nào vượt giới hạn | Đạt (`verify` (d) cả 8 lần) |
| 50 đơn/giây trong 5 phút | Đạt: 15.000 đơn, p95 8,3 ms |
| Bật dấu vết làm p95 tăng không quá 10% | **Không đạt trên máy này**: p95 phía máy chủ từ 8,8–9,4 ms lên 22,9–39,5 ms (gấp 2,4–4,5 lần), kèm 615–981 câu trả lời 503 vì số lượt đang xử lý chạm 64. Số này lẫn ảnh hưởng của Jaeger chạy cùng máy (tới 2,9 nhân khi nhận span) và PostgreSQL cũng tốn thêm CPU (75–81% so với 53–54%), nên chưa tách được phần của bộ đo trong tiến trình Go. Cần đo lại ở Step 6 với Jaeger ở máy khác |

**Bên nào chạm trần trước:** khi tắt dấu vết và máy không bận việc khác, không bên nào: Go dùng khoảng nửa nhân,
PostgreSQL khoảng nửa nhân trung bình, nhiều nhất 6 trong 20 kết nối cùng chạy; tốc độ do k6 đặt. Khi máy bận (lần
bị nhiễu) hoặc bật dấu vết, giới hạn 64 lượt đang xử lý chạm trước và trả 503 (đúng thiết kế: không dồn hàng vào
database). Theo mục "Xem lại khi" của ADR-44, chưa có dấu hiệu tiến trình Go là nút thắt.

## Đề xuất giá trị chốt cho ADR-46 (lượt mua flash sale)

- **`lock_timeout` 200 ms: giữ.** Không lần nào gặp `55P03` hay deadlock. Đơn vị lấy bằng `SKIP LOCKED` nên không
  chờ; chỗ duy nhất phải chờ là dòng `flash_sale_claims` của cùng một khách: 100 yêu cầu song song của một khách chờ
  nhiều nhất khoảng 95 ms, vì giới hạn 64 lượt đang xử lý chặn độ dài hàng chờ. Test
  `TestLockTimeoutIsRetriedThenBought` và `TestLockTimeoutExhaustedGivesRetryNotStored` cho thấy khi chờ quá 200 ms
  thì transaction được chạy lại, hết lượt thì trả 503 không lưu (khoảng 1 s cho 4 lần chạy).
- **`statement_timeout` 2 s: giữ.** Câu chậm nhất quan sát được dưới 300 ms (độ trễ cao nhất của cả yêu cầu, lần
  không nhiễu).
- Chốt chính thức sau Step 6: trên máy thử, `max-inflight` và kích cỡ pool có thể khác, làm hàng chờ trên dòng
  của một khách dài hơn.

## Việc cho Step 6 và cho chủ shop

- Chạy lại trên máy thử 13.140.64.56 (container thử, k6 và Jaeger ở máy khác, xóa container sau khi đo); cần OK riêng.
- Nếu ở máy thử bật dấu vết vẫn vượt 10%: chủ shop chọn giữa lấy mẫu dưới 100% cho điểm cuối mua, bỏ span của từng câu
  SQL (chỉ giữ span của transaction), hoặc nới ngưỡng. Chưa quyết ở đây.

## Cách chạy lại

```sh
make db-up                                   # PostgreSQL 5442, Jaeger 16686/4318
cd spikes/flashsale && go build -o /tmp/flashsale .
ITEM=$(/tmp/flashsale setup -units 500)      # dựng lại schema spike, nạp suất
/tmp/flashsale serve -limit 1 [-otel] &      # in p50/p95/p99 và bộ đếm chạy lại mỗi 10 s, dòng TỔNG khi tắt (Ctrl-C)
k6 run -e ITEM=$ITEM k6/spike.js             # -e START_RATE=1000: bắt đầu ở đỉnh
kill -INT %1 && /tmp/flashsale verify -item $ITEM -limit 1
# per_customer.js: setup -units 50, serve -limit 2, verify -limit 2
# sustained.js:    setup -units 15000, serve -limit 1
# deadlock: SELECT deadlocks FROM pg_stat_database WHERE datname = 'crm' (trước và sau)
```
