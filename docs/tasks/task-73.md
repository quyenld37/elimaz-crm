# Task 73 — M2-16: Tạo đơn — lõi (khách, giá, giữ hàng, trạng thái, lịch sử)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 73 = M2-16). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/modules/order/**`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep): `order.Actor` thêm trường `Type` (Q-T57-2; chỗ dựng `order.Actor{…}`: `handlers_order.go`, `handlers_order_settings.go`, `testkit/shiptest/shop.go`, `handlers_inventory_test.go`, `order/{env_test.go,schema_test.go}` — thêm trường, không phải sửa chỗ nào). `order.Writer.CreateManual` giữ chữ ký, thân thành lớp bọc `Create` (chỗ gọi: `handlers_order.go` (chuyển sang `Create`), `testkit/shiptest/shop.go`, `handlers_inventory_test.go`, `order/{env_test.go,schema_test.go}`). `order.Changed` (payload sự kiện) thêm `customerId`, `status` (chỉ thêm khóa JSON; không consumer nào giải mã nó — đã grep `cash`, `fulfillment`). Ký hiệu mới: `LineInput`, `CreateInput`, `Warning`, `CreateResult`, `Writer.Create`, hằng `Intent*`, `Channel*`, `WarningPhoneBelongsToOtherCustomer`, `StatusDraft`, `StatusNew` — tên mới, không trùng hàm nào trong gói. Không xóa ký hiệu xuất ra ngoài nào.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest` (test của `cash`, `fulfillment` dùng `shiptest`).
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng có từ Task 59/M2-02). `createOrder` bỏ 501 cho trường M2 (trừ `couponCode`: Task M2-17), trả 200 khi gửi lại sau khi bản ghi chống trùng hết hạn; thân trả thêm trường M2 tùy chọn (`version`, `label`, `warnings`, các tổng giảm…). oasdiff: không đổi.
- Chỗ dùng trên màn hình: console M1 gọi `createOrder` với thân kiểu M1 (không đổi hành vi); form lên đơn M2 là Task M2-27.
- Câu sqlc, bảng, pha migration: không migration. Commit cấu trúc: `SetOrderStatus` (orders.sql) và `InsertStatusHistory` (history.sql, xóa tệp) dời sang `queries/ord/transition.sql`; `markDelivered` dời sang `transition.go`. Commit hành vi: `queries/ord/create.sql` (`InsertOrder` đủ cột M2 — thay câu M1 trong orders.sql, xóa câu cũ; `OrderByIdempotencyKey`; `VariantExists` đọc view `catalog.variants_v`), `transition.sql` (`InsertStatusHistory` thêm `from_pending`, `to_pending`; `UpdateOrderStatus` có điều kiện version thay `SetOrderStatus`), `orders.sql` (`LockOrder` trả thêm `customer_id`, `address_complete`, `pending_reason`). Bảng chạm: `ord.orders`, `ord.order_items`, `ord.order_status_history` (INSERT/UPDATE), đọc `ord.traffic_sources`, `ord.order_settings`, view `catalog.variants_v`; qua API module khác: `customer` (FindOrCreateForOrder, ValidateUnits), `inventory` (Reserve), `platform.domain_events`, realtime.
- Test sẽ chạy: trong lúc làm `go test -race ./internal/modules/order/... ./internal/httpapi/... ./internal/modules/fulfillment/... ./internal/modules/cash/... ./internal/testkit/... ./tests/arch/...`; trước review cổng kiểm của task (ci-task) một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (events, realtime, logging.Phone/Address); `order/internal/pricing.Compute` (Task 64), `order/internal/state.Check` (Task 65), `internal/queries/orderview.Label` (Q32); `customer.Writer.FindOrCreateForOrder`, `customer.ValidateUnits`, `customer.NormalizePhone`, `ReverseDigits`; `catalog.VariantsForSale`, `VariantForSale`, `DefaultSeller`; `inventory.Writer.Reserve`, `inventory.DefaultWarehouse`; `order.ActiveSource`, `LoadSettings`, `nextCode`, `mergeLines`, `normalizedShipPhone`, `Writer.emit`; `httpapi.WriteTx`, `orderError`/`stockShortage` ở `errors.go`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các mục trên; tính tiền chỉ qua `pricing.Compute` (bỏ `sumLines`/`mulMoney`/`addMoney` của M1 — cách mới thay cách cũ). Viết mới: `create_m2.go` (điều phối), `transition.go` (`recordCreated`, `applyTransition`, ghi lịch sử, báo hiệu), dấu vân tay nội dung lần gửi lại (không có hàm sẵn), câu `VariantExists` (catalog không có hàm "biến thể có ở người bán nào trong shop không"; đọc view được phép theo G9). Không sentinel mới.

## Soát thiết kế

Mục 5:

