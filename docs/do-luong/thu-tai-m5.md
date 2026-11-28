# Thử tải M5 — flash sale và lượt đọc của website (kế hoạch M5 Task 219 / M5-36, ADR-42)

Bộ thử: `loadtest/m5_run.sh`, `loadtest/k6/m5/*.js`, `tools/loadkit` (`m5key`, `m5sale`, `m5stats`, `m5report`),
`crm verify-flash` (cách chạy, ngưỡng, cách đo ở `loadtest/README.md`, mục "Bộ thử tải M5"). Mỗi lần chạy thêm một mục,
không xóa mục cũ.

**Số đo tạm — đo lại trên server thử trước khi chạy thật** (Task 221 Step 4: bản thử, k6 từ máy khác, Valkey thật).

## Lần 1 — 28/11/2026, bể GitHub (máy thử tham khảo)

**Máy:** runner `ubuntu-24.04` của GitHub Actions (4 vCPU, 16 GB), một máy riêng cho mỗi lượt. Ba vai trò CRM (ingest,
api, worker) dựng bằng `go build ./cmd/crm` từ nhánh task-219 (gốc main 27d4fb0d, sau Task 520), chạy như tiến trình,
`GOMEMLIMIT` như compose dev; PostgreSQL 18, hai Valkey 9.1 (`valkey-cache`, `valkey-gate`) của compose dev và k6 v2.0.0
cùng máy. Cổng nhận vào flash, giới hạn theo SĐT, IP giữ mặc định (`FLASH_ADMIT_PER_SECOND` 100, toàn shop 300,
`FLASH_MAX_INFLIGHT` 64; SĐT 10 lượt/10 s, IP 30/10 s); `web_key` nâng lên 10.000.000/phút (QĐ-219-3).

Hai lượt: run 46106941415 (sáu bài lần lượt, 36 phút) và run 46109908569 (chạy lại `flash_spike`, `flash_abandon` sau khi
sửa cách đếm của chính bộ thử — xem "Lượt một đỏ vì bộ thử"). Mã CRM của hai lượt như nhau.

### Tóm tắt

| Bài | Kết quả |
|---|---|
| `flash_spike` [FS03] (run 46109908569) | **Mọi tiêu chí ĐẠT.** Đúng 500 bought / 64.986 lượt bấm (lên 1.000/giây trong 10 s, giữ 60 s, 20.000 SĐT); lượt mua qua cổng p95 ≤ 200 ms phía máy chủ (1.262 lượt), phía k6 p95 3,9 ms, p99 62,3 ms (mọi kết quả, gồm sold_out từ cờ); chỉ 503 `retry_later` (4.836 lần), không mã nào khác; verify-flash đạt (500 held, 0 available); deadlock +0; `tx_retry` 0, hết lượt 0 |
| `flash_per_customer` [FS05] (run 46106941415) | **ĐẠT.** 1 bought, 9 over_limit, 90 lần 429 (giới hạn 10 lượt/10 s của SĐT); verify-flash đạt |
| `flash_sustained` [FS16] (run 46106941415) | **Mọi tiêu chí ĐẠT.** 15.000 bought, 15.000 đơn hoàn tất trong 5 phút (46,9 lượt/giây đạt được); lượt mua p95 ≤ 50 ms phía máy chủ, k6 p95 35,1 ms; hoàn tất p95 32,5 ms; nhận việc sự kiện (`event.*`, 60.006 việc) p95 ≤ 400 ms (p99 ≤ 2 s); không lỗi; verify-flash đạt (15.000 sold); deadlock +0 |
| `flash_abandon` [FS06] (run 46109908569) | **Mọi tiêu chí ĐẠT.** Đợt một 500 bought, 250 hoàn tất; sau hạn giữ 5 phút đợt hai 250 người mua được cả 250 suất mở lại, 0 sold_out ("hết oan" 0); verify-flash đạt (250 sold + 250 held = 500) |
| `flash_close_with_holds` [FS17] (run 46106941415) | **Mọi tiêu chí ĐẠT.** 200 giữ suất; đóng đợt ở giây 60 được; 100 hoàn tất sau khi đóng; 100 còn lại hết hạn; verify-flash đạt (sold 100, released 400, held 0; (g) đối chiếu held của số dư khớp) |
| `web_read` [FS02, FS13] (run 46106941415) | **Mọi tiêu chí ĐẠT.** 300.001 lượt trong 10 phút (500/giây); k6 p50 1,3 ms, p95 1,7 ms, p99 3,9 ms; 0 lỗi; trúng bộ đệm 98,72% (2.073.102 trúng, 26.908 trượt) |

