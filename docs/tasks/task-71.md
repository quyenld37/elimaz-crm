# Task 71 — M2-14: Quản lý mã giảm giá và đợt mã

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-14). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** trong `promotion`, không sửa chữ ký có sẵn (`Coupon`, `Evaluate`, `Redeem`, `Release`, `Detach`, `Reprice`, `Check`, `CheckResult`, bốn sentinel có sẵn giữ nguyên; chỗ dùng hiện có: `cmd/crm/modules.go` (Background), `httpapi/errors.go`, test của promotion). Thêm: kiểu `Actor`, `Writer{Realtime}` với `CreateCoupon`, `UpdateCoupon`, `CreateBatch`; kiểu `CouponInput`, `CouponPatch`, `CouponChange`, `CouponTemplate`, `BatchInput`, `Batch`, `CouponFilter`, `RedemptionRow`; hàm đọc `GetCoupon`, `ListCoupons`, `GetBatch`, `ListRedemptions`, `CustomerRoot`; `(Coupon).RemainingUses`, `(Coupon).AuditFields`, `(Coupon).Bound`, hằng `CodeAlphabet`, hàm `NewCode`; sentinel mới `ErrNotFound`, `ErrVersionConflict`, `ErrInvalidInput` (Q-T71-2). `httpapi`: tám handler thay `UnimplementedHandler` (listCoupons, createCoupon, getCoupon, updateCoupon, createCouponBatch, getCouponBatch, listCouponRedemptions, checkCoupon), ba dòng vào `promotionError`, trường mới `Deps.Now` (nil → `time.Now`, Q-T71-3; trường tùy chọn nên chỗ dựng `Deps` ở `cmd/crm`, `serve_test.go` không phải sửa).
- `order` (thêm, Q-T71-8): hàm đọc `order.Codes(ctx, tx, sellerID, ids)` (chưa có chỗ dùng cũ) và câu sqlc `OrderCodes` trong `queries/ord/code.sql` (lọc `seller_id`, ADR-29).
- Gói phụ thuộc ngược (go list): `promotion` ← `cmd/crm`, `internal/httpapi` (order chưa nhập promotion trên nhánh này); `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest` (chỉ thêm hàm, không đổi chữ ký); `httpapi` ← `cmd/crm`, `tests/e2e`, `tests/arch`.
- Operation OpenAPI bị chạm, oasdiff: tám operation trên (khai ở Task M2-02, đang 501): chỉ bỏ câu "Chưa làm…" trong `description`; không đổi schema, không mã lỗi mới (`code_taken`, `coupon_locked_after_use`, `max_uses_below_used`, `invalid_phone`, `invalid_input`, `not_found`, `version_conflict` đã có). Không phá vỡ.
- Chỗ dùng trên màn hình: chưa có (màn Mã giảm giá là Task M2-30). `console/src/api/**` sinh lại theo hợp đồng.
- Câu sqlc, bảng, pha migration: migration mới `0209_promotion_manage.sql` (pha mở rộng, chỉ `GRANT UPDATE (cột)` trên `promotion.coupons` cho `crm_app`: name, discount_kind, value, max_discount, min_subtotal, channels, max_uses, per_customer_limit, release_on_cancel, valid_from, valid_to, status, updated_at, version; Q-T71-1). Tệp mới `queries/promotion/manage.sql`: `InsertCoupon` (ON CONFLICT DO NOTHING), `InsertCouponsBulk` (unnest, ON CONFLICT DO NOTHING), `InsertBatch`, `GetBatch`, `CouponByID`, `HasRedemptions`, `UpdateCoupon`, `ListCoupons`, `ListRedemptions` (chỉ bảng promotion; mã đơn do lớp HTTP ghép qua `order.Codes`), `CustomerRoot` (đọc `customer.customers_v`), `CouponByID`, `HasRedemptions`, `GetBatch`. Thêm `OrderCodes` ở `queries/ord/code.sql`. Câu có sẵn trong `coupons.sql`, `redeem.sql` giữ nguyên (`CouponByIDForUpdate` dùng lại cho sửa).
- Test sẽ chạy: `go test -race ./internal/modules/promotion/...`, `go test -race ./internal/modules/order/ -run TestCodes`, `go test -race ./internal/httpapi/ -run 'Coupon|DomainErrors|ErrorCode|M2'`, `tests/arch` (errors_mapped, m2contract, sqlusage, sellerfilter, lint), rồi cổng ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db, tenancy, realtime, idempotency); đường ghi HTTP `refusableWrite` + `h.audit` (`refusal.go`, `handlers_inventory.go`), `catalogRead` cho đọc theo tenant (như Task 72); `customer.NormalizePhone` cho SĐT gắn mã; `catalog.DefaultSeller` (như `readAsDefaultSeller`) cho người bán của đường đọc đơn; `promotion.Check`, `Evaluate`, `couponFrom`, `CouponByIDForUpdate` có sẵn (Task 63); mẫu báo hiệu realtime của module không nhập identity (`inventory/movement.go`: hằng quyền chưa định kiểu); mẫu sửa có version `order.UpdateSource`; phân trang theo id (`inventory.ListDocuments`); `identity.DefaultRoles` + `rolePerms`, `serveAs`, `expect`, `decodeJSON` trong test.
- jscpd: test HTTP dùng lại `mustOK` và `shiptest.Shop.Order` (bỏ đoạn chép); hàm `signal` của promotion giống khuôn của customer — giữ riêng (chủ đề, quyền, loại entity của module), ghi ứng viên gộp ở "Việc phát hiện thêm".
- Dùng lại / viết mới vì: viết mới phần nghiệp vụ riêng của promotion (sinh mã `crypto/rand` với bảng chữ riêng, đợt mã, khóa sửa sau khi dùng) — logic của module, ở lại trong module; không có bộ sinh mã ngẫu nhiên chung trong platform (order sinh mã đơn theo sequence, khác việc).

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: mã, đợt mã, lượt dùng thuộc `promotion`.
- 5.2 Không ghi bảng module khác: chỉ ghi `promotion.*`; audit ghi bằng `identity.RecordAudit` ở lớp HTTP (promotion không được nhập identity, `tests/arch/boundaries_test.go`).
- 5.3 Đọc module khác: `customer.customers_v` (root_id) qua view đã công bố. Mã đơn: promotion không được gọi order (04 mục 3, `tests/arch` sqlusage) nên lớp HTTP gọi public API `order.Codes` (Q-T71-8).
- 5.4 Nguyên tử: tạo/sửa/đợt mã, audit, báo hiệu, kết quả chống trùng trong một transaction (`WriteTx`).
- 5.5 Việc sau commit: báo hiệu realtime `promotion.coupon` qua `Bus.Publish(tx)` (chỉ đi khi commit); không sự kiện nội bộ (không ai nghe mã ở M2).
- 5.6 Gọi ra ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; INSERT lấy `tenant_id` từ `current_setting('app.tenant_id')`; bảng có RLS từ 0206.
- 5.8 Nghiệp vụ không biết HTTP: promotion không nhập net/http, ogen.
- 5.9 Đường ghi có khóa chống trùng và quyền: hợp đồng có `IdempotencyKey`, `x-permission: promotion.manage`; đọc `promotion.read`; kiểm mã `order.create`.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không (`crypto/rand` của thư viện chuẩn).
- 5.12 Theo module mẫu: đường ghi là phương thức `Writer`, sentinel trong `promotion/errors.go`, đổi HTTP ở `errors.go`, test gói `_test` trên PostgreSQL thật.

