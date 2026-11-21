# Task 436 — M2-r7: Đồng hồ trong module order; quyền phiếu đổi

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (task bù review độc lập M2, dòng 436 của `danh-so-task-toan-cuc.md`; phạm vi B-05, B-06 trong `docs/review-doc-lap-m2.md`, cộng ưu tiên của điều phối viên: main đỏ ở `TestOrderRead_EditCancelTimeline`, gốc do Task 440 phân tích). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `identity.AuditEntry` thêm trường `At time.Time` (zero → `now()` của transaction như cũ). Thêm trường, không đổi chữ ký: 13 tệp dựng `identity.AuditEntry{…}` (literal có tên trường) không phải sửa; chỉ `handlers_order_edit.go` đặt `At`.
  - `background.Deps` thêm trường `Now func() time.Time` và phương thức `Clock()` (nil → `time.Now`). 2 chỗ dựng: `cmd/crm/modules.go` (giữ nil → giờ máy), `order/drafts_test.go`.
  - `order.DuplicatesConsumer` từ `struct{}` thành có trường `Now func() time.Time` (bắt buộc; nil → lỗi). 4 chỗ dựng: `order/module.go`, `order/duplicates_test.go` (2), `httpapi/handlers_order_duplicates_test.go`.
  - Không đổi: `order.Timeline`, `order.Writer.*` (vẫn nhận `now` từ bên gọi).
