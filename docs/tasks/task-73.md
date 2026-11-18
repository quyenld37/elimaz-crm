# Task 73 — M2-16: Tạo đơn — lõi (khách, giá, giữ hàng, trạng thái, lịch sử)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 73 = M2-16). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/modules/order/**`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep): `order.Actor` thêm trường `Type` (Q-T57-2; chỗ dựng `order.Actor{…}`: `handlers_order.go`, `handlers_order_settings.go`, `testkit/shiptest/shop.go`, `handlers_inventory_test.go`, `order/{env_test.go,schema_test.go}` — thêm trường, không phải sửa chỗ nào). `order.Writer.CreateManual` giữ chữ ký, thân thành lớp bọc `Create` (chỗ gọi: `handlers_order.go` (chuyển sang `Create`), `testkit/shiptest/shop.go`, `handlers_inventory_test.go`, `order/{env_test.go,schema_test.go}`). `order.Changed` (payload sự kiện) thêm `customerId`, `status` (chỉ thêm khóa JSON; không consumer nào giải mã nó — đã grep `cash`, `fulfillment`). Ký hiệu mới: `LineInput`, `CreateInput`, `Warning`, `CreateResult`, `Writer.Create`, hằng `Intent*`, `Channel*`, `WarningPhoneBelongsToOtherCustomer`, `StatusDraft`, `StatusNew`, `StatusCancelRequested`, `StatusCancelled`, `StatusReturned`, `OrderTypeShip`, `OrderTypeExchange` (hằng `channelManual` cũ thành `ChannelManual`) — tên mới, không trùng hàm nào trong gói. Không xóa ký hiệu xuất ra ngoài nào.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest` (test của `cash`, `fulfillment` dùng `shiptest`).
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng có từ Task 59/M2-02). `createOrder` bỏ 501 cho trường M2 (trừ `couponCode`: Task M2-17), trả 200 khi gửi lại sau khi bản ghi chống trùng hết hạn; thân trả thêm trường M2 tùy chọn (`version`, `label`, `warnings`, các tổng giảm…). oasdiff: không đổi.
- Chỗ dùng trên màn hình: console M1 gọi `createOrder` với thân kiểu M1 (không đổi hành vi); form lên đơn M2 là Task M2-27.
- Helper test mới `testkit.Staff.SeedAdminUnits` (`internal/testkit/adminunits.go`, mã `SampleProvinceCode`, `SampleWardCode`), dùng ở `order/create_m2_test.go`, `httpapi/handlers_order_m2_test.go`.
- Test kiến trúc chạm: `tests/arch/sqlusage_test.go` (thư mục `queries/ord` → module `order`), `tests/arch/sellerfilter_test.go` (một dòng miễn), mới `tests/arch/order_status_sql_test.go`. Tài liệu: `docs/views.md` (payload `order.*`, dòng `order.<trạng thái>`), `CLAUDE.md` (Module mẫu, Step 4b).
- Câu sqlc, bảng, pha migration: không migration. Commit cấu trúc: `SetOrderStatus` (orders.sql) và `InsertStatusHistory` (history.sql, xóa tệp) dời sang `queries/ord/transition.sql`; `markDelivered` dời sang `transition.go`. Commit hành vi: `queries/ord/create.sql` (`InsertOrder` đủ cột M2 — thay câu M1 trong orders.sql, xóa câu cũ; `OrderByIdempotencyKey`; `VariantExists` đọc view `catalog.variants_v`; `TxNow` cho lớp bọc `CreateManual`), `transition.sql` (`InsertStatusHistory` thêm `from_pending`, `to_pending`; `UpdateOrderStatus` có điều kiện version thay `SetOrderStatus`), `orders.sql` (`LockOrder` trả thêm `customer_id`, `address_complete`, `pending_reason`). Bảng chạm: `ord.orders`, `ord.order_items`, `ord.order_status_history` (INSERT/UPDATE), đọc `ord.traffic_sources`, `ord.order_settings`, view `catalog.variants_v`; qua API module khác: `customer` (FindOrCreateForOrder, ValidateUnits), `inventory` (Reserve), `platform.domain_events`, realtime.
- Test sẽ chạy: trong lúc làm `go test -race ./internal/modules/order/... ./internal/httpapi/... ./internal/modules/fulfillment/... ./internal/modules/cash/... ./internal/testkit/... ./tests/arch/...`; trước review cổng kiểm của task (ci-task) một lần.

**Lượt 2 (gộp main + gom `optUUID`/`nullUUID`):**

