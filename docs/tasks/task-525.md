# Task 525 — Sửa e2e đỏ: flash 429 too_fast, đảo Thành công hãng giả 422

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task điều phối giao, không có trong tệp PLAN; số toàn cục 525). Loại việc (quy trình mục 3): A

Nguồn: lượt e2e Go trên main 020b5403 (GitHub run 46112628317) đỏ 2 test.

## Chẩn đoán gốc rễ

1. **TestFlashBuyHTTP_ValkeyDownAndTrace** (30 bought, 38 sold_out, 132 lượt 429 too_fast, 0 lỗi 5xx).
   - Mỗi khách giả trong test đã có SĐT riêng (`09300000NN`) và IP riêng (`10.20.x.y`). Cổng (`internal/httpapi/admission.go`)
     băm khóa theo `SubjectKey("phone", SĐT đã chuẩn hóa)` và `SubjectKey("ip", IP)` — khóa đúng theo từng khách, không chung.
   - Website giả gửi lại sau 5–25 ms mỗi khi 503/409, **bỏ qua Retry-After**, tối đa 2000 lần. Ngưỡng QĐ23: 10 lượt/10 s mỗi
     SĐT (khi Valkey dừng: 5 lượt/10 s). Khách bị 503 vài lần liền vượt ngưỡng của chính mình → 429 too_fast (câu trả lời cuối).
   - Spec 07-adr (ADR-41, bảng 503 retry_later): "website gửi lại với cùng khóa **sau `Retry-After`**". Website giả trái hợp
     đồng; mã đúng. Kết luận: **sửa test** (website giả chờ đúng Retry-After), không đổi mã. Website thật (Task 204) phải chờ
     Retry-After — đã là hợp đồng.
2. **TestM3ReverseDelivered_CarrierCorrected** (hãng giả `advance returning` → 422 invalid_status).
   - `tools/fakecarrier/server/admin.go` chỉ nhận `to` ∈ {created, picked_up, delivered} (danh sách viết tay từ M1), dù bộ nối
     `internal/adapters/carrier/fake` đã biết đủ 10 trạng thái thô (`MapCanonical`, có `returning`, `returned`…). Kịch bản
     TD10 (hãng sửa "đã giao" thành "đang hoàn") cần hãng giả đổi kiện sang `returning`. Lỗi nằm ở **hãng giả**; mã 118 và
     trình tự trạng thái của test đúng spec.
   - Sửa: Task 139 (đã vào main 1086279f, gộp vào nhánh này ở a28a7c81) đã sửa đúng chỗ này (`advanceTargets()`, 10 trạng
     thái thô, test `TestAdvanceEveryRawStatus`). Bản sửa riêng của phiên 1 (`knownRaw`, `TestAdvanceAllRawStatuses`) trùng
     việc nên bỏ, dùng bản main. Task này không còn đổi `tools/fakecarrier`.
3. **Lỗi thứ ba lộ ra sau khi (1) qua bước đếm:** `TestFlashBuyHTTP_ValkeyDownAndTrace` đếm `inventory.flash_sale_units`
   bằng `s.count` (pool chủ bảng, ngoài ngữ cảnh tenant) → RLS cho 0 dòng ("0 đơn vị suất đã lấy, muốn 30"). Bẫy RLS của
   CLAUDE.md. Sửa: đếm trong `tenancy.WithTenant(owner, tenant)`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa mã test: `tests/e2e/flash_http_test.go` (website giả, câu đếm), `internal/httpapi/admission_test.go` (test mới).
- Gói phụ thuộc ngược (go list): không có — chỉ tệp `_test.go`, không gói nào nhập.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `TestFlashAdmission_LimitsPerSubject` (mới) của internal/httpapi; e2e từ xa: `TestFlashBuyHTTP_ValkeyDownAndTrace`, `TestM3ReverseDelivered_CarrierCorrected` (job e2e Go, remote-test.sh); ci-task theo gói đổi.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm "danh sách trạng thái thô hãng giả"); tìm `RawReturning`, `MapCanonical` → `internal/adapters/carrier/fake/ops.go`.
- Dùng lại / viết mới vì: hãng giả dùng lại bản sửa của Task 139 trên main (`advanceTargets`), không viết bản thứ hai; test website giả dùng lại `post`/`website` sẵn có, chỉ thêm đọc Retry-After; test mới dùng lại `newAdmitEnv`, `flashBody`, `errCode` của admission_test.go; câu đếm dùng `tenancy.WithTenant` như `orderRun.count`.

## Soát thiết kế

- 5.1–5.9, 5.11: không áp dụng: không thêm dữ liệu, bảng, đường ghi, thư viện; chỉ sửa hãng giả (công cụ thử) và test.
- 5.10 (Valkey chỉ điều tiết): giữ nguyên — giới hạn tốc độ vẫn chỉ ở cổng, không quyết định nghiệp vụ.
- 5.12: test theo mẫu sẵn có của gói (admission_test.go, server_test.go).
- 6.2 (gửi lại): website giả gửi lại cùng Idempotency-Key sau Retry-After đúng ADR-41.
- 6.7 (bên kia báo quá tải): chính là điểm sửa — bên gọi lùi theo Retry-After.
- 6.12 (dữ liệu cá nhân): test mới không log SĐT/IP; checkSpans giữ nguyên.
- 6.1, 6.3–6.6, 6.8–6.11, 6.13: không áp dụng: không đổi transaction, khóa, tiền, quyền.

## Quyết định thay chủ shop

