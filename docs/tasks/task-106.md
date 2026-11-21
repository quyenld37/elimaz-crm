# Task 106 — M3-15: Chế độ nhập mã tay và hãng tự giao

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 106, M3-15; ca VC14). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Thêm `fulfillment.(*Service).SetManualStatus`, `fulfillment.ManualStatusInput`, sentinel `ErrManualReasonRequired`,
    `ErrManualNotAllowed` (mới; chỗ dùng ngoài module chỉ `internal/httpapi`).
  - `fulfillment.(*Service).Ship`: không đổi chữ ký; thêm nhánh tài khoản `mode = manual` (trước đây trả
    `ErrAccountUnavailable`). Chỗ gọi: `internal/httpapi/handlers_shipment.go` (CreateShipment) và test của
    fulfillment, httpapi — không đổi.
  - `ShipInput.TrackingCode`, `FeeActual` đã có (Task 103); giờ mới có nghĩa với tài khoản tay.
  - Hàm nội bộ `insertForShip` trả thêm mã vận đơn (1 chỗ gọi, `Ship`). `shareAccount` nhận cả mode manual.
  - `viewOf`: nhãn của vận đơn `self` ở `awaiting_pickup` là "Tự giao".
  - Gói mới `fulfillment/internal/manual` (quyết định thuần). `internal/create/create.go`: thêm `ManualTrackingCode`.
  - `accounts.go` `validateCreate`: tài khoản `self` mode manual không cần adapter (ghi chú Task 100); các ca khác
    giữ nguyên (`TestAccounts_Validation` không đổi, vẫn qua).
  - `internal/httpapi/errors.go`: hai dòng đổi sentinel mới sang 422 `invalid_input`, 409 `invalid_transition`.
  - Test dùng chung: `shiptest.ManualAccount` (mới, dùng ở test fulfillment và httpapi); `statusEvents` của test
    fulfillment dời từ `*applyEnv` sang `*shipEnv` (applyEnv nhúng shipEnv, mọi chỗ gọi cũ không đổi) để không trùng
    (jscpd).
- Gói phụ thuộc ngược (go list): fulfillment được nhập bởi `cmd/crm`, `internal/adapters/carrier/**`,
  `internal/httpapi`, `internal/modules/cash/**`, `internal/platform/files` (test), `internal/testkit/{codtest,shiptest}`,
  `tests/e2e`, `tools/fakecarrier/server`. Không ký hiệu cũ nào đổi chữ ký nên chỉ cần build lại; test chạy gói
  fulfillment (vùng rủi ro cao: cả bộ), httpapi, adapters/carrier, cmd/crm, tests/arch.
- Operation OpenAPI bị chạm, oasdiff: `setShipmentManualStatus` (hợp đồng có sẵn từ Task 95, chỉ hiện thực; bỏ dòng
  khỏi `tests/arch/testdata/m3_pending.txt`), `createShipment` (đã có `trackingCode`, `feeActual`). Không sửa
  `openapi.yaml` → oasdiff không đổi.
- Chỗ dùng trên màn hình: console chưa gọi `setShipmentManualStatus` (grep `console/src` ngoài `src/api` rỗng); không
  sửa màn hình trong task này.