- Gộp `main` một lần (Task 67 tìm khách, 404, 420): không xung đột.
- Ký hiệu mới xuất ra ngoài: `db.NullUUID(*uuid.UUID) pgtype.UUID` (`internal/platform/db/pgtype.go`). Xóa hai hàm chưa xuất trùng nhau: `customer.optUUID` (`search.go`, 2 chỗ gọi: `search`, `List`) `order.nullUUID` (`create_m2.go`, 5 chỗ gọi trong `insertOrder`) và `queue.pgUUID` (`pgstore.go`: `Enqueue`; `manage.go`: `ListAttention`, `RetryJob`, `AbandonJob`, `changeJob` — jscpd báo `db.NullUUID` trùng hàm này ở lần chạy cổng kiểm đầu). Không ký hiệu xuất ra ngoài nào đổi chữ ký.
- Gói phụ thuộc ngược của `platform/db`: gần như mọi gói (chỉ thêm hàm, không đổi gì cũ). Gói đổi thân: `internal/platform/queue` (vùng rủi ro cao), `internal/modules/customer`, `internal/modules/order` (người nhập: `httpapi`, `fulfillment`, `cash`, `promotion`, `testkit/...`, `cmd/crm`).
- Không đổi OpenAPI, câu sqlc, bảng, migration, màn hình. Tài liệu: `docs/danh-muc-ghi-chu.yaml` (ghi chú `db`), `docs/danh-muc-dung-chung.md` (`make catalog`).
- Test sẽ chạy: `go test -race ./internal/platform/db/ ./internal/modules/customer/... ./internal/modules/order/...`, rồi cổng kiểm của task (ci-task) một lần trọn vẹn (vùng rủi ro cao `platform/db`, `order` → cả bộ của vùng).

**Lượt 3 (gộp main: Task 408, 68, 71):**

