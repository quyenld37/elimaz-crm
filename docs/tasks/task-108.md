# Task 108 — M3-17: Đổi địa chỉ sau khi đã gửi hãng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 108, M3-17; ca DH15, WAT27; Q-23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `order`: thêm `CheckShipAddress` (kiểm và chuẩn hóa địa chỉ giao như `ApplyAddressChange`; tách từ thân
    `ApplyAddressChange`, hàm này gọi lại nó — không đổi hành vi). `ApplyAddressChange`: 1 chỗ gọi mã thật mới (fulfillment), 1 chỗ gọi trong test (hàm phụ `changeAddress`, `order/shipping_test.go:80`, dùng bởi 2 test).
  - `fulfillment`: thêm `AddressChange`, `AddressChangeInput`, `AddressResolveInput`, `AddressChangeResult`,
    `AuditRecord`, `AuditFunc`, `UpdateAddressKind()`, `UpdateAddressCommand`, `NewUpdateAddressCommand`,
    `(*Module).AddressCommand`, `(*Service).RequestAddressChange`, `(*Service).ResolveAddressChange`, lỗi
    `ErrAddressChangeOpen`, `ErrAddressChangeNotAllowed`. Không ký hiệu cũ nào đổi chữ ký.
  - `fulfillment/internal/carriers/fake`: thêm `SetUpdateMode` + hằng `UpdateMode*`; `Capabilities()` thêm cờ
    `UpdateAddress` (mặc định false: test cũ không đổi).
- Gói phụ thuộc ngược (go list): `order` ← cmd/crm, httpapi, cash, cash/internal/refunds, fulfillment, testkit/codtest,
  testkit/shiptest. `fulfillment` ← cmd/crm, adapters/carrier/{,best,carriertest,fake,manual,spx,vtp}, httpapi, cash
  (+ internal/formats, statement), fulfillment/internal/carriers/fake, testkit/{codtest,shiptest}, tools/fakecarrier/server.
- Operation OpenAPI bị chạm, oasdiff: `requestAddressChange`, `resolveAddressChange` (đã có trong hợp đồng từ Task M3-04;
  không sửa `openapi.yaml`, oasdiff không đổi); bỏ hai dòng khỏi `tests/arch/testdata/m3_pending.txt`. Mã lỗi chỉ dùng mã
  có sẵn: `already_exists`, `invalid_transition`, `address_unmapped`, `address_incomplete`, `invalid_input`, `not_found`.
- Chỗ dùng trên màn hình: chưa có (Task 134 M3-43 vẽ nút đổi địa chỉ). `console/src/api/**` không đổi (hợp đồng không đổi).
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/address.sql` (khóa, chèn, đổi trạng thái yêu cầu; đọc
  target của lệnh; ghi `ship_to` vận đơn). Bảng `fulfillment.address_change_requests` (có từ 0300). Migration mới
  `0329_fulfillment_address_change_note.sql`: pha mở rộng, thêm cột `note text` (≤ 1000 ký tự, đã che SĐT/địa chỉ có số)
  và GRANT UPDATE (note) cho crm_app — vì hợp đồng `AddressChange.note` cần chỗ lưu.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/modules/order/... ./internal/httpapi/...
  ./cmd/crm/... ./tests/arch/...`; ci-task.sh một lần (order, fulfillment là vùng rủi ro cao → cả bộ vùng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands.CommandKind,
  commands.Abandon, logging.ScrubText, queue.Enqueue); trong fulfillment: `lockForCancel` (khóa đơn rồi vận đơn, kèm kho,
  mode, tracking), `checkMapped`/`LocationMap` (Task 109/M3-12), `classifyCreateError`, `targetFailed`, `ResolveAccount`,
  `publishShipment`, `commandBusy`, `shipToDoc`; trong order: `Writer.ApplyAddressChange` (Task 97), `cleanAddress`.
- Dùng lại / viết mới vì:
  - Dùng lại `lockForCancel` (đúng thứ tự khóa ADR-46 nhóm 7 → 8, có Authorize), `checkMapped`, `classifyCreateError`,
    `ResolveAccount` + `LocationMap` (như lệnh hủy, lệnh báo phí), `commands.Abandon` (bỏ lệnh in_doubt khi nhân viên tự
    xác nhận), `shipToDoc` (dạng cột `ship_to`), `ApplyAddressChange`.
  - Viết mới `order.CheckShipAddress`: fulfillment phải kiểm địa chỉ lúc tạo yêu cầu (trước khi gọi hãng) mà không được
    nhập `customer`; tách từ `ApplyAddressChange` để một chỗ kiểm (không chép luật).
  - Viết mới lệnh `UpdateAddressCommand` (không gộp với `CancelCommand`): cùng khung (target → gọi hãng → OnDone/OnTerminal)
    nhưng khác bảng, khác kết quả, khác lý do thay đổi. jscpd/dupl nếu báo phần `target` giống lệnh hủy: giữ riêng vì lý do
    trên.
  - `AuditFunc`: fulfillment không được nhập identity (tests/arch moduleCalls) mà hook OnDone của lệnh (chạy trong worker)
    phải ghi audit khi địa chỉ đơn đổi (Task 97 dặn) → cmd/crm truyền hàm bọc `identity.RecordAudit`.

