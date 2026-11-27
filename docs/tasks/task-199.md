# Task 199 — Bảng flash sale; soạn đợt ở trạng thái nháp

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 199, M5-16). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không sửa ký hiệu có sẵn. Gói `promotion` thêm
  `FlashSale`, `FlashItem`, `FlashSaleInput`, `FlashItemInput`, `FlashSaleFilter`, `CreateFlashSale`, `UpdateFlashSale`,
  `AddFlashItem`, `UpdateFlashItem`, `RemoveFlashItem`, `GetFlashSale`, `ListFlashSales`, `FlashSaleChange`,
  `FlashItemChange`, phương thức `AuditFields`, `FlashMaxLength`, hằng trạng thái `FlashDraft…`,
  sentinel `ErrNotEditable`, `ErrNotADiscount`, `ErrFlashItemExists`, `ErrFlashWindow`, `ErrVariantNotForSale`.
  `identity.Permissions` thêm hai dòng (`promotion.flash.read`, `promotion.flash.manage`), `permissionLabels` thêm hai
  nhãn. Hằng `codeVariantNotForSale` dời từ `errors_order.go` sang `errors.go` (giờ hai vùng dùng; giá trị không đổi,
  0 chỗ tham chiếu ngoài `internal/httpapi`).
- Gói phụ thuộc ngược (go list): `promotion` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`,
  `internal/modules/order`, `internal/wiring/webcache` (chỉ thêm ký hiệu nên không gãy). `identity` nhập bởi gần như mọi
  gói (chỉ thêm quyền; owner, manager tự nhận vì `defaultRoles` lấy mọi quyền trừ danh sách loại trừ).
- Operation OpenAPI bị chạm, oasdiff: thêm 7 operation, không đổi operation cũ: `listFlashSales` `GET
  /api/v1/flash-sales`, `getFlashSale` `GET /api/v1/flash-sales/{flashSaleId}` (`promotion.flash.read`),
  `createFlashSale` `POST /api/v1/flash-sales`, `updateFlashSale` `PATCH /api/v1/flash-sales/{flashSaleId}`,
  `addFlashItem` `POST /api/v1/flash-sales/{flashSaleId}/items`, `updateFlashItem` `PATCH …/items/{flashItemId}`,
  `removeFlashItem` `DELETE …/items/{flashItemId}` (`promotion.flash.manage`, `IdempotencyKey`). Thêm schema
  `FlashSale*`, `FlashItem*`; thêm mã lỗi `not_editable`, `not_a_discount`, `invalid_flash_window` vào `x-error-codes`
  (oasdiff: chỉ thêm, không phá).
- Chỗ dùng trên màn hình: chưa có (màn Đợt flash sale là Task 206). `console/src/api/**` sinh lại theo hợp đồng;
  `console/src/app/errors.ts` thêm câu tiếng Việt cho 3 mã lỗi mới (errors.test.ts đòi).
- Câu sqlc, bảng, pha migration: migration mới **0532** `0532_flash_sales.sql` (pha mở rộng: CREATE TABLE, CREATE VIEW,
  GRANT, ADD CONSTRAINT FK NOT VALID + VALIDATE trên `ord.order_items.flash_sale_item_id` — cột luôn NULL tới nay) tạo
  bảng ở ba schema: `promotion.flash_sales`, `promotion.flash_sale_items`, `promotion.flash_sale_claims`,
  `inventory.flash_sale_units`; view `promotion.flash_sales_v`, `promotion.flash_sale_items_v`,
  `inventory.flash_units_summary_v`. Khóa ngoại liên module (đánh đổi đã chấp nhận ở 04 mục 3 quy tắc 2, ghi ở
  `docs/views.md` mục Khóa ngoại xuyên module): items → `catalog.variants`, `inventory.warehouses`; claims →
  `customer.customers`; units → `promotion.flash_sale_items`, `ord.order_items`; `ord.order_items` →
  `promotion.flash_sale_items`. Câu sqlc mới `queries/promotion/flash.sql` (khối promotion của sqlc.yaml có sẵn).
- Test sẽ chạy: `go test -race -run '^TestFlash' ./internal/modules/promotion/`, `-run '^TestFlashSaleAPI'
  ./internal/httpapi/`, `TestPermissionsDoc_MatchesCode` + test quyền của `identity`, `TestDomainErrorSnapshot*`,
  `TestErrorCodes_*`, `tests/arch` (lockorder_frame, viewsdoc, sqlusage, boundaries, routes),
  `console`: `npx vitest run src/app/errors.test.ts`; ci-task chạy cả gói đổi + phụ thuộc.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db, tenancy,
  idempotency, settings, textnorm); mẫu chiến dịch web (`promotion/webcampaign.go`, `handlers_web_campaigns.go`);
  `catalog.VariantForSale`, `inventory.DefaultWarehouse` (A5b), `promotion.pageLimit`, `maxNameLen`, `uniqueViolation`,
  `h.refusableWrite`, `h.readAsDefaultSeller`, `h.audit`, `couponCursor`, `testkit.CheckSchemaRulesClean`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên (biến thể bán được và giá thường đọc qua `catalog.VariantForSale`;
  kho qua `inventory.DefaultWarehouse`; audit ghi ở handler cùng tx như chiến dịch, vì `promotion` không được nhập
  `identity`). Viết mới: câu SQL và hàm của đợt/món (chưa có gì về flash sale ngoài phép đo `spikes/flashsale`, mã đo
  không dùng cho sản phẩm). Không gộp với chiến dịch web: khác bảng, khác luật (giá cố định, số suất), khác lý do đổi.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1 Chủ dữ liệu: đợt, món, số đã lấy thuộc `promotion`; đơn vị suất thuộc `inventory` (04 mục 3, 05 ERD). Một migration
  tạo cả bốn bảng như kế hoạch (bảng của inventory chưa có mã ghi ở task này).
- 5.2 Không ghi bảng module khác: chỉ ghi `promotion.*`; `ord.order_items` chỉ thêm khóa ngoại trong migration.
- 5.3 Đọc module khác: người bán của biến thể đọc qua view `catalog.variants_v` (promotion → catalog được phép);
  bán được và giá qua `catalog.VariantForSale`; kho qua `inventory.DefaultWarehouse`.
- 5.4 Nguyên tử: mỗi lần ghi một tx (khóa đợt, ghi, audit trong cùng tx của handler).
- 5.5, 5.6: không áp dụng (không sự kiện, không gọi ra ngoài: đợt nháp chưa ảnh hưởng website hay tồn).
- 5.7 Tenant: mọi bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`), `UNIQUE (tenant_id, id)`, khóa ngoại ghép
  `tenant_id`; test `CheckSchemaRulesClean`.
- 5.8 Module không nhập `net/http`; handler mỏng.
- 5.9 Mọi đường ghi có `IdempotencyKey` và `x-permission: promotion.flash.manage`.
- 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu chiến dịch web (Task 452).

Mục 6 (đồng thời):
- 6.1 Sửa đợt: khóa dòng đợt `FOR UPDATE` rồi so `version` (409 `version_conflict`); sửa món: khóa đợt rồi khóa món,
  so `version` của món. Thêm món: khóa đợt (tuần tự hóa với lên lịch của Task 200 — đợt hết `draft` thì `not_editable`),
  trùng biến thể chặn bằng UNIQUE (bắt 23505 → `ErrFlashItemExists`).
- 6.2 Idempotency-Key ở mọi POST/PATCH/DELETE; lỗi nghiệp vụ không lưu theo khóa (`isPromotionRefusal`).
- 6.3 Thứ tự khóa ADR-46 nhóm 10: `flash_sales` → `flash_sale_items` (đã có trong bảng; task này không sửa tệp ADR).
- 6.4, 6.5: theo `refusableWrite` (db.RetryTx sẵn có); hàm trong tx không cộng dồn biến ngoài.
- 6.6–6.9: không áp dụng (không việc nền, không gọi ra ngoài).
- 6.10 Tiền: `sale_price bigint` đồng > 0 và nhỏ hơn giá thường (QĐ24), CHECK ở database; hợp đồng dùng `Money`.
- 6.11 Quy luật: không áp dụng ở task soạn nháp (không bán vượt là của Task 201 với rapid).
- 6.12 Dữ liệu cá nhân: không có (audit chỉ tên đợt, giờ, mã biến thể, giá, số).
- 6.13 Quyền: route khai quyền; test 403 khi thiếu `promotion.flash.manage`, đọc được với chỉ `promotion.flash.read`.

## Quyết định thay chủ shop

1. **Số migration 0532 thay cho 0507 của kế hoạch.** Main đã có tới 0531; goose không chạy migration số nhỏ hơn số đã
   áp. Ghi chú điều phối M5 (27/11) bảo lấy số trống kế tiếp. Phương án khác: giữ 0507 và bật allow-missing (trái
   luật). Dễ đảo: đổi tên tệp trước khi gộp.
2. **`promotion.flash.manage` là loại `toàn shop` (ScopeShop), không `none` như kế hoạch ghi.** Luật Task 315 (sau khi
   viết kế hoạch, ghi ở `identity.Permissions` và `docs/quyen.md`): quyền mới ghi chính sách toàn shop (giá chung, mã
   giảm giá) là `toàn shop`; `promotion.manage` cũng vậy. `promotion.flash.read` giữ `none` như `promotion.read`.
   Phương án khác: theo kế hoạch (`none`) — gán theo kho cũng sửa được giá flash của cả shop. Dễ đảo trước khi mở.
3. **Mã lỗi:** `ends_at ≤ starts_at` hay đợt dài hơn 7 ngày → 422 `invalid_flash_window` (kế hoạch đòi 422; lỗi đầu
   vào chung `invalid_input` của promotion là 400); biến thể không bán được hay không có → 422
   `variant_not_for_sale` (mã có sẵn của đơn; `catalog.ErrNotForSale` của catalog là 409 nên bọc thành sentinel riêng);
   cùng biến thể hai lần → 422 `already_exists` (mã chung có sẵn).
4. **Người bán của món lấy từ biến thể** (kế hoạch: "seller_id lấy từ biến thể"), đọc qua `catalog.variants_v`; đợt
   không gắn người bán. Biến thể của người bán đối tác cũng thêm được (đợt là của shop).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bốn bảng mới đúng luật schema; held không dòng đơn, claims -1, đợt 8 ngày → CHECK 23514; món trỏ biến thể người bán B với seller A → FK 23503 (ADR-29); crm_app không sửa `created_by` | `TestFlashSchema_Constraints` | `go test -race -count=1 -run '^TestFlash' ./internal/modules/promotion/` | `ok elimaz.com.vn/crm/internal/modules/promotion 2.267s` |
| Tạo đợt, thêm món 199.000 (giá thường 350.000), 500 suất, đọc lại; giá bằng giá thường, trùng biến thể, cửa sổ sai bị từ chối; version; đợt scheduled → không sửa; lọc danh sách | `TestFlashSale_CreateAddRead`, `TestFlashItem_Rejections`, `TestFlashSale_EditVersionAndNotEditable`, `TestFlashSale_List` | như trên | như trên |
| Qua HTTP: 201 nháp; 422 `not_a_discount`, `already_exists`, `variant_not_for_sale`, `invalid_flash_window` (kết thúc trước, 8 ngày); PATCH chỉ trường đổi; 409 `version_conflict`; bỏ món, bỏ lại 404; scheduled → 422 `not_editable`; audit mỗi lần ghi (2 create, 1 update có before/after, 2 add, 1 update món, 1 remove) | `TestFlashSaleAPI_Draft` (+ hàm phụ `draftRejections`) | `go test -race -count=1 -run '^(TestFlashSaleAPI.*\|TestDomainErrorSnapshot.*\|TestErrorCodes_.*)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.518s` |
| Chỉ `promotion.flash.read` xem được, ghi 403; không quyền flash đọc 403; tenant khác 404 | `TestFlashSaleAPI_Permissions` | như trên | như trên |
| Ánh xạ lỗi và mã lỗi trong openapi | `TestDomainErrorSnapshot*`, `TestErrorCodes_*` | như trên | như trên |
| Quyền mới khớp `docs/quyen.md`, vai mặc định | test quyền của identity | `go test -race -count=1 -run 'Permission\|Role' ./internal/modules/identity/` | `ok elimaz.com.vn/crm/internal/modules/identity 29.962s` |
| `docs/views.md` có 3 view mới (tổng 51); test kiến trúc | `TestViewsDocParses` (số đếm 48 → 51), `tests/arch` | `go test -count=1 -run TestViewsDoc ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 1.564s`; cả gói xanh trong ci-task |
| Mỗi mã lỗi mới có câu tiếng Việt | `src/app/errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts && npx tsc --noEmit && npx eslint src/app/errors.ts` | `Tests 17 passed (17)`; tsc, eslint sạch |
| Cổng tĩnh + test gói đổi và phụ thuộc | ci-task | ci-task.sh (một lần) | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh b256382b…)`, run 45932180637: static success, test (1), test (2) success |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Migration 0532 thay 0507 (Quyết định 1); `promotion.flash.manage` là `toàn shop` (Quyết định 2).
- Không tạo `queries/inventory/flash_units.sql`: task này chưa có mã đọc/ghi đơn vị suất (bảng và view
  `inventory.flash_units_summary_v` có trong migration); câu SQL của đơn vị suất thuộc Task 200/201.
- Hàm module nhận `promotion.Actor` thay `identity.AuthUser`; `UpdateFlashSale`/`UpdateFlashItem` trả bản trước/sau để
  handler ghi `RecordAudit` cùng tx (promotion không được nhập identity, giống chiến dịch web Task 452).
- Gộp main một lần (b256382b, fast-forward, chỉ thêm mockup Task 514, không xung đột).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, không có phát hiện phải sửa).

Ghi chú không bắt buộc của reviewer:
- Index `order_items_flash_sale_item` trên `ord.order_items` dựng trong transaction của migration (không
  `CONCURRENTLY`); nay bảng nhỏ nên chấp nhận; index sau này trên bảng lớn nên tách tệp `-- +goose NO TRANSACTION`.
- `FlashItemsBySale` JOIN `catalog.variants_v`: món luôn có FK tới biến thể nên không mất dòng.
- `isConstraint` (flash.go) giống phép kiểm ở `webbatches.go`; có thể dùng chung sau, không bắt buộc.