- Xung đột duy nhất: `queries/ord/orders.sql` (và mã sinh `internal/modules/order/internal/store/orders.sql.go`). Giữ câu `ClearCurrentShipment` của Task 408 (chỗ gọi: `order.DetachShipment` ở `shipment_progress.go:72`, dùng bởi `fulfillment/create_shipment.go`); bỏ `SetOrderStatus` mà main còn giữ — nhánh này đã thay bằng `UpdateOrderStatus` trong `queries/ord/transition.sql` (lượt 1). grep `SetOrderStatus` trong `*.go`, `*.sql`: 0 chỗ (mã sinh cũ cũng mất sau `make gen`). Mã sinh không sửa tay: lấy bản main rồi `make gen`.
- Không ký hiệu xuất ra ngoài nào của nhánh đổi; không đổi OpenAPI, migration, màn hình. Gói chạm bởi bản gộp: `order`, `fulfillment`, `customer`, `httpapi`, `promotion`… (mã của main, đã được duyệt ở task của họ).
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/fulfillment/... ./tests/arch/...`, rồi ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (events, realtime, logging.Phone/Address); `order/internal/pricing.Compute` (Task 64), `order/internal/state.Check` (Task 65), `internal/queries/orderview.Label` (Q32); `customer.Writer.FindOrCreateForOrder`, `customer.ValidateUnits`, `customer.NormalizePhone`, `ReverseDigits`; `catalog.VariantsForSale`, `VariantForSale`, `DefaultSeller`; `inventory.Writer.Reserve`, `inventory.DefaultWarehouse`; `order.ActiveSource`, `LoadSettings`, `nextCode`, `mergeLines`, `normalizedShipPhone`, `Writer.emit`; `httpapi.WriteTx`, `orderError`/`stockShortage` ở `errors.go`.
- Cảnh báo trùng jscpd (lần chạy cổng kiểm đầu: 9 đoạn mới) đã gộp hoặc viết lại: danh mục hành chính mẫu dùng chung qua `testkit.Staff.SeedAdminUnits` (`internal/testkit/adminunits.go`; ban đầu đặt ở gói `customer/customertest` nhưng `TestDynSQLProductCode` chỉ miễn SQL cho `internal/testkit`); khóa dòng số dư trong test API thành helper `holdBalanceLock`; `pricingError` viết bằng bảng; `nullUUID` thay `pgUUID`/`pgTime` (tránh chép `queue.pgUUID` chưa xuất); báo hiệu `order.changed` giữ riêng ở order (cùng hình với `customer.signal` nhưng khác chủ đề, quyền, lý do đổi — luật "logic của module ở lại trong module"). Lần chạy sau: `Found 0 clones`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các mục trên; tính tiền chỉ qua `pricing.Compute` (bỏ `sumLines`/`mulMoney`/`addMoney` của M1 — cách mới thay cách cũ). Viết mới: `create_m2.go` (điều phối), `transition.go` (`recordCreated`, `applyTransition`, ghi lịch sử, báo hiệu), dấu vân tay nội dung lần gửi lại (không có hàm sẵn), câu `VariantExists` (catalog không có hàm "biến thể có ở người bán nào trong shop không"; đọc view được phép theo G9). Không sentinel mới.

**Lượt 2:** jscpd (lúc điều phối viên gộp) báo `customer/search.go` `optUUID` trùng `order/create_m2.go` `nullUUID`. Đã đọc `docs/danh-muc-dung-chung.md` mục `db` và mọi gói `internal/platform`: chưa có hàm đổi `*uuid.UUID` → `pgtype.UUID`. Thêm `db.NullUUID` vào gói `db` (gói sở hữu việc nói chuyện với PostgreSQL qua pgx; không tạo gói `util`/`common`). Tìm thêm theo việc: `queue.pgUUID` (`pgstore.go`) giống hệt → jscpd báo trùng với `db.NullUUID`, đã chuyển `queue` sang `db.NullUUID` và xóa `pgUUID` (QĐ9). `inventory.pgUUID` (`documents.go`, viết qua `deref`, jscpd không báo) cùng nghĩa; `files.optUUID(uuid.UUID)` khác nghĩa (`uuid.Nil` → NULL). Hai chỗ này ngoài phạm vi → Việc phát hiện thêm.

**Lượt 3:** không viết mã mới, chỉ giải xung đột; không có gì để tái dùng. `ClearCurrentShipment` (408) là câu đổi `current_shipment_id`, không gán `status` nên hợp luật `order_status_sql_test`.

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

**Lượt 2:** chỉ là đổi cấu trúc (gom một hàm thuần, không I/O): không chạm database, tiền, quyền, hàng đợi, khóa. Mục 5, 6 không áp dụng. Hành vi giữ nguyên: nil → NULL, khác nil → giá trị (kể cả `uuid.Nil` không nil → giá trị, giống hai hàm cũ).

**Lượt 3:** chỉ giải xung đột gộp. `ClearCurrentShipment` có `version = version + 1` trên đơn đã khóa bằng `LockOrder` — cùng mẫu `SetCurrentShipment`, không đụng trạng thái, không đụng `applyTransition`. Mục 5, 6 không có câu mới áp dụng.

## Quyết định thay chủ shop

- **QĐ1 — "So mã băm của `in`" làm bằng dấu vân tay tính lại từ đơn đã lưu**, không thêm cột băm: SHA-256 của phần nội dung lưu trên đơn (người bán, kho, kênh, nguồn, mã ngoài, khách khi gửi `CustomerID`, dòng (biến thể, số lượng, giảm dòng, theo thứ tự), phí ship, giảm cấp đơn, ghi chú hãng, người phụ trách, hội thoại, tài khoản kênh, loại đơn, phiếu trả thay thế, địa chỉ giao). Vì sao: không có cột lưu băm, thêm migration giữa dải số đã cấp (0200–0208) dễ đụng task song song; test M1 `TestCreateManual_DuplicateBusinessKey` (khách khác → `ErrDuplicateOrder`) vẫn đạt. Hệ quả: đơn đã bị sửa (Task M2-20) rồi gửi lại khóa cũ → `ErrDuplicateOrder` (an toàn: không tạo đơn thứ hai). Phương án khác: cột `create_fingerprint` (migration mới). Đảo ngược: thêm cột, so băm đã lưu.
- **QĐ2 — Từ chối nghiệp vụ không lưu vào khóa chống trùng** (giữ hành vi M1 và mẫu `refusableWrite`): kế hoạch ghi "409/422 được lưu", nhưng test M1 `TestOrderAPI_Refusals` (phải vẫn đạt) đòi gửi lại cùng khóa sau khi sửa thân được xử lý lại, và mọi đường ghi M1/M2 khác đều không lưu từ chối. Chỉ 201/200 được lưu. Phương án khác: savepoint + lưu thân lỗi (phá test M1, khác mọi handler khác).
- **QĐ3 — Audit `order.create` ghi ở handler** (như M1) chứ không trong `recordCreated`: `order` không được nhập `identity` (`tests/arch/boundaries_test.go`, 04 mục 3). `Order.AuditFields` chỉ mã, số.
- **QĐ4 — Lớp bọc `CreateManual` giữ lỗi M1 cho biến thể không có**: `Create` trả `ErrVariantNotFound` (không có trong shop) hoặc `ErrMixedSellers` (có ở người bán khác); `CreateManual` đổi `ErrVariantNotFound` → `ErrMixedSellers` như M1 (test M1 `TestOrderItems_SellerFK` không sửa). API dùng `Create` nên trả `variant_not_found`.
- **QĐ5 — `customer.phone` vắng thì SĐT người nhận chỉ được đưa sang `FindOrCreateForOrder` khi `phoneIsCustomers`** (không thì SĐT hợp lệ theo luật M1 nhưng không chuẩn hóa được sẽ bị `invalid_phone`, đổi hành vi M1). `customerId` và `customer` loại trừ nhau theo hợp đồng nên ca cảnh báo "SĐT của B, khách chọn A, phoneIsCustomers" chạy bằng `Create` trong test Go.
- **QĐ6 — Địa chỉ:** `intent` vắng hoặc `confirmed` mà không có `shipAddress` → 400 `invalid_input` (mô tả hợp đồng: bắt buộc). Mã tỉnh, xã gửi đủ cả hai → `ValidateUnits` (sai → `unknown_admin_unit`/`ward_not_in_province`), `address_complete = true`; gửi một nửa → lưu như nhập, `address_complete = false` (`confirmed` → 422 `address_incomplete`; nháp, mới vẫn tạo được, kiểm đủ khi chốt).
- **QĐ7 — `couponCode` vẫn 501** tới Task M2-17; `Create` với `CouponCode` khác rỗng → `ErrInvalidInput` (không bỏ qua lặng lẽ).
- **QĐ8 — `markDelivered` (M1) chuyển sang `applyTransition`** ở commit hành vi: đơn đã khóa nên điều kiện version tương đương điều kiện trạng thái cũ; lịch sử và sự kiện giữ như M1.

- **QĐ9 (lượt 2) — Hàm chung đặt ở `internal/platform/db` (`db.NullUUID`), và `queue.pgUUID` cũng chuyển sang nó**: gói `db` là chỗ nói chuyện với PostgreSQL qua pgx, chưa có helper `pgtype` nào trong `internal/platform`; luật cấm `util`/`common`. jscpd báo hàm mới trùng `queue.pgUUID` (giống từng dòng) nên theo luật "chuyển sang cách mới thì xóa cách cũ" `queue` dùng `db.NullUUID` (`queue` vốn đã nhập `db` trực tiếp ở `worker.go`; `db` không nhập `queue`, `customer`, `order` nên không vòng nhập). Phương án khác: gói mới `internal/platform/pgconv` (thêm gói cho một hàm), hoặc thêm `queue.pgUUID` vào baseline jscpd (giữ trùng). Đảo ngược: chép lại hàm vào từng gói.

- **QĐ10 (lượt 3) — Xung đột `orders.sql`: giữ `ClearCurrentShipment` (408), bỏ `SetOrderStatus`** như điều phối viên chỉ định: luật dự án đòi đổi trạng thái đơn chỉ qua `applyTransition`/`transition.sql`; giữ `SetOrderStatus` sẽ làm `TestOrderStatusSQL_OnlyInTransition` đỏ. Phương án khác: giữ cả hai (trái luật). Đảo ngược: thêm lại câu.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: dời `SetOrderStatus`, `InsertStatusHistory` sang `transition.sql`, `markDelivered` sang `transition.go`, không đổi hành vi | toàn bộ test `order`, `fulfillment` (M1 Task 30, 30b, 33 qua `shiptest`) | `go test -race -count=1 ./internal/modules/order/... ./internal/modules/fulfillment/...` | `ok order 46.3s`, `ok pricing`, `ok state`, `ok fulfillment 11.0s` |
| `order_status_sql_test`: mẫu sai bị báo, mã thật 0 | `TestOrderStatusSQL_DetectsSample`, `TestOrderStatusSQL_OnlyInTransition` | `go test ./tests/arch/ -run OrderStatusSQL -count=1` | `ok tests/arch 0.55s` (mẫu: đúng `[fulfillment/bad.sql]`; câu `SetCurrentShipment` chỉ có `status =` trong WHERE nên không bị báo) |
| Test thất bại trước khi viết mã (commit hành vi) | test mới ở `order` và `httpapi` | `go test -count=1 ./internal/modules/order/`; `go test -count=1 -run 'TestOrderAPI_(CreateM2\|M1Body\|OrderForSomeone\|BusinessKey\|CreateRetry)' ./internal/httpapi/` | `order`: build failed (`w.applyTransition undefined`, `order.CreateInput` …); `httpapi`: 6 `--- FAIL` (501 not_implemented) |
| DH01: confirmed 2 dòng, giảm dòng, phí → subtotal 680000, discountTotal 20000, grand 690000, cod 690000; Available V1 3, V2 0; 1 dòng lịch sử; `order.created` seq 1 + `order.confirmed` seq 2 (payload có customerId, status) | `TestCreate_ConfirmedTotals`; API: `TestOrderAPI_CreateM2Confirmed` (label confirmed, version 2, audit không SĐT/địa chỉ/tên) | `go test -race -count=1 ./internal/modules/order/`; `go test -race -count=1 -run 'TestOrderAPI\|TestOrderSettings' ./internal/httpapi/` | `ok order 161.5s`; `ok httpapi 30.6s` (lần chạy sau khi gỡ đoạn trùng jscpd) |
| DH04: kênh manual, nguồn "Nhập tay"; nguồn tắt / không có → `traffic_source_inactive` | `TestCreate_SourceAndChannel`; API `TestOrderAPI_CreateM2Refusals` (r4) | như trên | ok |
| KH11/AT21: giá chụp lúc tạo | `TestCreate_PriceSnapshot` | như trên | ok |
| DH07/AT06: 50 yêu cầu cùng khóa → 1 đơn, 201 cùng thân hoặc 409 request_in_progress; khóa chống trùng hết hạn → 200 đơn cũ; cùng khóa khác dòng → 422 `idempotency_key_reused`; held không đổi | `TestOrderAPI_SameKey50` (M1, nay qua `Create`), `TestOrderAPI_BusinessKeyAfterExpiry`, `TestCreate_BusinessKeyReplay` | như trên | ok |
| DH08/AT05: hai người tranh chiếc cuối, lặp 20 lần → mỗi lần 1 đơn + 1 OutOfStock (V2, 0), held 1 | `TestCreate_LastUnitRace`; API `TestOrderAPI_LastUnitRace` (409 `out_of_stock` kèm variantId, available 0) | như trên | ok |
| Nguyên tử: V2 thiếu → không đơn, không khách, không danh tính, không giữ | `TestCreate_AtomicOnOutOfStock` | như trên | ok |
| KH14: hai người bán → `ErrMixedSellers`; đơn W1 với SellerID S2 → tạo được | `TestCreate_MixedSellers` | như trên | ok |
| Ngừng bán → `ErrNotForSale`; biến thể tenant B / không có → `ErrVariantNotFound` (API 422 `variant_not_found`) | `TestCreate_VariantErrors`; API `TestOrderAPI_CreateM2Refusals` (r5: biến thể không có; tenant B chỉ ở test Go — RLS cho cùng kết quả "không có") | như trên | ok |
| Ý định: draft hạn now + 24h, giữ hàng, 1 sự kiện; confirmed thiếu xã → `address_incomplete`; new không địa chỉ → tạo; mã xã lạ → `ErrUnknownAdminUnit`; intent rỗng không địa chỉ → `ErrInvalidInput` | `TestCreate_Intents`; API `TestOrderAPI_CreateM2Refusals` (d1, n1, r1, r8) | như trên | ok |
| Tiền: phí 2^53 → 400 (hợp đồng); giảm dòng 500001 cho 2 × 250000 → 422 `line_discount_exceeds`; giảm đơn chia về dòng; mã giảm giá → 501 (API) / `ErrInvalidInput` (Go) | `TestCreate_MoneyErrors`; API `TestOrderAPI_CreateM2Refusals` (r2, r3, r7) | như trên | ok |
| DH12 qua API: ship_phone là số của mẹ, A không danh tính mới | `TestOrderAPI_OrderForSomeoneElse`; Go `TestCreate_PhoneOwnership` | như trên | ok |
| Cảnh báo `phone_belongs_to_other_customer` với refId = B | `TestCreate_PhoneOwnership` | như trên | ok |
| `retry_later`: dòng số dư V1 bị khóa 2 s, lock_timeout 50 ms → 503 có Retry-After, không dòng khóa chống trùng, không đơn; gửi lại → 201, 1 đơn | `TestOrderAPI_CreateRetryLater` | như trên | ok |
| Tương thích M1 (Q30): test M1 Task 30 không sửa đạt; thân M1 → 201 confirmed, addressComplete false; `CreateManual` và `Create` xen kẽ 100 đơn → 100 mã DH khác nhau | `create_test.go`, `property_test.go`, `shipment_progress_test.go` (M1, không sửa); `TestOrderAPI_M1BodyStillConfirmed`; `TestCreate_CodesSharedWithCreateManual`; `fulfillment`, `cash` (qua `shiptest`); e2e `TestMotDonDiTron`, `TestMotDonDiTron_HaiDonSongSongTon1`, `TestMotDonDiTron_Script` (M1 Task 33) | `go test -race -count=1 ./internal/modules/fulfillment/... ./internal/modules/cash/... ./internal/testkit/... ./cmd/...`; `go test -race -count=1 -run TestMotDonDiTron ./tests/e2e/` | `ok fulfillment 27.0s`, `ok cash 109.6s`, `ok testkit 57.4s`, `ok cmd/crm`; `ok tests/e2e 16.3s` |
| PQ02: domain_events.payload, jobs.payload, audit_logs không có SĐT, tên khách, địa chỉ | `TestCreate_NoPersonalDataInEventsAndJobs`; audit qua API ở `TestOrderAPI_CreateM2Confirmed` | như trên | ok |
| Thuộc tính (rapid, 200 lượt — số lượt chung của gói): 2–6 người tạo đơn đồng thời → held = tổng số lượng đơn đã tạo; không `held > on_hand − damaged − in_transit`; grand = Σ net + phí = COD; đơn qua trigger tổng lúc commit | `TestCreate_ConcurrentReservations` | `go test -race -count=1 ./internal/modules/order/` | ok |
| `applyTransition`: draft → confirmed (version, bỏ hạn nháp, confirmed_at, lịch sử from draft, `order.confirmed` seq 2); version cũ → `ErrVersionConflict`; draft → new → `ErrInvalidTransition`; nháp thiếu địa chỉ → `ErrAddressIncomplete`; draft → cancelled ghi cancelled_at | `TestApplyTransition_DraftToConfirmed`, `TestApplyTransition_Refusals`; `markDelivered` qua `TestApplyShipmentProgress_Flow`, `_DeliveredWithoutPickup`, `_ConcurrentDelivered` (M1) | như trên | ok |
| Luật SQL, người bán, lint | `TestOrderStatusSQL_*`, `TestSQLUsageProductCode`, `TestSellerFilterProductCode`, `TestLintCleanOnRealCode` | `go test -count=1 ./tests/arch/`; `golangci-lint run ./internal/modules/order/... ./internal/httpapi/... ./tests/arch/...` | `ok tests/arch 10.5s`; `0 issues.` |
| Cổng kiểm của task | ci-task (2 lần) + chạy lại có chọn sau lần 2 | `ci-task.sh`; sau sửa: `go test -race -count=1 ./tests/arch/ ./internal/testkit/`, `-run 'TestCreate_\|TestApplyTransition' ./internal/modules/order/`, `-run TestOrderAPI ./internal/httpapi/`, jscpd | Lần 1: build, vet, lint, sqlc-vet, catalog, gitleaks, oasdiff, test-guard, mọi test xanh; đỏ gen-check (chưa stage) và jscpd (9 đoạn trùng → đã gộp). Lần 2: mọi cổng xanh trừ `TestDynSQLProductCode` (SQL trong gói `customertest`) → dời vào `internal/testkit`. Sau sửa: `ok tests/arch 20.2s`, `ok testkit 11.2s`, `ok order 20.6s`, `ok httpapi 6.6s`, jscpd `Found 0 clones` |
| Lượt 2: `db.NullUUID` nil → NULL, có giá trị → giá trị, `uuid.Nil` không nil → giá trị | `TestNullUUID` (thất bại trước: `undefined: db.NullUUID`) | `go test -race -count=1 -run TestNullUUID ./internal/platform/db/` | `ok platform/db 1.5s` |
| Lượt 2: gom `customer.optUUID`, `order.nullUUID`, `queue.pgUUID` không đổi hành vi | toàn bộ test của gói đổi và gói phụ thuộc | cổng kiểm ci-task lần 1 (test toàn bộ vì nhánh có đổi testkit); sau khi chuyển `queue`: `go test -race -count=1 ./internal/platform/queue/... ./tests/arch/`, `golangci-lint run` 4 gói, `make jscpd` | Lần 1: build, vet, lint, sqlc-vet, gitleaks, oasdiff, test-guard xanh; `ok customer`, `ok order`, `ok db`, `ok httpapi`… ; đỏ gen-check, catalog-check (chỉ vì chưa stage), jscpd (`db.NullUUID` ~ `queue.pgUUID` → QĐ9), `queue` `TestRecordAfterLongHandler` (commit quá hạn do máy tải nặng; chạy riêng 3 lần ok). Sau sửa: `ok queue 52.5s`, `ok tests/arch 20.2s`, `0 issues.`, jscpd `Found 0 clones` |
| Lượt 2: cổng kiểm trọn vẹn trên bản đã gộp main và đã stage | mọi cổng tĩnh + test toàn bộ (nhánh có đổi testkit) | cổng kiểm ci-task lần 2 | `CI-TASK XANH (so với điểm tách nhánh 87ec126)`: build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, mọi gói `ok` (gồm `queue`, `db`, `customer`, `order`, `httpapi 57.1s`, `tests/e2e`). Hook Stop chạy chồng cùng lúc báo một gói đỏ ở phần bị cắt; chạy lại `go test -race -count=1 ./cmd/... ./internal/adapters/... ./internal/httpapi/... ./internal/imager/...` → tất cả `ok` |
| Lượt 3: giải xung đột gộp main (giữ `ClearCurrentShipment` của 408, bỏ `SetOrderStatus`) không làm hỏng order, fulfillment, luật SQL trạng thái | toàn bộ test `order`, `fulfillment` (gồm `terminal_test.go` của 408: `create_shipment.go` → `order.DetachShipment` → `ClearCurrentShipment`), `tests/arch` (`TestOrderStatusSQL_*`) | `make gen`; `go build ./...`; `go test -race ./internal/modules/order/... ./internal/modules/fulfillment/... ./tests/arch/...`; grep `SetOrderStatus` | `ok order 96.5s`, `ok pricing`, `ok state`, `ok fulfillment 13.5s`, `ok tests/arch 48.0s`; grep `SetOrderStatus` ngoài mã sinh: 0 |
| Lượt 3: cổng kiểm của task trên bản đã gộp | mọi cổng tĩnh + test toàn bộ (nhánh có đổi testkit) | `ci-task.sh` (một lần) | `CI-TASK XANH (so với điểm tách nhánh 87ec126)`; `ok platform/files 136.4s` (hook Stop chạy chồng trước đó báo `TestPublicVariants` đỏ một lần do tải SeaweedFS dùng chung; chạy riêng `-run TestPublicVariants` → `ok 2.4s`; nhánh không chạm `files`) |

## Test bị xóa hoặc tắt

- `internal/httpapi/errors_m2_test.go` `TestOrderAPI_M2FieldsPending` (Task 59): bỏ 4 ca mong 501 cho `intent`, `customerId`, `lineDiscount`, `provinceCode` — các trường này có nghiệp vụ từ task này (kế hoạch: "trước Task M2-16 gửi trường M2 → 501"); hành vi mới được test ở `handlers_order_m2_test.go`. Ca `couponCode` (501), "không khách", "không địa chỉ" giữ nguyên.

## Khác kế hoạch

- `applyTransition`, `recordCreated` là phương thức của `order.Writer` (cần `Events`, `Queue`, `Realtime` để phát sự kiện, báo hiệu; Q-T57-1), không phải hàm gói như kế hoạch viết.
- Audit `order.create` ghi ở handler (QĐ3), không trong `recordCreated`.
- Từ chối nghiệp vụ không lưu vào khóa chống trùng (QĐ2).
- `OrderByIdempotencyKey` lọc theo người bán của đơn (luật ADR-29 của `sellerfilter_test`); khóa nghiệp vụ đã dùng ở người bán khác → câu chèn đụng unique → `ErrDuplicateOrder` (cùng kết quả).
- Test kiến trúc: `sqlusage_test` coi `queries/ord/*` là của module `order` (trước đó tra bảng "Được gọi" bằng tên thư mục `ord` nên `ord` không được đọc view `catalog.*_v` dù `order → catalog` được phép — lỗi chưa lộ vì chưa câu nào của `ord` đọc view module khác); `sellerfilter_test` thêm một dòng miễn `queries/ord/create.sql#VariantExists` (chỉ trả có/không trên cả shop, để phân biệt `mixed_sellers` với `variant_not_found` như kế hoạch đòi).
- Số lượt rapid của test thuộc tính là 200 (số chung của gói order, `TestMain`), kế hoạch ghi 100.
- PQ02 quét `audit_logs`, `platform.jobs.payload`, `platform.domain_events.payload`; log và dấu vết không quét riêng trong task này (đường tạo đơn không ghi log nào ngoài lỗi 500; dữ liệu cá nhân trong lỗi đều là kiểu tự che).

- `order_status_sql_test` hiểu "chứa `UPDATE ord.orders` kèm `status =`" là **gán** `status` trong mệnh đề SET (như dòng 126 của kế hoạch "UPDATE ord.orders SET status"); `status = …` chỉ trong WHERE (câu `SetCurrentShipment`) không bị báo.

## Việc phát hiện thêm

- Tenant chưa chạy `order.EnsureDefaults` (chỉ `seed-dev` gọi) thì tạo đơn nháp lỗi 500 (không có dòng `ord.order_settings`). Cần đường tạo tenant thật gọi `EnsureDefaults` (task tạo tenant/khởi tạo shop).
- Audit do `customer` ghi khi tạo khách trong lúc lên đơn chưa có request id (order.Actor không mang RequestID; thêm trường là đổi kiểu xuất ra ngoài) — task sau.
- QĐ1: đơn không chọn khách có sẵn thì dấu vân tay không gồm tên, SĐT khách; gửi lại cùng khóa nghiệp vụ mà đổi SĐT khách vẫn nhận đơn cũ (cùng khóa = cùng một lần bấm).
- `createOrder` chưa trả `customer`, `careBy`, `createdBy` (cần tên nhân viên, hồ sơ khách): task đọc chi tiết đơn M2-24.

- Lượt 2: `inventory.pgUUID` cùng nghĩa với `db.NullUUID` (vùng rủi ro cao, jscpd không báo) — chuyển sang `db.NullUUID` ở một task dọn sau.

## Reviewer

Commit 2 (hành vi): **ĐẠT** — không phát hiện phải sửa. Đã kiểm riêng Step R: thứ tự khóa (đọc không khóa → khách → đơn, dòng → tồn theo variant_id → khóa chống trùng cuối), không gọi mạng, handler gán lại biến mỗi lần chạy lại, hàm M1 giữ chữ ký, không còn ký hiệu cũ. Ghi chú không bắt buộc: mục Ảnh hưởng thiếu vài hằng mới và câu `TxNow` (đã bổ sung); audit khách thiếu request id, hệ quả QĐ1 (đã ghi vào Việc phát hiện thêm); test retry có thể treo nếu câu khóa lỗi trước khi báo (chỉ ảnh hưởng chẩn đoán test).

Commit 2, review lại sau khi dời helper danh mục hành chính vào `internal/testkit` (cổng `TestDynSQLProductCode`): **ĐẠT** — chỉ đổi test và tài liệu, mã chạy thật giữ nguyên bản đã duyệt; `customertest` đã xóa hẳn.

Commit 1 (cấu trúc): **ĐẠT** — không phát hiện phải sửa; `markDelivered` và hai câu SQL dời nguyên văn, mã sinh khớp nguồn, test kiến trúc không báo nhầm câu chỉ có `status` trong WHERE. Ghi chú không bắt buộc: thêm dòng "Khác kế hoạch" về cách hiểu luật (đã thêm); các dạng `UPDATE ONLY`, tên có ngoặc kép, `SET (a, b) = …` không bị bắt (không có trong mã).

Lượt 2 (gộp main + gom `db.NullUUID`, commit cấu trúc): **ĐẠT** — không phát hiện phải sửa. Đã kiểm: ba hàm cũ cùng luật với `db.NullUUID` (nil → NULL, con trỏ tới `uuid.Nil` → giá trị), không còn chỗ gọi tên cũ (`files.optUUID`, `httpapi.optUUID` là hàm khác nghĩa/kiểu; `inventory.pgUUID` đã ghi Việc phát hiện thêm), không vòng nhập, danh mục sinh lại khớp, mục Ảnh hưởng khớp diff. Ghi chú không bắt buộc: câu "nhập `db` gián tiếp" trong QĐ9 sai (đã sửa thành trực tiếp); mục Ảnh hưởng chưa kê gói phụ thuộc ngược của `queue` (ci-task chạy toàn bộ test nên đã bao); xem lại `TestRecordAfterLongHandler` ở ci-task lần 2 (đã qua).

Lượt 3 (gộp main: 408, 68, 71; giải xung đột `queries/ord/orders.sql`): **ĐẠT** — không phát hiện phải sửa. Đã kiểm: so với `main` chỉ còn tệp của Task 73 (mã của 408/68/71 giữ nguyên); mã sinh khớp nguồn (có `ClearCurrentShipment`, không còn `SetOrderStatus`, 0 chỗ gọi); `DetachShipment` dùng `lockOrder` mới nhất quán, `markDelivered` đi qua `applyTransition`; `fulfillment/create_shipment.go` gọi đúng chữ ký; `docs/views.md` đủ hai bên; bằng chứng khớp log ci-task. Ghi chú không bắt buộc: `order.DetachShipment` (408) tăng version đơn mà không phát sự kiện `order.*` — thiết kế của 408, ngoài phạm vi.
