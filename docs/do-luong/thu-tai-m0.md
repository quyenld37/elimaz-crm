# Thử tải M0 (kế hoạch M0 Task 22, ADR-42)

Bộ thử: `loadtest/` (cách chạy, ngưỡng, cách đo ở `loadtest/README.md`). Mỗi lần chạy thêm một mục, không xóa mục cũ,
để so giữa các mốc.

## Lần 1 — 18/11/2026, laptop dev (chỉ để tham khảo)

**Máy:** MacBook (Apple Silicon, 10 nhân), Docker Desktop (VM 10 CPU, 7,65 GiB). Cùng lúc máy chạy nhiều phiên làm
task CRM song song và dự án khác: **load average 17–24 suốt các bài** (ghi ở đầu từng bảng). Mọi số đo dưới đây vì vậy
không phải tiêu chí đạt chính thức; tiêu chí chính thức đo lại trên máy thử (Task 23 bước 7.9, cần OK của chủ shop).
Image `crm:loadtest` dựng từ mã cùng ngày (sau Task 39). Giới hạn bộ nhớ của compose dev: ingest 256 MiB, api, worker
512 MiB; PostgreSQL không giới hạn.

### Tóm tắt

| Bài | Kết quả chính |
|---|---|
| Thường (1×, 30 phút) | **Không mất, không trùng**: 17.957/17.957 sự kiện tới đủ 50 client đúng một lần; mọi webhook trả 200. Độ trễ vượt ngưỡng: p50 webhook → báo hiệu 359 ms nhưng p95 95 s, do một lần PostgreSQL tự khởi động lại giữa bài (xem phát hiện 1) và máy quá tải |
| Nặng (10×, 15 phút) | **Sụp**: hàng đợi không theo kịp, câu nhận việc chậm dần tới 2,4 s rồi quá hạn liên tục (phát hiện 2); 5.293 webhook quá hạn 10 s; sau bài còn ~78.000 việc chờ, xả ~8 việc/phút |
| Hỏng giữa tải (ở tải 1×) | **Mọi tiêu chí đạt**: 0 webhook lỗi, 9.001/9.001 sự kiện xong, 701 lệnh không lệnh nào gửi hai lần, không client nào nhận trùng; tự trở lại sau 6,6 s (giết api), 6,4 s (giết worker), 3,8 s (sau khi bật lại PostgreSQL) |
| Ngâm (rút ngắn, ~100 phút) | Bộ nhớ sau giờ đầu: api +11,4%, ingest +17,4%, worker +1,6% (đạt ≤ 20%); dòng chết lớn nhất 26.168 (đạt < 50.000); bảng jobs tăng đều ~50 MiB/giờ do việc done giữ 30 ngày (phát hiện 4); k6 không tự kết thúc trên máy quá tải nên các tiêu chí đo bằng k6 CHƯA ĐO. Ngâm 8 giờ chưa chạy |

### Phát hiện

1. **PostgreSQL tự khởi động lại dưới tải vì không có `init`** (bài thường, 08:47:54 UTC). Log: `untracked child
   process (PID 7856) exited with exit code 2` → `terminating any other active server processes` → crash recovery.
   PostgreSQL là PID 1 trong container nên nhận mọi tiến trình mồ côi; lệnh healthcheck `pg_isready` (docker exec) quá
   hạn dưới tải bị bỏ lại, thoát mã 2 (không phản hồi), postmaster coi đó là tiến trình con sập. Hệ quả trong bài: api
   809 lỗi, ingest ghi tạm 428 webhook ra đĩa (vẫn trả 200), hub SSE nối lại 8 lần, việc đứng ~100 s; **không mất sự
   kiện nào**. Sửa: `init: true` cho dịch vụ postgres — Task 395 đã thêm vào `docker/compose.dev.yml` cùng ngày (gặp độc lập);
   compose máy thử, máy thật phải có như vậy. Các bài sau bài thường chạy với `init: true`.