- QĐ-525-1: Lỗi (1) sửa ở test, không nới ngưỡng QĐ23 và không bỏ đếm lượt gửi lại khỏi giới hạn theo SĐT. Lý do: spec bảo website gửi lại sau Retry-After; lượt gửi lại đúng nhịp (≥ 1 s) nằm dưới 10/10 s. Phương án khác: không đếm lượt X-Attempt ≥ 2 vào giới hạn SĐT — bị loại vì kẻ phá sẽ đặt X-Attempt 2 để vượt giới hạn.
- QĐ-525-2: Lỗi (2) lấy bản sửa của Task 139 trên main (cùng kết quả: hãng giả nhận đủ 10 trạng thái thô), bỏ bản của phiên 1 để không có hai danh sách. Phương án khác: giữ `knownRaw` (dẫn từ `MapCanonical`) — bị loại vì đụng xung đột với main và trùng test.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Cổng giới hạn theo từng khách (200 khách khác SĐT/IP như e2e: 2 lượt mỗi người đều 200, lượt 3 → 429 too_fast của riêng người đó) — xác nhận chẩn đoán (1): mã đúng, test sai | TestFlashAdmission_LimitsPerSubject | `go test -count=1 -run '^TestFlashAdmission_LimitsPerSubject$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 0.714s` (xanh ngay, đúng mong đợi: không có lỗi mã) |
| Trước sửa: hai test đỏ | TestFlashBuyHTTP_ValkeyDownAndTrace, TestM3ReverseDelivered_CarrierCorrected | lượt e2e main 020b5403 | GitHub run 46112628317: 30 bought, 38 sold_out, 132× 429; `advance returning` 422 |
| Sau sửa (1) chờ Retry-After: qua bước 30/170, lộ lỗi RLS câu đếm | TestFlashBuyHTTP_ValkeyDownAndTrace | `CRM_CI_E2E=1 CRM_CI_STATIC=false CRM_CI_RUNNER=github scripts/ci/remote-test.sh <tree 9c41034f> 48356ba9 <tệp rỗng>` | run 46129930700: `flash_http_test.go:54: 0 đơn vị suất đã lấy, muốn 30`; TestM3ReverseDelivered_CarrierCorrected không còn trong danh sách FAIL |
| Bản cuối (đã gộp main a28a7c81): hai test xanh | TestFlashBuyHTTP_ValkeyDownAndTrace, TestM3ReverseDelivered_CarrierCorrected | `CRM_CI_E2E=1 CRM_CI_STATIC=false CRM_CI_RUNNER=github scripts/ci/remote-test.sh <tree 4aa4a9fd> a28a7c81 <tệp rỗng>` | run 46130526877: danh sách `--- FAIL` của job e2e chỉ còn 4 test M3 của Task 139 (dưới "Việc phát hiện thêm"), không có hai test này |
| Cổng tĩnh + test gói đổi (sau khi bỏ một chú thích `//nolint:wrapcheck` thừa ở flash_http_test.go:56 do lint báo — chỉ đổi chú thích, không đổi mã so với tree 4aa4a9fd) | ci-task | `scripts/agent/ci-task.sh` | lượt 1 run 46131239714: `nolintlint … unused`; lượt 2 run 46132133486: `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh a28a7c81)` |

## E2E viết sẵn, chưa chạy

(không có — hai test e2e đã có sẵn, chạy từ xa ở bằng chứng)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Bốn e2e mới của Task 139 (vừa vào main 1086279f) đỏ ở run 46130526877, ngoài phạm vi task này, chưa sửa:
  - `TestM3Chaos_StatementBatchRestart` — `m3_full_flow_test.go:277: GET /api/v1/cod-statements/…?limit=1 = 429 rate_limited` (giới hạn tốc độ của người dùng console trong stack e2e).
  - `TestM3FullFlow` — `m3_full_flow_test.go:61: sổ COD sau chốt bảng kê = carrier_holding:280000 sales_clearing:-280000 to_collect:0, phí 25000` (số dư sổ COD khác mong đợi).
  - `TestM3FullFlow_CarrierReturn` — `m3_full_flow_test.go:106: quá 5s chờ vận đơn returned`.
  - `TestM3TwoSellers` — người bán khác đọc được vận đơn (`… status:delivered …, muốn 404`).
- console-e2e (Playwright) đỏ 46 ca ở cả hai run; theo chủ shop 28/11 không chạy Playwright, bỏ qua.

## Reviewer

Kết luận: ĐẠT (lần 1, trên bản đã gộp main a28a7c81). Không có phát hiện phải sửa. Ảnh hưởng khớp (chỉ hai tệp test, không ký hiệu xuất ra ngoài đổi, `tools/fakecarrier` không đổi). Đối chiếu spec 07-adr.md:1210 ("website gửi lại với cùng khóa sau `Retry-After`"): sửa website giả, không nới ngưỡng là đúng. Số lượt gửi lại tối đa 2000 → 100 là chặt hơn, không nới.
Ghi chú không bắt buộc của reviewer: 409 (yêu cầu cùng khóa đang chạy) không có Retry-After nên website giả vẫn gửi lại sau 5–25 ms và lượt đó vẫn tính vào giới hạn SĐT — nếu test sau này chập chờn 429 thì nghi chỗ này trước; `TestFlashAdmission_LimitsPerSubject` chỉ chứng minh khóa theo từng khách, bằng chứng chính cho chẩn đoán (1) là hai lượt e2e từ xa.
