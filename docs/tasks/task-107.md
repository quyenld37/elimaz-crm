# Task 107 — M3-16: In nhãn theo lô, phiếu nhặt hàng, đóng gói

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 107, M3-16; ca VC04, AT08). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu nào đang có. Thêm mới: `fulfillment.LabelsKind`,
  `fulfillment.LabelsCommand`/`NewLabelsCommand`, `(*Service).CreateLabelBatch`, `(*Service).LabelBatch`, kiểu
  `LabelBatchCreated`/`LabelBatchSummary`/`LabelSkip`/`LabelBatchView`, `ErrLabelBatchInvalid`, `MaxLabelShipments`,
  `LabelFileExpired`, `LabelsKindName`; `queries.Picking` (+ `PickingList`, `PickingLine`, `PickingOrder`),
  `queries.OrderWarehouses`. Hàm không xuất mới `doneJSON` (lệnh done kèm JSON) dùng chung cho lệnh nhãn và
  `quoteOutcome.outcome` (`quote_command.go`, 1 chỗ đổi thân, chữ ký giữ nguyên) — jscpd báo trùng. Các hàm vùng labels
  (`labelsHandlers`… trong `area_labels.go`, không xuất) đổi thân: 1 chỗ gọi mỗi hàm (`wire.go`). Bốn handler
  `CreateLabelBatch`, `GetLabelBatch`, `GetPickingList`, `SetPackingStatus` (`handlers_labels.go`) thay thân 501 (1 chỗ
  dùng: `gen.Handler`). `order.Writer.SetPackingStatus` (Task 97) chỉ được gọi thêm, không đổi chữ ký.
- Gói phụ thuộc ngược (go list): `fulfillment` ← `cmd/crm`, `httpapi`, `cash` (+ `internal/{statement,formats}`),
  `adapters/carrier/*`, `testkit/{shiptest,codtest}`, `tools/fakecarrier/server`, `fulfillment/internal/carriers/fake`;
  `queries` ← `httpapi`. Gói mới `fulfillment/internal/labels` chỉ fulfillment nhập.
- Operation OpenAPI bị chạm, oasdiff: `createLabelBatch`, `getLabelBatch`, `getPickingList`, `setPackingStatus` đã có
  trong hợp đồng (Task M3-04); `openapi.yaml` chỉ thêm `cancelled` vào **mô tả** của `LabelBatchSkipped.reason`
  (không phá vỡ, oasdiff xanh; mã sinh `gen`, `console/src/api` đổi đúng dòng mô tả); bỏ bốn dòng khỏi
  `tests/arch/testdata/m3_pending.txt`; thêm `ErrLabelBatchInvalid` → 400 `invalid_input` ở `errors.go`. Nối kho tệp:
  một dòng `Files: d.Files` trong `cmd/crm/wire_m3.go`.