2. **Câu nhận việc (`ClaimJobs`) chậm theo độ dài hàng chờ khi nhiều việc cùng `order_key`** (bài nặng). Mọi ping
   selftest của một tenant cùng `order_key`; khi có ~10.000 việc `inbound.selftest` chờ, mỗi lần nhận việc phải kiểm
   "việc đứng trước" cho từng ứng viên, và câu con `NOT EXISTS` quét tuần tự cả bảng (`b.tenant_id IS NOT DISTINCT FROM
   j.tenant_id` không dùng được index `jobs_order`):

   ```
   Limit (actual time=2370.832..2370.837 rows=16)
     -> Index Scan using jobs_pending_run_at on jobs j (rows=17)
          -> Seq Scan on jobs b (actual time=0.352..0.352 rows=1 loops=6679)
   Execution Time: 2370.971 ms        (78.000 việc pending, 18/11 09:39 UTC)
   ```

   Worker log `nhận việc: timeout: context deadline exceeded` liên tục; hàng đợi càng dài càng chậm (sụp, không tự hồi).
   Kênh thật (Meta) cũng đặt `order_key` theo hội thoại; một hội thoại hay một Page nóng có thể dựng đúng tình huống này.
   Đây là vùng rủi ro cao (`internal/platform/queue`), cần một task riêng: viết lại điều kiện để dùng được index (ví dụ
   tách nhánh `tenant_id IS NULL`), thêm bài đo nhận việc khi hàng chờ 10.000+ việc cùng khóa.
3. **`COMMIT` bị hủy vì `lock_timeout` (55P03)**: `tx_retry{55P03}` 178 lần trong 30 phút bài thường. Transaction có
   `pg_notify` giữ khóa toàn cục của hàng NOTIFY lúc commit; máy chậm thì chờ quá 5 s. Đã có thử lại; cần đo trên máy
   thử trước khi kết luận (ADR-42 đã ghi trần NOTIFY).
4. **Hạn giữ việc done 30 ngày mâu thuẫn với tiêu chí ngâm "bảng jobs không lớn thêm sau giờ thứ hai"** (xem bài ngâm).
5. Dấu vết của các vai trò gửi tới Jaeger compose dev bị 404 (`traces export: failed … 404`). Không ảnh hưởng số đo.

### Bài thường (1×, 30 phút)

Lượt normal20261118152740: tải từ 2026-11-18T08:28:42Z tới 2026-11-18T08:58:47Z (UTC); k6 thoát mã 99.
Máy lúc bắt đầu: 15:27  up 16 days, 19:12, 1 user, load averages: 21.66 21.98 22.92
Máy lúc kết thúc: 15:59  up 16 days, 19:44, 1 user, load averages: 19.28 22.34 23.67

| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Nhận webhook p95 < 300 ms | < 300 ms | 1432.7 ms | KHÔNG ĐẠT |
| Webhook tới báo hiệu p95 < 2 s | < 2000 ms | 95314.0 ms | KHÔNG ĐẠT |
| Webhook tới báo hiệu p99 < 5 s | < 5000 ms | 107174.0 ms | KHÔNG ĐẠT |
| Việc nền ghi tới nhận p95 < 1 s | ≤ 1000 ms (cận ngăn histogram) | ≤ +Inf ms (36018 lần nhận việc) | KHÔNG ĐẠT |
| Việc nền ghi tới nhận p99 < 3 s | ≤ 3000 ms (cận ngăn histogram) | ≤ +Inf ms (36018 lần nhận việc) | KHÔNG ĐẠT |
| API đọc p95 < 200 ms | < 200 ms | 847.0 ms | KHÔNG ĐẠT |
| API đọc p99 < 500 ms | < 500 ms | 3597.2 ms | KHÔNG ĐẠT |
| Không webhook nào nhận mã khác 200 | 0 | 0 | ĐẠT |
| Mỗi eventId tới mỗi client đúng một lần | mỗi client: duy nhất = số webhook 200, trùng = 0 | 50 client; thiếu nhiều nhất 0; trùng 0 | ĐẠT |
| Không mất sự kiện (mọi webhook 200 có việc inbound done) | done ≥ số webhook 200, chưa done = 0 | webhook 200: 17957; done: 17957; chưa done: 0 | ĐẠT |

