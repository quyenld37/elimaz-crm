# Task 77 — M2-20: Sửa đơn có kiểm phiên bản

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 77, M2-20). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/modules/order/**`, `internal/modules/inventory/byitems.go`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu có sẵn. Mới: `order.EditInput`, `order.EditLine`, `order.EditResult`, `(order.Writer).Edit`, hằng `order.EditField*` (tên trường cho audit), `inventory.(Writer).ResizeHeld` (0 chỗ tham chiếu trước task). Handler mới `(*handler).UpdateOrder` thay bản 501 của `gen.UnimplementedHandler`. `order.Writer`, `order.ShipAddress`, `inventory.Writer`, `inventory.InsufficientError` giữ nguyên chữ ký. `order/errors.go`, `order/module.go` không đổi (Task 78 song song chỉ thêm dòng).
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest`; `inventory` ← `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/inventory/invtest`. Không ai đổi mã vì chỉ thêm hàm.
- Operation OpenAPI bị chạm, oasdiff: `updateOrder` (PATCH `/api/v1/orders/{id}`) đã có trong hợp đồng từ Task M2-02 (501); task này chỉ hiện thực handler, **không sửa `openapi.yaml`** (như Task 76 với confirmOrder) → oasdiff không đổi, mã ogen không sinh lại. Mã lỗi dùng mã có sẵn trong `internal/httpapi/errors.go` (`version_conflict`, `order_locked_by_shipment`, `out_of_stock`, `coupon_rejected`, `discount_needs_approval`, `invalid_transition`, `address_incomplete`, …); không thêm dòng vào `errors.go`.
- Chỗ dùng trên màn hình: chưa có (màn sửa đơn là task màn hình M2 sau); `console/src/api/**` không đổi vì không sửa hợp đồng.
- Câu sqlc, bảng, pha migration: mới `queries/ord/edit.sql` (`LockOrderItems` khóa dòng đơn `ORDER BY id FOR UPDATE`, `UpdateOrderEdit` sửa đơn khi version khớp, gồm tính lại `cod_amount`, `payment_status`; `UpdateEditedItems`, `DeleteOrderItems`); mới `queries/inventory/byitems.sql` (`HeldReservationByItem`, `ResizeReservation`). Bảng chạm: `ord.orders`, `ord.order_items` (sửa, chèn, xóa), `inventory.stock_balances`, `inventory.stock_reservations`, `inventory.stock_ledger` (qua `applyDelta`), `promotion.*` (qua public API). Migration mới `0210_order_edit.sql` pha **mở rộng**, chỉ GRANT: `DELETE ON ord.order_items` (0204 ghi "Task sửa đơn tự thêm khi cần"), `UPDATE (quantity) ON inventory.stock_reservations`.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/inventory/... ./internal/httpapi/ -run 'Edit|UpdateOrder|ResizeHeld'` khi viết; trước review `scripts/agent/ci-task.sh` (chạm migration → bộ đầy đủ theo luật của script).

### Lượt 2 (gộp main có Task 78)

- Ký hiệu Go xuất ra ngoài: **thêm** `promotion.LockCoupons(ctx, tx, codes []string) error` (khóa các dòng mã theo `id`
  tăng dần; 0 chỗ dùng trước). `order.Edit` đổi chỗ gọi `promotion.ActiveCouponCode` (Task 78 đổi tên thành
  `OrderCouponCode`, đọc thêm lượt đã trả của đơn đã hủy) sang `OrderCouponCode`. `order.ExpireDrafts` (Task 78) bỏ
  sắp theo chữ mã, khóa mọi mã bằng `LockCoupons` trước khi trả lượt; chú thích `cancelAll` (`cancel.go`) sửa theo.
  Không ký hiệu nào đổi chữ ký. Test mới `TestEdit_CouponSwapNoDeadlock` (`edit_deadlock_test.go`).
- Gói phụ thuộc ngược: `promotion` ← `order`, `httpapi`, `cmd/crm`; `order` như lượt 1. Chỉ thêm hàm.
- OpenAPI: không chạm. Màn hình: không.
- sqlc: thêm `LockCouponsByCodes` vào `queries/promotion/coupons.sql` (`SELECT id … WHERE code = ANY(…) ORDER BY id FOR
  UPDATE`). Bảng `promotion.coupons` (khóa dòng có sẵn). Không migration mới; 0210 vẫn là số kế tiếp (main tới 0209).
- Hợp nhất hai bên khi gộp: `usedCount` có ở cả `edit_test.go` (Task 77) và `cancel_test.go` (Task 78), cùng thân →
  giữ bản của `cancel_test.go`, xóa bản trùng.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/promotion/...` (+ `httpapi -run
  'OrderEdit|Cancel'`); trước review `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.NullUUID`, `logging.Phone/Address`, `tenancy`), trong gói `order`: `lockOrder`, `lockedOrder`, `changeLocked` (confirm.go), `applyTransition`, `insertHistory`, `emit`, `signal`, `saleLines`, `cleanAddress`, `normalizedShipPhone`, `pricingError`, `overThreshold`, `money`, `optText`, `validText`, `ActiveSource`, `LoadSettings`; trong `inventory`: `applyDelta`, `changed`, `insufficient`, `LockBalancesOfReservations`, `LockReservations`, `ReleaseHeldByItems`, `Reserve`; trong `promotion`: `Detach`, `Redeem`, `Reprice`, `ActiveCouponCode`; `customer.RootOf`, `customer.ValidateUnits`; `state.Editable`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên. Viết mới `ResizeHeld` (kế hoạch giao; M1 chỉ có giữ/nhả trọn dòng, unique một dòng giữ mỗi dòng đơn nên không thể nhả rồi giữ lại cùng dòng đơn) dựng trên `applyDelta` để mọi đổi số dư vẫn ghi sổ một chỗ. Không dùng `changeLocked` cho Edit vì Edit cần cả dòng đơn đã khóa và đọc đầy đủ đơn trước khi tính; khóa đơn bằng `lockOrder` như changeLocked. Handler dùng lại `refusableWrite`, `optUUID`, `orderOut` và mẫu `orderChange` (audit cùng tx) nhưng cần ghi danh sách trường đổi vào audit nên có hàm riêng mỏng. Luật `payment_status` theo tổng mới (`editedPaymentStatus`) trùng 4 dòng với `ApplyPaymentTotals` (payments.go): giữ riêng ở task này vì gộp là thay đổi cấu trúc phải ở commit riêng (CLAUDE.md mục 3); ghi ở "Việc phát hiện thêm".
- Lượt 2: tìm trong `promotion` hàm khóa nhiều mã: chỉ có `CouponByCodeForUpdate` (một mã) và
  `CouponByIDForUpdate` (một id); `lockActive` khóa mã của lượt một đơn. Không có hàm khóa nhiều mã theo thứ tự → viết
  `LockCoupons` (một câu `ORDER BY id FOR UPDATE`, đúng luật "khóa trước rồi sửa" của ADR-46). Dùng chung cho sửa đơn
  đổi mã và `ExpireDrafts` (cùng mục đích: thứ tự khóa dòng mã, cùng lý do đổi).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1, 5.2: dữ liệu của `ord`; tồn chỉ đổi qua `inventory` (hàm mới trong gói inventory), lượt mã qua `promotion.Detach/Redeem/Reprice`. Không ghi bảng module khác (migration chỉ GRANT; FK `ON DELETE SET NULL` có từ 0204).
- 5.3: đọc biến thể qua `catalog.VariantsForSale` (view), khách gốc qua `customer.RootOf`.
- 5.4: một transaction (`WriteTx`): đơn + dòng + mã + tồn; lỗi nào cũng lùi cả lần sửa (DH10).
- 5.5: sự kiện `order.updated` ghi outbox trong tx (`events.Emit`), không gọi ngoài.
- 5.6: không áp dụng: không gọi dịch vụ ngoài.
- 5.7: mọi câu trong `tenancy.WithTenant` qua `WriteTx`; câu đọc đơn có `seller_id` (ADR-29). Không bảng mới.
- 5.8: `order` không nhập `net/http`; handler mỏng `handlers_order_edit.go`.
- 5.9: PATCH có `IdempotencyKey`, `x-permission: order.update` (có sẵn trong hợp đồng).
- 5.10: không áp dụng: không Valkey.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu Task 73 (`create_m2.go`) và Task 76 (`confirm.go`): khóa đơn, so version, lỗi sentinel, test gói `_test` trên PostgreSQL thật, rapid.

Mục 6 (đồng thời) — vùng cao, kế hoạch đòi câu 1, 3, 5, 11:
- 6.1: khóa đơn `FOR UPDATE` rồi so `version` (khác → `ErrVersionConflict`); câu `UPDATE … WHERE version = @version`. Tồn: tăng giữ bằng `ApplyDelta` điều kiện `available ≥ d`, giảm điều kiện `held ≥ d`; `stock_reservations.quantity` đổi có điều kiện `state = 'held'`.
- 6.2: `Idempotency-Key` qua `WriteTx`; từ chối nghiệp vụ không lưu theo khóa (`refusableWrite`).
- 6.3: thứ tự ADR-46: `ord.orders` (FOR UPDATE) → `ord.order_items` của đơn (`LockOrderItems` ORDER BY id FOR UPDATE, trước mọi nhóm sau; mọi câu sửa/xóa dòng đơn sau đó chạm dòng đã khóa) → `promotion` (Detach/Redeem/Reprice, nhóm 10) → `inventory` (nhóm 11) theo **variant_id tăng dần trên toàn bộ biến thể bị chạm** (nhả, đổi số lượng, giữ mới gọi từng biến thể một; mỗi lần khóa số dư trước giữ hàng). Xóa dòng đơn bị bỏ chạy **sau** khi đã nhả (FK SET NULL đụng dòng giữ hàng đã khóa). Test `edit_deadlock_test` 200 lần.
- 6.4: `WriteTx` chạy lại cả tx khi 40P01/55P03; `Edit` không giữ trạng thái giữa các lần (biến kết quả gán lại trong thân).
- 6.5: READ COMMITTED + khóa dòng + câu có điều kiện; mọi quyết định đọc dưới khóa đơn.
- 6.6–6.8: không áp dụng: không việc nền, không gọi ngoài; tiến trình chết → tx tự lùi.
- 6.9: theo cấu hình `db` (lock_timeout, statement_timeout) có sẵn.
- 6.10: tiền `int64`, chỉ `pricing.Compute` tính; trigger `check_order_totals` kiểm lúc commit.
- 6.11: rapid `edit_property_test` (chuỗi sửa ngẫu nhiên trên 1–3 đơn: tổng tiền qua trigger, tổng giữ mỗi biến thể = tổng số lượng các đơn còn mở, used_count = số lượt active) và rapid `ResizeHeld` ở `byitems_test.go` (sổ = số dư, held = tổng giữ held, available ≥ 0, chạy song song).
- 6.12: audit chỉ ghi tên trường đổi và số tiền, không địa chỉ, SĐT; sự kiện chỉ mang mã.
- 6.13: quyền `order.update` (hợp đồng); quyền duyệt giảm giá tính ở lớp HTTP từ grant (`identity.Can`), không lấy từ thân.

Lượt 2 (thứ tự khóa hai mã, việc Task 78 giao):
- 6.3: lượt 1 sửa đơn đổi mã gọi `Detach` (khóa mã cũ) rồi `Redeem` (khóa mã mới) → thứ tự theo hướng đổi; hai lần
  sửa đổi chéo A→B, B→A, hoặc sửa B→A song song `ExpireDrafts` (khóa theo chữ mã A rồi B) tạo vòng chờ trong nhóm 10.
  Bảng ADR-46: trong một bảng theo khóa chính tăng dần; sửa nhiều dòng thì khóa trước bằng `ORDER BY id FOR UPDATE`.
  Sửa: sửa đơn đổi mã gọi `LockCoupons([cũ, mới])` trước `Detach`; `ExpireDrafts` gọi `LockCoupons(mọi mã)` trước khi
  trả lượt (thay sắp theo chữ — chữ không phải khóa chính). Hủy một đơn (`Cancel`) chỉ khóa một mã: không đổi.
  Thứ tự nhóm vẫn đơn (7) → mã (10) → tồn (11) ở cả ba đường.
- 6.4: `LockCoupons` khóa lại dòng đã giữ thì không chờ; chạy lại trong `RetryTx` không giữ trạng thái.
- 6.11: test `TestEdit_CouponSwapNoDeadlock` 200 lần: hai lần sửa đổi chéo mã + `ExpireDrafts` hai đơn nháp mang hai
  mã + hủy một đơn mang mã, song song; `pg_stat_database.deadlocks` không tăng; `used_count` khớp số lượt active.

## Quyết định thay chủ shop

1. **Bước sổ tồn là `edit_v<version mới>`**, không phải `edit:v<version>` như kế hoạch: CHECK của `stock_ledger.step` là `^[a-z][a-z0-9_]{0,31}$`, không cho dấu `:`. Cùng nghĩa, unique `(doc, bucket, step)` vẫn khác nhau giữa các lần sửa. Khác: migration nới CHECK (không cần).
2. **Migration `0210_order_edit.sql` chỉ GRANT** (`DELETE` dòng đơn, `UPDATE (quantity)` giữ hàng). 0204 đã ghi trước "Task sửa đơn tự thêm khi cần". Số 0210 là số kế tiếp sau 0209 trên main; nếu task song song lấy cùng số, điều phối viên đổi số khi gộp (tệp chỉ GRANT, đổi số an toàn).
3. **`Edit` là phương thức `Writer` nhận `sellerID`** (`w.Edit(ctx, tx, sellerID, id, in, a, now)`), như Confirm (ADR-29, Writer cần Events/Queue cho tồn); trả `EditResult{Order, Fields}` — `Fields` là tên nhóm trường đã đổi để lớp HTTP ghi audit (order không nhập identity).
4. **`ShipAddress` dùng `*order.ShipAddress`** (kiểu của CreateInput), không `*customer.AddressInput` như dòng Interfaces: hợp đồng `UpdateOrderRequest.shipAddress` là `ShipAddress`, và đơn lưu địa chỉ chụp lúc đặt chứ không phải địa chỉ sổ khách. `Quantity` là `int32` như `LineInput` (số lượng int32 theo Global Constraints).
5. **Chỉ trường thật sự khác giá trị hiện tại mới tính là "sửa"** (gửi lại nguyên form không bị chặn bởi vận đơn); không trường nào đổi → trả đơn, không ghi gì, version giữ nguyên.
6. **Trường bị khóa:** đơn có vận đơn hiện hành → `ErrLockedByShipment` (409, gồm cả đơn đã giao như kế hoạch "đơn delivered → 409"); không có vận đơn mà trạng thái không cho sửa → `ErrInvalidTransition` (422).
7. **Dòng mới (không `itemId`) trùng biến thể với một dòng đang có mà không dòng nào trỏ `itemId` tới** → coi là dòng đang có (giữ đơn giá cũ). Lý do: unique `(order_id, variant_id)` và dòng đơn bị bỏ chỉ xóa được sau khi nhả hàng (nhóm 11); cùng biến thể thì vẫn là dòng đó. Gửi hai dòng cùng biến thể, hai dòng cùng `itemId`, `itemId` lạ hay `itemId` mà `variantId` khác biến thể của dòng → `ErrInvalidInput` (400).
8. **Vị trí dòng:** dòng giữ nguyên giữ `position` cũ, dòng mới nối sau `position` lớn nhất hiện có (unique `(order_id, position)` không hoãn; crm_app không có quyền sửa `position`). Thứ tự trong yêu cầu không đổi thứ tự hiện.
9. **Ngưỡng duyệt giảm giá khi sửa** chỉ xét khi dòng hoặc giảm giá đổi, và chỉ khi tỷ lệ giảm tay mới vượt ngưỡng **và lớn hơn tỷ lệ cũ** (sửa không làm giảm tay nặng thêm thì không đòi duyệt lại, ví dụ tăng số lượng của đơn đã duyệt). Đơn `new`/`draft` cần duyệt → sau câu sửa (order.updated) gọi `applyTransition` cùng trạng thái, lý do chờ `discount_approval` (đúng một đường đổi trạng thái): version tăng 2, thêm sự kiện `order.<trạng thái>`. Đơn `confirmed` cần duyệt mà người sửa không có quyền → `ErrDiscountNeedsApproval` (403), không đổi gì. Người có quyền duyệt làm vượt ngưỡng → ghi `discount_approved_by/at` là người sửa (như Create), trừ khi đơn đang chờ duyệt (bỏ chờ chỉ qua ApproveDiscount). Đơn đang chờ duyệt mà sửa về dưới ngưỡng thì vẫn chờ (bỏ chờ chỉ qua duyệt, như SetPending) — an toàn hơn, đảo ngược dễ.
10. **Mã giảm giá khi sửa:** SĐT đưa vào luật mã là SĐT người nhận của đơn sau sửa (đơn không lưu SĐT danh tính dùng lúc tạo); khách là hồ sơ gốc hiện tại (`customer.RootOf`). `Reprice` chạy khi giữ mã mà `base` đổi (như kế hoạch). Mã gửi lên trùng mã đang dùng (không phân biệt hoa thường, cắt khoảng trắng) → coi như không đổi mã.
11. **Đơn `confirmed` sửa địa chỉ thành thiếu tỉnh/xã** → `ErrAddressIncomplete` (422), vì confirmed đòi địa chỉ đủ (máy trạng thái).
12. **Dòng giữ nguyên tăng số lượng không kiểm lại "còn bán"** (chỉ dòng mới kiểm, như kế hoạch bước 3); tăng vẫn phải đủ hàng có thể bán.
13. **Sửa tổng tiền của đơn đã trả trước:** `cod_amount = max(grand_total − prepaid_total, 0)` (CHECK `orders_cod_prepaid_check`), `payment_status` tính lại như `ApplyPaymentTotals` (unpaid/partial/paid; `refunded` giữ nguyên). Kế hoạch không nói; không tính lại thì trạng thái thanh toán sai.

14. **(Lượt 2) Thứ tự khóa nhiều mã = `id` tăng dần** (luật "cùng bảng theo khóa chính" của ADR-46), không phải
    chữ mã như `ExpireDrafts` của Task 78 đang sắp. Sửa đơn đổi mã và `ExpireDrafts` cùng khóa trước bằng
    `promotion.LockCoupons` (một câu `ORDER BY id FOR UPDATE`). Phương án khác: giữ thứ tự chữ (`COLLATE "C"`) cho cả
    hai — đúng về deadlock nhưng lệch câu chữ ADR-46, nên không chọn. Đảo ngược dễ (một câu SQL).
15. **(Lượt 2) `Edit` đọc mã đang dùng bằng `OrderCouponCode`** (tên mới của `ActiveCouponCode` sau Task 78). Với đơn
    chưa hủy hai hàm cho cùng kết quả. Đơn đã hủy: gửi lại đúng mã của lượt đã trả nay là "không đổi gì" (trả đơn, không
    ghi), như mọi PATCH không đổi trường nào; đổi mã thật vẫn bị `checkEditable` chặn. Không thêm hàm đọc riêng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tách hàm dùng chung không đổi hành vi (commit cấu trúc: `lockInState`, `orderActor`, `shipAddressIn`, `checkConcurrentOps`) | toàn bộ test `inventory`, `httpapi` | `go test -race ./internal/modules/inventory/... ./internal/httpapi/` | `ok inventory 234.9s`, `ok httpapi 158.0s` |
| ResizeHeld: tăng thiếu → InsufficientError, không đổi; tăng/giảm đúng sổ, số dư, giữ hàng; cùng số không ghi; đầu vào sai; không held | `TestResizeHeld` | `go test -race ./internal/modules/inventory/ -run ResizeHeld` | `ok` |
| Bất biến sổ = số dư, held = tổng giữ held khi có ResizeHeld chạy song song (rapid) | `TestResizeHeldInvariants` | như trên | `ok` (300 lượt) |
| DH10/AT07: sửa V1 thành 6 → out_of_stock, không đổi gì, có thể bán vẫn 3; thành 5 → 0; thành 1 → 4; order.updated seq = version; sổ edit_v<N> | `TestEdit_DH10_Quantity` (hai người bán), `TestOrderEdit_Quantity` (409 qua API, gửi lại cùng khóa, audit có tên trường không SĐT/địa chỉ, 403 thiếu quyền) | `go test -race ./internal/modules/order/ -run TestEdit_`; `go test -race ./internal/httpapi/ -run TestOrderEdit_` | `ok` |
| Bỏ dòng V2 đã giữ hàng (việc Task 62), thêm V3 → V2 nhả (order_edited, mất liên kết dòng đơn), V3 giữ | `TestEdit_RemoveAndAddLines` | như trên | `ok` |
| DH11/AT28: cùng version, đúng một người lưu, người kia version_conflict, dữ liệu của người lưu | `TestEdit_DH11_VersionConflict` (song song), `TestOrderEdit_ConflictAndApproval` (API 409) | như trên | `ok` |
| KH11: dòng giữ nguyên giữ 250.000 sau khi đổi giá; dòng mới giá hiện hành | `TestEdit_KH11_PriceSnapshot` | như trên | `ok` |
| Mã min_subtotal: base 500.000 → coupon_rejected min_subtotal, không đổi gì; "" → bỏ mã, used_count − 1; đổi mã; Reprice | `TestEdit_Coupon` | như trên | `ok` |
| Q25/DH15: có vận đơn → sửa dòng/phí/địa chỉ/ghi chú hãng order_locked_by_shipment; người phụ trách được; delivered → 409, người phụ trách được | `TestEdit_LockedByShipment` | như trên | `ok` |
| Ngưỡng duyệt: confirmed, sale vượt ngưỡng → discount_needs_approval (403 qua API), manager được; new → chờ duyệt | `TestEdit_DiscountApproval`, `TestOrderEdit_ConflictAndApproval` | như trên | `ok` |
| Đầu vào sai, không có đơn, người bán khác, version cũ | `TestEdit_Errors` | như trên | `ok` |
| Không trường nào đổi → không ghi; địa chỉ thiếu xã ở confirmed → address_incomplete | `TestEdit_NoChangeAndAddress` | như trên | `ok` |
| Đơn đã trả trước: COD, payment_status tính lại | `TestEdit_PrepaidOrder` | như trên | `ok` |
| Thuộc tính (rapid): tổng tiền khớp dòng; tổng giữ mỗi biến thể = số lượng các đơn mở = số dư held; used_count = lượt active; bị từ chối thì đơn không đổi | `TestEdit_Invariants` | `go test -race ./internal/modules/order/ -run TestEdit_Invariants` | `[rapid] OK, passed 200 tests`; đột biến bỏ ResizeHeld → test đỏ ở bước 4 (đã hoàn lại) |
| Không deadlock: 200 lần hai lần sửa song song đổi chéo V1/V3, đổi số lượng V2 | `TestEdit_NoDeadlock` | `go test -race ./internal/modules/order/ -run TestEdit_NoDeadlock` | `ok` (5.96s), deadlocks không tăng |
| (Lượt 2) Sửa đơn đổi mã khóa hai mã cùng thứ tự với ExpireDrafts và Cancel: 200 lần song song X A→B, Y B→A, ExpireDrafts hai nháp (A, B), hủy một đơn mang A; deadlocks không tăng; used_count A = B = 1, đúng 2 lượt active | `TestEdit_CouponSwapNoDeadlock` | `go test -race ./internal/modules/order/ -run TestEdit_CouponSwapNoDeadlock -count=1` | trước sửa: `FAIL … pg_stat_database.deadlocks 0 → 289` (279s, WriteTx chạy lại nên không lỗi ra ngoài); sau sửa: `ok … 17.377s` |
| (Lượt 2) Gộp main không làm hỏng Task 78 và Task 77 | toàn bộ test (ci-task chạy cả bộ vì nhánh có migration) | `ci-task.sh` (một lần, nền) rồi `make gen-check` sau khi stage | build, vet, lint `0 issues`, jscpd `Found 0 clones`, `ok …/modules/order 235.657s`, `ok …/modules/promotion 71.896s`, `ok …/httpapi 61.323s`, `ok …/inventory 146.984s`, không dòng FAIL; gen-check đỏ ở lần ci-task chỉ vì tệp chưa stage (`git diff` so cây với index), sau `git add` → `make gen-check` thoát 0 |

## Test bị xóa hoặc tắt

- Lượt 2: không xóa test nào. Xóa hàm hỗ trợ `usedCount` trùng trong `edit_test.go` (Task 78 thêm hàm cùng tên, cùng
  thân vào `cancel_test.go`; giữ một bản để gói test biên dịch được). Mọi chỗ gọi dùng bản của `cancel_test.go`.

## Khác kế hoạch

- Hai commit: (1) **cấu trúc** — rút `inventory.lockInState` (khóa số dư rồi giữ hàng, kiểm trạng thái; trước nằm trong `transition`), `httpapi.orderActor`, `httpapi.shipAddressIn` (trước lặp ở createOrder, orderChange), `checkConcurrentOps` trong `inventory/property_test.go` (khung rapid song song của TestInventoryInvariants); không đổi hành vi. Lý do: cổng jscpd không cho đoạn trùng mới và mã mới cần đúng các đoạn đó (cùng mục đích, cùng lý do đổi). (2) **hành vi** — sửa đơn.

- Lượt 2: sửa `order/drafts.go`, `order/cancel.go` (chú thích) của Task 78 — Task 78 giao việc thống nhất thứ tự khóa
  hai mã cho task này (mục "Việc phát hiện thêm" của task-78.md).

- Bước sổ `edit_v<N>` thay `edit:v<N>` (Quyết định 1); `queries/inventory/byitems.sql` chưa có nên tạo mới (kế hoạch ghi Modify); thêm migration 0210 chỉ GRANT (Quyết định 2); chữ ký `Edit` (Quyết định 3, 4).

## Việc phát hiện thêm

- Luật `payment_status` theo tổng đã trả nằm ở hai chỗ (`ApplyPaymentTotals` trong payments.go và `editedPaymentStatus` trong edit.go): gộp thành một hàm ở một commit cấu trúc riêng (task dọn của M2).
- Mô tả `updateOrder`, `confirmOrder`… trong `openapi.yaml` vẫn ghi "Chưa làm ở Task M2-02 (trả 501)": dọn chữ một lần ở task hợp đồng cuối M2.
- sqlc: `@tham_số - cột` bị hiểu sai (PostgreSQL có toán tử tiền tố `@` trị tuyệt đối) → lỗi "edited query syntax is invalid" ở vị trí lệch; viết `sqlc.arg(tham_số)::kiểu - cột`. Đáng ghi vào "Bẫy đã gặp" nếu gặp lần hai.

## Reviewer

Lượt 2 (gộp main, thứ tự khóa hai mã; agent `reviewer`, 10 tệp đã stage): **ĐẠT**. Phát hiện phải sửa: không có.
Reviewer kiểm từng đường: Edit (đơn → dòng đơn → `LockCoupons` theo id → Detach/Redeem → tồn), ExpireDrafts (mọi đơn →
`LockCoupons` mọi mã → Release → tồn một lần), Cancel (một mã, không thể tạo vòng chờ nhóm 10); `TestEdit_CouponSwapNoDeadlock`
chạy 4 đường thật song song và bắt deadlock bằng bộ đếm. Ghi chú không bắt buộc: câu chữ Quyết định 15 về đơn đã hủy
(đã sửa); ExpireDrafts không có đơn vẫn gửi `LockCoupons` mảng rỗng (đúng, giữ nguyên).

Lượt 1:

Kết luận: ĐẠT (hai lượt, mỗi commit một lượt).

- Commit cấu trúc b15256a: ĐẠT, không phát hiện phải sửa. Thứ tự khóa ADR-46 nhóm 11, lỗi trả về, câu lỗi giữ nguyên; test thuộc tính không bị nới (cùng khoảng, cùng nhãn rapid, `kinds = 6`).
- Commit hành vi: ĐẠT, không phát hiện phải sửa. Đã đối chiếu từng dòng bảng bằng chứng, thứ tự khóa (đơn → dòng đơn → promotion → inventory theo variant_id tăng dần, xóa dòng sau khi nhả), migration chỉ GRANT, COD khớp CHECK. Ghi chú không bắt buộc (không sửa, không ảnh hưởng tính đúng): câu chữ mục Ảnh hưởng về hợp đồng (đã sửa trong tệp này); chú thích `TestEdit_Errors` nhắc ca "mã quá dài" không có trong bảng ca; sửa chỉ người phụ trách vẫn ghi lại tổng tiền tính lại từ dòng (đúng nhờ trigger, chỉ lỗi nếu có đơn cũ mà `pricing.Compute` từ chối — chưa thấy đường nào tạo ra).