- 5.1 Chủ dữ liệu: đơn, dòng, lịch sử thuộc `ord` (module `order`).
- 5.2 Không ghi bảng module khác: khách qua `customer.Writer.FindOrCreateForOrder`, tồn qua `inventory.Writer.Reserve`, sự kiện qua `events.Registry.Emit`. Audit `order.create` ghi ở handler (order không được nhập identity, `boundaries_test`).
- 5.3 Đọc module khác qua view: `catalog.variants_v` (VariantExists), còn lại qua public API.
- 5.4 Nguyên tử: khách + đơn + dòng + lịch sử + giữ hàng + sự kiện + audit + khóa chống trùng trong một transaction (`WriteTx`); lỗi bất kỳ → lùi hết (test "nguyên tử": không đơn, không khách mới, giữ không đổi).
- 5.5 Không nguyên tử: sự kiện `order.*` qua outbox trong tx; báo hiệu `order.changed` qua realtime bus (gửi khi commit).
- 5.6 Gọi ra ngoài: không áp dụng (M2 không có lệnh ra ngoài).
- 5.7 Ngữ cảnh shop: mọi câu trong `WriteTx`/`tenancy.WithTenant`; tenant_id lấy từ `app.tenant_id`.
- 5.8 Không biết HTTP: module không nhập `net/http`; handler chỉ dịch thân → `CreateInput`.
- 5.9 Đường ghi: `createOrder` có `x-permission: order.create` và `IdempotencyKey` (có sẵn).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12 Module mẫu: Task 12 (đường ghi), Task 27 (Writer tự ghi sự kiện, báo hiệu), Task 7.

Mục 6:

- 6.1 Sửa cùng thứ: `UpdateOrderStatus … WHERE id AND version = $v` (0 dòng → `ErrVersionConflict`); tồn qua `Reserve` (câu có điều kiện, không bán vượt — test DH08 lặp 20 lần, rapid).
- 6.2 Gửi lại: `Idempotency-Key` (M0) + khóa nghiệp vụ unique `orders_idempotency_key_key`; khóa nghiệp vụ trùng: so dấu vân tay nội dung → trả đơn cũ (200, `Replayed`) hoặc `ErrDuplicateOrder`.
- 6.3 Thứ tự khóa (ADR-46): khách (nhóm 5) → đơn, dòng, lịch sử (nhóm 7, dòng mới) → số dư tồn (nhóm 11, `Reserve` tự sắp, đưa vào đã sắp theo `variant_id`) → khóa chống trùng (cuối, `Complete`). Đọc nguồn, cài đặt, biến thể không khóa.
- 6.4 Lỗi tạm: `WriteTx` chạy lại cả hàm; handler gán lại `out`, `refused`, `replayed` mỗi lần; `Create` không giữ trạng thái giữa các lần (Writer dựng mới; không cộng dồn biến ngoài). Test `retry_later` 503 không lưu khóa.
- 6.5 READ COMMITTED: dùng mặc định; khóa nghiệp vụ trùng song song → 23505 → `ErrDuplicateOrder`.
- 6.6–6.8: không áp dụng (không việc nền, không gọi ngoài).
- 6.9 Treo lâu: chờ khóa theo `WriteLockTimeout` (test 50 ms → 503).
- 6.10 Tiền: int64 đồng, chỉ `pricing.Compute`; trigger `check_order_totals` kiểm lúc commit (test thuộc tính).
- 6.11 Quy luật: rapid 100 lượt — tổng giữ = tổng số lượng đơn đã tạo, không `held > on_hand − damaged − in_transit`, mọi đơn qua trigger tổng.
- 6.12 Dữ liệu cá nhân: audit `AuditFields` (mã, số), payload sự kiện chỉ mã + trạng thái, lỗi không mang giá trị; test quét `audit_logs`, `platform.jobs.payload`, `platform.domain_events.payload`.
- 6.13 Quyền: `order.create` (guard); tenant khác: biến thể của tenant B → `variant_not_found` (RLS, không lộ).

## Quyết định thay chủ shop

