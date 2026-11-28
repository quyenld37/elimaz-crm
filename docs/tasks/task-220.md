# Task 220 — Hỏng giữa tải — Điểm dừng D (M5-37)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 220). Loại việc (quy trình mục 3): A (bộ thử + tài liệu; Go chỉ ở công cụ `tools/loadkit`)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không có (gói `main` của `tools/loadkit`; thêm hằng `m5Chaos*`, `critM5Chaos*`, `critM5NoDupRef`, trường `m5Input.Dups`, hàm `m5Dups`, `m5ChaosErrRate`; không đổi chữ ký cũ).
- Gói phụ thuộc ngược (go list): không gói nào nhập `tools/loadkit` (gói main).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không. Bộ thử chỉ đọc `ord.orders` (đếm `external_ref` trùng) bằng psql trên database thử.
- Test sẽ chạy: `go test -race -run TestM5 ./tools/loadkit/` (mới: `TestM5ChaosPasses`, `TestM5ChaosFailures`); `bash -n` hai script; bài `m5:chaos` ở bể GitHub (số đo tạm).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không liên quan: công cụ thử tải); `loadtest/fb_chaos.sh` (mẫu hỏng giữa tải M4), `loadtest/fb_lib.sh` (kill_role/start_role/role_healthy), `loadtest/m5_run.sh` (dựng đợt, đo trước/sau, verify-flash, m5report), `k6/m5/flash_spike.js`.
- Dùng lại / viết mới vì: dùng lại hạ tầng m5_run.sh (bài `chaos` gọi `chaos-m5.sh`), `flash_spike.js` làm tải, `m5sale/m5stats/m5report`. Viết mới: kịch bản gây lỗi (không có sẵn) và ba chế độ chấm `chaos_*` trong m5report (tiêu chí khác bài thường).

## Soát thiết kế

- 5.x (kiến trúc): không áp dụng, không chạm mã ứng dụng; không module nào nhập công cụ.
- 6.x (đồng thời): không áp dụng cho mã sản phẩm. Với bộ thử: `chaos-m5.sh` không bao giờ chạy trên dự án compose `crm-shared*` (thoát 2); giết đúng PID của vai trò do bộ thử khởi động (`kill_role`), không kill theo tên.

## QĐ — Quyết định thay chủ shop

- QĐ-220-1 (Điểm dừng D, Step 4): chủ shop vắng, chốt theo lệnh /goal 28/11: không hỏi. Bản tóm tắt tiếng Việt viết vào `thu-tai-m5.md`; "xin OK chạy lại trên bản thử" ghi là "chờ server thử của hệ mới"; quyết định công bố flash sale (QĐ2) và số bộ điều tiết: đề xuất giữ mặc định (100 vé/giây một món, 300 cả shop, 64 lượt đang xử lý) và `web_key` 120.000/phút như QĐ-219-8; công bố chỉ sau khi chạy lại trên bản thử. Phương án khác: đổi số điều tiết ngay — không có số đo nào đòi hỏi.
- QĐ-220-2: website bản sao ở 4185 ("tạm ngưng", hàng đợi cục bộ, chạy tiếp ≤ 2 phút) không thuộc repo CRM và không dựng được trong bể: bước (e) chỉ chấm phía CRM (lượt mua nhận 503, verify-flash, không đơn trùng); phần website ghi "chờ server thử của hệ mới".
- QĐ-220-3: bước cách nhau: mỗi bước dài hơn 60 s (20 s chờ mở + ~60 s tải + 70 s chờ chỉ số + verify), nên khoảng 60 s là cận dưới; bước chạy nối nhau.
- QĐ-220-4: "cờ dựng lại ≤ 2 s [FS10]" không có bộ đo riêng: chấm gián tiếp bằng đúng 500 bought và tỷ lệ lỗi < 0,1% trong cả bước (bước a, b, c, e; bước d chỉ chấm nhất quán vì lượt đang bay lúc giết api mất kết nối); mốc gây/gỡ lỗi ghi ở report.md.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chấm chaos_valkey/api/pg đạt khi số liệu tốt | `TestM5ChaosPasses` | `go test -race -count=1 -run TestM5 ./tools/loadkit/` | ok (3,3 s) |
| Mỗi tiêu chí hỏng → KHÔNG ĐẠT (11 ca: bán thiếu, 500 hàng loạt, verify sai, đơn trùng, không thấy 503…) | `TestM5ChaosFailures` | như trên | ok |
| Chạy thật 5 bước ở bể | `m5_run.sh chaos` | `CRM_CI_LOADTEST=m5:chaos scripts/ci/remote-test.sh` | run 46135866042 (chấm bản đầu): bought 500 ×5, verify-flash mã 0 ×5, đơn trùng 0 ×5; sau review chấm bước e thêm bought và tỷ lệ lỗi nên bước e KHÔNG ĐẠT (52,5% lỗi khác 503), bước a–d ĐẠT, `docs/do-luong/thu-tai-m5.md` mục Lần 2 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bài chạy qua `m5_run.sh chaos` (không phải script độc lập) để dùng chung dựng vai trò, khóa dịch vụ, đo; `chaos-m5.sh` vẫn nằm ở `loadtest/k6/m5/` như kế hoạch.

## Việc phát hiện thêm

- Khi PostgreSQL ngừng 30 s (bước e, run 46135866042), 15.168 / 28.865 yêu cầu mua nhận mã khác 503 (k6 `buy_err`), chỉ 1.372 nhận 503 `retry_later`; kế hoạch muốn 503 để website hiện "tạm ngưng". Nghi: lỗi nối database (`pgconn` connect, hết hạn ngữ cảnh) không được đổi sang `db.ErrRetryLater` ở đường mua flash. Cần task riêng ở vùng cao (`platform/db`, `httpapi/errors*`); việc đầu tiên: chạy lại bước e với DEBUG=1 để biết mã thật.
- `verify-flash` kiểm đúng cho cả 5 bước; bộ thử chưa đo riêng thời gian dựng lại cờ ≤ 2 s [FS10] (QĐ-220-4).

## E2E viết sẵn, chưa chạy

Không có e2e Go/Playwright; bài thử tải chạy ở bể (không phải e2e).

## Reviewer

Kết luận: ĐẠT (lượt 2). Lượt 1 CẦN SỬA: RUNBOOK nói PostgreSQL ngừng thì 503 "không cần làm gì" trái số đo; chấm `chaos_pg` thiếu bought và tỷ lệ lỗi — đã sửa (thêm 2 ca test, tài liệu ghi bước e KHÔNG ĐẠT). Lượt 2: chỉ còn lệch số ca trong bảng bằng chứng (9 → 11), đã sửa; không còn phát hiện về tính đúng.
