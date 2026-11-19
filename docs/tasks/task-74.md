# Task 74 — M2-17: Tạo đơn — mã giảm giá và ngưỡng duyệt giảm giá

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 74 = M2-17). Loại việc (quy trình mục 3): A. `order`, `promotion` không thuộc danh sách vùng rủi ro cao của CLAUDE.md; `inventory` chỉ được gọi qua public API (không sửa mã).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep):
  - `promotion.Redeem` (hàm gói) → `promotion.Writer.Redeem(ctx, tx, code, OrderRef, EvalInput)` (phương thức, tự phát `coupon.used`). Chỗ gọi: `promotion/redeem_test.go` (helper `env.redeem`, dùng bởi các test Redeem/Release/Detach/Reprice và `property_test.go`), `httpapi/handlers_promotion_test.go:260`. Chưa có mã chạy thật nào gọi. Cách cũ xóa (không giữ hàm gói song song).
  - `promotion.Writer` thêm trường `Events *events.Registry`, `Queue queue.JobQueue` (chỗ dựng `promotion.Writer{…}`: `handlers_promotion.go:262`, test `batch_test.go`, `manage_test.go` — chỉ thêm trường, không phải sửa; chỉ `Redeem` đòi hai trường này).
  - Mới: `promotion.OrderRef`, `promotion.TopicCouponUsed`, `promotion.Used` (payload), `promotion.ActiveCouponCode`; `order.NeedsApproval`, `order.PendingDiscountApproval`, trường `order.Actor.CanApproveDiscount` (chỗ dựng `order.Actor{…}`: `handlers_order.go`, `handlers_order_settings.go`, `testkit/shiptest/shop.go`, `handlers_inventory_test.go`, `order/*_test.go` — thêm trường, không phải sửa).
  - `order.CreateInput.CouponCode`: trước đây khác rỗng → `ErrInvalidInput`; nay dùng mã. `order.Writer.Create` giữ chữ ký.