- Gói phụ thuộc ngược (go list): `background` ← cmd/crm, cash, customer, fulfillment, order, promotion; `identity` ← cmd/crm, adapters/cfaccess, httpapi, catalog, customer, platform/files, platform/realtime, testkit, tools/loadkit; `order` ← cmd/crm, httpapi, cash (+ refunds), fulfillment, testkit/codtest, testkit/shiptest.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Hành vi `createReturn` loại `exchange`: thêm 403 `forbidden` khi người gọi không có `order.create` ở kho của đơn gốc (mã `forbidden` đã có trong hợp đồng chung của mọi route). oasdiff: không chênh.
- Chỗ dùng trên màn hình: `console/src/screens/don/ReturnDialog.tsx` (nút "Đổi hàng" khóa khi không có `order.create`; lỗi 403 theo kho hiện qua `write.message` đã có), `OrderDetail.tsx` (truyền quyền).
- Câu sqlc, bảng, pha migration: `queries/identity/audit.sql` `InsertAuditLog` thêm `COALESCE(sqlc.narg(created_at)::timestamptz, now())` → `make gen` sinh lại `identity/internal/store`. Không migration (cột `created_at` đã có, mặc định giữ).
- Đồng hồ lớp HTTP: các handler đơn trong `handlers_order.go`, `_cancel.go`, `_confirm.go`, `_edit.go`, `_duplicates.go`, `_returns.go` đổi `time.Now()` → `h.now()` (Deps.Now, nil → `time.Now`; production không đổi). Không sửa `handlers_order_read.go` (tệp của Task 119 song song; ghi chú vẫn `time.Now()`, xem Việc phát hiện thêm), không sửa `errors.go` (Task 441).
- Test sẽ chạy: `go test -race ./internal/modules/order/...`, `./internal/modules/identity/...`, `./internal/platform/background/...`, `./internal/httpapi/ -run 'TestOrder|TestReturn|TestDuplicate'`; console `npm test -- ReturnDialog OrderDetail`, `tsc`, `eslint`; rồi `ci-task.sh` một lần (vùng rủi ro cao order, identity → cả bộ của vùng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục identity (`RecordAudit`, `ListEntityAudit`), platform/background; tìm `Now func() time.Time` (có ở `httpapi.Deps.Now`, `fulfillment.Deps.Now`, `RateLimit.Now`), `TxNow` (order `create.sql`), `orderWarehouseAt`, `identity.GrantedAt`.
- Dùng lại / viết mới vì: dùng lại `h.now()` (Deps.Now) cho mọi handler đơn thay vì thêm đồng hồ mới; dùng lại `orderWarehouseAt` (Task 411) để kiểm kho của đơn thay thế, `errForbiddenTarget` và `isOrderRefusal` (không lưu theo khóa); dùng lại `order.Get` để đọc kho đơn gốc. Mới: trường `AuditEntry.At` (không có cách nào khác để dòng audit mang mốc của bên gọi), `background.Deps.Now`/`Clock()` (cùng mẫu `fulfillment.Deps.Now`; worker chưa có đồng hồ nào để module nhận).

## Soát thiết kế

- 5.1–5.3: không đổi chủ dữ liệu; audit vẫn ghi qua `identity.RecordAudit` (public API), order không nhập identity.
- 5.4: kiểm quyền kho của đơn thay thế nằm trong cùng transaction của `createReturn`, trước `CreateReturn` (đọc không khóa `order.Get`, kho của đơn đã giao không đổi được).
- 5.5–5.6, 5.10, 5.11: không áp dụng (không sự kiện, lệnh, Valkey, thư viện mới).
- 5.7: mọi câu vẫn trong `WithTenant` / tx của worker như cũ.
- 5.8: module không nhập `net/http`; đồng hồ của worker đi qua `background.Deps` (gói platform), module không gọi `time.Now()` (xóa 2 chỗ của B-05).
- 5.9: không thêm đường ghi; `createReturn` đã có khóa chống trùng; 403 là từ chối không lưu theo khóa (`isOrderRefusal` có `errForbiddenTarget`).
- 5.12: theo mẫu Task 411 (`orderWarehouseAt`).
- 6.1–6.3: không thêm khóa; `order.Get` là đọc không khóa trước khóa đơn của `CreateReturn` (ADR-46 không đổi).
- 6.4–6.9: không áp dụng (không đổi đường lỗi tạm, việc nền, gọi ngoài; worker vẫn chạy lại được: `now` lấy lại mỗi lần Handle).
- 6.10: không đổi tiền.
- 6.11: quy luật "lịch sử đơn đúng thứ tự dù đồng hồ Go lệch database ±5 phút" có test (lệch hai chiều).
- 6.12: audit vẫn không chứa dữ liệu cá nhân; chỉ thêm mốc giờ.
- 6.13: mặc định từ chối: đổi hàng không có `order.create` ở kho → 403; phiếu trả thường không đổi.

## Quyết định thay chủ shop

- **Một đồng hồ cho lịch sử đơn = đồng hồ của lớp gọi (Go), không phải `now()` của PostgreSQL.** Kế hoạch M2 (Global Constraints "Đồng hồ") đã chọn: hàm nghiệp vụ nhận `now` từ lớp HTTP hoặc worker. Lịch sử trạng thái, ghi chú, `updated_at`, `cancelled_at` đều đã theo `now` đó; chỉ mục sửa (audit `order.update`) lấy `now()` của database. Nên sửa phần lẻ: `identity.AuditEntry.At` (zero giữ mặc định database, mọi chỗ khác không đổi), handler sửa đơn truyền cùng `now` đã đưa vào `Writer.Edit`. Phương án khác: ghi `occurred_at` bằng `now()` của transaction — phải đổi cả ghi chú, `updated_at`… và trái quy ước kế hoạch; bỏ. Đảo ngược dễ: bỏ trường `At`.
- **Worker nhận đồng hồ từ `background.Deps.Now` (B-05),** không dùng `TxNow`: hủy đơn nháp quá hạn ghi một dòng lịch sử trạng thái, dùng `now()` của database ở đó lại trộn hai đồng hồ trong lịch sử đơn. `cmd/crm` để nil → `time.Now` ở gói platform.
- **B-06: đơn thay thế của phiếu đổi cần `order.create` ở kho của đơn gốc** (như luật Task 411 cho `POST /orders`), ngoài `order.return` của route. Không đòi thêm `order.confirm`: tạo đơn `confirmed` qua `POST /orders` cũng chỉ cần `order.create`, giữ một luật cho hai đường. Phiếu khách trả thường (không tạo đơn) không đổi. Ghi vào `docs/quyen.md` (thay câu "không kiểm thêm"). Màn: nút "Đổi hàng" khóa khi không có `order.create` ở grant nào; có nhưng theo kho khác → máy chủ trả 403, hộp thoại hiện câu lỗi chung. Phương án khác: coi `order.return` là đủ — bỏ vì nhân viên Kho/ngoài kho giữ hàng được ở kho mình không có quyền lên đơn (đúng lỗ D-02 đã đóng). Đảo ngược dễ: bỏ một lần gọi `orderWarehouseAt`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lịch sử đơn một đồng hồ: Go lệch PostgreSQL +5 phút và −5 phút, tạo → sửa → hủy vẫn mới nhất trước (hủy, sửa, tạo) và mọi mốc (cả mục sửa) theo đồng hồ máy chủ ứng dụng | `TestOrderTimeline_ClockSkew` (`internal/httpapi/order_clock_test.go`, 2 ca) | `go test -race -count=1 -run 'TestOrderTimeline_ClockSkew\|TestOrderRead_EditCancelTimeline' ./internal/httpapi/` | Trước sửa: FAIL cả 2 ca (mục sửa đứng sai chỗ, mốc theo `now()` của database). Sau sửa: ok. Kiểm ngược: bỏ `At: now` ở `handlers_order_edit.go` → FAIL `5m0s`, `-5m0s`; trả lại → ok. `TestOrderRead_EditCancelTimeline` (đỏ trên main) ok |
| `AuditEntry.At` khác zero lưu đúng mốc đó (±5 phút so với `now()` của transaction) và đọc theo mốc đó; zero → `now()` của transaction | `TestRecordAudit_At` (`identity/audit_list_test.go`) | `go test -race -count=1 -run 'TestRecordAudit\|TestListEntityAudit' ./internal/modules/identity/` | ok |
| B-05: việc hủy đơn nháp quá hạn dùng giờ của `background.Deps.Now` (không `time.Now` trong module); dòng hủy mang đúng giờ đó | `TestExpireDraftsSchedule` (sửa: đơn tạo năm sau, đồng hồ = tạo + 48 giờ, kiểm `CancelledAt` = đồng hồ) | `go test -race -count=1 -run 'TestExpireDrafts\|TestDuplicates' ./internal/modules/order/` | ok (với giờ thật đơn năm sau không quá hạn, nên chỉ qua khi dùng đồng hồ của Deps) |
| B-05: consumer đơn trùng lấy mốc cặp từ `Now` được giao; thiếu `Now` → lỗi, không ghi cặp | `TestDuplicates_ConsumerClock` | như trên | ok |
| `Deps.Clock()`: Now khi có, nil → giờ máy | `TestDepsClock` | `go test -race -count=1 ./internal/platform/background/` | ok |
| B-06: phiếu đổi cần `order.create` ở kho của đơn gốc: chỉ `order.return` → 403; `order.create` theo kho phụ → 403; không phiếu, không giữ hàng; phiếu khách trả chỉ với `order.return` → 201; theo đúng kho chính → 201; được cấp rồi gửi lại cùng khóa → 201 (không lưu theo khóa) | `TestReturnAPI_ExchangeWarehouseScope` (`handlers_order_returns_scope_test.go`) | `go test -race -count=1 -run 'TestReturnAPI' ./internal/httpapi/` | Trước sửa: FAIL (chỉ `order.return` tạo được phiếu đổi 201, giữ 1). Sau sửa: ok; `TestReturnAPI_Exchange`, `_Flow`, `_ReceiveQty` vẫn ok |
| Màn: không có `order.create` → nút "Đổi hàng" khóa kèm câu giải thích, "Khách trả hàng" vẫn chọn được; có `order.create` nhưng máy chủ 403 → hộp thoại hiện "Bạn chưa được cấp quyền làm việc này", không đóng | `ReturnDialog.test.tsx`: "chỉ có order.return: khóa Đổi hàng…", "order.create theo kho khác: máy chủ trả 403…" (ca "đổi hàng: cần chọn hàng đổi" thêm grant `order.create`) | `cd console && npx vitest run src/screens/don && npx tsc --noEmit && npx eslint src/screens/don` | 7 tệp, 57 test qua; tsc, eslint sạch |
| Cổng kiểm của task | — | `ci-task.sh` (sau khi stage); gói order chạy riêng `go test -race -count=1 -timeout 30m ./internal/modules/order/...` | Cổng tĩnh: build, vet, lint, gen-check, sqlc-vet, catalog-check, gitleaks, oasdiff, test-guard, console-check qua; jscpd báo 1 đoạn trùng mới (tạo kho phụ trong `handlers_order_returns_scope_test.go` giống `labels_test.go`) → viết lại bằng `tx.Exec` với id tạo sẵn, `make jscpd`: 0 clone. Test: mọi gói qua trừ (a) `internal/modules/order` hết trần 10 phút chung của `go test` khi máy bận (test đang chạy lúc cắt: `TestMarkDeliveredPartial_FromHeld` mới 3 s, không treo) — chạy riêng với trần 30 phút: `ok elimaz.com.vn/crm/internal/modules/order 426.898s`; (b) `TestDeepHealth` của observability — ngoài phạm vi, xem Việc phát hiện thêm. `internal/httpapi` ok, `internal/modules/identity` ok 290 s, `platform/background` ok, `tests/arch` ok, `tests/e2e` ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `handlers_order_read.go` (`AddOrderNote`) còn gọi `time.Now()` thay `h.now()` cho mốc ghi chú (một mục của lịch sử đơn). Không sửa vì tệp đang thuộc Task 119 chạy song song; production cùng đồng hồ máy (h.now nil → time.Now) nên thứ tự không sai, chỉ test không cắm được giờ. Đổi một dòng sau khi 119 gộp.
- Các audit khác của đơn (`order.create`, `order.confirm`, `order.cancel`…) vẫn lấy `now()` của database: không thành mục của lịch sử đơn (chỉ audit có `after.fields` thành mục sửa), nên không ảnh hưởng thứ tự. Nếu sau này thêm `fields` cho audit khác thì phải đặt `At` như `order.update`.
- Cổng kiểm (cả hai lần chạy): đỏ `TestDeepHealth` (`internal/platform/observability`, `WorkerHeartbeatAgeS` > 30 — đồng hồ máy ảo PostgreSQL lệch đồng hồ Mac, cùng loại lỗi Task 440; gói không nhập gói nào task này đổi; đỏ lại ×3 khi chạy riêng, vùng của Task 445) và, ở lần đầu, `TestExpiredLeaseRetry` (`internal/platform/queue`, chạy riêng ×3 qua, lần sau cả gói qua: chập chờn khi máy bận). Không sửa (ngoài phạm vi). Giữa chừng PostgreSQL chung cổng 5600 từ chối kết nối khoảng 10 phút (13:20–13:31) rồi tự lên lại; phiên này không động vào bộ chung.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên `git diff --cached`, 27 tệp).

