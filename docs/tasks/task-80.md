# Task 80 — M2-23: Đơn trùng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 80, M2-23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn. Mới trong gói `order`:
  `DuplicatesConsumer` (consumer `order_duplicates`, `DuplicatesConsumerName`), `Summary` (kế hoạch gọi `OrderSummary`; xem Khác kế hoạch), `Duplicate`,
  `DuplicateCursor`/`ParseDuplicateCursor`, `CheckDuplicates`, `ListDuplicates`, `GetDuplicate`,
  `Writer.ResolveDuplicate`, hằng `DuplicateNotDuplicate`, `DuplicateCancelOther`, `DuplicateCancelThis`,
  `MatchedByCustomer`, `MatchedByPhone`. `Background` (module.go) thêm phần tử `Consumers` — chữ ký không đổi, một nơi
  gọi (`cmd/crm/modules.go`); test `cmd/crm/main_test.go` (`TestNewModulesRegistersCash`) liệt kê handler của sổ sự
  kiện nên thêm `event.order_duplicates` vào danh sách mong đợi. Dùng lại không đổi: `Writer.Cancel`, `lockOrder`, `LoadSettings`, `normalizedShipPhone`.
- Gói phụ thuộc ngược (go list): `order` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`,
  `internal/testkit/shiptest`.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng đã có từ Task M2-02). Hiện thực
  `checkOrderDuplicates`, `listOrderDuplicates`, `resolveOrderDuplicate` (hết 501). oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có màn đơn trùng trong `console` (màn làm ở task màn hình M2 sau); client sinh sẵn có.
- Câu sqlc, bảng, pha migration: tệp mới `queries/ord/duplicates.sql` (bảng `ord.orders`, `ord.order_items`,
  `ord.order_duplicate_links`, view `customer.customers_v`). Không migration: bảng `order_duplicate_links`, cột
  `duplicate_suspect`, quyền UPDATE (`duplicate_suspect`; `resolved_at, resolved_by, resolution`) đã có ở 0204.
  Sinh lại `internal/modules/order/internal/store` bằng `make gen`.
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (vùng không thuộc danh sách rủi ro cao, nhưng chạm hủy
  đơn → nhả tồn qua inventory), `go test -race ./internal/httpapi/ -run 'OrderDuplicate'`, rồi `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cursor, khóa, events);
  tìm "duplicate", "Cursor", "Consumer" trong `internal/`.
- Dùng lại / viết mới vì:
  - Hủy đơn: `Writer.Cancel` (Task 78) với `CancelDuplicate` — không viết đường hủy thứ hai.
  - Khóa đơn: `lockOrder` / câu `LockOrder` có sẵn (ADR-46 nhóm 7), gọi theo `id` tăng dần.
  - Cửa sổ: `LoadSettings().DuplicateWindow`. SĐT: `normalizedShipPhone` (cùng luật cột `ship_phone`).
  - Consumer theo mẫu `cash.Consumer`; đăng ký qua `background.Parts.Consumers`.
  - Nhãn: `orderview.Label`. Phân trang: con trỏ mờ riêng `DuplicateCursor` (khóa `(created_at, order_id,
    other_order_id)`, ba phần; `customer.ListCursor`/`TimelineCursor` có khóa khác và thuộc module customer). Bản đầu
    mã hóa nhị phân giống `customer.ListCursor` → jscpd báo trùng; đổi sang chuỗi "micro.uuid.uuid" base64 (không còn
    trùng; không gộp vì khác khóa và khác lý do thay đổi). jscpd cũng báo đoạn dựng cặp lặp trong test → gom thành
    helper `dupEnv.pair`.
  - `Summary` mới: Task 81 (`OrderRow`, danh sách đơn) chưa có; khi Task 81 làm thì gộp hoặc giữ kèm lý do.
  - Test HTTP dùng lại `orderAPIEnv`, `m2Prepare`, `m2Body`, `serveAs`, `expect`; test module dùng lại `newM2Env`,
    `m2Input`, `fullAddress`, `newCustomer`, `get`, `events`, `heldOf`, `balance`, `cancel` (không sửa tệp helper chung;
    helper riêng `dupEnv` nằm trong `duplicates_test.go`).

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: `ord.order_duplicate_links`, `ord.orders.duplicate_suspect` thuộc `order` (04 mục 3: "đơn trùng").
- 5.2 Không ghi bảng module khác: chỉ ghi `ord.*`; hủy đơn nhả tồn, trả lượt mã qua public API (đường `Cancel` có sẵn).
- 5.3 Đọc module khác qua view: gốc khách đọc `customer.customers_v` (`root_id`, `full_name`); biến thể chỉ so `variant_id` trên `ord.order_items`.
- 5.4 Nguyên tử: resolve (ghi kết luận + hủy đơn + gỡ cờ) một transaction (`WriteTx` của handler).
- 5.5 Không nguyên tử: phát hiện chạy sau commit bằng consumer `order_duplicates` nghe `order.created`, `order.updated` (không làm chậm tạo đơn).
- 5.6 Gọi ra ngoài: không áp dụng (hủy đơn có vận đơn đi `cancel_requested`, M3 gửi lệnh hãng).
- 5.7 Ngữ cảnh shop: consumer chạy trong WithTenant của sự kiện, handler trong WithTenant/WriteTx; bảng đã có RLS và khóa ngoại ghép `tenant_id` (0204).
- 5.8 Không HTTP trong module: handler mỏng ở `handlers_order_duplicates.go`.
- 5.9 Đường ghi `resolveOrderDuplicate` có `IdempotencyKey` (mode required) và `x-permission: order.duplicates.resolve` — đã có trong hợp đồng.
- 5.10 Valkey: không áp dụng. 5.11 Thư viện mới: không. 5.12 Theo mẫu module `catalog`/`order` (sellerID ở mọi hàm đọc, ADR-29).