- Gói phụ thuộc ngược (go list): `promotion` ← `cmd/crm`, `internal/httpapi`, (mới) `internal/modules/order`; `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest` (test `cash`, `fulfillment` dùng shiptest).
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml`. `createOrder` bỏ 501 cho `couponCode` (hợp đồng Task 59 đã có 422 `coupon_rejected` kèm `reason`); thân đơn trả `couponDiscount`, `pendingReason`, `discountApprovedBy` nếu hợp đồng có trường (không thêm trường). oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có form lên đơn M2 (Task M2-27); console M1 không gửi `couponCode`. Không đổi màn hình.
- Câu sqlc, bảng, pha migration: không migration. `queries/ord/create.sql`: `InsertOrder` thêm `pending_reason`, `discount_approved_by`, `discount_approved_at`; mới `SetOrderCouponTotals` (UPDATE `coupon_discount`, `discount_total`, `grand_total`, `cod_amount` — không gán `status`, hợp luật `order_status_sql_test`) và `SetOrderItemAllocation` (UPDATE `allocated_order_discount`, `net_amount`, `updated_at`; cột đã GRANT ở 0204). `queries/promotion/redeem.sql`: mới `ActiveCouponCodeByOrder`. Bảng chạm: `ord.orders`, `ord.order_items` (dòng của chính đơn vừa chèn), `promotion.coupons`, `coupon_customer_uses`, `coupon_redemptions` (qua `Redeem` có sẵn), `platform.domain_events`, `platform.jobs` (Emit). Tài liệu: `docs/views.md` thêm dòng sự kiện `coupon.used`.
- Tệp test đổi thêm (ca giữ chỗ M2-17, xem "Test bị xóa hoặc tắt"): `httpapi/errors_m2_test.go`, `order/create_m2_test.go`.
- Test sẽ chạy: trong lúc làm `go test -race ./internal/modules/order/... ./internal/modules/promotion/...` và `./internal/httpapi/ -run 'Order|Coupon'`; trước review cổng kiểm của task (ci-task) một lần (order, promotion, httpapi, fulfillment, cash, testkit/shiptest, cmd/crm, tests/arch).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (events, queue, db); `promotion.Redeem`/`Evaluate`/`RejectError` (Task 63) — thân giữ nguyên, chỉ thành phương thức và phát sự kiện; `order/internal/pricing.Compute`, `ManualDiscountBP` (Task 64, đúng công thức `ceil(…×10000/Subtotal)`); `order.LoadSettings` (Task 72); `orderview.Label` (nhãn `awaiting_discount_approval` có sẵn); `insertHistory`, `recordCreated`, `Writer.emit` (Task 73); `identity.Can` (lớp HTTP kiểm `order.discount.approve`); mã lỗi `coupon_rejected` + `reason` ở `httpapi/errors.go` (có sẵn); test: `invtest.Shop`, `testkit.Staff.Grant`, `rolePerms`, `newM2Env`, `m2Input`.
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. Viết mới: `discountpolicy.go` (`NeedsApproval`, quyết định trạng thái/lý do chờ — chưa có), bước "áp mã" trong `create_m2.go` (gọi `Redeem`, tính lại bằng `pricing.Compute`, ghi tổng + phân bổ dòng), hai câu UPDATE tiền (chưa có câu nào sửa tổng đơn trước Task M2-20), câu đọc mã của lượt active (promotion chưa có hàm công khai trả mã theo đơn; `ActiveRedemptionByOrder` là câu nội bộ chỉ trả coupon_id). Không sentinel mới (dùng `promotion.ErrCouponRejected`).

## Soát thiết kế

Mục 5:

- 5.1 Chủ dữ liệu: lượt mã, `used_count`, sự kiện `coupon.used` thuộc `promotion`; tổng tiền, trạng thái, lý do chờ, người duyệt thuộc `ord` (`order`).
- 5.2 Không ghi bảng module khác: order gọi `promotion.Writer.Redeem` (public API); promotion không chạm `ord.*` (chỉ khóa ngoại tới đơn).
- 5.3 Đọc module khác: mã của lượt đọc qua `promotion.ActiveCouponCode` (API của promotion).
- 5.4 Nguyên tử: khách + đơn + dòng + lượt mã + sự kiện + giữ hàng + audit trong một transaction; mã bị từ chối → `*RejectError` → bên gọi lùi cả transaction (test: không còn đơn, giữ hàng không đổi).
- 5.5 Không nguyên tử: `coupon.used`, `order.*` qua outbox (Emit trong tx).
- 5.6 Gọi ra ngoài: không áp dụng (không có lệnh ra ngoài).
- 5.7 Ngữ cảnh shop: mọi câu trong `WriteTx`/`tenancy.WithTenant`; bảng có RLS sẵn.
- 5.8 Không biết HTTP: quyền duyệt giảm giá do lớp HTTP tính (`identity.Can`) và truyền vào `order.Actor.CanApproveDiscount` (order không nhập identity).
- 5.9 Đường ghi: `createOrder` có `x-permission: order.create`, `IdempotencyKey` (có sẵn).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12 Module mẫu: Task 27 (Writer tự phát sự kiện), Task 73 (điều phối nhiều module trong tx của bên gọi).

Mục 6:

- 6.1 Sửa cùng thứ: `used_count` tăng có điều kiện dưới khóa dòng mã (Redeem có sẵn); test WAT07 hai đơn cùng mã `max_uses 1` lặp 20 lần → đúng một thành công, một `exhausted`.
- 6.2 Gửi lại: khóa nghiệp vụ + dấu vân tay; dấu vân tay thêm mã giảm giá (so không phân biệt hoa thường) để cùng khóa khác mã → `ErrDuplicateOrder`, cùng mã → trả đơn cũ, không dùng mã lần hai.
- 6.3 Thứ tự khóa (ADR-46 "tạo đơn bán thường"): khách (5) → đơn, dòng (7, dòng mới) → `coupons` → `coupon_customer_uses` → `coupon_redemptions` (10) → `stock_balances` (11). Sửa tổng đơn/dòng sau Redeem chỉ chạm dòng mới của chính đơn (chưa ai thấy). Hủy (M2-21): đơn (7, FOR UPDATE) → Release (10) → tồn (11): cùng chiều. Test `deadlock_test` 200 lần lặp.
- 6.4 Lỗi tạm: `Create` không giữ trạng thái giữa các lần chạy lại (biến kết quả gán lại); Writer dựng mới mỗi yêu cầu.
- 6.5 READ COMMITTED + câu có điều kiện + khóa dòng mã (có sẵn).
- 6.6–6.8: không áp dụng (không việc nền, không gọi ngoài).
- 6.9 Treo lâu: chờ khóa mã theo `WriteLockTimeout` của `WriteTx` (có sẵn).
- 6.10 Tiền: int64; `base = Subtotal − LineDiscountTotal − OrderDiscount`; tính lại chỉ bằng `pricing.Compute` với `CouponDiscount` (phân bổ về dòng); trigger `check_order_totals` kiểm lúc commit (test DH01 có mã commit qua trigger).
- 6.11 Quy luật: test đồng thời WAT07 (`used_count` = số đơn có mã); rapid sẵn có của promotion vẫn chạy.
- 6.12 Dữ liệu cá nhân: payload `coupon.used` chỉ mã (couponId, orderId, customerId) và số (discount); SĐT gắn mã không vào lỗi, sự kiện.
- 6.13 Quyền: chỉ người có `order.discount.approve` được bỏ qua chờ duyệt — máy chủ tự tính từ grant (không nhận cờ từ client); test `sale` và `manager`.

## Quyết định thay chủ shop

- **QĐ1 — Sự kiện `coupon.used` gắn với đơn, không với mã:** aggregate `order_coupon` (mã đơn), `Seq` = `version` của đơn ở bên gọi (đơn vừa chèn hoặc đã khóa). Vì sao: luật của `events` đòi `Seq` lấy từ một cột version tăng dưới khóa dòng; `coupons.version` là version người sửa mã (tăng mỗi lần dùng sẽ làm người quản lý sửa mã bị `version_conflict` liên tục), `used_count` giảm được khi trả lượt nên không đơn điệu. Version đơn luôn tăng dưới khóa đơn, nên đổi mã khi sửa đơn (M2-20) cũng không trùng Seq. Payload chỉ mã và số `{couponId, orderId, customerId, discount}`. Phương án khác: thêm cột đếm sự kiện vào `promotion.coupons` (cần migration). Đảo ngược: đổi aggregate khi chưa có consumer nào nghe (hiện chưa có).
- **QĐ2 — Người có quyền duyệt tạo đơn vượt ngưỡng:** ghi `discount_approved_by` = người tạo, `discount_approved_at` = giờ tạo **chỉ khi** giảm tay vượt ngưỡng (không vượt thì không có gì để duyệt, cột để trống). Phương án khác: ghi cho mọi đơn của người có quyền (làm nhiễu báo cáo "ai đã duyệt").
- **QĐ3 — Đơn không giảm tay không đọc cài đặt đơn:** chỉ đơn nháp (hạn nháp) hoặc có giảm tay mới đọc `order_settings` (tỷ lệ 0 không bao giờ vượt ngưỡng ≥ 0). Vì sao: tenant chưa chạy `EnsureDefaults` (đơn kiểu M1, test M1 của `fulfillment`, `cash`, `shiptest`) vẫn lên đơn được như trước. Đơn có giảm tay mà tenant thiếu dòng cài đặt → lỗi 500 (dữ liệu lệch, như hành vi đơn nháp của Task 73).
- **QĐ4 — Dấu vân tay gửi lại tính cả mã giảm giá** (viết hoa để so không phân biệt hoa thường; đơn đã lưu lấy mã của lượt đang hiệu lực qua `promotion.ActiveCouponCode`): cùng khóa nghiệp vụ khác mã hay bỏ mã → `ErrDuplicateOrder`, cùng mã → trả đơn cũ, không dùng mã lần hai. Phương án khác: bỏ mã khỏi dấu vân tay (gửi lại với mã khác sẽ trả đơn cũ mà không báo).
- **QĐ5 — SĐT đưa vào luật mã:** SĐT khách đã chuẩn hóa từ đầu vào (`customer.OrderCustomer.Phone`) nếu có, không thì SĐT người nhận. Khi chọn khách có sẵn (`customerId`) không kèm SĐT thì dùng SĐT người nhận (mã gắn khách vẫn khớp theo hồ sơ gốc). Không đọc thêm danh tính của khách (customer chưa có API công khai trả SĐT của hồ sơ; mã gắn SĐT đã có nhánh khớp theo khách).
- **QĐ6 — Lý do ở lịch sử:** dòng lịch sử tạo đơn chờ duyệt có `reason = manual_discount_over_threshold` (cột chỉ nhận mã `^[a-z][a-z0-9_]*$`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước khi viết mã | `discountpolicy_test.go`, `create_coupon_test.go` | `go vet ./internal/modules/order/` | `undefined: order.NeedsApproval` (đỏ trước khi có `discountpolicy.go`) |
| DH01 + SALE10: couponDiscount 50.000, grandTotal 640.000, chia về dòng, trigger tổng qua commit, used_count 1, dòng coupon_redemptions, sự kiện coupon.used | `order/create_coupon_test.go` `TestCreateCoupon_DH01` | `go test -race ./internal/modules/order/ -run 'TestCreateCoupon_DH01\|…'` | `ok … 6.415s` |
| KM01 max_uses 2 → đơn thứ ba exhausted, không đơn, giữ hàng không đổi | `TestCreateCoupon_Exhausted` | như trên | `ok` |
| KM02 hết hạn → expired; mã không có → not_found; cả lần tạo lùi (không khách mới); mã toàn khoảng trắng → ErrInvalidInput | `TestCreateCoupon_Rejected` | như trên | `ok` |
| WAT07: max_uses 1, hai đơn cùng lúc ×20 → một thành công, một exhausted, used_count 1 | `TestCreateCoupon_ConcurrentLastUse` | `go test -race ./internal/modules/order/ -run 'TestCreateCoupon_ConcurrentLastUse\|TestCreateCoupon_NoDeadlockWithCancel' -v` | `--- PASS: TestCreateCoupon_ConcurrentLastUse (2.95s)` |
| Không deadlock: 200 lần tạo đơn dùng mã X + V1 song song hủy đơn cũ (khóa đơn → Release → nhả tồn) → không 40P01, pg_stat_database.deadlocks không tăng | `order/deadlock_test.go` `TestCreateCoupon_NoDeadlockWithCancel` | như trên | `--- PASS: TestCreateCoupon_NoDeadlockWithCancel (18.30s)` |
| Gửi lại cùng khóa: cùng mã → trả đơn cũ, không dùng mã lần hai; khác mã / bỏ mã / thêm mã → ErrDuplicateOrder | `TestCreateCoupon_Replay` | như dòng DH01 | `ok` |
| NeedsApproval (Q3): 100.000 → không; 100.001 → có; có quyền → không; mã 200.000 giảm tay 0 → không; ngưỡng 2000 + 150.000 → không | `order/discountpolicy_test.go` `TestNeedsApproval` | như dòng DH01 | `ok` |
| DH16/AT29: sale 100.000 → confirmed; 100.001 → new + discount_approval (lịch sử có lý do, chỉ order.created, vẫn giữ hàng); manager → confirmed, discountApprovedBy = manager; mã 200.000 → confirmed; draft/new vượt ngưỡng → chờ duyệt | `TestCreate_DiscountApproval` | như dòng DH01 | `ok` |
| Ngưỡng đổi thành 2000 (UpdateSettings) → 150.000 confirmed, 200.001 chờ duyệt | `TestCreate_DiscountApprovalThresholdSetting` | `go test -race ./internal/modules/order/...` | `ok` (cùng lần chạy cả gói) |
| promotion phát coupon.used (aggregate order_coupon, seq = version đơn, payload 4 khóa chỉ mã và số); bị từ chối không phát; Writer thiếu Events → lỗi, không dùng lượt | `promotion/redeem_test.go` `TestRedeem_EmitsCouponUsed` | `go test -race ./internal/modules/promotion/ -run TestRedeem_EmitsCouponUsed` | `ok … 3.349s` |
| Test cũ của promotion vẫn đúng sau khi Redeem thành phương thức (đồng thời, per-customer, Release/Detach, rapid 200 lượt) | `promotion/*_test.go` | `go test -race ./internal/modules/promotion/...` | `ok … 246.108s` |
| API: SALE10 → 201 couponDiscount 50.000; hết lượt/hết hạn/không có → 422 coupon_rejected kèm reason, không đơn mới; nhân viên chỉ order.create giảm 100.001 → new, pendingReason discount_approval, label awaiting_discount_approval; quản lý (vai trò mẫu manager) → confirmed, discount_approved_by có giá trị | `httpapi/handlers_order_m2_test.go` `TestOrderAPI_CouponAndDiscountApproval`; `errors_m2_test.go` `TestOrderAPI_M2FieldsPending` | `go test -race ./internal/httpapi/ -run 'TestOrderAPI\|TestCouponAPI'` | `ok … 21.991s` |
| Cổng tĩnh của task (build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard) | — | ci-task (lần 2) | mọi bước tĩnh xanh; bước test ĐỎ vì môi trường: bộ nhớ đệm go-build bị phiên khác dọn ("link: cannot open file …go-build…: no such file") và PostgreSQL chung quá tải nên cả gói không chạm (catalog, identity, inventory) quá 10 phút |
| Gói của task chạy riêng, tuần tự | order, promotion, httpapi, tests/arch | `go test -race -count=1 -p 1 -timeout 50m ./internal/modules/order/... ./internal/modules/promotion/... ./internal/httpapi/... ./tests/arch/...` | `ok order 661.561s`, `ok promotion 465.888s`, `ok httpapi 499.462s`; tests/arch bắt `SetOrderItemAllocation` thiếu `seller_id` (ADR-29) → đã thêm điều kiện, chạy lại `go test -count=1 ./tests/arch/...` → `ok 31.794s`; `make gen-check`, `make sqlc-vet`, golangci-lint order: xanh |
| fulfillment, cash (dùng `shiptest` → `CreateManual`) | — | ci-task lần 2 | `ok fulfillment 79.709s`, `ok cash 590.881s` |

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào. Ba ca giữ chỗ "mã giảm giá trước M2-17" đổi kỳ vọng theo hành vi mới (vốn được viết để đổi ở task này):

- `httpapi/handlers_order_m2_test.go` `TestOrderAPI_CreateM2Refusals` ca `r7` (`couponCode` → 501): bỏ khỏi bảng; ca thay thế đầy đủ hơn là `TestOrderAPI_CouponAndDiscountApproval` (422 `coupon_rejected` với `exhausted`, `expired`, `not_found`).
- `httpapi/errors_m2_test.go` `TestOrderAPI_M2FieldsPending` ca `couponCode` (501): nay kiểm 422 `coupon_rejected` `reason not_found` (viết riêng ngoài bảng vì `expect` đòi thân lỗi chỉ có `error`, `request_id`).
- `order/create_m2_test.go` `TestCreate_MoneyErrors` ca cuối (`CouponCode` → `ErrInvalidInput`): nay mã không có → `promotion.ErrCouponRejected`.

## Khác kế hoạch

- Kế hoạch ghi `promotion.Redeem(code, orderID, EvalInput)` (hàm gói); thực tế là `promotion.Writer{Events, Queue}.Redeem(ctx, tx, code, OrderRef{TenantID, ID, Version}, EvalInput)` vì kế hoạch đòi "sự kiện `coupon.used` do `promotion` phát" (cần Registry, Queue, tenant và Seq — QĐ1). Theo mẫu module Task 27 (đường ghi là phương thức của `Writer` tự phát sự kiện). Hàm gói cũ đã xóa.
- Kế hoạch ghi sửa `internal/modules/order/create.go`; từ Task 73 thân `Create` nằm ở `create_m2.go`, nên sửa ở đó và thêm `create_coupon.go` (bước áp mã) thay vì nhồi vào `create.go` (tệp lớp bọc M1).
- Lớp HTTP (`handlers_order.go`) bỏ 501 cho `couponCode` và tính `CanApproveDiscount` bằng `identity.Can` (chú thích của Task 73 hẹn việc này cho M2-17; kế hoạch không ghi tệp này).

## Việc phát hiện thêm

- (Cho Task M2-21, hủy đơn) Gửi lại cùng khóa nghiệp vụ sau khi đơn gốc đã hủy và lượt mã đã trả: `promotion.ActiveCouponCode` trả rỗng nên yêu cầu kèm cùng mã nhận `ErrDuplicateOrder` thay vì trả đơn cũ. Không sai tiền hay lượt mã; M2-21 cân nhắc khi đổi dấu vân tay.
- `ci-task.sh` chạy các gói song song trên PostgreSQL chung: khi máy tải nặng, gói không liên quan (catalog, identity, inventory) chạm mốc 10 phút mặc định của `go test`; bộ nhớ đệm go-build dùng chung bị phiên khác dọn giữa chừng làm "build failed". Có thể cân nhắc `-timeout` dài hơn hoặc `GOCACHE` riêng mỗi phiên (cấu hình quy trình: chủ shop quyết).

## Reviewer

Kết luận: ĐẠT (lượt 1, agent `reviewer`, trên phần đã stage gồm 22 tệp). Không có phát hiện phải sửa. Bảy mục: đủ Interface và test của kế hoạch; bảng bằng chứng trỏ tới test có thật, kiểm trên database thật; Ảnh hưởng khớp (lệch nhỏ: mục Ảnh hưởng chưa ghi tên `transition.go` cho lý do ở lịch sử — chỉ là tài liệu; các hàm không xuất ra ngoài đổi chữ ký đều một chỗ gọi, đã sửa); không gói util/common; cách cũ (`promotion.Redeem` hàm gói, nhánh 501) đã xóa; ba ca giữ chỗ đổi kỳ vọng có ghi lý do; không vi phạm luật dự án (SQL qua sqlc có `seller_id`, trigger tổng tiền DEFERRABLE, thứ tự khóa 5 → 7 → 10 → 11, payload không dữ liệu cá nhân, quyền do máy chủ tính). Ghi chú không bắt buộc: ca gửi lại sau khi hủy (ghi ở Việc phát hiện thêm); `TestCreateCoupon_NoDeadlockWithCancel` đọc bộ đếm deadlock của cả database thử (mỗi test có database riêng từ testkit nên không bị gói khác làm đỏ oan).
