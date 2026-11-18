# Task 333 — Gắn vận đơn, áp bước vận đơn lên đơn và tồn (M1-30b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 333 (M1-30b)). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/modules/order`, `inventory`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu có sẵn nào đổi chữ ký. Thêm mới trong
  `order`: `AttachShipment`, `ShipmentStep` (`StepPickedUp`, `StepDelivered`), `Writer.ApplyShipmentProgress`,
  `StatusDelivered`, `TopicOrderDelivered`, sentinel `ErrShipmentAlreadyAttached`, `ErrStatusConflict`. Thêm trong
  `inventory`: hàm đọc `ReservationStatesByItems` (đếm dòng giữ hàng theo trạng thái của các dòng đơn).
  `inventory.StartTransitByItems`, `ConsumeByItems` dùng nguyên (chưa ai gọi ngoài test `inventory`).
- Gói phụ thuộc ngược (go list): `order` ← `internal/httpapi` (← `cmd/crm`, `tests/arch`, `tests/e2e`);
  `inventory` ← `order`, `internal/httpapi`, `cmd/crm`. Chỉ thêm mới nên không chỗ gọi nào phải sửa.
- Operation OpenAPI bị chạm, oasdiff: không (gọi từ `fulfillment` ở Task 31; chưa có route).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không migration (cột `current_shipment_id`, quyền UPDATE có từ `0117`). Câu mới
  `queries/ord/orders.sql`: `LockOrder` (FOR UPDATE), `SetCurrentShipment`, `SetOrderStatus` (có điều kiện trạng
  thái cũ, tăng `version`); `queries/ord/items.sql`: `OrderItemIDs`; `queries/inventory/reservations.sql`:
  `CountReservationsByItems`. `make gen` sinh lại store của `order`, `inventory`.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/inventory/... ./internal/httpapi/...
  ./tests/arch/...` (vùng cao: cả bộ order, inventory); `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md`, `go doc` của
  `inventory` (`StartTransit`, `Consume`, `StartTransitByItems`, `ConsumeByItems`, `ReservationIDsByItems`), `order`
  (`Writer.emit`, `InsertStatusHistory`), `events.Emit`; tìm "FOR UPDATE", "current_shipment", "ByItems".
- Dùng lại / viết mới vì: dùng lại `inventory.Writer.StartTransitByItems` / `ConsumeByItems` (lõi `transition`
  khóa số dư theo ADR-46 nhóm 11, ghi sổ, `stock.changed`), `order.Writer.stock()`, `Writer.emit`,
  `InsertStatusHistory`, bộ test `orderEnv`. Viết mới: `inventory.ReservationStatesByItems` — `order` không đọc
  bảng `inventory.*` và không có view giữ hàng; cần biết "còn dòng held không" trước khi gọi (lưu ý reviewer Task 30:
  không nuốt `ErrStateConflict`), nên đếm phải nằm ở chủ dữ liệu.

## Soát thiết kế

- 5.1 Chủ dữ liệu: trạng thái đơn, vận đơn hiện hành → `order`; giữ hàng, số dư → `inventory`.
- 5.2 Không ghi bảng module khác: đổi tồn chỉ qua `inventory.Writer.*ByItems`; đọc trạng thái giữ hàng qua hàm công
  khai mới của `inventory`.
- 5.3 Đọc qua view: `order` không đọc `inventory.*` bằng SQL.
- 5.4 Nguyên tử: khóa đơn + đổi giữ hàng + số dư + trạng thái đơn + lịch sử + sự kiện trong tx của bên gọi
  (`fulfillment` ở Task 31). Lỗi bất kỳ → trả lỗi, bên gọi lùi; không bao giờ trả `applied=false, nil` sau khi đã
  ghi một phần.
- 5.5 Không nguyên tử: `order.delivered` qua `events.Emit` (outbox trong tx), seq = version mới.
- 5.6 Gọi ra ngoài: không áp dụng (không gọi hãng; lệnh tạo vận đơn ở Task 31).
- 5.7 Ngữ cảnh shop: chạy trong tx `tenancy.WithTenant` của bên gọi; RLS của `ord.orders` lọc tenant (đơn tenant khác
  → `ErrNotFound`).
- 5.8 Không biết HTTP: không nhập `net/http`, `gen`.
- 5.9 Đường ghi HTTP: không áp dụng (chưa có route; bên gọi là worker `fulfillment`).
- 5.10 Valkey: không áp dụng. 5.11 Thư viện mới: không.
- 5.12 Module mẫu: `Writer` tự ghi lịch sử, sự kiện (như `CreateManual`); lỗi sentinel trong `order/errors.go`.
- 6.1 Đồng thời: `SELECT … FROM ord.orders WHERE id = $1 FOR UPDATE` trước mọi việc; hai bước cùng đơn chạy lần
  lượt; `SetOrderStatus` có điều kiện `status = <cũ>`; `transition` của inventory đổi giữ hàng `WHERE state = <cũ>`.
- 6.2 Gửi lại: gọi lại cùng bước → `applied=false`, không đổi gì (quyết định theo trạng thái đã khóa: đơn, giữ hàng);
  `AttachShipment` cùng vận đơn lần hai → không đổi, không lỗi.
- 6.3 Thứ tự khóa: nhóm 7 `ord.orders` (FOR UPDATE) → nhóm 11 số dư theo `(variant_id, warehouse_id)` tăng dần →
  giữ hàng theo id (lõi `transition`) → nhóm 7 lịch sử (dòng mới, không khóa dòng có sẵn) → outbox. ADR-46 dòng 52:
  khóa đơn trước rồi nhóm sau — khớp. `ReservationStatesByItems` đọc không khóa, an toàn vì mọi đổi giữ hàng của
  đơn đi qua đường đã khóa đơn (M1: tạo đơn, bước vận đơn) và lõi vẫn kiểm `state = <cũ>` dưới khóa.
- 6.4 Lỗi tạm: tx của bên gọi chạy lại (`RetryTx` của worker); hàm không giữ trạng thái.
- 6.5 Cách ly: READ COMMITTED + khóa dòng + câu điều kiện.
- 6.6–6.8 Việc nền: không áp dụng ở task này (handler `fulfillment` ở Task 31).
- 6.9 Treo lâu: giới hạn `db.TxAPI`/`TxWorker` của M0.
- 6.10 Tiền: không chạm tiền.
- 6.11 Quy luật: rapid — chuỗi bước ngẫu nhiên (vận đơn hiện hành hay khác, `picked_up`/`delivered`, lặp lại): số
  dư luôn khớp trạng thái đơn; bước của vận đơn khác không bao giờ đổi gì; `on_hand` giảm đúng một lần.
- 6.12 Dữ liệu cá nhân: sự kiện payload chỉ `{orderId, sellerId, version}`; lịch sử không có SĐT, địa chỉ.
- 6.13 Quyền: không áp dụng (không route; quyền của webhook/worker ở Task 31).

## Quyết định thay chủ shop

1. **`ApplyShipmentProgress` là phương thức của `order.Writer`** (cần `inventory.Writer`, `events.Registry`), như
   Quyết định 1 của Task 30; `AttachShipment` giữ hàm gói đúng chữ ký kế hoạch (không ghi tồn, không sự kiện).
   Khác: hàm gói nhận thêm phụ thuộc. Đảo ngược dễ.
2. **Gọi lại / thứ tự lạ không lỗi, sai bất biến thì lỗi:** `picked_up` khi đơn còn `confirmed` mà không còn dòng
   `held` nhưng có `in_transit` → `applied=false`; đơn không còn dòng giữ hàng `held`/`in_transit` nào (bất biến hỏng)
   → lỗi `ErrStatusConflict` (bên gọi lùi, việc nền báo động) thay vì im lặng. Lỗi của `inventory` không bao giờ bị
   nuốt (lưu ý reviewer Task 30).
3. **`delivered` khi chưa nhận `picked_up`** (hãng bỏ qua tín hiệu lấy hàng; webhook chỉ là tín hiệu, Task 31): chuyển
   các dòng còn `held` sang `in_transit` rồi `consumed` trong cùng tx, đơn `delivered`. Khác: trả lỗi và chờ
   `picked_up` — có thể kẹt đơn mãi nếu hãng không bao giờ báo lấy hàng.
4. **`delivered`/`picked_up` khi đơn đã `delivered`** → `applied=false`. Đơn ở trạng thái khác `confirmed`,
   `delivered` (M1 chưa có, M2 thêm hủy/hoàn) → `ErrStatusConflict`.
5. **`AttachShipment`:** chỉ đơn `confirmed` (05 mục 348: hủy vận đơn đưa đơn về `confirmed` để gửi hãng khác);
   gắn cùng vận đơn lần hai → không đổi, không lỗi; đơn có vận đơn hiện hành khác → `ErrShipmentAlreadyAttached`.
   Gắn thành công tăng `version`, `updated_at` (đơn đổi; sự kiện `order.updated` chưa phát — M1 không có consumer,
   hàng đợi `strict` chấp nhận seq có khoảng trống). Không có đơn (hay tenant khác) → `ErrNotFound`.
6. **Tác nhân của bước vận đơn** ghi sổ và lịch sử là `carrier` (không mã). Thời điểm lịch sử là giờ máy (UTC) lúc
   áp (chữ ký kế hoạch không có thời điểm từ hãng; Task 31 có thể thêm tham số).
7. **Sentinel mới chưa đổi sang mã HTTP** (`errors.go`): chưa có route nào trả chúng; Task 31 thêm nếu cần.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước: biên dịch hỏng khi chưa có mã; với khung rỗng (`return false`/`nil`) cả 6 test bước vận đơn và `TestReservationStatesByItems` FAIL | các test dưới | `go test -race -count=1 ./internal/modules/order/... -run Shipment`; `go test ./internal/modules/inventory/ -run TestReservationStatesByItems` | `--- FAIL` ×6, `--- FAIL: TestReservationStatesByItems` |
| `AttachShipment` lần hai, vận đơn khác → `ErrShipmentAlreadyAttached`, `current_shipment_id` và `version` giữ nguyên; cùng vận đơn → không lỗi; đơn lạ → `ErrNotFound` | `TestAttachShipment` | `go test -race -count=1 ./internal/modules/order/ -run Shipment` | `--- PASS` |
| Vận đơn không hiện hành → `applied=false`, số dư giữ; `picked_up` → held 0, in_transit 2; gọi lại → `false`; `delivered` → on_hand −2, đơn `delivered`, 2 dòng lịch sử, `order.delivered` seq = version; gọi lại sau giao → `false`, version giữ | `TestApplyShipmentProgress_Flow` | như trên | `--- PASS` |
| Giao khi chưa báo lấy hàng (2 dòng) → trừ tồn đủ, đơn `delivered` | `TestApplyShipmentProgress_DeliveredWithoutPickup` | như trên | `--- PASS` |
| Đơn không còn giữ hàng (bất biến hỏng) → lỗi (không `applied=false` im lặng), không đổi gì | `TestApplyShipmentProgress_BrokenReservationsIsError` | như trên | `--- PASS` |
| Bước lạ → `ErrInvalidInput` | `TestApplyShipmentProgress_UnknownStep` | như trên | `--- PASS` |
| 8 lần báo giao song song → đúng 1 lần áp, on_hand trừ một lần, 1 sự kiện | `TestApplyShipmentProgress_ConcurrentDelivered` | như trên | `--- PASS` |
| rapid 200 lượt: chỉ vận đơn hiện hành đổi đơn và tồn, mỗi bước áp nhiều nhất một lần, số dư khớp mô hình | `TestApplyShipmentProgress_OnlyCurrentShipment` | như trên | `--- PASS` (≈ 40–50 s) |
| Đếm giữ hàng theo trạng thái của dòng đơn | `TestReservationStatesByItems`, `TestByItems_SameAsCore` | `go test -race -count=1 ./internal/modules/inventory/ -run 'TestReservationStatesByItems\|TestByItems'` | `ok` |
| Cả bộ Task 30 vẫn qua | cả gói `order`, `inventory` | `make ci` (test) | `ok …/modules/order 291.8s`, `ok …/modules/inventory 439.4s`, `ok …/tests/arch` |
| Cổng đầy đủ (sau gộp `main` 371a9d3, bản đã stage) | — | `make ci`; phần sau `test` chạy lại bằng `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` | build, vet, golangci-lint 0, gen-check, sqlc-vet qua; `go test -race ./...`: mọi gói `ok` trừ `TestRetryJobFiftyParallel` (httpapi, M0) và 2 test đo thời gian của `imager` — máy tải ≈ 24, không chạm mã đó; chạy lại riêng: `ok …/httpapi`, `ok …/imager`. govulncheck "No vulnerabilities found"; jscpd 0 đoạn trùng (sau khi gộp helper `attachedOrder`); oasdiff, catalog-check, test-guard, gitleaks, console-check qua. |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `ApplyShipmentProgress` là phương thức của `order.Writer` (Quyết định 1).
- Thêm ngoài danh sách tệp: `inventory.ReservationStatesByItems` (`internal/modules/inventory/byitems.go`, câu
  `CountReservationsByItems` trong `queries/inventory/reservations.sql`, test `TestReservationStatesByItems`) — cách
  để kiểm "còn dòng held" trước khi gọi mà không đọc bảng `inventory.*` từ `order`; `queries/ord/items.sql`
  (`OrderItemIDs`); sentinel trong `order/errors.go`; `StatusDelivered`, `TopicOrderDelivered`; dòng `order.delivered`
  ở "Sự kiện công bố" của `docs/views.md`.
- Thêm test ngoài kế hoạch: `delivered` khi chưa `picked_up`, bất biến hỏng → lỗi, bước lạ, 8 lần báo giao song song,
  rapid "chỉ vận đơn hiện hành".

## Việc phát hiện thêm

- Cho Task 31: `ErrShipmentAlreadyAttached`, `ErrStatusConflict` chưa có mã HTTP trong `errors.go` (chưa route nào trả);
  handler việc nền của `fulfillment` gọi `ApplyShipmentProgress` trong tx tenant và coi lỗi là lỗi thật (báo động),
  `applied=false` là bình thường. Thời điểm sự kiện từ hãng chưa đi vào lịch sử (Quyết định 6).
- Cho M2: hủy vận đơn phải xóa `current_shipment_id` (đưa đơn về `confirmed`, 05 mục 2) — chưa có câu nào làm việc này;
  hủy đơn phải nhả giữ hàng cũng qua khóa đơn để `ReservationStatesByItems` (đọc không khóa) giữ đúng.
- Kế hoạch Task 31 và M3 (G-M2-3) ghi `order.ApplyShipmentProgress(ctx, tx, …)` là hàm gói; mã là phương thức
  `order.Writer.ApplyShipmentProgress` (Quyết định 1). Task 31 dựng `order.Writer{Events, Queue, Realtime}`.
- `TestRetryJobFiftyParallel` (M0, `handlers_jobs_test.go:261`) đỏ một lần khi máy tải cao: thân trả lại từ khóa chống
  trùng có khoảng trắng khác thân lần đầu (`{"id": …}` so với `{"id":…}`) — câu trả lời lưu dạng jsonb bị chuẩn hóa?
  Chạy lại qua. Không thuộc task này; nên xem ở một task M0.

## Reviewer

Kết luận: ĐẠT

- Lần 1 (bản đã stage, sau gộp `main` 371a9d3): ĐẠT, không phát hiện phải sửa. Đã kiểm: khóa đơn trước rồi số dư,
  giữ hàng (ADR-46); mọi đổi giữ hàng của đơn chỉ đi qua đường đã khóa đơn nên đọc không khóa an toàn; mọi nhánh
  `applied=false, nil` nằm trước lần ghi đầu; lưu ý của Task 30 đã xử lý (đếm trước, lỗi inventory trả nguyên); gọi
  lại không đổi gì; seq = version (khoảng trống seq do `AttachShipment` được hàng đợi strict chấp nhận); payload,
  lịch sử không có dữ liệu cá nhân; không migration, không thư viện mới.
- Ghi chú không bắt buộc: (1) kế hoạch Task 31 (dòng ~779) và M3 còn viết `order.ApplyShipmentProgress` là hàm gói —
  Task 31 gọi `order.Writer{…}.ApplyShipmentProgress` (Quyết định 1; ghi ở "Việc phát hiện thêm"); (2) chưa test
  `AttachShipment` trên đơn không `confirmed` chưa có vận đơn (M1 chưa sinh được trạng thái đó) và đơn tenant khác →
  `ErrNotFound` (RLS) — để M2 khi có hủy đơn.