## Soát thiết kế

Mục 5:
- 5.1–5.3: yêu cầu đổi địa chỉ thuộc fulfillment (bảng của fulfillment); địa chỉ đơn chỉ đổi qua `order.Writer.ApplyAddressChange`
  (public API của order), không ghi `ord.orders`. Đọc đơn qua `order.Get`/`order.LockSnapshot`.
- 5.4: tạo yêu cầu + ghi lệnh trong một transaction (WriteTx); xác nhận (OnDone hay resolve) đổi yêu cầu, `ship_to`, địa
  chỉ đơn, audit trong cùng một transaction.
- 5.5: gọi hãng sau commit qua lệnh `command.shipment.update_address`.
- 5.6: gọi hãng chỉ qua lệnh có `in_doubt`; `AutoRetryNotSent = false`; không rõ → yêu cầu `unknown`, nhân viên xác nhận.
- 5.7: bảng có sẵn RLS, unique ghép tenant; mọi câu trong `tenancy.WithTenant`/WriteTx; chèn lấy tenant từ `app.tenant_id`.
- 5.8: module không nhập net/http; handler mỏng.
- 5.9: hai operation có `x-permission: fulfillment.ship`, `IdempotencyKey`; phạm vi kho kiểm dưới khóa (`shipAuthorize`).
- 5.10: không áp dụng: không dùng Valkey. 5.11: không thêm thư viện. 5.12: theo mẫu lệnh hủy (Task M3-14) và vùng manual.

Mục 6:
- 6.1: hai yêu cầu cùng vận đơn: cả hai khóa đơn rồi vận đơn (`lockForCancel`), kiểm "còn yêu cầu mở" dưới khóa; index
  unique `address_change_requests_open` là chốt cuối (vi phạm unique → `ErrAddressChangeOpen`). Có test hai yêu cầu song song.
  Resolve và OnDone cùng khóa vận đơn rồi yêu cầu (`FOR UPDATE`), chỉ đổi khi yêu cầu còn ở trạng thái cho phép.
- 6.2: Idempotency-Key ở HTTP; lệnh có DedupeKey `address:<id yêu cầu>`.
- 6.3: thứ tự khóa ADR-46: ord.orders (7) → shipments → address_change_requests (8) → platform.jobs (13, `Enqueue`,
  `Abandon` cuối). ApplyAddressChange khóa lại đơn đã khóa (không đổi thứ tự).
- 6.4: WriteTx/RetryTx chạy lại được: thân hàm gán lại kết quả mỗi lần.
- 6.5: READ COMMITTED + khóa dòng. 6.6: lệnh `dead` (chắc chưa gửi, hãng từ chối) → yêu cầu `rejected` (OnTerminal).
- 6.7: 429 → not_sent; không tự gửi lại (AutoRetryNotSent false) → dead → rejected, nhân viên tạo yêu cầu mới.
- 6.8: worker chết giữa lúc gọi hãng → in_doubt → OnTerminal → `unknown`; Looker luôn `unknown` (không tra được kết quả
  sửa địa chỉ ở mức chung).
- 6.9: lệnh timeout 20 s. 6.10: không áp dụng: không đụng tiền.
- 6.11: rapid: không áp dụng riêng ở task này (quyết định thuần nhỏ, test bảng đủ mọi ca trong `internal/address`); quy
  luật "địa chỉ đơn chỉ đổi khi hãng nhận" có test luồng mỗi nhánh.
- 6.12: địa chỉ mới là dữ liệu cá nhân: chỉ ở cột `new_address`, `ship_to`, `ord.orders`; không vào payload lệnh (chỉ id),
  không log, audit chỉ `changed_fields` `ship_address` (giá trị bị che theo khóa `address`); ghi chú qua `logging.ScrubText`.
- 6.13: quyền `fulfillment.ship` theo kho của tài khoản hãng của vận đơn, kiểm dưới khóa; thiếu → 403.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): tách `checkShape`, `validateUnits` khỏi `ApplyAddressChange`, không đổi hành vi | `TestApplyAddressChange`, `TestApplyAddressChange_Delivered` và cả gói order | `go test -race ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 216.850s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc, tách `checkShape`, `validateUnits`): ĐẠT, không phát hiện phải sửa (thứ tự kiểm, lỗi, JSON giữ nguyên). Ghi chú đã làm: sửa số chỗ gọi trong mục Ảnh hưởng; commit 2 phải có test nhánh mã địa giới không có.

Kết luận: ĐẠT (commit 1)