Mục 6 (đúng khi chạy đồng thời):

- 6.1 Hai người cùng sửa: `UpdateCoupon` khóa dòng mã `FOR UPDATE` rồi so `version`; `UPDATE … WHERE version = $v`. Sửa mã cùng lúc với `Redeem`: cả hai khóa dòng mã trước (ADR-46 nhóm 10), nên `used_count` và "đã có lượt" đọc sau khóa là đúng; CHECK `used_count <= max_uses` của bảng là chốt cuối. Hai người tạo cùng mã: unique `(tenant_id, code)` + `ON CONFLICT DO NOTHING` → bên sau `ErrCodeTaken`, không làm hỏng transaction.
- 6.2 Gửi lại: `Idempotency-Key` qua WriteTx; từ chối nghiệp vụ không lưu (refusableWrite).
- 6.3 Thứ tự khóa: sửa mã chỉ khóa một dòng `coupons` (đọc `coupon_redemptions` không khóa, sau khi đã khóa mã — đúng thứ tự nhóm 10). Tạo đợt: chèn `coupon_batches` rồi `coupons` (đúng thứ tự nhóm 10).
- 6.4 Lỗi tạm: `WriteTx`/`db.RetryTx`; hàm gán lại kết quả mỗi lần chạy; mã sinh lại mỗi lần chạy (không cộng dồn).
- 6.5 Mức cách ly: mặc định cộng khóa dòng.
- 6.6–6.8: không áp dụng: không việc nền, không gọi ra ngoài.
- 6.9 Treo lâu: timeout mặc định của transaction API; đợt lớn nhất 100.000 mã = 100 câu chèn 1.000 dòng.
- 6.10 Tiền: `int64` đồng; `value` phần trăm 1–10000 phần vạn; kiểm khoảng trước khi ghi, CHECK là chốt cuối.
- 6.11 Quy luật: rapid cho sinh mã (mọi mã khớp bảng chữ, dài 8) và khóa sửa sau khi dùng (đổi trường bị khóa luôn bị từ chối, trường được phép luôn qua).
- 6.12 Dữ liệu cá nhân: SĐT gắn mã không vào audit, log, báo hiệu, câu trả lời (chỉ `bound: true`); lượt dùng trả mã đơn, không SĐT.
- 6.13 Quyền: kiểm ở API (bảng route, guard); test `sale` tạo mã 403, kiểm mã 200.

