# Task 91 — M3-00: Đối chiếu hiện trạng sau M1, M2 và task chung; tiêu chí nghiệm thu; soát bảo mật M3

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 91 = M3-00). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu. Gộp phần M3 của Task 439 (review độc lập M2, E-02).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task không sửa mã).
- Gói phụ thuộc ngược (go list): không có.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (chỉ đọc migration để đối chiếu).
- Test sẽ chạy: không có test mã; kiểm tài liệu bằng lệnh đếm mã ca (bảng Bằng chứng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm, kiểu nào.
- Dùng lại / viết mới vì: theo mẫu có sẵn `docs/m2/doi-chieu-m1.md` (bảng đối chiếu), `docs/nghiem-thu-m2.md` (tiêu chí), `docs/threat-model-m2.md` (STRIDE).

## Soát thiết kế

Không áp dụng mục 5.1–5.12, 6.1–6.13: task chỉ đổi tài liệu, không chạm database, tiền, quyền, hàng đợi. Các điểm thiết kế phát hiện khi đối chiếu (thứ tự khóa của `tenancy.PutRoute`, khóa chống trùng webhook theo tài khoản, vận đơn kẹt `creating`, hãng hoàn khi đơn `cancel_requested`) được ghi thành quyết định Q-T91-x và giao cho task M3 tương ứng, không làm ở đây.

## Kết quả đối chiếu

Bảng đầy đủ (mọi dòng phần M0, G-M1-1…11, G-M2-1…9; cột Giả định / Thực tế / Ảnh hưởng tới task / Kế hoạch đã sửa) ở
`docs/m3/doi-chieu-m2.md`. Tóm tắt:

| Dòng | Kết quả | Chỗ lệch chính | Task bị ảnh hưởng |
|---|---|---|---|
| M0 | khác (tên) | `events.Emit`/`Register`, `schedules.Define`/`RegisterCleanup` là phương thức; không có `testkit.SignAccessJWT` | 6, 8, 11, 14, 20, 29 |
| G-M1-1, 2, 6, 7 | khớp | — | — |
| G-M1-3 | khác (nhỏ) | `warehouses.status ('active','closed')`; `release_reason`; `reconcile_runs` có | 1, 5 |
| G-M1-4 | khác (chữ ký) | Đường ghi tồn là phương thức `inventory.Writer` | 5, 23, 26, 27 |
| G-M1-5 | khác (chữ ký) | `files.Register(ctx, tx, NewFile)`; cột `file_purposes.mode` | 4, 16, 31 |
| G-M1-8 | khác (cấu trúc) | Màn tự đăng ký `console/src/screens/<tên>/route.tsx`; không `features`, không sửa `router.tsx` | 4, 40–47 |
| G-M1-9 | khác (cơ chế) | `SyncDefaultRoles` sau mỗi migrate → bỏ `0305` | 4 |
| G-M1-10 | khác (nhỏ) | Thiếu `route_key`, `shipments.seller_id` (Task 1 thêm, đúng kế hoạch); GRANT UPDATE theo cột; hãng giả là hai gói; cả hai lệnh `AutoRetryNotSent = true` | 1, 3, 10, 12 |
| G-M1-11 | khác (đã hơn) | `actor_id` đã `uuid`; sổ đã chặn ở database (trigger cân, chỉ ghi thêm) | 2, 49 |
| G-M2-1 | khác (chữ ký) | `customer.Writer.RecomputeStats(…, customerID, …)`; thứ tự `customers_v`; consumer đã nghe `order.delivery_reversed` | 6, 21 |
| G-M2-2 | khác (thêm) | `return_items.condition NOT NULL` gồm `missing`; `orders_v` không có `ship_address` | 1, 12, 23 |
| G-M2-3 | khác (chữ ký, E-02) | Phương thức `order.Writer`, `sellerID` sau `tx`, `Actor{TenantID, UserID, Type, CanApproveDiscount}`, `Edit`; `OnShipmentReturning` chỉ nhận `confirmed` | 6, 8, 12, 14, 17, 23–27, 30 |
| G-M2-4, 6, 7, 8 | khớp | — | 28 (truyền trạng thái vận đơn thật vào `orderview`) |
| G-M2-5 | khác (nhỏ) | `order.returned` hai nghĩa | 23, 29 |
| G-M2-9 | khác (chữ ký) | `ParseKeyring`, `tenancy.PutRoute` (nhóm 1 ADR-46), chưa có bí mật webhook theo hãng; `/hooks/{source}` không khóa còn mở | 3, 9, 10 |

Không có lệch nào làm đổi thiết kế (ba điều kiện dừng của Step 1: `returns` ở `order`, `FileStore.Put` có, `RoutedSource` có). Không tách task mới (440–459 chưa dùng).

### Dòng đã đổi trong kế hoạch M3

Tệp kế hoạch nằm ngoài repo CRM (`../../docs/superpowers/plans/`, không có git); bản trước khi sửa giữ ở scratchpad của phiên. Các chỗ đổi:

- Đầu kế hoạch: khối "Đối chiếu với mã thật (Task 91)" với luật đọc tên Q-T91-1.
- "Quyết định thay chủ shop": thêm 12 dòng Q-T91-1…12.
- "Giả định về mốc trước": dòng M0 (phương thức `Registry`, `AccessKeys.Sign`), G-M2-1 (thứ tự `customers_v`, `Writer.RecomputeStats`), G-M2-3 (chữ ký thật đứng đầu ô).
- Global Constraints (dải migration): 0305 bỏ trống. Cấu trúc file: 0305, hãng giả, `console/src/screens`.
- Bảng Phụ thuộc: Task 4 không sửa `router.tsx`.
- Task 2 (93): bỏ nhánh `actor_uuid`. Task 3 (94): dời `internal/adapters/fakecarrier` → `internal/adapters/carrier/fake`, gói trong bộ nhớ giữ. Task 4 (95): bỏ `0305_m3_role_permissions.sql`, bỏ sửa `router.tsx`, bỏ tệp định tuyến `routes.m3.*.tsx`. Task 7 (98): thêm cạnh `cancel_requested` + `Returning`. Task 8 (99): gọi `w.OnShipmentEvent(ctx, tx, sellerID, …, order.StepPickedUp/StepDelivered, …)`, test ca Task 431, `ErrStockShort`. Task 14 (105): lý do cố định `order_cancel`, hủy vận đơn `creating` kẹt. Task 21 (112): không sửa `customer/module.go`. Task 49 (140): 0308 không bỏ cột `cod_transfers`.
- Mục "Bổ sung Task 91 (20/11)" đầu phần Interfaces của Task 1, 6, 7, 8, 9, 10, 11, 12, 14, 20, 23 (số toàn cục 92, 97, 98, 99, 100, 101, 102, 103, 105, 111, 114).
- Task 41–47: đường dẫn `console/src/features/…` → `console/src/screens/…`; tệp M2 sửa là `console/src/screens/don/{ReturnDialog,ReceiveReturnDialog}.tsx`, `console/src/screens/len-don/` (`CustomerPicker`).

## Quyết định thay chủ shop

Chủ shop vắng mặt, giao toàn quyền trong phạm vi kế hoạch và spec. Chi tiết (vì sao, đảo bằng) ở bảng "Quyết định thay chủ shop" của kế hoạch M3.

| # | Chọn | Phương án khác |
|---|---|---|
| Q-T91-1 | Kế hoạch đọc theo chữ ký thật của M2 (phương thức `Writer`, `sellerID` sau `tx`, `Edit`); không lớp bọc | Viết hàm bọc bỏ `sellerID` (trái ADR-29) |
| Q-T91-2 | Hãng hoàn khi đơn `cancel_requested`: `[EffCancelRejected, EffReturning]`; consumer hủy dùng lý do `order_cancel` | Thêm cạnh `cancel_requested → returned` vào máy trạng thái của M2 (sửa module M2) |
| Q-T91-3 | `return_items.condition` thêm `'mixed'` | Bỏ `NOT NULL` |
| Q-T91-4 | Task 6 thêm hằng Go `partial_delivery`, `received` | Dùng chuỗi trần |
| Q-T91-5 | Bỏ `0305`, dùng `SyncDefaultRoles` | Migration chèn quyền (trùng cơ chế) |
| Q-T91-6 | Màn M3 ở `console/src/screens/<tên>/` | Thêm `console/src/features` (lệch cấu trúc thật) |
| Q-T91-7 | Dời adapter HTTP hãng giả vào `internal/adapters/carrier/fake`, giữ gói trong bộ nhớ làm đồ test; khóa trùng webhook theo `routeKey` | Dời cả gói trong bộ nhớ (vướng luật module không nhập adapter) |
| Q-T91-8 | `fulfillment` lọc `order.returned` theo `kind`; `ReceiveReturnQty` kiểm `shipment_id` = vận đơn hiện hành | Kiểm trạng thái vận đơn trong `order` (phải đọc view của `fulfillment`) |
| Q-T91-9 | Không thêm `actor_uuid` (đã `uuid`) | — |
| Q-T91-10 | `tenancy.PutRoute` đầu transaction | Ghi ngoại lệ "chỉ chèn" vào ADR-46 |
| Q-T91-11 | `stock_short` → "Cần xem" + cảnh báo ngày, không thử lại vô ích | Để `dead` chung như hiện tại |
| Q-T91-12 | Xóa dấu `create_in_doubt_at` khi chưa gửi; hủy vận đơn `creating` kẹt sau khi nhân viên tra hãng; "Đơn kẹt" thấy ca này | Sửa `queue.AbandonJob` của M0 gọi `OnTerminal` (đổi nền chung) |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chữ ký móc nối `order` thật | — | `go doc ./internal/modules/order Writer` | `AttachShipmentBy(ctx, tx, sellerID, orderID, shipmentID, …)`, `DetachShipment`, `OnShipmentEvent`, `OnShipmentReturning` cùng dạng; `Cancel(ctx, tx, sellerID, id, version, …)`; `Edit(ctx, tx, sellerID, id, in EditInput, a, …)` |
| Nền chung Task 90 | — | `go doc ./internal/platform/fieldcrypt Keyring`; `go doc ./internal/platform/tenancy.PutRoute`; `go doc ./internal/platform/commands.Abandon`; `go doc ./internal/platform/webhooks.RoutedSource` | `ParseKeyring(spec logging.Secret)`, `Seal(plain, aad) (ct, kid, err)`, `Open(ct, kid, aad)`, `NeedsRewrap`; `PutRoute(ctx, tx, kind, key) error`; `Abandon(ctx, tx, jobID, reason) error`; `RoutedSource{Source; SplitRouted(raw, routeKey)}` |
| Sổ COD M1 | — | `grep -n actor_id migrations/0119_cash_cod.sql` | `actor_id uuid NULL` (dòng 18) |
| Dòng phiếu trả M2 | — | `grep -n condition migrations/0204_order.sql` | `condition text NOT NULL CHECK (condition IN ('sellable', 'damaged', 'missing'))` |
| Sự kiện M3 phải phát đúng tên | — | `grep -rn delivery_reversed internal docs/views.md` | `customer.TopicOrderDeliveryReversed = "order.delivery_reversed"`; `views.md` dòng 91 giữ chỗ "M3 phát" |
| Bảng, view, CHECK fulfillment/cash/ord/inventory | — | đọc `migrations/0118, 0119, 0130, 0117, 0204, 0205, 0115–0122, 0132, 0201, 0290, 0291` | ghi ở `docs/m3/doi-chieu-m2.md` |
| Step 3: mỗi mã ca M3 đúng một lần trong `tieu-chi-m3.md` | — | `for c in DH03 DH15 KH09 VC01…VC18 TI01…TI10 TD02…TD10; do grep -o "\b$c\b" docs/tieu-chi-m3.md \| wc -l; done`; `grep -oE '\b(DH\|KH\|VC\|TI\|TD)[0-9]{2}\b' docs/tieu-chi-m3.md \| sort -u \| wc -l` | không mã nào khác 1; 40 mã khác nhau |
| Step 3: mỗi dòng STRIDE trỏ task có thật | — | đối chiếu cột "Task" của `docs/threat-model-m3.md` với danh sách task M3-00…49 | mọi số trong 1–48; các dòng bắt buộc của kế hoạch có đủ: giả mạo webhook (10, 11), lộ khóa API (3, 9), sửa trộm bảng kê (31, 32), chối bỏ thao tác tiền (2, 30, 32, 33), `.xlsx` độc (31), leo quyền sang kho khác (4, 12, 16), gửi đơn bằng tài khoản người bán khác (1, 48), SSRF URL nhãn (16) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bảng đối chiếu đầy đủ đặt ở `docs/m3/doi-chieu-m2.md` (như `docs/m2/doi-chieu-m1.md` của M2, theo lệnh điều phối viên); tệp task giữ bảng tóm tắt.
- Không chạy `psql \d+` trên database: trạng thái bảng đọc từ migration (task chỉ tài liệu; bộ PostgreSQL chung nhiều phiên đang dùng). Task 92 (M3-01) có test schema thật sẽ bắt mọi lệch còn sót.
- Không chạy `scripts/agent/ci-task.sh`: task không có mã.

## Việc phát hiện thêm

- Task 439 còn hai phần ngoài M3: kế hoạch M5 (B1: `customer.FindOrCreateForOrder`, `AddIdentity` là phương thức `customer.Writer`) và Interfaces Task 79 của kế hoạch M2. Điều phối viên sửa.
- Kế hoạch M3 còn nhiều chỗ viết tắt `order.X(ctx, tx, orderID, …)` ngoài các dòng đã sửa; luật đọc tên Q-T91-1 ở đầu kế hoạch áp cho mọi chỗ đó. Mỗi task M3 điền mục Ảnh hưởng theo chữ ký thật.
- Từ Task 90: `FindWebhookDelivery` không lọc `body_kind='event'`; `requeued` của `resolver.go` đếm cả lệnh `dead` theo Q-5 — xét ở Task 101, 103 khi đụng tới.
- Kế hoạch M3 Q-5 ghi `selftest` có `AutoRetryNotSent = true`, mã thật (Task 90 Q-T90-3) là `false`; không ảnh hưởng M3.

## Reviewer

Kết luận: không cần (task chỉ đổi tài liệu; CLAUDE.md bước 7, kế hoạch M3-00 "không cần reviewer").