### Bên nào chạm trần trước

- Lượt mua flash sale: **cổng nhận vào** (thiết kế, QĐ23) — 100 vé/giây cho một món nên ở đỉnh 1.000 lượt/giây phần lớn
  lượt trước khi hết suất nhận 503 `gate_busy` (4.823 lần) rồi gửi lại theo `Retry-After`; sau khi hết suất cờ
  `sold_out` của Valkey trả 63.654 lượt không chạm database. PostgreSQL không chạm trần ở bài này (tx_retry 0).
- Trong máy: PostgreSQL dùng CPU nhiều nhất (lớn nhất 321% trên 4 nhân ở lượt sáu bài, lúc `flash_sustained`/`web_read`;
  145% ở lượt hai bài); api ≤ 33%, worker ≤ 36%; Valkey cache ≤ 33%, gate ≤ 18%.
- Bộ nhớ lớn nhất: api 128,6 MiB, worker 52,1 MiB, ingest 40,9 MiB, PostgreSQL 410,7 MiB, Valkey cache 7,8 MiB, Valkey
  gate 9,3 MiB. Kết nối PostgreSQL lúc cuối mỗi bài: 33.

### FS14 — thứ tự đến so với mua được (ghi, không chấm; QĐ5)

`flash_spike`: người mua được bấm lần đầu trong 5,1 s đầu (p50 3,0 s, p95 5,0 s) trong khi lượt bấm trải 70 s (p50
37,5 s): ai đến trong mấy giây đầu (lúc tải đang lên 1.000/giây) mới có suất, sau đó là sold_out. Thứ tự trong mấy
giây đầu không giữ đúng từng người (cổng nhận vào cho qua theo vé mỗi giây và lượt gửi lại theo `Retry-After`), đúng
như QĐ5 ("không hứa ai đến trước mua trước tuyệt đối").

### Đọc số đo

- "p95 phía máy chủ" là `flash_buy_ms` của lượt đã qua cổng nhận vào (cận ngăn histogram); lượt sold_out từ cờ và 503
  của cổng không vào histogram này (trả trong ~1 ms, xem k6 p50 0,6 ms).
- 83 lượt bấm của `flash_spike` nhận 503 cả 10 lần gửi lại (chỉ 503 `retry_later`, không phải lỗi khác): là người bấm
  lúc đỉnh, gửi lại từ lần 2 bỏ qua cờ hết suất (QĐ21) nên tiếp tục vào cổng. Website sẽ báo khách thử lại.
- Nhận việc sự kiện ở `flash_abandon` p95 ≤ 2 s: phần lớn là việc còn tồn của `flash_sustained` (15.000 đơn × 4
  consumer) đang cạn khi bài sau bắt đầu; không phải tiêu chí của bài đó.
- Không đo ở lượt này: CPU, bộ nhớ theo từng bài (stats.csv chỉ lấy lớn nhất của cả lượt), p95 phía máy chủ của lượt
  đọc web (chỉ đo phía k6, cùng máy nên gần bằng).

### Giới hạn `web_key` cho production (QĐ-219-8, Task 219)

Mặc định QĐ23 (1.200 yêu cầu/phút mỗi khóa, Valkey ngừng 600) chặn ngay đợt flash sale: mọi lượt của website đi qua
một khóa dịch vụ. Theo số đo lần 1, lúc đỉnh một khóa mang ~1.000 lượt bấm/giây + ~500 lượt đọc/giây + lượt gửi lại
theo `Retry-After` (4.836 lần trong ~10 s) ≈ 1.700 yêu cầu/giây. Chốt cho production, đặt bằng biến môi trường của api
(không đổi mặc định trong mã, vì mặc định là số của spec QĐ23):

```
RATE_LIMITS_JSON='{"web_key": {"max": 120000, "window_s": 60, "fallback_max": 60000}}'
```

120.000/phút = 2.000/giây (dư ~15% trên đỉnh đo); khi Valkey ngừng còn 1.000/giây. Bảo vệ database vẫn là cổng nhận vào
(100 vé/giây một món, 300 toàn shop) và giới hạn SĐT, IP — không phải giới hạn khóa. Đo lại trên server thử (Task 221)
và chỉnh số này theo đỉnh đo được.

