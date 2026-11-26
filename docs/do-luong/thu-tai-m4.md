# Thử tải M4 — ca live Facebook (kế hoạch M4 Task 180, HT17 lặp lại, ADR-42)

Bộ thử: `loadtest/fb_run.sh`, `loadtest/fb_chaos.sh`, `loadtest/k6/fb_*.js`, `tools/loadkit` (cách chạy, ngưỡng, cách đo
ở `loadtest/README.md`, mục "Bộ thử tải M4"). Mỗi lần chạy thêm một mục, không xóa mục cũ.

## Lần 1 — 26/11/2026, bể Blacksmith (máy thử tham khảo)

**Máy:** runner `blacksmith-4vcpu-ubuntu-2404` (4 vCPU), một máy riêng cho mỗi bài, load average lúc bắt đầu 0,3. Ba
vai trò CRM (ingest, api, worker) dựng bằng `go build ./cmd/crm` từ mã của nhánh task-180 (gốc m4 sau Task 172), chạy
như tiến trình trên máy, `GOMEMLIMIT` như compose dev; PostgreSQL 18, SeaweedFS của compose dev trong cùng máy; máy Meta
giả `tools/fakegraph` và k6 v2.0.0 cũng trên máy đó. Không phải máy chủ thật: số đo chính thức trên staging với Page
thử là Task 181 (Step 8).

Tải mỗi bài: 15 phút, 100 sự kiện/giây (70 comment live/giây: một nửa webhook `feed`, một nửa luồng SSE của máy giả,
10% qua cả hai đường; 30 tin hộp thư/giây, 5% gửi trùng cùng lúc), 2 Page, 2.000 PSID, 2 phiên live, 20 biến thể ×
500 tồn; 50 client SSE; 20 nhân viên đọc mỗi 20 s, 10 gửi mỗi 10 s, 5 chốt mỗi 30 s.

### Tóm tắt

| Bài | Kết quả |
|---|---|
| Tải 10× (run 45826397968) | **Mọi tiêu chí ĐẠT.** Tin khách tới màn hình p95 296 ms, p99 305 ms; comment live p95 292 ms; nhận webhook p95 1,3 ms; bấm gửi → Send API p95 6 ms; API đọc p95 4,2 ms, p99 200,7 ms. 27.001 tin và 63.001 comment, mỗi cái đúng một dòng; 900 tin gửi, không client_ref trùng; 149 lần chốt, giữ 149 ≤ tồn; deadlock 0, việc dead 0 |
| Hỏng giữa tải 10× (run 45826397630) | **Mọi tiêu chí ĐẠT.** Trở lại ≤ 3,1 s sau mỗi lần hỏng (giết worker, giết api, sau khi bật lại PostgreSQL); luồng live mở lại 10,0 s sau khi giết worker, 4,5 s sau khi bật lại PostgreSQL; không thiếu tin, comment nào (27.001 / 63.000); 840 tin gửi, không tin nào tới máy giả hai lần; deadlock 0, việc dead 0 |

Chạy khói trước đó (run 45825756924, 1 phút cùng tải, Blacksmith 4 vCPU): mọi tiêu chí ĐẠT. Chạy khói trên máy Mac
(5 sự kiện/giây, 20 s; bài chaos không ngắt PostgreSQL dùng chung) chỉ để kiểm kịch bản, không lấy số.

### Đọc số đo

- "Trở lại sau 3,1 s" là sàn của cách đo: `fb_chaos.sh` kiểm lần đầu 3 s sau khi gỡ hỏng (để việc mới kịp quá hạn 2 s
  nếu worker chưa chạy), mỗi lần kiểm ~0,1 s; cả ba lần hỏng đều qua ngay ở lần kiểm đầu.
- Luồng live mở lại 10,0 s sau khi giết worker (từ lúc giết tới lúc máy giả thấy luồng mới, gồm cả lúc worker mới
  khởi động); dưới ngưỡng 15 s (LV10). Không đo ở bài này phần nào của 10 s là chờ giữ chỗ của tiến trình cũ. Comment trong lúc đó đến qua webhook `feed` và lấy bù khi nối lại: không
  thiếu comment nào.