Mục 6 (đồng thời):

- 6.1 Hai việc cùng lúc: hai consumer của O1, O2 cùng thấy cặp → mỗi việc khóa một lần đơn X và mọi ứng viên theo `id` tăng dần (không khóa từng cặp: X nằm giữa các ứng viên sẽ thành khóa ngược chiều) rồi `INSERT … ON CONFLICT DO NOTHING` trên khóa chính `(tenant_id, order_id, other_order_id)` với `order_id < other_order_id` → đúng một dòng; chỉ dòng vừa chèn mới bật cờ. Resolve khóa hai đơn, rồi dòng liên kết `FOR UPDATE`; đã có kết luận → `ErrInvalidTransition` (không ghi đè).
- 6.2 Gửi lại: `Idempotency-Key` của middleware; consumer chạy lại được (ON CONFLICT, cờ chỉ bật khi chèn được).
- 6.3 Thứ tự khóa: đơn (nhóm 7, theo id tăng dần) → `order_duplicate_links` (nhóm 7 sau dòng đơn) → (khi hủy) lượt mã nhóm 10 → tồn nhóm 11 qua `Cancel`. Test: hai lần tạo song song rồi worker (TestDuplicates_ConcurrentCreate); bốn lần giao lại cùng sự kiện song song (TestDuplicates_ConsumerReplay).
- 6.4 Lỗi tạm: consumer trả lỗi → hàng đợi chạy lại; handler qua `WriteTx`/RetryTx. Thân hàm gán lại mọi biến (chạy lại được).
- 6.5 Cách ly: READ COMMITTED + khóa dòng; sau khóa đọc lại trạng thái đơn (đơn vừa bị hủy thì bỏ qua cặp).
- 6.6 Việc nền lỗi: lịch thử lại của queue; payload hỏng → `queue.ErrPermanent`.
- 6.7, 6.8: không áp dụng (không gọi ra ngoài; transaction tự lùi).
- 6.9 Treo: câu tìm dùng index `orders_customer_created`, `orders_ship_phone`, `order_items` theo `order_id`; cửa sổ tối đa 7 ngày.
- 6.10 Tiền: không đổi tiền (hủy đi đường có sẵn).
- 6.11 Quy luật: "mỗi cặp nhiều nhất một dòng, cờ = có liên kết mở" kiểm bằng test bảng và test song song (không vùng rủi ro cao mới).
- 6.12 Dữ liệu cá nhân: SĐT không vào log/sự kiện/audit; audit resolve chỉ mã hai đơn, hành động.
- 6.13 Quyền: check `order.create`, list `order.read`, resolve `order.duplicates.resolve` (guard mặc định từ chối); test 403.

## Quyết định thay chủ shop