### Lượt một đỏ vì bộ thử

Run 46106941415 chấm KHÔNG ĐẠT ở hai bài do cách đếm của k6/m5report, không do CRM; đã sửa rồi chạy lại hai bài:

- `flash_spike`: 127 lượt bấm nhận 503 cả 10 lần bị đếm là "lỗi khác" (máy chủ ghi đủ 70.161 yêu cầu, chỉ 200 và 503) →
  tách bộ đếm `buy_gave_up`.
- `flash_abandon`: `buy_bought` đếm cả đợt hai (750) → đợt một = tổng − đợt hai.

## Lần 2 — 28/11/2026, hỏng giữa tải (Task 220, M5-37; bể GitHub, số đo tạm)

Bộ thử: `loadtest/m5_run.sh chaos` → `loadtest/k6/m5/chaos-m5.sh` (cách chạy: `CRM_CI_LOADTEST=m5:chaos`). Mỗi bước một đợt
mới 500 suất, k6 `flash_spike.js` (đỉnh 500 lượt bấm/giây, giữ 50 s), lỗi gây ở giây 15 sau giờ mở; sau bước: `crm
verify-flash`, đếm `external_ref` trùng. Máy như Lần 1 (runner `ubuntu-24.04`, 4 vCPU, 16 GB, mọi thứ cùng máy), run
46135866042, 15 phút máy. Run chạy với bản chấm cũ của bước e (chỉ đòi có 503, verify-flash đạt, không đơn trùng) nên báo "mọi bài ĐẠT"; sau review bản chấm của bước e thêm "đúng 500 bought" và "lỗi khác 503/429 < 0,1%" (như bước a–c). **Chấm lại theo số của run: bước a–d ĐẠT (d chỉ chấm nhất quán: verify-flash, đơn trùng), bước e KHÔNG ĐẠT** (15.168 / 28.865 yêu cầu lỗi khác 503 = 52,5%).

| Bước | Lỗi gây (30 s) | bought | Lỗi khác 503/429 | 503 nhận | verify-flash | Đơn trùng |
|---|---|---|---|---|---|---|
| a | dừng `valkey-gate` | 500 | 0 / 109.724 yêu cầu | 88.921 (2.929 lượt bấm hết 10 lần gửi vẫn 503) | đạt | 0 |
| b | dừng `valkey-cache` | 500 | 0 / 28.857 | 1.364 | đạt | 0 |
| c | `FLUSHALL` cả hai | 500 | 0 / 28.831 | 1.338 | đạt | 0 |
| d | SIGKILL `api`, chạy lại ngay | 500 | 29 (mất kết nối lúc api chết) | 1.343 | đạt (không `held` mồ côi) | 0 |
| e | dừng PostgreSQL | 500 | **15.168 / 28.865 (52,5%): KHÔNG ĐẠT** | 1.372 | đạt | 0 |

### Tóm tắt cho chủ shop (Điểm dừng D)

- **Đạt:** Valkey ngừng hay bị xóa sạch không làm bán sai (đúng 500, không 500 hàng loạt, k6 p95 ≤ 13,5 ms). Giết `api`
  giữa đợt không để suất `held` nào mất đơn (verify-flash đạt). Ngắt PostgreSQL rồi bật lại: dữ liệu vẫn đúng (500, verify-flash đạt, không đơn trùng) nhưng cách báo lỗi chưa đạt (xem dưới).
- **Bên nào chạm trần:** khi `valkey-gate` ngừng (bước a), cổng nhận vào chạy dự phòng và trả 503 `retry_later` nhiều
  hơn hẳn (88.921 so với ~1.350 ở bước khác): cổng dự phòng chặt hơn, đúng thiết kế (chậm hơn, không sai); 2.929 lượt bấm
  hết 10 lần gửi vẫn 503 (website báo khách thử lại).
- **Phát hiện chưa đạt kỳ vọng của kế hoạch (bước e):** trong 30 s PostgreSQL ngừng, phần lớn yêu cầu mua (15.168) nhận
  mã KHÁC 503 (k6 đếm là "lỗi khác"; chưa ghi mã cụ thể), chỉ 1.372 nhận 503. Kế hoạch muốn 503 để website hiện "tạm ngưng"
  và xếp hàng đợi. Chưa sửa (đổi cách phân loại lỗi kết nối database nằm ở vùng rủi ro cao `platform/db`, `httpapi/errors`,
  cần task riêng): ghi ở "Việc phát hiện thêm" của `docs/tasks/task-220.md`. Verify-flash vẫn đạt nên không sai dữ liệu.