- Phát hiện phải sửa: không có.
- Đã kiểm: `AuditEntry.At` (bảng `audit_logs` không partition, không trigger theo `created_at`; chỉ `handlers_order_edit.go` đặt `At`); mã sqlc sinh lại khớp nguồn; `background.Deps.Now`/`Clock()` và 2 chỗ dựng; mọi chỗ dựng `DuplicatesConsumer` đã có `Now` (trừ ca test cố ý thiếu); không còn `time.Now()` trong `internal/modules/order`; handler đơn đổi đủ sang `h.now()` trừ `handlers_order_read.go:172` (ràng buộc Task 119, đã ghi); `exchangeWarehouseAllowed` chạy trước `CreateReturn`, `errForbiddenTarget` không lưu theo khóa, 403 đã ánh xạ sẵn ở `errors.go` (không sửa); không chạm các tệp của Task 119 và `errors.go`; mỗi dòng bằng chứng trỏ tới test có thật và kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc (không sửa): luật quyền B-06 là quyết định thay chủ shop, cần chủ shop xác nhận (đảo ngược: bỏ một lần gọi); câu gợi ý trên màn xét `order.create` ở mọi gán nên người có quyền ở kho khác gặp 403 lúc gửi (đúng như mục Quyết định); `AddOrderNote` dùng `time.Now()` sửa sau khi Task 119 gộp.