- Bài hỏng giữa tải không chấm độ trễ: khi giết api, 50 client SSE mất luồng và nối lại (50 lần), 300 cặp (tin, client)
  không nhận khung (nhận `resync` thay vào); khi PostgreSQL ngừng 60 s, sự kiện vào được ingest giữ ở đĩa (mọi webhook
  vẫn 200, HT01) và xử lý sau, nên p95 tới màn hình của cả bài là 21,7 s. 130 lượt đọc API lỗi trong lúc PostgreSQL
  ngừng, api chết.
- Chỉ số máy chủ `live_comment_to_signal_ms` (từ `created_time` của Meta) chỉ biết cận ngăn và `created_time` của
  webhook `feed` tính bằng giây, nên ở bài tải nó là "≤ 1.000 ms"; bảng chấm dùng đo phía client (xem README).

### Tải 10× (15 phút)

Lượt fb20261126130948: tải từ 2026-11-26T13:09:54Z tới 2026-11-26T13:25:27Z (UTC); k6 fb_live mã 0, fb_staff mã 0.
Máy: lõi: 4 lõi; lúc bắt đầu:  13:09:48 up 0 min,  0 user,  load average: 0.28, 0.06, 0.02; lúc kết thúc:  13:26:39 up 17 min,  0 user,  load average: 0.31, 0.56, 0.47
| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Nhận webhook p95 < 300 ms | < 300 ms | 1.3 ms | ĐẠT |
| Tin khách tới báo hiệu p95 < 2 s | < 2000 ms | 296.0 ms | ĐẠT |
| Tin khách tới báo hiệu p99 < 5 s | < 5000 ms | 305.0 ms | ĐẠT |
| Comment live tới báo hiệu p95 < 3 s | < 3000 ms | 292.0 ms | ĐẠT |
| Bấm gửi tới lúc gọi Send API p95 < 1 s | < 1000 ms | 6.0 ms | ĐẠT |
| API đọc p95 < 200 ms | < 200 ms | 4.2 ms | ĐẠT |
| API đọc p99 < 500 ms | < 500 ms | 200.7 ms | ĐẠT |
| Không webhook nào nhận mã khác 200 | 0 | 0 | ĐẠT |
| Mỗi tin khách tới database đúng một lần | dòng = mã khác nhau = tin ingest nhận (200) | gửi 27001 (trùng cùng lúc 1335); dòng 27001; mã khác nhau 27001 | ĐẠT |
| Mỗi comment live tới database đúng một lần, đủ comment | dòng = mã khác nhau = comment đã giao, đều thuộc phiên live; đẩy vào máy giả lỗi 0 | đã giao 63001 (qua cả hai đường 6252); dòng 63001; mã khác nhau 63001; thuộc phiên live 63001; đẩy lỗi 0 | ĐẠT |
| Máy Meta giả nhận mỗi client_ref tối đa một lần | client_ref trùng = 0 | 900 lời gọi Send API, 900 có client_ref, trùng 0 | ĐẠT |
| Tổng giữ hàng không vượt tồn | mỗi dòng held ≤ on_hand; sổ giữ = tổng held | tồn 10000, giữ 149 (sổ giữ 149), dòng vượt 0; chốt 149 | ĐẠT |
| deadlocks không tăng | cuối − đầu = 0 | 0 → 0 | ĐẠT |
| Không việc dead | 0 (bài không có lỗi cố ý) | 0 | ĐẠT |
| Mọi tin gửi ra đã gửi (không in_doubt, failed, expired) | 0 | 900 tin; đã gửi 900, đang chờ 0, hỏng/không rõ 0 | ĐẠT |