- **QĐ1 — "So mã băm của `in`" làm bằng dấu vân tay tính lại từ đơn đã lưu**, không thêm cột băm: SHA-256 của phần nội dung lưu trên đơn (người bán, kho, kênh, nguồn, mã ngoài, khách khi gửi `CustomerID`, dòng (biến thể, số lượng, giảm dòng, theo thứ tự), phí ship, giảm cấp đơn, ghi chú hãng, người phụ trách, hội thoại, tài khoản kênh, loại đơn, phiếu trả thay thế, địa chỉ giao). Vì sao: không có cột lưu băm, thêm migration giữa dải số đã cấp (0200–0208) dễ đụng task song song; test M1 `TestCreateManual_DuplicateBusinessKey` (khách khác → `ErrDuplicateOrder`) vẫn đạt. Hệ quả: đơn đã bị sửa (Task M2-20) rồi gửi lại khóa cũ → `ErrDuplicateOrder` (an toàn: không tạo đơn thứ hai). Phương án khác: cột `create_fingerprint` (migration mới). Đảo ngược: thêm cột, so băm đã lưu.
- **QĐ2 — Từ chối nghiệp vụ không lưu vào khóa chống trùng** (giữ hành vi M1 và mẫu `refusableWrite`): kế hoạch ghi "409/422 được lưu", nhưng test M1 `TestOrderAPI_Refusals` (phải vẫn đạt) đòi gửi lại cùng khóa sau khi sửa thân được xử lý lại, và mọi đường ghi M1/M2 khác đều không lưu từ chối. Chỉ 201/200 được lưu. Phương án khác: savepoint + lưu thân lỗi (phá test M1, khác mọi handler khác).
- **QĐ3 — Audit `order.create` ghi ở handler** (như M1) chứ không trong `recordCreated`: `order` không được nhập `identity` (`tests/arch/boundaries_test.go`, 04 mục 3). `Order.AuditFields` chỉ mã, số.
- **QĐ4 — Lớp bọc `CreateManual` giữ lỗi M1 cho biến thể không có**: `Create` trả `ErrVariantNotFound` (không có trong shop) hoặc `ErrMixedSellers` (có ở người bán khác); `CreateManual` đổi `ErrVariantNotFound` → `ErrMixedSellers` như M1 (test M1 `TestOrderItems_SellerFK` không sửa). API dùng `Create` nên trả `variant_not_found`.
- **QĐ5 — `customer.phone` vắng thì SĐT người nhận chỉ được đưa sang `FindOrCreateForOrder` khi `phoneIsCustomers`** (không thì SĐT hợp lệ theo luật M1 nhưng không chuẩn hóa được sẽ bị `invalid_phone`, đổi hành vi M1). `customerId` và `customer` loại trừ nhau theo hợp đồng nên ca cảnh báo "SĐT của B, khách chọn A, phoneIsCustomers" chạy bằng `Create` trong test Go.
- **QĐ6 — Địa chỉ:** `intent` vắng hoặc `confirmed` mà không có `shipAddress` → 400 `invalid_input` (mô tả hợp đồng: bắt buộc). Mã tỉnh, xã gửi đủ cả hai → `ValidateUnits` (sai → `unknown_admin_unit`/`ward_not_in_province`), `address_complete = true`; gửi một nửa → lưu như nhập, `address_complete = false` (`confirmed` → 422 `address_incomplete`; nháp, mới vẫn tạo được, kiểm đủ khi chốt).
- **QĐ7 — `couponCode` vẫn 501** tới Task M2-17; `Create` với `CouponCode` khác rỗng → `ErrInvalidInput` (không bỏ qua lặng lẽ).
- **QĐ8 — `markDelivered` (M1) chuyển sang `applyTransition`** ở commit hành vi: đơn đã khóa nên điều kiện version tương đương điều kiện trạng thái cũ; lịch sử và sự kiện giữ như M1.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: dời `SetOrderStatus`, `InsertStatusHistory` sang `transition.sql`, `markDelivered` sang `transition.go`, không đổi hành vi | toàn bộ test `order`, `fulfillment` (M1 Task 30, 30b, 33 qua `shiptest`) | `go test -race -count=1 ./internal/modules/order/... ./internal/modules/fulfillment/...` | `ok order 46.3s`, `ok pricing`, `ok state`, `ok fulfillment 11.0s` |
| `order_status_sql_test`: mẫu sai bị báo, mã thật 0 | `TestOrderStatusSQL_DetectsSample`, `TestOrderStatusSQL_OnlyInTransition` | `go test ./tests/arch/ -run OrderStatusSQL -count=1` | `ok tests/arch 0.55s` (mẫu: đúng `[fulfillment/bad.sql]`; câu `SetCurrentShipment` chỉ có `status =` trong WHERE nên không bị báo) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `order_status_sql_test` hiểu "chứa `UPDATE ord.orders` kèm `status =`" là **gán** `status` trong mệnh đề SET (như dòng 126 của kế hoạch "UPDATE ord.orders SET status"); `status = …` chỉ trong WHERE (câu `SetCurrentShipment`) không bị báo.

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc): **ĐẠT** — không phát hiện phải sửa; `markDelivered` và hai câu SQL dời nguyên văn, mã sinh khớp nguồn, test kiến trúc không báo nhầm câu chỉ có `status` trong WHERE. Ghi chú không bắt buộc: thêm dòng "Khác kế hoạch" về cách hiểu luật (đã thêm); các dạng `UPDATE ONLY`, tên có ngoặc kép, `SET (a, b) = …` không bị bắt (không có trong mã).
