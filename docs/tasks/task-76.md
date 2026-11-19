# Task 76 — M2-19: Xác nhận đơn, duyệt giảm giá, chờ khách, chốt đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 76 = M2-19). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn. Thêm `order.Writer.Confirm`, `order.Writer.ApproveDiscount`, `order.Writer.SetPending`, hằng `order.PendingAwaitingCustomer`, `order.PendingOutOfStock` (0 chỗ dùng trước task). Thêm 3 phương thức `handler.ConfirmOrder`, `ApproveOrderDiscount`, `SetOrderPending` (hiện thực `gen.Handler`, thay bản 501 của `gen.UnimplementedHandler`). Gói riêng `order/internal/state`: `Check` thêm cạnh `draft → draft` (chỉ đổi lý do chờ, như `new → new`); `state.Check` chỉ có 1 chỗ gọi (`order.applyTransition`, grep).
- Gói phụ thuộc ngược (go list): `order` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest`; `order/internal/state` chỉ bởi `order`. Chỉ thêm hàm nên các gói này không đổi hành vi; `applyTransition` (dùng bởi shiphooks.go của Task 75) không đổi chữ ký.
- Operation OpenAPI bị chạm, oasdiff: `confirmOrder`, `approveOrderDiscount`, `setOrderPending` đã có trong hợp đồng từ Task M2-02 (trả 501). Không sửa `openapi.yaml`, không `make gen`; oasdiff không đổi. Mã lỗi `version_conflict`, `invalid_transition`, `address_incomplete`, `needs_approval`, `discount_needs_approval`, `not_found` đã đổi ở `errors.go`.
- Chỗ dùng trên màn hình: chưa có màn hình gọi ba route này (grep `confirmOrder` trong `console/src`: chỉ mã sinh); không chạm console.
- Câu sqlc, bảng, pha migration: thêm câu `RecordDiscountApproval` (`:execrows`) vào `queries/ord/transition.sql` (ghi `discount_approved_by/at` của đơn đã khóa, không đổi status); dùng lại `LockOrder` (orders.sql), `UpdateOrderStatus`, `InsertStatusHistory`. Bảng `ord.orders`, `ord.order_status_history`, `platform.domain_events`, `identity.audit_logs`. Không migration.
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (gồm `internal/state`), `go test -race ./internal/httpapi/ -run 'OrderConfirm|OrderAPI'`, `go test ./tests/arch/...` (order_status_sql), rồi cổng kiểm của task (ci-task) cho gói phụ thuộc.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (idempotency, tenancy, events); trong `order`: `applyTransition`, `LockOrder`, `lockOrder`, `lockedOrder` (Task 75), `Get`, `Order.AuditFields`; httpapi: `refusableWrite` (refusal.go), `orderOut`, `permDiscountApprove`, `identity.Can`, `identity.RecordAudit`, `catalog.DefaultSeller`.
- Dùng lại / viết mới vì: mọi đổi trạng thái qua `applyTransition` (luật Task 73); khóa bằng `LockOrder`; ghi HTTP bằng `refusableWrite` (lỗi nghiệp vụ không lưu theo khóa); thân trả bằng `orderOut`. Sau khi gộp main (Task 75): `changeLocked` (khóa → so version người gọi → đổi → đọc lại, chung cho ba hàm để không lặp; jscpd báo Confirm/ApproveDiscount trùng nên gộp phần khung) dùng lại `lockOrder` (khóa, ErrNotFound) và `lockedOrder` (dựng `Order` từ dòng khóa). Test song song viết vòng đếm mã ngắn (jscpd báo trùng với test inventory, jobs; không gộp helper test dùng chung vì ngoài phạm vi). Viết mới: câu `RecordDiscountApproval` (chưa có câu ghi người duyệt sau lúc tạo đơn).

## Soát thiết kế

- 5.1, 5.2: chỉ ghi bảng của `order` (`ord.*`); sự kiện qua `events.Emit`; audit do lớp HTTP ghi bằng `identity.RecordAudit` (order không nhập identity).
- 5.3: không đọc bảng module khác (người bán mặc định qua `catalog.DefaultSeller`, public API).
- 5.4: duyệt + xác nhận + lịch sử + sự kiện + audit + khóa chống trùng trong một transaction (`WriteTx`).
- 5.5: sự kiện `order.*` qua outbox của `events` (xử lý sau commit); báo hiệu realtime như `applyTransition` đã làm.
- 5.6: không áp dụng: không gọi ra ngoài.
- 5.7: mọi câu chạy trong `WriteTx` (tenancy.WithTenant); không bảng mới.
- 5.8: module không nhập `net/http`; handler mỏng.
- 5.9: ba route đã có `x-permission` và `IdempotencyKey` trong hợp đồng.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo mẫu Task 12 (đường ghi), Task 73 (`applyTransition`), Task 27 (`Writer`, version).
- 6.1: khóa đơn `FOR UPDATE` (`LockOrder`) rồi so `version` của người gọi; `UpdateOrderStatus` còn `WHERE version =`. Hai khóa khác cùng version: bên sau chờ khóa, thấy version mới → 409.
- 6.2: `Idempotency-Key` bắt buộc (middleware); lỗi nghiệp vụ không lưu (`refusableWrite`), kết quả 200 lưu cùng transaction.
- 6.3: chỉ khóa một dòng đơn (ADR-46 nhóm 7), sau đó chỉ chèn dòng mới (lịch sử, sự kiện, audit) → không thêm thứ tự khóa mới.
- 6.4: `WriteTx` chạy lại theo ADR-46; thân hàm gán lại mọi biến kết quả mỗi lần.
- 6.5: READ COMMITTED + khóa dòng + điều kiện version.
- 6.6–6.8: không áp dụng: không việc nền mới, không gọi ngoài.
- 6.9: chờ khóa theo cấu hình chung (`lock_timeout`).
- 6.10: không đổi tiền.
- 6.11: "50 yêu cầu cùng khóa → 1 dòng lịch sử", "hai khóa khác cùng version → một 200 một 409" có test song song; máy trạng thái đã có test đủ 49 cặp (cập nhật cho cạnh `draft → draft`).
- 6.12: audit chỉ mã đơn, trạng thái, số tiền, người duyệt (uuid); không SĐT, địa chỉ, tên.
- 6.13: `confirm`, `pending` cần `order.confirm`; `approve-discount` cần `order.discount.approve` ở guard, và module tự kiểm `Actor.CanApproveDiscount` (tính từ grant, không từ thân yêu cầu).

## Quyết định thay chủ shop

1. **QĐ1 — duyệt giảm giá đơn nháp đi qua máy trạng thái bằng cạnh `draft → draft`.** Kế hoạch đòi `ApproveDiscount` trên đơn nháp "bỏ lý do, vẫn draft", nhưng `state.Check` (Task M2-08) chỉ cho `new → new` khi đổi lý do chờ, và luật Task 73 bắt mọi đổi trạng thái/lý do chờ đi qua `applyTransition`. Chọn: thêm `draft → draft` vào `Check` cùng nghĩa với `new → new` (chỉ đổi lý do chờ, không phải cạnh mới của sơ đồ 05 mục 2 vì trạng thái không đổi); test 49 cặp đổi từ 11 thành 12 cặp đi được và thêm một ca điều kiện. Lý do: một đường đổi duy nhất (lịch sử, sự kiện `order.draft` seq = version, báo hiệu), không viết câu SQL đổi `pending_reason` thứ hai. Khác: câu SQL riêng bỏ lý do ngoài `applyTransition` (hai đường, mất lịch sử/sự kiện chung). Đảo lại dễ: bỏ một cặp trong `Check`.
2. **QĐ2 — duyệt rồi xác nhận là hai bước lịch sử.** `ApproveDiscount` trên đơn new (và `Confirm` của người có quyền duyệt trên đơn chờ duyệt) ghi `new → new` (lý do `discount_approved`, bỏ `discount_approval`) rồi `new → confirmed`: version tăng 2, sự kiện `order.new` rồi `order.confirmed`, mỗi sự kiện một seq (như lúc tạo thẳng confirmed). Khác: một dòng lịch sử `new → confirmed` kèm lý do — phải thêm điều kiện "đã duyệt" vào `state.Conditions` và đổi chữ ký `applyTransition` (Task 75 đang dùng song song).
3. **QĐ3 — hàm nhận `sellerID` và là phương thức của `Writer`** (Q-T57-1, Q-T57-3): kế hoạch viết `Confirm(ctx, tx, id, version, a, now)`; thêm `sellerID` theo ADR-29 và dùng `Writer` để phát sự kiện. Handler dùng người bán mặc định (`catalog.DefaultSeller`) như `createOrder`.
4. **QĐ4 — `SetPending` trên đơn chờ duyệt giảm giá → 422 `needs_approval`** (lý do đó chỉ bỏ được bằng duyệt); lý do ngoài `awaiting_customer`/`out_of_stock`/rỗng → 400 `invalid_input`; đặt lại đúng lý do đang có → 200 trả đơn, không ghi lịch sử. Khác: 422 `invalid_transition` (không nói rõ đơn đang chờ gì).
5. **QĐ5 — `confirm` chỉ từ `new`, `draft`.** `cancel_requested → confirmed` là cạnh có thật nhưng thuộc luồng hủy/hãng (M2-21, M3), nên `confirm` trên đơn khác `new`/`draft` → 422 `invalid_transition`.
6. **QĐ6 — module tự kiểm quyền duyệt**: `ApproveDiscount` với `Actor.CanApproveDiscount = false` → `ErrDiscountNeedsApproval` (403), dù guard đã chặn `order.discount.approve`. Audit `order.confirm`, `order.approve_discount`, `order.set_pending` thêm `pendingReason` và `discountApprovedBy` (uuid) vào `Order.AuditFields`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `new` → confirm → 200, lịch sử `new → confirmed`, `order.confirmed` seq = version 2, audit `order.confirm` không SĐT/địa chỉ; version cũ → 409 `version_conflict`; đơn không có → 404; chỉ xem → 403 | `TestOrderConfirm_New` (httpapi); `TestConfirm_New` (order, cả hai người bán), `TestConfirm_OtherSeller` | `go test -race ./internal/httpapi/ -run OrderConfirm`; `go test -race ./internal/modules/order/ -run 'Confirm\|ApproveDiscount\|SetPending'` | `ok elimaz.com.vn/crm/internal/httpapi 3.7s`; `ok elimaz.com.vn/crm/internal/modules/order 3.4s` |
| DH16: đơn chờ duyệt; sale confirm → 422 `needs_approval`; sale approve-discount → 403; manager approve-discount → `confirmed`, lịch sử lý do `discount_approved`, audit có `discountApprovedBy` = cột của đơn | `TestOrderConfirm_DiscountApproval`; `TestApproveDiscount_New` (module: nhân viên → `ErrDiscountNeedsApproval`, version 3, `order.new` seq 2, `order.confirmed` seq 3), `TestConfirm_ApproverConfirmsPending`, `TestApproveDiscount_NotPending` | như trên | ok |
| `draft` thiếu phường → 422 `address_incomplete`; draft khác địa chỉ đủ → `confirmed`, `draftExpiresAt` null (không dùng Edit) | `TestOrderConfirm_Draft`; `TestConfirm_Draft` (thêm: nháp chờ duyệt → manager duyệt → vẫn draft, giữ hạn, `order.draft` seq 2, rồi chốt được) | như trên | ok |
| `setOrderPending awaiting_customer` → `label awaiting_customer`; rồi confirm → `confirmed`; pending trên `confirmed` → 422 `invalid_transition`; confirm `cancelled` → 422 `invalid_transition` | `TestOrderConfirm_Pending`; `TestSetPending`, `TestConfirm_Cancelled` | như trên | ok |
| 50 confirm song song cùng khóa → 1 dòng lịch sử (200 hoặc 409 `request_in_progress`); hai khóa khác cùng version → một 200, một 409 `version_conflict` | `TestOrderConfirm_Parallel` | `go test -race ./internal/httpapi/ -run OrderConfirm` | ok |
| Máy trạng thái: 12 cặp đi được, 37 cặp `ErrInvalidTransition`, `draft → draft` | `TestCheck_AllPairs`, `TestCheck_Conditions` | `go test -race ./internal/modules/order/internal/state/` | `ok …/order/internal/state 1.2s` |
| Không vỡ gói khác: order, arch (`TestOrderStatusSQL_OnlyInTransition`), httpapi | cả gói | `go test -race ./internal/modules/order/... ./tests/arch/... ./internal/httpapi/...` (trước gộp main); ci-task sau gộp | `ok order 83.6s`, `ok tests/arch 24.8s`, `ok httpapi 71.6s`; ci-task (sau gộp main): build, vet, lint, sqlc-vet, catalog, gitleaks, oasdiff, test-guard qua; test gói đổi + phụ thuộc: `ok cmd/crm`, `ok httpapi 64.5s`, `ok cash 49.9s`, `ok fulfillment 16.7s`, `ok order 64.8s`, `ok order/internal/state`, `ok realtime 10.7s`, `ok tests/arch 19.8s`. Lần đó gen-check ĐỎ (mã sinh chưa stage) và jscpd ĐỎ (3 đoạn trùng) → đã sửa (gộp `changeLocked`, viết lại vòng đếm), chạy lại riêng: `make gen-check` exit 0 trên bản đã stage, `jscpd … Found 0 clones`, `golangci-lint` `0 issues`, test task `ok order 4.4s`, `ok httpapi 3.8s` |

## Test bị xóa hoặc tắt

- Không xóa, tắt test nào. Đổi kỳ vọng một test: `internal/modules/order/internal/state/machine_test.go` `TestCheck_AllPairs` — số cặp đi được 11 → 12 (thêm `draft → draft`, chỉ đổi lý do chờ; QĐ1); 37 cặp còn lại vẫn đòi `ErrInvalidTransition` với mọi tổ hợp điều kiện, `draft → new` vẫn bị từ chối. Thêm ca `draft → draft` vào `TestCheck_Conditions`.

## Khác kế hoạch

- Chữ ký: thêm `sellerID`, là phương thức của `Writer` (QĐ3).
- `internal/state/machine.go` và `machine_test.go` (ngoài danh sách tệp của task): thêm cạnh `draft → draft` chỉ đổi lý do chờ; test 49 cặp đổi kỳ vọng 11 → 12 (QĐ1). Không nới kiểm nào khác.
- Thêm câu `RecordDiscountApproval` vào `queries/ord/transition.sql` (mã sinh `store/transition.sql.go` qua `make gen`).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên `git diff --cached` sau khi gộp main ff3d6fc).

- Phát hiện phải sửa: không có.
- QĐ1 (`draft → draft`): reviewer xác nhận đúng kế hoạch (dòng 931), an toàn: `state.Check` chỉ gọi trong `applyTransition`, chỉ `approve` đi cạnh này với lý do rỗng, `SetPending` chặn nháp trước; hạn nháp giữ nguyên; 37 cặp khác vẫn bị từ chối.
- Đã kiểm: lỗi nghiệp vụ lùi cả transaction (kể cả duyệt + chốt nháp thiếu địa chỉ), khóa rồi mới so version, `RecordDiscountApproval` cùng tx với version mới, quyền tính từ grant và module tự chặn lại, audit không dữ liệu cá nhân.
- Ghi chú không bắt buộc (không sửa): hằng `PendingAwaitingCustomer/OutOfStock` lặp chuỗi của `state` (giống `PendingDiscountApproval` đã có); `RecordDiscountApproval` không ghi `updated_at` (applyTransition vừa ghi cùng giá trị).
- Reviewer không chạy được `go test` (hook chỉ cho lệnh đọc); kết quả test là của phiên làm task (bảng Bằng chứng).