- Chỗ dùng trên màn hình: chưa màn nào gọi bốn operation (console chỉ có client sinh).
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/labels.sql` (đọc vận đơn + `ord.orders_v` để nhóm lô,
  chèn/đọc/đổi `fulfillment.label_batches`, khóa `shipments` theo id, ghi `label_file_id`, `label_printed_at`). Bảng,
  cột, GRANT đã có ở 0300 → **không cần migration** (số 0325 để trống). Phiếu nhặt và kho của đơn (đóng gói) đọc qua
  view công bố (`fulfillment.shipments_v`, `ord.orders_v`, `ord.order_items_v`, `catalog.variants_v`) trong
  `internal/queries` (`picking_dynsql.go`, mẫu `aged_dynsql.go` của Task 113): fulfillment không được đọc view của
  catalog (`tests/arch/sqlusage_test.go`). Ghi `platform.files` qua `files.Register` (vùng rủi ro cao: chỉ gọi API
  công khai, không sửa gói files).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...` (cả bộ; gồm `internal/labels`),
  `./internal/queries/...`, `./internal/httpapi/ -run 'Label|Picking|Packing|TestM3Routes'`, `./tests/arch/...`,
  `./cmd/crm/...`; order (vùng rủi ro cao) không đổi mã nên chạy trong ci-task theo gói phụ thuộc; cuối cùng cổng kiểm
  ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục files
  (`FileStore.Put`/`Head`, `ServerKey`, `Register`, `DownloadURL`), commands (`CommandKind`, `Outcome`, `DoneHook`,
  `Looker`), queue (`TerminalHook`), realtime (`WarehouseScope`); trong fulfillment: `getAccount`, `(*Service).account`,
  `adapter`, `MarkAuthFailed`, `QuoteCommand` (mẫu lệnh chỉ đọc), `isTimeout`; httpapi: `refusableWrite`, `canAt`,
  `inventoryRead`, `audit`, `orderActor`, `catalog.DefaultSeller`; queries: `scanEach`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các mục trên. Viết mới: nhóm vận đơn thành lô, kiểm tệp PDF, mã lỗi lô
  (`internal/labels`, thuần, không database: việc riêng của vùng nhãn); phân lớp lỗi hãng của lệnh nhãn vì ba bộ phân
  lớp có sẵn (tạo, tra, báo phí) gắn mã lý do và kết quả riêng của từng lệnh — cùng mẫu, khác lý do thay đổi, giữ
  riêng như các lệnh trước. Phiếu nhặt viết mới trong `internal/queries` (chưa có câu gộp dòng đơn theo SKU).

## Soát thiết kế

- 5.1 Lô nhãn thuộc `fulfillment` (`label_batches`, 05); tệp nhãn thuộc `platform.files`, ghi qua `files.Register`.
- 5.2 Không ghi bảng module khác: đóng gói đổi `ord.orders` qua `order.Writer.SetPackingStatus` (public API).
- 5.3 Kho của đơn đọc qua `ord.orders_v` (fulfillment được gọi order); phiếu nhặt cần `catalog.variants_v` → đặt ở
  `internal/queries` (gói ghép view, không nhập module).
- 5.4 Đóng gói nhiều đơn: một transaction (`WriteTx`), một lời gọi `SetPackingStatus` (khóa đơn theo id tăng dần), rồi
  kiểm phạm vi kho của các đơn đã khóa; thiếu phạm vi một đơn → cả lệnh lùi.
- 5.5, 5.6 Tải nhãn là lệnh `command.shipment.labels` (ghi bằng `Enqueue` cùng tx tạo lô); Execute gọi hãng và
  `FileStore.Put` ngoài transaction; tải nhãn chỉ đọc nên Looker luôn `not_sent`, `AutoRetryNotSent = true`.
- 5.7 Mọi câu trong `tenancy.WithTenant`/`WriteTx`; bảng có sẵn RLS (0300).
- 5.8 Module không nhập HTTP; handler chỉ đọc yêu cầu, kiểm phạm vi kho, gọi Service/`order.Writer`/`queries`, audit.
- 5.9 Hai đường ghi đã có `IdempotencyKey`, `x-permission` (`fulfillment.print`, `fulfillment.pack`).
- 5.10, 5.11 không áp dụng (không Valkey, không thư viện mới).
- 5.12 Theo mẫu: lệnh như `QuoteCommand` (Task M3-13); đường ghi như Task 12 (`refusableWrite`); ghép view như Task 113.
- 6.1 In lại tạo lô mới (không sửa lô cũ); OnDone đổi lô bằng câu có điều kiện `status = 'pending'`; đóng gói dùng
  khóa dòng và `version` của order.
- 6.2 `Idempotency-Key` ở hai đường ghi; việc nhãn chống trùng theo mã lô (`DedupeKey` = mã lô); tệp nhãn khóa cố định
  `priv/<tenant>/<mã lô>`: Execute chạy lại gặp `ErrObjectExists` thì dùng lại đối tượng đã ghi (`Head`),
  `files.Register` theo khóa trả id cũ.