## Quyết định thay chủ shop

- Q-T71-1: Quyền UPDATE theo cột cho sửa mã nằm ở migration mới `0209_promotion_manage.sql` (dải M2 đã cấp sẵn 0200–0208; 0207, 0208 thuộc Task M2-10, M2-13 chưa gộp, nên lấy số kế tiếp 0209 để không đụng; migrate chấp nhận lệch thứ tự từ Task 90). Khác: sửa 0206 (cấm, đã commit).
- Q-T71-2: Thêm ba sentinel `promotion.ErrNotFound` (404), `ErrVersionConflict` (409), `ErrInvalidInput` (400) — mã HTTP có sẵn, không thêm mã lỗi. Kế hoạch chỉ khai bốn sentinel cho promotion nhưng sửa có version, mã không có, đầu vào sai là bắt buộc theo luật chung (mục 128 của kế hoạch). Khác: mượn sentinel của module khác (trái luật "lỗi nằm trong module sinh ra nó").
- Q-T71-3: `checkCoupon` cần "hôm nay" để test KM01/KM02 ngày 15/12/2026 và 01/01/2027: thêm `Deps.Now func() time.Time` (nil → `time.Now`), như `RateLimit.Now`. Khác: đọc `now()` của database (không cắm được trong test).
- Q-T71-4: "Đổi trường khác" khi mã đã có lượt: chỉ tính trường gửi lên **có giá trị khác** giá trị hiện tại (màn hình gửi cả form không bị chặn oan). Trường được phép: `name`, `validTo`, `maxUses`, `status`.
- Q-T71-5: Đợt mã có tiền tố: mã = `<tiền tố>-<8 ký tự>` (ví dụ `SN-ABCD2345`, tối đa 17 ký tự, khớp CHECK); không tiền tố: 8 ký tự. Có `boundPhones`: `quantity` phải bằng số SĐT; SĐT sai → 422 `invalid_phone` (không nêu số); SĐT trùng sau chuẩn hóa → 400 `invalid_input`. Mỗi mã của đợt `max_uses 1`.
- Q-T71-6: Chủ đề báo hiệu realtime là `promotion.coupon` (đặt theo cách `catalog.product`, `inventory.balance` mà console đang dùng; kế hoạch gọi là "coupon.changed"), quyền `promotion.read`.
- Q-T71-8: Danh sách lượt dùng cần mã đơn, nhưng promotion không được đọc `ord.orders_v` (order gọi promotion; luật 04 mục 3, `TestSQLUsageProductCode` đỏ). Chọn: promotion trả `OrderID`, lớp HTTP gọi hàm mới `order.Codes(ctx, tx, sellerID, ids)` với người bán mặc định của shop (cùng cách mọi đường đọc đơn `readAsDefaultSeller`, ADR-29; câu có `seller_id`, không cần miễn luật `sellerfilter`). Đơn của người bán khác (chưa có ở M2) trả `orderCode` rỗng. Khác: thêm dòng miễn trong `tests/arch/sellerfilter_test.go` cho câu không lọc người bán (là nới luật kiểm, không chọn); chép mã đơn vào `coupon_redemptions` (đổi schema và Redeem, ngoài phạm vi).
- Q-T71-7: `customerId` của `checkCoupon`, `boundCustomerId` của `createCoupon`: đổi về hồ sơ gốc qua `customer.customers_v`; khách không có → 400 `invalid_input`. `checkCoupon` không có người bán của đơn: dùng "không người bán" (mọi mã hôm nay `seller_id` rỗng, Q10).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| KM01: tạo SALE10 → 201; kiểm `sale10`, base 660000, manual ngày 15/12 → ok, giảm 50.000, còn 2 lượt | `TestCouponAPI_CreateAndCheck` (httpapi) | `go test -race ./internal/httpapi/ -run 'Coupon\|DomainErrors\|ErrorCode\|M2'` | `ok elimaz.com.vn/crm/internal/httpapi 5.120s` |
| KM02: cùng mã ngày 01/01/2027 → `expired`; base 299999 với tối thiểu 300000 → `min_subtotal` | `TestCouponAPI_CreateAndCheck` | như trên | ok |
| Tạo lại `sale10` → 409 `code_taken` (không phân biệt hoa thường; không hỏng tx) | `TestCouponAPI_CreateAndCheck`, `TestCreateCoupon_CodeAndTaken` | trên + `go test -race ./internal/modules/promotion/...` | `ok elimaz.com.vn/crm/internal/modules/promotion 90.457s` |
| Mã sinh: bảng chữ 32 ký tự, dài 8, tiền tố; trùng sinh lại tối đa 5 lần | `TestNewCode_Alphabet` (rapid), `TestNewCode_Distinct`, `TestNewCode_BadPrefix`, `TestCreateCoupon_GeneratedRetry`, `TestCreateBatch_RegeneratesTaken` | `go test -race ./internal/modules/promotion/...` | ok |
| Đợt 500 mã → 500 mã khác nhau khớp `^[A-HJ-NP-Z2-9]{8}$`; lô > 1.000 có tiền tố | `TestCreateBatch_Quantity`, `TestCreateBatch_ChunksAndPrefix` (2.001 mã) | như trên | ok |
| Đợt 3 `BoundPhones` → 3 mã, mỗi mã gắn đúng một SĐT; đầu vào sai không ghi gì | `TestCreateBatch_BoundPhones`, `TestCreateBatch_Invalid`, `TestCouponAPI_Batch` | promotion + httpapi như trên | ok |
| Mã đã dùng: đổi `value` → 409 `coupon_locked_after_use`; `max_uses` < `used_count` → 422; đổi `valid_to` → 200; còn dòng released vẫn khóa | `TestUpdateCoupon_LockedAfterUse`, `TestCouponAPI_UpdateAfterUse` | như trên | ok |
| Quy luật khóa sau khi dùng (rapid 200 lượt): trường khóa đổi luôn bị từ chối và không đổi version; chỉ trường được phép luôn qua | `TestUpdateCoupon_LockProperty` | `go test -race ./internal/modules/promotion/...` | ok |
| Sửa trước khi dùng, version cũ → 409, mã không có → 404, đầu vào sai → 400 | `TestUpdateCoupon_BeforeUse`, `TestCreateCoupon_Invalid`, `TestCouponAPI_UpdateAfterUse` | như trên | ok |
| `checkCoupon` 100 lần → `used_count` không đổi | `TestCouponAPI_CreateAndCheck` | httpapi như trên | ok |
| Vai trò `sale`: `createCoupon`, `updateCoupon`, `createCouponBatch` → 403; `checkCoupon`, `getCoupon`, `listCoupons`, `getCouponBatch`, `listCouponRedemptions` → 200 | `TestCouponAPI_CreateAndCheck`, `TestCouponAPI_Read`, `TestCouponAPI_UpdateAfterUse`, `TestCouponAPI_Batch`, `TestCouponAPI_Redemptions` | httpapi như trên | ok |
| Danh sách mã: mới nhất trước, trang, q theo mã/tên (có dấu, không phải mẫu LIKE), trạng thái, `batchId`, tenant khác không thấy | `TestListCoupons`, `TestCouponAPI_Read`, `TestCouponAPI_Batch` | promotion + httpapi | ok |
| Lượt dùng: mã đơn, số giảm, trạng thái, không SĐT (6 trường); mã không có → 404 | `TestListRedemptions`, `TestCouponAPI_Redemptions`, `TestCodes` (order, lọc người bán, tenant) | trên + `go test -race ./internal/modules/order/ -run TestCodes` | `ok elimaz.com.vn/crm/internal/modules/order 59.787s` (cả gói) |
| Mã gắn khách lưu hồ sơ gốc; audit không có SĐT (`bound: true`); audit tạo/sửa ghi đúng | `TestCreateCoupon_Bound`, `TestCouponAPI_CreateAndCheck`, `TestCouponAPI_UpdateAfterUse` | như trên | ok |
| Luật kiến trúc: lỗi ánh xạ, hợp đồng M2, SQL theo schema, lọc người bán, lint | `tests/arch` | `go test ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 11.414s` |
| Cổng ci-task (một lần, sau khi gộp main) | `ci-task.sh` | như tên | gen-check đỏ (mã sinh chưa stage — hết sau `git add`, `make gen-check` → 0), jscpd đỏ (đã sửa, `make jscpd` không còn đoạn trùng mới), test toàn bộ đỏ nhưng log bị cắt; chạy lại mọi gói bằng `go test -race` theo nhóm đều `ok` — không tái hiện, nghi PostgreSQL dùng chung. Lần 2 (sau khi stage, sau review): mọi cổng tĩnh xanh (gen-check, jscpd, sqlc vet, oasdiff, test-guard…); test toàn bộ chỉ đỏ ở `internal/platform/files` (`TestProcessPrivate_PropertyNeverPublic`: rapid tự báo "flaky test, can not reproduce a failure"; hook Stop chạy cùng lúc đỏ ở `TestFileProcess_HangDoesNotBlockWorker` vì quá hạn khóa) — gói task không chạm, test nhạy thời gian khi máy chạy hai bộ test song song. Chạy riêng `go test -race -count=1 ./internal/platform/files/` → `ok 68.943s`. Mọi gói khác `ok`. |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `order.Codes` + câu `OrderCodes` (`internal/modules/order/code.go`, `queries/ord/code.sql`, test `internal/modules/order/code_test.go`) — tệp ngoài danh sách Files của kế hoạch, cần cho mã đơn trong lượt dùng (Q-T71-8).
- Test viết ở phiên trước, sửa trong phiên này trước khi commit (chưa từng commit): `TestListRedemptions` không còn kiểm `OrderCode` ở promotion (chuyển sang `TestCouponAPI_Redemptions` ở HTTP); `TestCouponAPI_CreateAndCheck` tách phần đọc ra `TestCouponAPI_Read`, biến toàn cục `vnZone` thành hàm `vnTime`, `TestListCoupons` tách hàm phụ `listCodes` (luật gochecknoglobals, gocognit). Không bớt ca nào.
- `openapi.yaml`: bỏ câu "Chưa làm ở Task M2-02…" trong `description` của 8 operation; `make gen` sinh lại `internal/httpapi/gen`, `console/src/api/sdk.gen.ts` (chỉ chú thích).