1. **Hàm đọc nhận `sellerID`** (`CheckDuplicates`, `ListDuplicates`, `GetDuplicate`, `ResolveDuplicate`): kế hoạch ghi chữ
   ký không có `sellerID`; CLAUDE.md (module mẫu catalog, ADR-29) đòi mọi hàm đọc nhận người bán. Chọn thêm tham số,
   handler truyền `catalog.DefaultSeller`. Phương án khác: bỏ chiều người bán (trái ADR-29).
2. **Cửa sổ 0 = tắt hẳn** (không liên kết cả hai đơn tạo cùng một micro giây) — đúng chú thích Q7 "0 = tắt".
3. **`cancel_*` khi đơn đã gửi hãng**: `Cancel` đưa đơn sang `cancel_requested` (chờ hãng); vẫn ghi kết luận `cancelled`
   vì nhân viên đã quyết định hủy, phần còn lại đi đường hãng (M3). Phương án khác: chặn 422 tới khi hãng xác nhận —
   nhân viên phải quay lại xử lý lần hai.
4. **Danh sách và cờ chỉ tính cặp "mở"** = chưa xử lý **và** cả hai đơn chưa hủy. Cặp có một đơn đã bị hủy theo đường
   khác (hủy tay) không hiện ở `listOrderDuplicates`; sau `resolve`, đơn nào hết cặp mở thì gỡ cờ (kể cả đơn vừa hủy).
5. **Cặp đã xử lý mà gọi lại** → `ErrInvalidTransition` (422 `invalid_transition`, mã có sẵn trong hợp đồng; không thêm
   mã lỗi mới vào enum của `openapi.yaml`). Gửi lại cùng khóa chống trùng trả đúng kết quả cũ (middleware).
6. **`listOrderDuplicates`**: `order` là đơn có mã (uuid) nhỏ hơn của cặp, `otherOrder` đơn còn lại; `resolve` trả theo
   chiều đường dẫn. Trang mặc định 50, tối đa 200 (như timeline khách).
7. **`careBy` của `OrderListItem` để trống** ở ba operation này: tên nhân viên đọc qua identity ở lớp HTTP; danh sách đơn
   đầy đủ là Task 81 (M2-24). Trường không bắt buộc trong hợp đồng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| DH09: O1 10:00, O2 13:00 (SĐT X, V1) → sau worker cả hai cờ, một dòng `phone`, có trong list, không hủy, không tăng version, không phát `order.updated` | `TestDuplicates_DH09` | `go test -race ./internal/modules/order/ -run TestDuplicates -count=3 -v` | `--- PASS: TestDuplicates_DH09` ×3 |
