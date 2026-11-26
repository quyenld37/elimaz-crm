# Task 500 — Sửa sau gộp M4 đợt 3: e2e xanh ở bể

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (task sửa sau gộp M4 đợt 3, ngoài bảng kế hoạch; nhánh gốc `m4`). Loại việc (quy trình mục 3): A

Mục tiêu: lượt `CRM_CI_E2E=1 scripts/ci/merge-test.sh` (toàn bộ test + `tests/e2e` + Playwright) xanh ở bể trên m4. Bốn lỗi của run 45867168803 (docs/tasks/task-499.md, Việc phát hiện thêm), sửa đúng gốc.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa hàm không xuất `liveFlow.call` (`tests/e2e/live_flow_test.go`, thêm header `Origin` cho lệnh ghi) và quyền cấp trong `newFBLatency` (`tests/e2e/fb_inbound_latency_test.go`, thêm `realtime.subscribe`).
- Gói phụ thuộc ngược (go list): `tests/e2e` là gói test (nhãn build e2e), không gói nào nhập.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: `console/src/screens/order-requests/RequestDetail.tsx` `StateBox` (khung "Yêu cầu đã đóng" thêm lớp có sẵn `wrap-any`), test `RequestDetail.test.tsx`; `console/e2e/carriers.spec.ts` thêm dữ liệu giả `GET /api/v1/web-carrier-settings` (khối `WebCarriers` nhúng trong `CarrierAccountsPage`) và tìm tên dài trong bảng `Tài khoản hãng` (tên có cả ở khối web); `console/e2e/inbox.spec.ts` thêm dữ liệu giả cho hội thoại đang mở (Task 175: chi tiết, tin, đọc, ai đang xem) và khung bên phải (Task 176: thẻ khách, order-context, cuộc gọi, người nhận giao, nhắc việc).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: vitest `src/screens/order-requests/`; tsc, eslint; go vet + golangci-lint `-tags e2e ./tests/e2e/`; ở bể: `CRM_CI_E2E=1 scripts/ci/merge-test.sh <tree> HEAD ALL ALL` (toàn bộ -race + e2e + console-e2e), lặp tới xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: hằng `stackOrigin` (`tests/e2e/stack_test.go`) và cách đặt `Origin` của `mot_don_di_tron_test.go:244`, `idempotency_test.go:85`; lớp CSS `wrap-any` (`console/src/styles.css`, đã dùng ở `dd` địa chỉ gửi trả cùng tệp); kiểu `WebCarrierSettings`, `WebCarrierStat` sinh từ OpenAPI.
- Dùng lại / viết mới vì: dùng lại cả ba; không thêm hàm, lớp hay token mới.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng: không đổi mã máy chủ, module, hợp đồng, database; chỉ sửa test e2e và một lớp CSS của màn hình.
- Quyền (5.x quyền): `realtime.subscribe` là quyền `ScopeNone` (`internal/modules/identity/permissions.go:46`): có hiệu lực toàn shop từ grant bất kỳ, kể cả grant theo Page, nên cấp trong cùng vai trò theo Page là đúng cách hệ thống thật cấp cho nhân viên chat; không nới quyền của máy chủ. Middleware Origin (`internal/httpapi/origin.go`) giữ nguyên: test phải gửi Origin như màn hình thật.
- 6.1–6.13: không áp dụng: không có mã chạy đồng thời mới.
- Màn hình: chỉ thêm `overflow-wrap: anywhere` qua lớp có sẵn cho khung đã đóng; không đổi màu, cỡ chữ, bố cục (không cần mockup: sửa lỗi tràn, đúng mockup cũ).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lỗi 1: lệnh ghi của `liveFlow.call` gửi `Origin` = `stackOrigin` như màn hình (middleware `origin.go` giữ nguyên) | `tests/e2e` `TestLiveFlow_CommentToConfirmedOrder` | ở bể, các run dưới | run 45867168803 (trước): `403 bad_origin`; run 45871293809 trở đi: job `e2e: success` |
| Lỗi 2: nhân viên chat theo Page có `realtime.subscribe` (ScopeNone) trong cùng vai trò | `tests/e2e` `TestFBInboundLatency` | như trên | trước: `mở luồng SSE: mã 403`; sau: `e2e: success` |
| Lỗi 3: màn Kết nối vận chuyển có dữ liệu giả `web-carrier-settings`; tên dài tìm trong bảng | `console/e2e/carriers.spec.ts` 3 khổ | như trên | trước: "yêu cầu tới API chưa khai"; run 45871293809: 1280 đỏ "strict mode violation … resolved to 2 elements" (tên ở bảng và khối web) → kiểm trong bảng; run 45872469978: qua |
| Lỗi 4: khung "Yêu cầu đã đóng" ngắt được chữ dài không dấu cách (lỗi thật của màn hình ở 360, không phải phông: lý do do nhân viên gõ hiện trong `.box` không có `overflow-wrap`) | vitest `RequestDetail.test.tsx` "đã đóng (Từ chối)…" (câu kiểm `.box` có `wrap-any`); Playwright `order-requests.spec.ts:213` 360 × 800 | `npx vitest run src/screens/order-requests/` | trước khi sửa: đỏ (1 failed); sau: `Tests 39 passed (39)`; Playwright ở bể: qua (run 45872469978, 45874345322) |
| Spec M4 mới: Inbox mở hội thoại ở 360/390 có dữ liệu giả của khung hội thoại (Task 175) và khung bên phải (Task 176) | `console/e2e/inbox.spec.ts` "mở hội thoại…" | ở bể | run 45871293809: đỏ thiếu `GET /conversations/{id}`, `POST …/presence`; run 45873354962 (sau gộp m4 có Task 176): đỏ thiếu `GET …/customer-card`; run 45874345322: qua |
| Màn hình: kiểu, lint, định dạng | tsc, eslint, prettier | `cd console && npx tsc --noEmit && npx eslint <tệp> && npx prettier --check e2e src/screens/order-requests` | không lỗi (run 45871293809 cổng tĩnh đỏ prettier `RequestDetail.test.tsx` → đã định dạng) |
| Go e2e biên dịch, vet, lint | `tests/e2e` | `go vet -tags e2e ./tests/e2e/ && golangci-lint run --build-tags e2e ./tests/e2e/` | `0 issues.` |
| Cổng tĩnh của task (không gói Go thường đổi; tests/e2e chỉ chạy ở lượt e2e) | ci-task | `ci-task.sh`, log ngoài worktree | run 45876485672: `static: success, 4 phút 14 giây`; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh c738996967d4…)` |
| **Toàn bộ + e2e + Playwright ở bể trên m4 c7389969 + phần của task (tree d9ad53d2)** | mọi gói (-race ALL), `tests/e2e`, `console-e2e` (Chromium mọi spec, WebKit .phone), cổng tĩnh | `CRM_CI_E2E=1 scripts/ci/merge-test.sh d9ad53d268f28f5db436202a294f58882d88dea4 HEAD ALL ALL` | **run 45874345322**: `static success 3 phút 28 giây`, `test (1) success`, `test (2) success`, `e2e success 1 phút 27 giây`, `console-e2e success 3 phút 58 giây`; `MERGE-TEST XANH (blacksmith)`; 20 phút máy blacksmith |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

- Lỗi 4 (tràn 163 px ở 360): kết luận là lỗi thật của màn hình, không phải do phông Linux — ở 390 ca chi tiết có cùng chuỗi dài trong lời nhắn của khách vẫn qua (khối đó có `overflow-wrap`), chỉ khung "Yêu cầu đã đóng" thiếu. Sửa bằng lớp có sẵn `wrap-any` (không token, màu, cỡ chữ mới; không cần mockup vì bố cục không đổi). Phương án khác: thêm quy tắc vào `order-requests.css` — bỏ vì đã có lớp dùng chung.
- Lỗi 2: cấp `realtime.subscribe` cho nhân viên thử thay vì nới yêu cầu quyền của `/api/v1/stream`: máy chủ đúng spec, nhân viên thật cần quyền này để nhận báo hiệu.
- Spec carriers: tên dài xuất hiện hai nơi là đúng hành vi (khối "Hãng trên website" liệt kê tài khoản ẩn); thu hẹp chỗ tìm vào bảng tài khoản, không bỏ câu kiểm.

## E2E viết sẵn, chưa chạy

(không có: mọi test e2e và Playwright đã chạy ở bể, run 45874345322.)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, không có phát hiện phải sửa). Reviewer đối chiếu: `realtime.subscribe` là ScopeNone nên cấp
trong grant theo Page đúng như nhân viên thật; `Origin` chỉ cho lệnh ghi, khớp `internal/httpapi/origin.go` và cách làm
của `idempotency_test.go`, `mot_don_di_tron_test.go`; mọi đường giả có trong `openapi.yaml`, mã trả (204 cho read,
presence POST) và kiểu khớp `types.gen.ts`; thu hẹp locator ở carriers không làm yếu câu kiểm; tree của run xanh
45874345322 chỉ khác phần đã stage ở tệp task. Ghi chú không bắt buộc (không sửa, chỉ ảnh hưởng dữ liệu giả của ca bố
cục): `order-context` giả lấy id Page làm `sellerId`; Page của hội thoại mở đang `degraded` mà `sendPolicy.allowed`.