- Câu sqlc, bảng, pha migration: thêm `queries/fulfillment/manual.sql`: `MarkShipmentManual` (create_state manual,
  fee_actual), `StaffCancelTaskOpen` (vận đơn có việc `manual_cancel`/`in_doubt_cancel` mở không),
  `ResolveStaffCancelAnomalies` (đóng hai loại đó, tính lại needs_review). Bảng `fulfillment.shipments`,
  `shipment_anomalies` (cột đã GRANT ở 0300). Không cần migration mới (số 0327 không dùng). Đối soát (Task 32) khớp
  `(carrier_account_id, tracking_code)` ở `queries/cash/statements.sql` không lọc theo mode → vận đơn tay khớp như vận
  đơn API, không đổi mã.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...` (cả bộ, vùng rủi ro cao), `./internal/httpapi/...`,
  `./internal/adapters/carrier/...`, `./cmd/crm/...`, `./tests/arch/...`; rồi cổng kiểm ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db.Savepoint,
  logging.ScrubText, tenancy), trong fulfillment: `Ship`/`insertForShip`/`lockShippable` (Task 103),
  `AttachTrackingCode` (gắn mã + bắt trùng `shipments_tracking_key` + phát `shipment.created`, `status_changed`),
  `ApplyTracking` (máy trạng thái, tác động, lịch sử có nguồn và người làm), `lockForCancel`, `openAnomaly`,
  `ResolveCancelAnomaly`; ở httpapi: `shipAuthorize`, `refusableWrite`, `orderActor`.
- Dùng lại / viết mới vì: tạo vận đơn tay đi đúng đường `Ship` (khóa đơn, phạm vi, `AttachShipmentBy`, mã
  `NewShipmentCode`) rồi `AttachTrackingCode` thay cho lệnh gửi hãng — cùng sự kiện, cùng bắt trùng mã như vận đơn
  API; cập nhật tay dựng một `CarrierEvent` tổng hợp và gọi `ApplyTracking(SrcStaff)` — không viết máy trạng thái thứ
  hai. Viết mới: quyết định thuần "ai được ghi tay trạng thái nào" (`internal/manual`), câu đóng việc hủy tay theo cả
  hai loại (câu có sẵn `ResolveCancelAnomaly` chỉ đóng `in_doubt_cancel` và không ghi người làm).

## Soát thiết kế

Mục 5: 5.1 dữ liệu thuộc fulfillment (shipments, shipment_events, anomalies). 5.2 đơn, tồn đổi qua `order.Writer`
(AttachShipmentBy, tác động của ApplyTracking), không ghi bảng ord/inv. 5.3 đọc đơn qua `order.LockSnapshot`/view như
Ship. 5.4 tạo vận đơn tay + gắn đơn + gắn mã trong một transaction; ghi tay trạng thái + đơn + tồn + sự kiện trong một
transaction. 5.5 sự kiện qua `events.Emit` (cash nghe status_changed). 5.6 nhánh tay không gọi adapter, không ghi lệnh
`command.*`. 5.7 mọi câu trong WriteTx/tenancy. 5.8 module không nhập HTTP; handler mỏng. 5.9 `setShipmentManualStatus`
có `x-permission: fulfillment.manual_status` và `IdempotencyKey` (Task 95). 5.10 không áp dụng: không dùng Valkey.
5.11 không thêm thư viện. 5.12 theo mẫu Ship/RequestCancel của fulfillment.

Mục 6: 6.1 hai người ghi tay cùng vận đơn: khóa đơn rồi vận đơn FOR UPDATE (lockForCancel, rồi ApplyTracking khóa lại
trong cùng tx), `version` tùy chọn so dưới khóa → 409; người thứ hai thấy trạng thái mới, cạnh không hợp lệ → 409 (tx
lùi nên không để lại dòng lịch sử hay việc cần xem). Hai đơn cùng mã hãng tay: unique `shipments_tracking_key` bắt trong
savepoint → 409. 6.2 Idempotency-Key của WriteTx; khóa sự kiện `staff:<uuid mới>` mỗi lần ghi. 6.3 thứ tự khóa ADR-46:
đơn (7) → vận đơn (8) → shipment_events/anomalies; không chạm platform.jobs. 6.4 WriteTx chạy lại cả hàm: biến kết quả
gán lại trong thân. 6.5 READ COMMITTED + khóa dòng. 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài. 6.9 không
áp dụng: không câu dài. 6.10 COD hãng thu (int64 đồng) ghi `cod_collected_amount` và payload `codCollected`; sổ COD do
cash ghi khi nghe status_changed như vận đơn API. 6.11 máy trạng thái không đổi (test quy luật của shipstate giữ
nguyên); quyết định mới có test bảng đủ mọi tổ hợp. 6.12 lý do ghi tay là chữ nhân viên gõ → chỉ qua
`logging.ScrubText` (lịch sử, audit). 6.13 `fulfillment.manual_status` theo kho của tài khoản hãng (thiếu → 403); tạo
vận đơn tay vẫn cần `fulfillment.ship`.

## Quyết định thay chủ shop

1. **Tạo vận đơn tay đi qua trạng thái `creating` trong cùng transaction rồi gắn mã bằng `AttachTrackingCode`**, thay
   vì chèn thẳng `awaiting_pickup`. Kết quả nhìn từ ngoài đúng như kế hoạch (vận đơn trả về đã `awaiting_pickup`,
   `create_state = manual`, không lệnh ra ngoài), và vận đơn tay phát đúng hai sự kiện `shipment.created`,
   `shipment.status_changed` như vận đơn API, bắt trùng mã bằng cùng một chỗ. Phương án khác: câu INSERT riêng có mã
   sẵn — phải chép lại phần phát sự kiện, bắt trùng. Đảo ngược được (chỉ mã, không dữ liệu).
2. **Self không nhận mã nhập; tài khoản tay của hãng thật thiếu mã → 400 `invalid_input`** (`ErrShipInvalid`, cùng mã
   đã dùng cho "api có mã nhập tay"), không âm thầm bỏ qua mã. Mã nhập được cắt khoảng trắng hai đầu.
3. **Ghi tay mà máy trạng thái không nhận (cạnh không hợp lệ, trùng trạng thái, giờ cũ hơn sự kiện đã xét) → 409
   `invalid_transition`, transaction lùi** — không lưu dòng lịch sử "bỏ qua" hay việc `invalid_edge` như tin của hãng:
   nhân viên đang đứng trước màn hình, báo lỗi ngay rõ hơn tạo việc cần xem. `returned` cũng 409 (theo kế hoạch).
4. **Lý do chỉ có khoảng trắng → 422 `invalid_input`**; lý do rỗng hẳn đã bị hợp đồng (`minLength: 1`) chặn ở 400 như
   mọi trường sai dạng. Giờ `at` đi trước đồng hồ máy chủ quá 5 phút, `codCollected` âm hay đi kèm trạng thái khác
   `delivered` → 400 `invalid_input`.
5. **Vận đơn api có việc hủy tay**: nhận cả `manual_cancel` lẫn `in_doubt_cancel` (ghi chú điều phối: "hãng đã hủy /
   hãng không cho hủy" cho cả hai), chỉ `cancelled` hoặc `shipping`. Ghi xong mà vận đơn rời `cancel_requested` thì
   đóng cả hai loại việc với `resolution = staff_<trạng thái>` và người làm.
6. **Nhãn "Tự giao"** dùng cho vận đơn `self` ở `awaiting_pickup` (thay "Chờ thu gom": không có hãng tới lấy); trạng
   thái khác giữ nhãn chung. Hợp đồng không có trường tên hãng nên không đặt nhãn ở chỗ khác.
7. **Ghi chú Task 100 (production bộ hãng rỗng):** không đổi `cmd/crm/wire_m3.go` (nối adapter `self` vào bộ hãng ở
   production sẽ bật luôn lệnh gửi hãng, lệnh hủy, đổi cấu hình production ngoài phạm vi). Thay vào đó tài khoản
   `self` mode manual không cần adapter (`validateCreate`), và nhánh tay của `Ship` không đòi adapter. Hãng thật
   (best, vtp, spx) mode manual ở production vẫn cần adapter trong bộ hãng — ghi ở "Việc phát hiện thêm".
8. Migration 0327 không dùng: mọi cột cần ghi đã có và đã GRANT ở 0300.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| VC14: Best mode manual, `trackingCode` → awaiting_pickup, create_state manual, phí thật, 0 việc command; ghi tay shipping rồi delivered (COD thu = COD đơn 530.000) → đơn delivered, đúng 1 dòng xuất kho, 3 status_changed (có codCollected, effects delivered), lịch sử nguồn staff + actor_id, lý do che SĐT | `TestManual_VC14` | `go test -race -count=1 ./internal/modules/fulfillment/ -run TestManual` | `--- PASS: TestManual_VC14` |
| Self: mã hãng = mã nội bộ, nhãn "Tự giao", 0 việc command; self có mã nhập, tay thiếu mã, api có mã nhập → ErrShipInvalid, không để lại vận đơn | `TestManual_SelfAndInputs` | như trên | `--- PASS: TestManual_SelfAndInputs` |
| Trùng mã Best ở tài khoản tay khác → ErrTrackingCodeConflict, đơn 2 không vận đơn, 0 anomaly; cùng mã hãng khác được | `TestManual_TrackingConflict` | như trên | `--- PASS: TestManual_TrackingConflict` |
| Thiếu lý do → ErrManualReasonRequired; returned, api không việc, cạnh sai, trùng, giờ cũ → ErrManualNotAllowed không để lại lịch sử/anomaly; version lệch → ErrVersionConflict; giờ tương lai/thiếu, COD sai → ErrShipInvalid; vận đơn lạ → ErrNotFound | `TestManual_Refusals` | như trên | `--- PASS: TestManual_Refusals` |
| Task 105 để lại: api có manual_cancel → "hãng đã hủy" cancelled (đơn confirmed, giữ hàng), "không cho hủy" shipping (hàng in_transit), việc đóng có người làm; delivered khi có việc → 409 | `TestManual_StaffCancelTask` | như trên | `--- PASS: TestManual_StaffCancelTask` |
| Task 100 để lại: bộ hãng rỗng → self manual tạo được (active) và gửi được; self api, best manual/api → ErrCarrierNotSupported | `TestManual_SelfAccountWithoutCarriers` | như trên | `ok elimaz.com.vn/crm/internal/modules/fulfillment` |
| HTTP: tạo vận đơn tay 202; thiếu `fulfillment.manual_status` → 403; lý do khoảng trắng → 422 invalid_input; cùng khóa → cùng thân, 1 lịch sử, 1 audit che SĐT; delivered + codCollected 200; api không việc → 409 invalid_transition; lạ → 404; trùng mã → 409 tracking_code_conflict; tay thiếu mã → 400 | `TestShipmentManualAPI` | `go test -race -count=1 ./internal/httpapi/ -run TestShipmentManualAPI` | `ok elimaz.com.vn/crm/internal/httpapi 3.179s` |
| Quyết định thuần: mọi tổ hợp mode/việc/đích/lý do; Canonical chỉ tới đúng trạng thái; quy luật rapid; nhãn | `TestCheck`, `TestCanonical`, `TestCheck_Property`, `TestLabel` | `go test -race ./internal/modules/fulfillment/internal/manual/` | `ok` |
| Mã hãng tay: self/hãng thật, cắt khoảng trắng | `TestManualTrackingCode` | `go test -race ./internal/modules/fulfillment/internal/create/` | `ok` |
| `manual.New(key)` giữ khóa, mọi khả năng false, ErrNotSupported, qua contract | `TestAdapter` | `go test -race ./internal/adapters/carrier/manual/` | `ok` |
| `setShipmentManualStatus` không còn 501 | `TestM3Routes_PendingList` (tests/arch) | cổng kiểm ci-task | `ok elimaz.com.vn/crm/tests/arch` |
| Cả bộ gói đổi + phụ thuộc + vùng rủi ro cao (fulfillment, cash, httpapi, e2e, arch…) | — | cổng kiểm ci-task (lần 1) | lần 1: mọi gói `ok`, đỏ gen-check (chưa stage) và jscpd (2 đoạn trùng trong test) → đã sửa; lần 2 (đã stage, đổi testkit nên chạy toàn bộ test): `CI-TASK XANH (so với điểm tách nhánh ba186be…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký `SetManualStatus(ctx, tx, id, in ManualStatusInput, a order.Actor) (ShipmentView, error)` thay cho danh sách
  tham số rời của kế hoạch: thêm `Version` (hợp đồng có trường `version`) và `Authorize` (phạm vi kho, như
  `RequestCancel`), trả vận đơn cho thân 200. `Actor` là `order.Actor` như `Ship`, `RequestCancel`.
- Kế hoạch ghi "Create `internal/adapters/carrier/manual/manual.go` (thay khung của Task 3)": khung `adapter.go` đã
  đúng việc (không gọi hãng, mọi khả năng false), giữ nguyên tên tệp, chỉ thêm `manual_test.go`. Lõi không gọi adapter
  cho tài khoản tay nên không cần thêm gì.
- Đối soát (Task 32) không cần mã: câu khớp bảng kê đã theo `(carrier_account_id, tracking_code)` không lọc mode.

## Việc phát hiện thêm

- Production: tài khoản tay của hãng thật (best, vtp, spx) chưa tạo được vì bộ hãng chỉ dựng khi có hãng giả
  (`cmd/crm/wire_m3.go`). Task nối hãng thật cần cho mode manual dùng được khóa hãng đã biết dù adapter chưa sẵn
  (hoặc nối `carrier.All` không điều kiện).
- `ApplyTracking` ghi `last_synced_at = giờ ghi tay` cho cả ghi tay (FinishShipmentSync); với vận đơn api có việc hủy
  tay, lịch tra trạng thái có thể lùi một chu kỳ. Không sai trạng thái; nếu cần tách "mốc tra hãng" khỏi "mốc ghi tay"
  thì làm ở task lịch tra.
- Console chưa có màn ghi tay trạng thái (task màn hình M3).

## Reviewer

Kết luận: ĐẠT (lần 1, agent reviewer trên phần đã stage sau khi gộp main và ci-task xanh).

- Phát hiện phải sửa: không có. Reviewer đã kiểm riêng: từ chối nghiệp vụ làm lùi transaction (refusableWrite →
  `final=false`, không để lại vận đơn creating, việc tracking_conflict, lịch sử bỏ qua); lịch tra không chọn vận đơn tay
  (`poll.sql` lọc mode api); thứ tự khóa đơn → vận đơn, version so dưới khóa; lý do qua `ScrubText`; đổi lỗi HTTP một chỗ.
- Ảnh hưởng khớp thực tế; không sót nơi gọi `insertForShip`; không test nào bị xóa, tắt, nới; không thư viện mới.
- Ghi chú không bắt buộc: (1) tên câu trong mục Ảnh hưởng ghi nhầm `OpenStaffCancelTask` — đã sửa thành
  `StaffCancelTaskOpen`; (2) lý do rỗng hẳn ra 400 do hợp đồng, chỉ khoảng trắng ra 422 — đã ghi ở Quyết định 4;
  (3) `MarkShipmentManual` không kiểm số dòng — luôn đúng 1 dòng (cùng tx, dòng đã khóa), không sửa.

### Lượt gộp main sau Task 107 (21/11)

- `git merge main` (f9a34e2, Task 107 in nhãn): xung đột duy nhất ở `internal/httpapi/errors.go` — hai bên cùng thêm
  dòng ánh xạ lỗi; giữ cả hai (M3-15: `ErrManualReasonRequired` 422, `ErrManualNotAllowed` 409; M3-16:
  `ErrLabelBatchInvalid` 400), gofmt sạch. `tests/arch/testdata/m3_pending.txt` tự gộp (bỏ `setShipmentManualStatus`).
- `go build ./...` qua; `make gen` không sinh chênh; cổng ci-task của task → `CI-TASK XANH` (gói đổi, phụ thuộc,
  vùng cao: fulfillment, labels, httpapi, tests/arch, tests/e2e…; 225 giây).
- Reviewer trên bản gộp: ĐẠT, không phát hiện phải sửa. Đã soát: mỗi sentinel đúng một dòng ánh xạ; cây gộp = main +
  đúng 18 tệp của e763d13; vận đơn mode `manual` bị `labels.Plan` bỏ qua (`no_carrier_label`, có test); lệnh tải nhãn
  không đổi `status`/`version`; thứ tự khóa vận đơn → `label_batches` giữ ADR-46.