| O3 chỉ V3 → không liên kết; O4 25 giờ sau O1 → liên kết O2 (22 giờ), không O1 | `TestDuplicates_DH09` | như trên | PASS |
| check lúc 14:00 (SĐT X, V1) → O1, O2; V3 → rỗng | `TestDuplicates_DH09` | như trên | PASS |
| Cùng khách gốc, SĐT khác → `customer`; check theo khách; đơn đã hủy không liên kết | `TestDuplicates_SameCustomer` | như trên | `--- PASS` ×3 |
| Cửa sổ 0 → không liên kết, check rỗng | `TestDuplicates_WindowZero` | như trên | `--- PASS` ×3 |
| `not_duplicate` → hết cờ; `order.updated` của O2 sau đó → không liên kết lại; xử lý lại → `ErrInvalidTransition`; đơn còn cặp mở giữ cờ | `TestDuplicates_NotDuplicate` | như trên | `--- PASS` ×3 |
| `cancel_other` từ O1 → O2 `cancelled` lý do `duplicate`, hàng nhả (held V1 = 1), kết luận `cancelled`, hết cờ; `cancel_this`; lỗi đầu vào, 404 | `TestDuplicates_Cancel` | như trên | `--- PASS` ×3 |
| O1, O2 tạo song song → đúng một dòng; bốn đơn song song → đủ 6 cặp, worker không lỗi (không deadlock) | `TestDuplicates_ConcurrentCreate` | như trên | `--- PASS` ×3 |
| Consumer tên `order_duplicates`, nghe 2 topic; nhận lại song song không đổi cặp đã xử lý; payload hỏng → `ErrPermanent` | `TestDuplicates_ConsumerReplay` | như trên | `--- PASS` ×3 |
| Phân trang list: 3 cặp, limit 2 → 2 + con trỏ, rồi 1 không con trỏ, không lặp; con trỏ hỏng → `ErrInvalidInput` | `TestDuplicates_ListPages` | `go test -race ./internal/modules/order/ -run TestDuplicates -count=2 -v` | `ok …/order 13.922s` |
| Mọi câu đọc/ghi có điều kiện người bán (ADR-29) | `TestSellerFilterProductCode` | `go test -race ./tests/arch/ -run 'TestSellerFilter\|TestSqlcVet'` | `ok …/tests/arch` |
| Ba handler: check 200 / 403, list 200 / 400 con trỏ hỏng, resolve 200 / 403 / 404 / 422, gửi lại cùng khóa, audit không SĐT | `TestOrderAPI_Duplicates` | `go test -race -count=2 ./internal/httpapi/ -run TestOrderAPI_Duplicates -v` | `--- PASS` ×2 |
| Consumer được nối vào tiến trình | `TestNewModulesRegistersCash` | `go test -race ./cmd/crm/...` | `ok elimaz.com.vn/crm/cmd/crm` |
| Không hỏng test cũ của module order | cả gói | `go test -race ./internal/modules/order/...` | `ok …/order 108.272s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kiểu tóm tắt tên `order.Summary` (kế hoạch: `OrderSummary`) vì linter `revive` chặn tên lặp tên gói.
- Thêm `sellerID` vào chữ ký (Quyết định 1); `ResolveDuplicate` là phương thức của `Writer` (cần `Cancel`).
- Thêm `ListDuplicates`, `GetDuplicate`, `DuplicateCursor` (handler list và resolve cần; kế hoạch chỉ nêu handler).
- Ca "`order.updated` của O2 (đổi ghi chú hãng)": việc sửa ghi chú hãng là Task 77 (chưa gộp vào main). Test dùng
  `Writer.AttachShipmentBy` — đường có sẵn phát `order.updated` của O2 — kèm kiểm sự kiện đã phát.
- Test thêm ngoài kế hoạch: đơn đã hủy không liên kết; ba đơn (gỡ một cặp, đơn còn cặp mở giữ cờ); consumer nhận lại
  song song; `cancel_this`; lỗi đầu vào, cặp không có, cặp đã xử lý; test HTTP ba operation (quyền, 404, 400, gửi lại
  khóa, audit không có SĐT).

## Việc phát hiện thêm

- Đơn bị hủy theo đường khác (hủy tay, đơn nháp hết hạn) không gỡ `duplicate_suspect` của đơn đó và của đơn cùng cặp;
  danh sách đã lọc bỏ cặp có đơn hủy nên không hiện sai, nhưng cờ trên đơn còn lại có thể còn `true`. Muốn sạch thì
  consumer nghe thêm `order.cancelled` để tính lại cờ — để task sau (không nằm trong Interfaces của M2-23).
- `resolveOrderDuplicate` với `cancel_*` chỉ ghi một dòng audit `order.duplicate.resolve` (gắn đơn trên đường dẫn,
  có `otherOrderId`, `action`); không có thêm dòng `order.cancel` cho đơn bị hủy (lịch sử trạng thái vẫn ghi người hủy,
  lý do `duplicate`). Task 81 (lịch sử thao tác) quyết định có cần thêm dòng audit cho đơn bị hủy không (ghi chú reviewer).
- Consumer xét tối đa 100 ứng viên mỗi đơn (LIMIT 100); thực tế khó vượt.
- Màn hình "Đơn trùng" và cảnh báo ở form lên đơn (console) chưa có; client sinh sẵn.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau khi gộp main — main không đổi kể từ điểm tách nhánh
85b71b7).

- Phát hiện phải sửa: không có. Reviewer đã kiểm thứ tự khóa ADR-46 (consumer khóa X và ứng viên theo id tăng dần một
  lần; resolve khóa hai đơn rồi dòng cặp rồi `Cancel`), chạy lại được (ON CONFLICT, cờ chỉ bật khi chèn được), so uuid
  theo byte khớp CHECK và keyset, không log/không SĐT trong audit, mã sinh đúng nguồn, không migration mới.
- Ảnh hưởng dự kiến khớp thực tế; không nơi gọi bị sót; không test bị xóa, tắt, nới.
- Ghi chú không bắt buộc (chuyển vào "Việc phát hiện thêm"): audit `order.cancel` cho đơn bị hủy qua resolve; giờ phát
  hiện cặp lấy từ đồng hồ máy; LIMIT 100 ứng viên.