- 6.3 OnDone khóa `shipments` của lô theo id tăng dần (`ORDER BY id FOR UPDATE`) rồi mới `platform.files` (nhóm 12b)
  và `label_batches` (sau `shipments` trong nhóm 8) — đúng ADR-46; test hai lô chung vận đơn (thứ tự mảng ngược nhau)
  chạy song song không deadlock; thứ tự câu kiểm bằng tx ghi câu lệnh.
- 6.4 Đường yêu cầu qua `WriteTx` (chạy lại khi 40P01/55P03 có sẵn); việc nền theo lịch thử lại của hàng đợi.
- 6.5 READ COMMITTED + khóa dòng + câu có điều kiện.
- 6.6 Hãng 429/5xx/quá giờ/chưa gửi, lỗi đọc database, lỗi ghi kho tệp → `not_sent` (thử lại; 429 theo `Retry-After`);
  hãng từ chối, không hỗ trợ, PDF sai, quá lớn, tài khoản không dùng được → `done` kèm lô `failed` + `error_code`; hết
  lượt/hết hạn (`dead`/`expired`) → OnTerminal lô `failed` `error_code = 'timeout'`. `in_doubt`: Looker `not_sent` đưa
  lệnh về hàng chờ, OnTerminal không đổi gì.
- 6.7 `RateLimited` → `not_sent` với `RetryAfter` của hãng.
- 6.8 Chết giữa Execute: lệnh in_doubt → tra lại `not_sent` → chạy lại (Put cùng khóa an toàn).
- 6.9 Lệnh hạn 25 s mỗi lần, hết hạn sau 10 phút chờ; tệp ≤ 20 MiB kiểm trước khi Put; tối đa 200 vận đơn mỗi yêu cầu.
- 6.10 Tiền: chỉ đọc `cod_amount` (int64) cho phiếu nhặt.
- 6.11 Quy luật "in không đổi trạng thái vận đơn hay đóng gói" (AT08) kiểm bằng so `status`, `version`,
  `packing_status` trước/sau in lại; "chỉ đi tới" của đóng gói là của `SetPackingStatus` (Task 97, đã có test).
- 6.12 Phiếu nhặt không có SĐT, địa chỉ (test JSON không có khóa `phone`, `address`); payload lệnh chỉ có mã lô; kết
  quả lệnh chỉ có mã tệp, kích thước, băm, mã lỗi; audit chỉ mã.
- 6.13 Mặc định từ chối: phạm vi theo kho của **đơn** (như `createShipment`, Task M3-12); một vận đơn/đơn ngoài phạm
  vi → cả yêu cầu 404 (không in lộ, không cho biết có); không grant → guard 403.

## Quyết định thay chủ shop

1. **Phạm vi quyền theo kho của đơn** (`ord.orders.warehouse_id`) cho cả in nhãn, đọc lô, phiếu nhặt, đóng gói — như
   `createShipment` (Task M3-12): hàng được nhặt ở kho của đơn. Khác: kho của tài khoản hãng (getShipment dùng). Đảo
   ngược: đổi câu `LabelShipments`/`LabelBatchWarehouses` và `queries.Picking` sang `carrier_accounts.warehouse_id`.
2. **201 mã → 400 `bad_request`** (không phải 422 như kế hoạch): `maxItems: 200` của hợp đồng do ogen kiểm trước
   handler, mọi lỗi kiểm hợp đồng của CRM ra 400 (`errors.go`); đổi riêng cho một operation là sửa đường lỗi chung.
   Mức module, 0 hay > 200 mã → `ErrLabelBatchInvalid` (400 `invalid_input`).
3. **Vận đơn đã hủy không in nhãn** → `skipped` lý do `cancelled` (thêm vào mô tả của hợp đồng; trường là chuỗi, không
   enum, không phá vỡ): in nhãn của kiện đã hủy dễ làm kho gửi nhầm.