- webhook 200: 62995.0
- webhook p99 (ms): 2.0
- lượt đọc API: 3750.0
- tin nhân viên gửi được (201): 900.0
- gửi tin: thời gian trả lời p95 (ms): 3.6
- chốt được (201): 149.0
- chốt bị từ chối nghiệp vụ (409): 1.0
- chốt: thời gian trả lời p95 (ms): 11.2
- tin khách → báo hiệu (ms, mọi cặp sự kiện × client): p50 156, p95 296, p99 305, lớn nhất 549; 27001 sự kiện, 1350050 cặp, thiếu khung 0
- comment live → báo hiệu (ms, mọi cặp sự kiện × client): p50 160, p95 292, p99 305, lớn nhất 1426; 63001 sự kiện, 3150050 cặp, thiếu khung 0
- client SSE: 50; nối lại 0 lần, nối hỏng 0 lần
- webhook_to_signal_ms (máy chủ, metrics_minute): 90002 lần; p50 ≤ 50, p95 ≤ 50, p99 ≤ 50 ms
- live_comment_to_signal_ms (máy chủ, metrics_minute): 63001 lần; p50 ≤ 1000, p95 ≤ 1000, p99 ≤ 1000 ms
- send_click_to_api_ms (máy chủ, metrics_minute): 900 lần; p50 ≤ 50, p95 ≤ 50, p99 ≤ 50 ms
- job_pickup_ms (máy chủ, metrics_minute): 162054 lần; p50 ≤ 50, p95 ≤ 50, p99 ≤ 50 ms
- bấm gửi → Send API (ms): p50 4, p95 6, p99 8, lớn nhất 111; 900 tin; client_ref trùng 0
- việc in_doubt 0, còn chờ 0; deadlock (cả database) 0

| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |
|---|---|---|---|---|
| crm-api | 4.3 | 27.7 | 67.0 | 67.0 |
| crm-ingest | 2.4 | 2.7 | 46.8 | 46.5 |
| crm-worker | 14.5 | 20.1 | 101.6 | 101.6 |

### Hỏng giữa tải 10× (15 phút)

Lượt fbc20261126130949: tải từ 2026-11-26T13:09:54Z tới 2026-11-26T13:25:17Z (UTC); k6 fb_live mã 0, fb_staff mã 0.
Máy: lõi: 4 lõi; lúc bắt đầu:  13:09:49 up 0 min,  0 user,  load average: 0.29, 0.06, 0.02; lúc kết thúc:  13:26:29 up 17 min,  0 user,  load average: 0.36, 0.74, 0.58
| Tiêu chí | Ngưỡng | Đo được | Kết quả |
|---|---|---|---|
| Không webhook nào nhận mã khác 200 | 0 | 0 | ĐẠT |
| Mỗi tin khách tới database đúng một lần | dòng = mã khác nhau = tin ingest nhận (200) | gửi 27001 (trùng cùng lúc 1388); dòng 27001; mã khác nhau 27001 | ĐẠT |
| Mỗi comment live tới database đúng một lần, đủ comment | dòng = mã khác nhau = comment đã giao, đều thuộc phiên live; đẩy vào máy giả lỗi 0 | đã giao 63000 (qua cả hai đường 6268); dòng 63000; mã khác nhau 63000; thuộc phiên live 63000; đẩy lỗi 0 | ĐẠT |
| Máy Meta giả nhận mỗi client_ref tối đa một lần | client_ref trùng = 0 | 840 lời gọi Send API, 840 có client_ref, trùng 0 | ĐẠT |
| Tổng giữ hàng không vượt tồn | mỗi dòng held ≤ on_hand; sổ giữ = tổng held | tồn 10000, giữ 132 (sổ giữ 132), dòng vượt 0; chốt 132 | ĐẠT |
| deadlocks không tăng | cuối − đầu = 0 | 0 → 0 | ĐẠT |
| Không việc dead | 0 (bài không có lỗi cố ý) | 0 | ĐẠT |
| Tự trở lại trong 2 phút: giết crm-worker | ≤ 120 s | 3.1 s | ĐẠT |
| Tự trở lại trong 2 phút: giết crm-api | ≤ 120 s | 3.1 s | ĐẠT |
| Tự trở lại trong 2 phút: ngắt PostgreSQL 60 s | ≤ 120 s | 3.1 s | ĐẠT |
| Luồng live nhận lại trong 15 s: giết crm-worker, video 500000000000001 | ≤ 15 s | 10.0 s | ĐẠT |
| Luồng live nhận lại trong 15 s: giết crm-worker, video 500000000000002 | ≤ 15 s | 10.0 s | ĐẠT |
| Luồng live nhận lại trong 15 s: ngắt PostgreSQL 60 s, video 500000000000001 | ≤ 15 s | 4.5 s | ĐẠT |
| Luồng live nhận lại trong 15 s: ngắt PostgreSQL 60 s, video 500000000000002 | ≤ 15 s | 4.5 s | ĐẠT |