Số đo khác:

- webhook gửi được (200): 17957.0
- webhook p99 (ms): 5087.7
- webhook lớn nhất (ms): 7248.7
- lượt đọc API: 44817.0
- API đọc lớn nhất (ms): 9996.2
- lượt đọc khác 200: 809.0
- webhook → báo hiệu (ms): p50 359, p95 95314, p99 107174, lớn nhất 112395; 897850 lần nhận; 0 lần nối lại, 0 lần nối hỏng
- việc dead trong khung: 0; lệnh: 0 (done 0, in_doubt 0, gửi hơn một lần 0)
- platform.jobs: 728 dòng chết, 26.3 MiB

| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |
|---|---|---|---|---|
| crm-api | 20.6 | 66.0 | 56.1 | 47.4 |
| crm-ingest | 9.1 | 63.5 | 34.7 | 29.2 |
| crm-worker | 13.7 | 28.7 | 29.0 | 28.4 |
| jaeger | 1.7 | 12.1 | 20.4 | 20.1 |
| postgres | 55.3 | 170.3 | 178.3 | 178.3 |
| s3-bootstrap | 3.0 | 15.3 | 3.5 | 0.7 |
| seaweedfs | 4.6 | 30.4 | 95.2 | 94.1 |
| seaweedfs-offsite | 5.4 | 40.8 | 97.7 | 96.8 |

### Bài nặng (10×, 15 phút)

Lượt heavy20261118162127: tải 2026-11-18T09:21:52Z → 09:36:53Z (UTC); run.sh vỡ sau k6 (script bị sửa lúc đang chạy), bước chấm chạy tay lúc 09:39Z; hàng đợi CHƯA cạn.
Máy lúc bắt đầu: 16:21  up 16 days, 20:05, 1 user, load averages: 23.27 20.79 21.94
Máy lúc chấm: 16:39  up 16 days, 20:23, 1 user, load averages: 18.81 19.44 20.06



| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Nhận webhook p95 < 300 ms | < 300 ms | 2263.0 ms | KHÔNG ĐẠT |
| Webhook tới báo hiệu p95 < 2 s | < 2000 ms | 146911.0 ms | KHÔNG ĐẠT |
| Webhook tới báo hiệu p99 < 5 s | < 5000 ms | 178387.0 ms | KHÔNG ĐẠT |
| Việc nền ghi tới nhận p95 < 1 s | ≤ 1000 ms (cận ngăn histogram) | ≤ +Inf ms (21034 lần nhận việc) | KHÔNG ĐẠT |
| Việc nền ghi tới nhận p99 < 3 s | ≤ 3000 ms (cận ngăn histogram) | ≤ +Inf ms (21034 lần nhận việc) | KHÔNG ĐẠT |
| API đọc p95 < 200 ms | < 200 ms | 996.3 ms | KHÔNG ĐẠT |
| API đọc p99 < 500 ms | < 500 ms | 4474.8 ms | KHÔNG ĐẠT |
| Không webhook nào nhận mã khác 200 | 0 | 5293 | KHÔNG ĐẠT |
| Mỗi eventId tới mỗi client đúng một lần | mỗi client: duy nhất = số webhook 200, trùng = 0 | 50 client; thiếu nhiều nhất 78789; trùng 0 | KHÔNG ĐẠT |
| Không mất sự kiện (mọi webhook 200 có việc inbound done) | done ≥ số webhook 200, chưa done = 0 | webhook 200: 84310; done: 5551; chưa done: 9960 | KHÔNG ĐẠT |

Số đo khác:

