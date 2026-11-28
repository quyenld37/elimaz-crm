# Task 269 — Báo cáo sẵn sàng dữ liệu

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không đổi ký hiệu cũ. Thêm mới ở gói `reporting`: `LaunchReadiness`, `Readiness`, `ReadinessItem`, hằng `ReadinessOK/Warn/Fail` (0 chỗ tham chiếu cũ). Đổi thân `(*handler).GetLaunchReadiness` (stub 501 của Task 260 → thật): chỉ `gen.Handler` gọi nó (1 chỗ).
- Gói phụ thuộc ngược (go list): `reporting` chỉ được `internal/httpapi` và `cmd/crm` nhập; không gói nào khác đổi.
- Operation OpenAPI bị chạm, oasdiff: `getLaunchReadiness` (GET /api/v1/reports/launch-readiness) đã có hợp đồng ở Task 260; không đổi `openapi.yaml`, nên oasdiff không có chênh. Xóa dòng `getLaunchReadiness` khỏi `tests/arch/testdata/m7_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn của đêm mở để task màn sau); không đổi `console`.
- Câu sqlc, bảng, pha migration: `queries/reporting/readiness.sql` (6 câu đọc, chỉ đọc view). Migration `0711_readiness_views.sql` thêm MỘT view `catalog.images_processing_v` (thêm mới, pha expand, không `-- contract`) vì chưa view nào cho biết ảnh nào đang xử lý từ lúc nào (`catalog.product_readiness_v` chỉ có mã lý do, không có thời gian). Không bảng mới.
- Test sẽ chạy: `go test -race -count=1 -run '^TestLaunchReadiness' ./internal/modules/reporting/`, `TestGetLaunchReadiness` ở `internal/httpapi`, `tests/arch` (m7_pending, views) và bộ ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (observability.Summary cho sao lưu, diễn tập khôi phục, cảnh báo gấp; `tenancy.WithTenant`; `testkit.OpenStaff`; `reportseed` dựng Page, nguồn đơn, kho, biến thể, sổ tồn), `docs/views.md` (mọi view nguồn đã có: `catalog.product_readiness_v`, `inventory.warehouses_v`, `inventory.stock_ledger_v`, `channel.channel_accounts_v`, `conversation.{automation_rules,reply_templates,tags}_v`, `ord.traffic_sources_v`, `fulfillment.carrier_accounts_v`, `promotion.coupons_v`). `TenantOpsSummary` (Task 271) chưa có.
- Dùng lại / viết mới vì: dùng lại toàn bộ view có sẵn; chỉ viết mới `catalog.images_processing_v` (thiếu mốc thời gian) và hàm đánh giá. Không có chỗ nào trùng với mã khác (mỗi mục một luật riêng của báo cáo này).

## Soát thiết kế

- 5.x (khớp kiến trúc): `reporting` đọc module khác chỉ qua view (`catalog.*_v`, `channel.*_v`…) và hàm công khai của platform (`observability.Summary`), không nhập module khác, không đọc bảng gốc. Câu SQL tĩnh trong `queries/reporting/readiness.sql`. Handler mỏng. Lỗi không có (đọc thuần). Log: không ghi. Không dữ liệu cá nhân: chỉ số đếm và mã, test kiểm JSON không chứa tên, email gieo vào dữ liệu thử. Không gọi ngoài. Không thêm thư viện.
- 6.x (đồng thời): chỉ đọc, một transaction tenant của `WithTenant` (một ảnh dữ liệu cho mọi câu của lượt); không khóa; không ghi nên không cần idempotency; `observability.Summary` đọc bảng platform không RLS ngoài transaction đó (một lần, chỉ đọc). Hàm không có bước chạy lại có tác dụng phụ nên an toàn khi `RetryTx` chạy lại. Phép đếm rỗng đều qua `count(*)` (không NULL); ca tập rỗng có test.
- Mục thuộc task để sau (không có nguồn): `identity.enrollment_complete` (view `identity.direct_login_enrollment_v` của Task 253), `identity.no_grant_users` (Task 253), `conversation.t2_pass` (Task 266), phần mở gửi `live_since` của Task 267. Trả `warn`, Value "chưa có nguồn", Target "Task NNN để sau"; không giả `ok`.

## Quyết định thay chủ shop

1. Mục thiếu nguồn trả `warn` "chưa có nguồn" (đúng chỉ thị điều phối 28/11). Phương án khác: trả `ok` để báo cáo xanh (từ chối: giả sẵn sàng); bỏ mục khỏi báo cáo (từ chối: người xem tưởng đã kiểm). Khi Task 253, 266, 267 vào thì đổi sang luật thật, danh sách ở mục "Phụ thuộc task để sau".
2. `platform.urgent_open`: bảng `platform.alert_events` chỉ có "đã gửi", không có trạng thái "đã xử lý" nên "còn mở" = cảnh báo `urgent` gửi trong 24 giờ qua (cửa sổ của `observability.Summary`). Phương án khác: thêm cột đóng cảnh báo (ngoài phạm vi, đổi nền tảng).
3. `catalog.images_processing`: ảnh có tệp `uploaded` hay `processing` mà `updated_at` cũ hơn 30 phút; tạo view `catalog.images_processing_v` ở 0711. Phương án khác: đọc `platform.files` thẳng từ reporting (từ chối: phá ranh giới view).
4. Ca rỗng: không có sản phẩm nháp → `warn` (không có gì để mở bán); không có Page → `pages_connected` và `rules_prepared` đều `warn` "chưa nối Page"; không có tài khoản hãng `api` → `ok` (shop giao tay hợp lệ); không có kho `active` → `fail`. Đều đảo lại được bằng một hằng.
5. Kho "chưa mở sổ": kho `active` đã có dòng sổ tồn → `fail` đúng chữ của kế hoạch (báo cáo dùng trước đêm mở).

## Phụ thuộc task để sau

Đổi 4 mục sang luật thật khi nguồn có: `identity.enrollment_complete` và `identity.no_grant_users` (Task 253, view `identity.direct_login_enrollment_v`), `conversation.t2_pass` (Task 266, `Pass7Days` mọi Page), và phần `live_since`/mở gửi của `channel.pages_connected` (Task 267; hiện chỉ đọc `send_mode` của M4).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mục đỏ rồi xanh theo dữ liệu (kho, sản phẩm, Page, mã, hãng…) | `TestLaunchReadiness_FailThenOK` | `go test -race -count=1 -run '^TestLaunchReadiness_FailThenOK$' ./internal/modules/reporting/` | ok |
| Mục nền tảng (sao lưu, khôi phục, cảnh báo gấp) | `TestLaunchReadiness_Platform` | cùng lệnh, đổi tên test | ok |
| Shop rỗng (ca tập rỗng, không NULL) | `TestLaunchReadiness_EmptyShop` | cùng lệnh | ok |
| JSON không chứa dữ liệu cá nhân | `TestLaunchReadiness_NoPersonalData` | cùng lệnh | ok |
| Mục thiếu nguồn trả warn, không giả ok; đường HTTP + quyền | `TestLaunchReadiness_API` | `go test -race -count=1 -run '^TestLaunchReadiness_API$' ./internal/httpapi/` | ok |
| 3.000 biến thể đo ~18 ms | `TestLaunchReadinessPerf_3000Variants` (nhãn perf, không chạy ở ci) | `make perf-test` (đã chạy tay) | ~18 ms |
| Cổng tĩnh + test theo gói (lint, jscpd, gitleaks, kiến trúc: luật SQL đọc view, đếm view 57) | cổng ci-task | chạy ci-task trên github, run 46161846007 | CI-TASK XANH, 6/6 phần test, static xanh |
| Nhánh gộp main không vỡ build | (không có test riêng) | `go build ./... && go vet ./internal/modules/reporting/ ./internal/httpapi/` | qua |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận (commit gộp main): ĐẠT sau 2 vòng. Vòng 1 CẦN SỬA: `invEnv.secondWarehouse` trùng giữa `opening_test.go` (Task 262) và `env_test.go` (Task 264) làm gói test `inventory` không biên dịch; đã xóa bản ở `opening_test.go`, `go vet ./internal/modules/inventory/` sạch. Vòng 2 ĐẠT: giải xung đột `errors.go`, `errors_inventory.go` (giữ cả `Count` của 262 và `ExistingID` của 264), `m7_pending.txt` đúng.
Kết luận (commit mã Task 269): CHƯA REVIEW