- **Chưa đo được:** (1) cờ hết suất dựng lại ≤ 2 s sau khi Valkey trở lại (chỉ chấm gián tiếp: đúng 500, 0 lỗi); (2) website
  bản sao 4185 ("tạm ngưng", hàng đợi cục bộ, tự chạy tiếp ≤ 2 phút): không có trong bể; (3) k6 từ máy khác, Valkey và
  PostgreSQL ở server thật: chờ server thử của hệ mới (Task 221 Step 4).
- **Đề xuất cho quyết định của chủ shop (QĐ-220-1, tự chốt theo /goal 28/11):** giữ số bộ điều tiết mặc định (100 vé/giây
  một món, 300 cả shop, 64 lượt đang xử lý) và `web_key` 120.000/phút; chưa công bố flash sale cho tới khi (1) sửa mã trả
  503 khi PostgreSQL ngừng và (2) chạy lại M5-36, 37 trên bản thử.

## Lần 3 — 28/11/2026, chạy lại hỏng giữa tải sau khi sửa mã 503 (Task 529)

Nguyên nhân của bước e Lần 2 (tái hiện bằng test `TestDBUnavailable_WebRouteChain`: pool của api qua proxy cắt kết
nối): lỗi nối database (`*pgconn.ConnectError`, 57P01 khi PostgreSQL tắt, kết nối đứt giữa câu, "conn closed") không
phải `db.ErrRetryLater` nên lớp HTTP trả **500 `internal`** (kèm log lỗi và lần ghi `error_events` cũng hỏng). Sửa:
`db.Unavailable` phân loại lỗi "database ngừng, quá tải, quá hạn"; `httpapi/errors.go` đổi thành 503 `retry_later` +
`Retry-After: 1` (cả ở bước xác thực). k6 đếm thêm lỗi khác theo nhóm mã (500, 4xx, 5xx khác 503, mất kết nối).

Cùng bộ thử, cùng máy (runner `ubuntu-24.04`, 4 vCPU, 16 GB), `CRM_CI_LOADTEST=m5:chaos`, run 46158491123, 15 phút máy.
**Mọi bước ĐẠT.**

| Bước | Lỗi gây (30 s) | bought | Lỗi khác 503/429 | 503 nhận | verify-flash | Đơn trùng |
|---|---|---|---|---|---|---|
| a | dừng `valkey-gate` | 500 | 0 / 113.752 | 93.953 (4.179 lượt bấm hết 10 lần gửi vẫn 503) | đạt | 0 |
| b | dừng `valkey-cache` | 500 | 0 / 28.827 | 1.334 | đạt | 0 |
| c | `FLUSHALL` cả hai | 500 | 0 / 28.832 | 1.339 | đạt | 0 |
| d | SIGKILL `api`, chạy lại ngay | 500 | 30 (cả 30 là mất kết nối lúc api chết; d không chấm tỷ lệ) | 1.341 | đạt | 0 |
| e | dừng PostgreSQL | 500 | **0 / 126.830: ĐẠT** (Lần 2: 15.168 / 28.865) | 113.586 (10.513 lượt bấm hết 10 lần gửi vẫn 503) | đạt | 0 |

- Bước e: trong 30 s ngừng mọi lượt mua nhận 503 `retry_later` kèm `Retry-After: 1`, nên website có tín hiệu để hiện
  "tạm ngưng" (website bản sao 4185 vẫn chờ server thử của hệ mới, QĐ-220-2). 10.513 lượt bấm gửi lại đủ 10 lần trong lúc
  database ngừng (k6 chờ theo `Retry-After`, ~10 s) — đây là người bấm website sẽ báo "tạm ngưng"; sau khi PostgreSQL
  trở lại, đợt bán tiếp đúng 500 suất.
- Bảng chấm Lần 2 của bước e (KHÔNG ĐẠT) được thay bằng bảng này; QĐ-220-1 điều kiện (1) "sửa mã trả 503 khi PostgreSQL
  ngừng" đã xong, còn điều kiện (2) chạy lại trên bản thử (server thật).