- webhook gửi được (200): 84310.0
- webhook p99 (ms): 5537.4
- webhook lớn nhất (ms): 10009.4
- lượt đọc API: 86791.0
- API đọc lớn nhất (ms): 10024.5
- lượt đọc khác 200: 103.0
- webhook → báo hiệu (ms): p50 1039, p95 146911, p99 178387, lớn nhất 841037; 276050 lần nhận; 0 lần nối lại, 0 lần nối hỏng
- việc dead trong khung: 0; lệnh: 0 (done 0, in_doubt 0, gửi hơn một lần 0)
- platform.jobs: 911 dòng chết, 78.2 MiB

| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |
|---|---|---|---|---|
| crm-api | 30.2 | 189.2 | 66.5 | 54.1 |
| crm-ingest | 23.4 | 112.1 | 69.2 | 62.1 |
| crm-worker | 7.3 | 38.9 | 32.9 | 24.3 |
| jaeger | 1.4 | 11.3 | 21.5 | 21.2 |
| postgres | 272.0 | 643.7 | 297.6 | 282.2 |
| s3-bootstrap | 2.5 | 34.2 | 4.1 | 0.7 |
| seaweedfs | 3.6 | 21.5 | 140.5 | 138.7 |
| seaweedfs-offsite | 2.9 | 21.6 | 100.2 | 96.9 |

### Hỏng giữa tải — chạy cùng tải thường (1×), 15 phút

Kế hoạch đòi gây hỏng trong lúc chạy tải nặng; trên laptop tải nặng tự làm hàng đợi sụp (phát hiện 2) nên "tự trở lại" không đo được gì. Lần này `CHAOS_LOAD=normal DURATION=15m loadtest/run.sh chaos`: giết crm-api (phút 3), giết crm-worker (phút 6), dừng PostgreSQL 60 s (phút 9), thêm lệnh `command.selftest` suốt bài. Thời gian "trở lại" có sai số tới 5 s (nhịp healthcheck của Docker). Bài hỏng giữa tải nặng đo lại trên máy thử.

Lượt chaos20261118163944: tải từ 2026-11-18T09:40:04Z tới 2026-11-18T09:55:07Z (UTC); k6 thoát mã 0.
Máy lúc bắt đầu: 16:39  up 16 days, 20:24, 1 user, load averages: 20.04 19.74 20.15
Máy lúc kết thúc: 16:56  up 16 days, 20:40, 1 user, load averages: 8.21 14.65 17.50



| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Không webhook nào nhận mã khác 200 | 0 | 0 | ĐẠT |
| Không client nào nhận trùng eventId | 0 | 0 | ĐẠT |
| Không mất sự kiện (mọi webhook 200 có việc inbound done) | done ≥ số webhook 200, chưa done = 0 | webhook 200: 9001; done: 9001; chưa done: 0 | ĐẠT |
| Không lệnh nào gửi hai lần | lệnh có attempts > 1 = 0 | 701 lệnh; gửi hơn một lần: 0; done 701; in_doubt 0 | ĐẠT |
| Tự trở lại trong 2 phút: giết crm-api | ≤ 120 s | 6.6 s | ĐẠT |
| Tự trở lại trong 2 phút: giết crm-worker | ≤ 120 s | 6.4 s | ĐẠT |
| Tự trở lại trong 2 phút: ngắt PostgreSQL 60 s | ≤ 120 s | 3.8 s | ĐẠT |

Số đo khác:

- webhook gửi được (200): 9001.0
- webhook p99 (ms): 185.5
- webhook lớn nhất (ms): 5010.4
- lượt đọc API: 22501.0
- API đọc lớn nhất (ms): 10001.4
- lượt đọc khác 200: 1575.0
- webhook → báo hiệu (ms): p50 223, p95 26798, p99 58247, lớn nhất 65928; 448750 lần nhận; 50 lần nối lại, 150 lần nối hỏng
- việc dead trong khung: 0; lệnh: 701 (done 701, in_doubt 0, gửi hơn một lần 0)
- platform.jobs: 1534 dòng chết, 12.9 MiB

| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |
|---|---|---|---|---|
| crm-api | 10.1 | 94.0 | 35.9 | 30.4 |
| crm-ingest | 3.6 | 29.2 | 21.4 | 20.3 |
| crm-worker | 8.0 | 36.7 | 23.8 | 23.2 |
| jaeger | 0.4 | 2.0 | 18.6 | 18.5 |
| postgres | 24.9 | 93.3 | 139.6 | 119.6 |
| s3-bootstrap | 1.3 | 7.4 | 1.6 | 0.7 |
| seaweedfs | 2.2 | 16.2 | 92.4 | 92.3 |
| seaweedfs-offsite | 2.4 | 15.0 | 93.8 | 93.3 |

### Ngâm rút ngắn (1×), dự định 100 phút

`DURATION=100m loadtest/run.sh soak`, bắt đầu 16:56 (load average 7,6 lúc bắt đầu, lên 28–34 từ 17:15 vì phiên khác). k6 không kết thúc đúng hạn trên máy quá tải và bị dừng ở giới hạn 2 giờ của tiến trình nền lúc 18:56, trước khi ghi tóm tắt: các tiêu chí đo bằng k6 (webhook, API đọc, đủ sự kiện) là CHƯA ĐO. Mẫu bộ nhớ (mỗi 15 s, ~2 giờ), số đo bảng jobs (mỗi 5–6 phút) và SSE vẫn đủ; bước chấm chạy tay. Không có việc nào dead; mọi việc của lượt chạy đã xong khi chấm (59.836 done, 0 chưa xong).



| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Nhận webhook p95 < 300 ms | < 300 ms | không có số đo | CHƯA ĐO |
| Webhook tới báo hiệu p95 < 2 s | < 2000 ms | 3307.0 ms | KHÔNG ĐẠT |
| Webhook tới báo hiệu p99 < 5 s | < 5000 ms | 41570.0 ms | KHÔNG ĐẠT |
| Việc nền ghi tới nhận p95 < 1 s | ≤ 1000 ms (cận ngăn histogram) | ≤ 1000 ms (120022 lần nhận việc) | ĐẠT |
| Việc nền ghi tới nhận p99 < 3 s | ≤ 3000 ms (cận ngăn histogram) | ≤ +Inf ms (120022 lần nhận việc) | KHÔNG ĐẠT |
| API đọc p95 < 200 ms | < 200 ms | không có số đo | CHƯA ĐO |
| API đọc p99 < 500 ms | < 500 ms | không có số đo | CHƯA ĐO |
| Không webhook nào nhận mã khác 200 | 0 | không có số đo | CHƯA ĐO |
| Mỗi eventId tới mỗi client đúng một lần | mỗi client: duy nhất = số webhook 200, trùng = 0 | không có số đo | CHƯA ĐO |
| Không mất sự kiện (mọi webhook 200 có việc inbound done) | done ≥ số webhook 200, chưa done = 0 | không có số đo | CHƯA ĐO |
| Bộ nhớ không tăng quá 20% sau giờ đầu: crm-api | ≤ +20% | 45.0 MiB → 50.1 MiB (+11.4%) | ĐẠT |
| Bộ nhớ không tăng quá 20% sau giờ đầu: crm-ingest | ≤ +20% | 30.6 MiB → 36.0 MiB (+17.4%) | ĐẠT |
| Bộ nhớ không tăng quá 20% sau giờ đầu: crm-worker | ≤ +20% | 25.4 MiB → 25.8 MiB (+1.6%) | ĐẠT |
| Dòng chết của platform.jobs < 50.000 | < 50000 | 26168 (lớn nhất) | ĐẠT |
| Bảng jobs không lớn thêm sau giờ thứ hai | cuối ≤ lúc giờ thứ hai | chạy chưa quá giờ thứ hai | CHƯA ĐO |