- webhook 200: 62976.0
- webhook p99 (ms): 1.5
- lượt đọc API: 3630.0
- lượt đọc khác 200: 130.0
- tin nhân viên gửi được (201): 840.0
- gửi tin: thời gian trả lời p95 (ms): 2.9
- chốt được (201): 132.0
- chốt bị từ chối nghiệp vụ (409): 8.0
- chốt: thời gian trả lời p95 (ms): 11.3
- tin khách → báo hiệu (ms, mọi cặp sự kiện × client): p50 170, p95 21668, p99 54946, lớn nhất 504597; 27001 sự kiện, 1350050 cặp, thiếu khung 300
- comment live → báo hiệu (ms, mọi cặp sự kiện × client): p50 172, p95 21715, p99 55831, lớn nhất 388284; 63000 sự kiện, 3150000 cặp, thiếu khung 0
- client SSE: 50; nối lại 50 lần, nối hỏng 0 lần
- webhook_to_signal_ms (máy chủ, metrics_minute): 89690 lần; p50 ≤ 50, p95 ≤ 5000, p99 ≤ +Inf ms
- live_comment_to_signal_ms (máy chủ, metrics_minute): 62782 lần; p50 ≤ 1000, p95 ≤ +Inf, p99 ≤ +Inf ms
- send_click_to_api_ms (máy chủ, metrics_minute): 838 lần; p50 ≤ 50, p95 ≤ 50, p99 ≤ 1000 ms
- job_pickup_ms (máy chủ, metrics_minute): 161259 lần; p50 ≤ 50, p95 ≤ 1000, p99 ≤ 5000 ms
- bấm gửi → Send API (ms): p50 3, p95 5, p99 806, lớn nhất 4041; 840 tin; client_ref trùng 0
- việc in_doubt 0, còn chờ 0; deadlock (cả database) 0

| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |
|---|---|---|---|---|
| crm-api | 2.9 | 18.7 | 66.6 | 62.6 |
| crm-ingest | 1.7 | 1.9 | 47.4 | 47.0 |
| crm-worker | 11.0 | 18.7 | 101.5 | 101.5 |

`chaos.log`:

fb_chaos: 13:12:57 giết crm-worker
fb_chaos: 13:13:00 giết crm-worker: trở lại sau 3.1 s
fb_chaos: 13:13:07 giết crm-worker: luồng video 500000000000001 mở lại sau 10.0 s
fb_chaos: 13:13:07 giết crm-worker: luồng video 500000000000002 mở lại sau 10.0 s
fb_chaos: 13:15:57 giết crm-api
fb_chaos: 13:16:00 giết crm-api: trở lại sau 3.1 s
fb_chaos: 13:18:57 ngắt PostgreSQL 60 s
fb_chaos: 13:20:01 ngắt PostgreSQL 60 s: trở lại sau 3.1 s
fb_chaos: 13:20:02 ngắt PostgreSQL 60 s: luồng video 500000000000001 mở lại sau 4.5 s
fb_chaos: 13:20:02 ngắt PostgreSQL 60 s: luồng video 500000000000002 mở lại sau 4.5 s
fb_chaos: 13:20:02 xong

`chaos.csv`:

fault,start,recovered
giết crm-worker,1795698777.5,1795698780.6
giết crm-api,1795698957.5,1795698960.6
ngắt PostgreSQL 60 s,1795699198.0,1795699201.1

`live.csv`:

fault,video,start,regained
giết crm-worker,500000000000001,1795698777.5,1795698787.5
giết crm-worker,500000000000002,1795698777.5,1795698787.5
ngắt PostgreSQL 60 s,500000000000001,1795699198.0,1795699202.5
ngắt PostgreSQL 60 s,500000000000002,1795699198.0,1795699202.5

`k6-live.txt`:


`k6-staff.txt`:


### Việc còn lại

- Đo trên staging với Page thử (Task 181 Step 8, cần OK của chủ shop); máy Meta giả không thay được Meta thật (độ trễ,
  cách cắt luồng, giới hạn tốc độ).
- Lặp lại trước ngày mở (ADR-42) trên máy chủ thật.