## Việc phát hiện thêm

- Hàm báo hiệu realtime "Realtime nil → bỏ qua, Publish, bọc lỗi" lặp ở catalog, inventory, customer, promotion (jscpd báo trùng giữa `customer/write.go` và `promotion/manage.go`). Ứng viên gộp thành một hàm của `internal/platform/realtime` (đổi cả bốn module, task riêng); task này giữ bản của promotion (chủ đề, quyền, entity riêng).

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau khi gộp main).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế; thiếu tên kiểu mới `promotion.Actor` trong mục Ảnh hưởng (đã bổ sung ở đây; kiểu mới, không có nơi gọi cũ).
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí (KM01/KM02 với `Deps.Now` cố định, khóa sau khi dùng bằng lượt thật, quy luật rapid kiểm version không đổi, lượt dùng 6 trường không SĐT, `TestCodes` lọc người bán và tenant).
- Mục 6 khớp code: `UpdateCoupon` khóa dòng mã trước `HasRedemptions`; `ON CONFLICT DO NOTHING` không làm hỏng tx; hàm trong RetryTx sinh lại mã mỗi lần chạy.
- Ghi chú: nên có một lần cổng kiểm xanh trọn vẹn trên phần đã stage — đã chạy lại (dòng cuối bảng bằng chứng).