Số đo khác:

- webhook → báo hiệu (ms): p50 271, p95 3307, p99 41570, lớn nhất 930512; 2991200 lần nhận; 0 lần nối lại, 0 lần nối hỏng
- việc dead trong khung: 0; lệnh: 0 (done 0, in_doubt 0, gửi hơn một lần 0)
- platform.jobs: 21070 dòng chết, 98.2 MiB

| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |
|---|---|---|---|---|
| crm-api | 16.1 | 139.5 | 60.2 | 50.1 |
| crm-ingest | 6.6 | 130.0 | 47.0 | 36.0 |
| crm-worker | 12.8 | 134.9 | 35.5 | 25.8 |
| jaeger | 1.4 | 26.3 | 20.8 | 20.4 |
| postgres | 106.6 | 650.2 | 311.6 | 273.4 |
| s3-bootstrap | 2.8 | 35.7 | 3.9 | 1.2 |
| seaweedfs | 4.5 | 53.0 | 101.3 | 95.4 |
| seaweedfs-offsite | 4.2 | 33.8 | 98.0 | 94.9 |

Bảng jobs theo thời gian (UTC; việc inbound done, chưa xong, dòng chết, kích thước MiB):

| Lúc | done | chưa xong | dòng chết | MiB |
|---|---|---|---|---|
| 09:56 | 0 | 0 | 1536 | 12.9 |
| 10:02 | 3428 | 0 | 4734 | 17.6 |
| 10:08 | 6993 | 0 | 4594 | 22.5 |
| 10:14 | 10597 | 0 | 4585 | 27.4 |
| 10:20 | 14204 | 0 | 9452 | 32.6 |
| 10:26 | 17876 | 0 | 2529 | 37.7 |
| 10:32 | 21518 | 0 | 4305 | 42.9 |
| 10:38 | 25182 | 0 | 4562 | 48.0 |
| 10:44 | 28708 | 0 | 4258 | 52.9 |
| 10:50 | 32332 | 0 | 1975 | 57.9 |
| 10:56 | 35967 | 5 | 16726 | 63.6 |
| 11:02 | 39667 | 0 | 12169 | 69.0 |
| 11:09 | 43372 | 31 | 5477 | 74.3 |
| 11:15 | 46961 | 36 | 20006 | 79.8 |
| 11:21 | 50904 | 0 | 11695 | 85.0 |
| 11:27 | 54495 | 1 | 26168 | 90.3 |
| 11:33 | 58123 | 0 | 14092 | 95.4 |

Bảng jobs tăng đều ~50 MiB/giờ (12,9 → 95,4 MiB trong 97 phút) vì việc done được giữ 30 ngày (`internal/platform/schedules/cleanup.go`, `retention`). Với hạn giữ đó, tiêu chí "bảng jobs không lớn thêm sau giờ thứ hai" của kế hoạch không thể đạt ở tải đều: ở 1× liên tục là ~1,2 GiB mỗi ngày, ~36 GiB sau 30 ngày. Cần chủ shop chọn: rút hạn giữ việc done (ví dụ 2 ngày, chỉ giữ lâu việc dead, in_doubt), hoặc sửa tiêu chí thành "không lớn hơn mức của hạn giữ". Dòng chết dao động 2.000–26.000 (autovacuum theo kịp, dưới ngưỡng 50.000).

### Việc còn lại

- Chạy lại cả bốn bài trên máy thử (Task 23 bước 7.9, cần OK của chủ shop): đó là số đo chính thức. Trên Linux đặt
  `LOADTEST_ACCESS_LISTEN` (xem `loadtest/README.md`).
- Ngâm đủ 8 giờ (`loadtest/run.sh soak`) — trên máy thử hoặc qua đêm trên một máy rảnh.
- Hỏng giữa tải nặng (`loadtest/run.sh chaos`, không đặt `CHAOS_LOAD`) sau khi sửa phát hiện 2.