4. **Thứ tự trong OnDone: khóa `shipments` (id tăng dần) → `files.Register` → gắn nhãn → `label_batches`** (kế hoạch
   ghi `files.Register` trước khóa): ADR-46 đặt `platform.files` (nhóm 12b) sau `fulfillment` (nhóm 8); khóa ngoại
   `shipments.label_file_id` vẫn thỏa vì tệp được chèn trước câu UPDATE trong cùng transaction.
5. **Mã tệp nhãn = mã lô, khóa `priv/<tenant>/<mã lô>`** (cố định): Execute chạy lại không tạo đối tượng mồ côi; gặp
   `ErrObjectExists` thì dùng lại đối tượng đã ghi (Head lấy kích thước, không băm). Khác: mã mới mỗi lần, để việc dọn
   Q16 xử lý mồ côi.
6. **Lỗi hãng không rõ (quá giờ, 5xx, mạng) → `not_sent`, tự thử lại** (tải nhãn chỉ đọc); hết 8 lượt hay hết hạn 10
   phút → lô `failed` `timeout`. Hãng từ chối khóa → lô `failed` `auth` và tài khoản `auth_failed` (`MarkAuthFailed`,
   như lệnh báo phí).
7. **Báo hiệu `label_batch.ready` phát cả khi lô failed** (lô có kết quả), một tín hiệu mỗi kho của các đơn trong lô
   (`fulfillment.print` theo kho); màn hình đọc lại `getLabelBatch`.
8. **Tệp nhãn hết hạn giữ 30 ngày** (`ExpiresAt`) hay mất: `getLabelBatch` vẫn `ready`, không `url`, `errorCode =
   file_expired` (không 500); in lại tạo lô mới.
9. **Đóng gói theo người bán mặc định** (`orderWriteAsDefaultSeller`, như mọi đường ghi đơn của M2): đơn của người bán
   đối tác → 404. Khác: thêm tham số người bán vào hợp đồng (task sau, khi màn đối tác cần).
10. **Phiếu nhặt đặt ở `internal/queries`** (cần `catalog.variants_v`, fulfillment không được đọc view của catalog);
    vận đơn đã hủy được chọn vẫn hiện trong `orders`; hai vận đơn cùng đơn không đếm hàng hai lần.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi viết mã: test đỏ | `labels_test.go` (module) | `go vet ./internal/modules/fulfillment/` | `undefined: fulfillment.LabelsCommand` (FAIL biên dịch) |
| Chia lô: A 3 (id tăng dần), B 1, nhập tay → no_carrier_label, chưa mã → no_tracking_code, đã hủy → cancelled, mã trùng một lần; hãng không in theo lô → mỗi vận đơn một lô; quy luật rapid: mỗi vận đơn đúng một chỗ, mỗi tài khoản một lô | `TestPlan_GroupsByAccount`, `TestPlan_SingleAndUnsupported`, `TestPlan_Partition` | `go test -race ./internal/modules/fulfillment/internal/labels/` | `ok …/internal/labels 4.104s` |
| `%PDF-` và ≤ 20 MiB (đúng biên, quá 1 byte, rỗng, html) | `TestCheckPDF` | như trên | ok |
| VC04: 5 mã → 2 lô (A 3, B 1) + 1 skipped `no_carrier_label`; sau worker hai lô ready, có URL, tệp bắt đầu `%PDF-` có đủ mã hãng; hãng gọi 1 lần/lô; vận đơn có `label_file_id`, `label_printed_at`; vận đơn tay không | `TestLabels_BatchPerAccount` | `go test -race ./internal/modules/fulfillment/ -run TestLabels -count=1` | 9 ca PASS (`ok …/fulfillment 7.922s` sau gộp main) |
| AT08: in lại 3 mã (đơn đã packed) → lô mới; `status`, `version` vận đơn, `version`, `packing_status = packed` của đơn không đổi; vận đơn trỏ tệp lô mới | `TestLabels_ReprintKeepsState` | như trên | ok |
| Thân không phải PDF → failed `invalid_pdf`; 21 MB → `too_large`; hãng từ chối → `carrier_rejected`; không gắn nhãn, không có tệp trên kho | `TestLabels_BadFiles` | như trên | ok |
| 429 → lô vẫn pending (thử lại); dead → failed `timeout`; in_doubt không đổi | `TestLabels_RetryThenTerminal` | như trên | ok |
| OnDone khóa `shipments` (`ORDER BY id FOR UPDATE`) trước `INSERT platform.files`, `UPDATE shipments`, `UPDATE label_batches`; không khóa `label_batches`; `label_printed_at` có giá trị (tx ghi câu lệnh); đột biến đưa khóa ra sau Register → test đỏ | `TestLabels_OnDoneLockOrder` | như trên; đột biến tay rồi khôi phục | đột biến: `--- FAIL: TestLabels_OnDoneLockOrder`; bản thật ok |
| Hai lô chung 3 vận đơn, thứ tự mảng ngược, OnDone song song 8 vòng: mỗi tx chạy đúng 1 lần, không lỗi, cả hai ready | `TestLabels_ConcurrentBatchesNoDeadlock` | như trên | ok |
| Hãng không in nhiều mã một lần → 2 lô, 2 lần gọi một mã | `TestLabels_SinglePerShipmentWhenNoBatch` | như trên | ok |
| Một vận đơn ngoài phạm vi / mã lạ → cả yêu cầu `ErrNotFound`, 0 lô; 0 và 201 mã → `ErrLabelBatchInvalid`; đọc lô ngoài phạm vi, lô lạ → `ErrNotFound` | `TestLabels_ScopeAndInput` | như trên | ok |
| Lệnh nhãn chỉ đăng ký khi có database + kho tệp + adapter; `Check()` không trùng | `TestLabels_CommandWiring` | như trên | ok |
| Phiếu nhặt: vB ở 2 đơn (2 + 1) → một dòng qty 3, tên `product_name`, biến thể `Đen / M`, `orderCodes` hai mã; dòng theo SKU; 3 vận đơn (mã lạ không có), số món 3 / 1, COD, kho; vận đơn hủy cùng đơn không đếm hai lần; tenant B không thấy | `TestPickingList` | `go test -race ./internal/queries/ -run 'Picking\|OrderWarehouses' -count=1` | `ok …/queries 1.962s` |
| Kho của đơn qua `ord.orders_v`, mã lạ không có | `TestOrderWarehouses` | như trên | ok |
| HTTP in nhãn: W2 → 404 không ghi lô; không quyền in → 403; cùng khóa → cùng thân 202, 1 lô `count 2`; 201 mã → 400; audit 1 dòng; pending không url → worker → ready có url; W2 đọc lô → 404; lô lạ → 404 | `TestLabelBatchAPI` | `go test -race ./internal/httpapi/ -run 'TestLabelBatchAPI\|TestPickingListAPI\|TestPackingAPI\|TestM3Routes' -count=1` | `ok …/httpapi 5.097s` |
| HTTP phiếu nhặt: dòng qty 3 với hai mã đơn; JSON không có `phone`, `address`, `shipTo`, `receiver`; W2 → 404; mã lạ → 404 | `TestPickingListAPI` | như trên | ok |
| HTTP đóng gói: W2 → 404, không đơn nào đổi; packed → 2 `updated`; rồi picking → 2 `already_further`; in lại sau packed → vẫn packed (AT08); audit 2 dòng; đơn lạ → 404 | `TestPackingAPI` | như trên | ok |
| Bốn operation không còn 501, danh sách chờ khớp | `TestM3Routes_*`, `TestM3Contract_*` (tests/arch) | như trên; `go test ./tests/arch/` | ok |
| Cổng kiểm của task sau khi gộp main (Task 105): build, vet, lint, gen-check, sqlc-vet, catalog, jscpd (0 trùng mới), gitleaks, oasdiff, test-guard, console-check, test gói đổi + phụ thuộc + vùng cao (fulfillment, files, …) | — | ci-task | `CI-TASK XANH (so với điểm tách nhánh b9fc2511…)`; `ok …/fulfillment 110.947s`, `ok …/httpapi 80.742s`, `ok …/platform/files 86.902s`, `ok …/queries 24.858s`, `ok …/tests/arch 33.240s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `LabelsKind` là hàm `LabelsKind()` (như `QuoteKind()`, `CreateCommandKind()`), không phải biến cấp gói (luật không biến
  toàn cục).
- Test: kế hoạch ghi `internal/labels/labels_test.go`; gói đó chỉ có phần thuần (nhóm lô, kiểm PDF, rapid), các ca
  database/worker nằm ở `internal/modules/fulfillment/labels_test.go` (cần Service, worker; gói internal không nhập được
  gói cha), phiếu nhặt ở `internal/queries/picking_dynsql_test.go`, HTTP ở `internal/httpapi/labels_test.go`.
- "Máy chủ giả trả thân không phải PDF / 21 MB": dùng hãng giả trong bộ nhớ có `Labels` (bọc `fake.Carrier`), cùng nhánh
  mã (`CheckPDF` sau `adapter.Labels`); adapter HTTP giả (`internal/adapters/carrier/fake`) đã có contract test riêng.
- Thứ tự OnDone, 201 mã → 400: xem Quyết định 2, 4.
- Không cần migration 0325 (bảng, cột, GRANT có sẵn ở 0300).
- Đường dẫn nhãn do hãng trả (SSRF): hiện không adapter nào tải nhãn qua URL (hãng giả trả PDF trong thân) → không có
  mã tải URL để giới hạn; adapter hãng thật làm sau phải tự giữ danh sách trắng (ghi ở Việc phát hiện thêm).

## Việc phát hiện thêm

- Dọn lô nhãn quá 30 ngày (`labelsCleanups`, chú thích của khung vùng labels) chưa làm: `label_batches` không có GRANT
  DELETE cho `crm_app`, kế hoạch Task M3-16 không ghi Interface dọn; tệp đã có `ExpiresAt` 30 ngày. Cần task riêng
  (migration GRANT + lịch dọn).
- Adapter hãng thật (GHN, SPX, VTP, BEST) khi làm `Labels` mà hãng trả URL: chỉ tải từ máy chủ đã khai của hãng
  (threat model, SSRF) — ghi vào task adapter tương ứng.

## Reviewer

Kết luận: ĐẠT (lần 1, trên bản đã gộp main sau Task 105).

- Phát hiện phải sửa: không có. Đã kiểm: đóng gói ngoài phạm vi → `order.ErrNotFound` là lỗi từ chối, `WriteTx` lùi cả
  transaction, test xác nhận đơn vẫn `none`; view của phiếu nhặt đều `security_invoker`; ghi lại tệp nhãn với
  `SHA256 = nil` hợp lệ; `target` gán lại mỗi lần chạy, `ReadyLabelBatch`/`FailLabelBatch` có điều kiện `pending`.
- Ảnh hưởng dự kiến khớp thực tế, không sót nơi gọi; 10 quyết định thay chủ shop trong phạm vi kế hoạch, spec, an toàn.
- Ghi chú không bắt buộc (không sửa, không ảnh hưởng tính đúng): ca in_doubt của `TestLabels_RetryThenTerminal` gọi khi
  lô đã failed; `SetShipmentsLabel` gắn tệp cả vận đơn mất mã hãng giữa chừng (hiếm); `LabelBatch` có thể chốt thêm
  `len(whs) == 0 → ErrNotFound` (vận đơn không bao giờ bị xóa nên không xảy ra).
