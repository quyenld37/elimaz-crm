# Task 449 — Bán hàng chọn được tài khoản hãng nhập tay trên đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (task thêm của điều phối viên 21/11, số toàn cục M3-x2; nguồn: `docs/tasks/task-133.md` mục Việc phát hiện thêm, dòng đầu). Loại việc (quy trình mục 3): A

Mục tiêu: vai Bán hàng (`sale`: có `fulfillment.ship`, `fulfillment.quote` theo kho, không có `fulfillment.carrier_accounts.read`) thấy được tài khoản nhập mã tay, tự giao của kho của đơn trong khối Vận chuyển để dán mã. Không cấp thêm quyền cho vai nào.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM — kiểu `fulfillment.ShipAccount`, phương thức `(*fulfillment.Service).ShipAccounts`, handler `ListOrderShippingAccounts` (hiện thực `gen.Handler` sinh mới). Không đổi chữ ký nào đang có (0 chỗ tham chiếu bị chạm). Câu sqlc `QuoteAccounts` dùng lại (chỉ sửa chú thích: ids phải là mảng rỗng, không NULL; có thêm người dùng). Hàm nội bộ httpapi: thêm `shipOrder`, `orderAt`; `quoteOrder` giữ chữ ký, thân gọi `orderAt` (2 chỗ gọi `RequestShippingQuotes`, `ListShippingQuotes` không đổi). `area_create.go`: chỉ thêm một dòng chú thích ở `shareAccount` trỏ tới `ShipAccounts`.
- Gói phụ thuộc ngược (go list): `internal/modules/fulfillment` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/adapters/carrier/**`, `internal/modules/cash/**`, `internal/platform/files`, `internal/testkit/{codtest,shiptest}`, `fulfillment/internal/carriers/fake` — chỉ thêm ký hiệu nên không gãy; cổng kiểm của task chạy test gói đổi + gói phụ thuộc.
- Operation OpenAPI bị chạm, oasdiff: THÊM `GET /api/v1/orders/{id}/shipping-accounts` (`listOrderShippingAccounts`, `x-permission: fulfillment.ship`) và schema mới `ShipAccount`, `ShipAccountList`. Không sửa operation cũ → oasdiff không phá vỡ. `make gen` sinh lại `internal/httpapi/gen/**`, `routes_gen.go`, `console/src/api/**`.
- Chỗ dùng trên màn hình: `console/src/screens/shipping/useShipment.ts` (`useManualAccounts` đổi sang API mới, nhận id đơn), `ShippingPanel.tsx` (bỏ điều kiện `fulfillment.carrier_accounts.read` và câu "chỉ hiện với người được xem kết nối hãng"; đọc khi có `fulfillment.ship` ở kho của đơn), `QuoteTable.tsx` (kiểu dòng manual là `ShipAccount`), `shippingTestkit.tsx`, `ShippingPanel.test.tsx`. Không đụng phần sau khi gửi (Task 134 làm song song).
- Câu sqlc, bảng, pha migration: không câu mới, không migration; đọc `fulfillment.carrier_accounts` qua câu có sẵn `QuoteAccounts` (người bán + kho, không khóa).
- Test sẽ chạy: `go test -race ./internal/httpapi/ -run 'TestOrderShippingAccountsAPI|TestShippingQuotesAPI'` (đường mới đi qua `Service.ShipAccounts` trên PostgreSQL thật nên không thêm test riêng ở gói fulfillment; không có lỗi mới nên không đổi `errors_fulfillment.go`, `errors_snapshot_test.go`), console `npx vitest run src/screens/shipping`, `npx tsc --noEmit`, `npx eslint .`; cuối cùng `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm "tài khoản gửi được của đơn"); tìm `QuoteAccounts`, `ShareAccountForShip`, `ListAccounts`, `ActiveAccounts`, `quoteOrder`, `canAt`, `shipAuthorize`.
- Dùng lại / viết mới vì: dùng lại câu sqlc `QuoteAccounts` (đúng lọc người bán + kho, xếp `web_sort`), `catalog.DefaultSeller` + `order.Get` + `canAt` (cùng cách `quoteOrder` của so phí), `h.inventoryRead`. Viết mới `ShipAccounts` vì `ListAccounts`/`AccountView` trả cả cấu hình (dịch vụ, tùy chọn mặc định, webhook, kết quả kiểm) — yêu cầu chỉ được trả id, tên, hãng, chế độ, trạng thái; `ActiveAccounts` giải mã khóa API, không hợp cho đường đọc này. Điều kiện "dùng được" (active, mode api/manual, api phải có adapter) cùng điều kiện với `shareAccount` + kiểm adapter của `Ship` (chú thích trỏ nhau). jscpd báo trùng giữa `quoteOrder` và `shipOrder` mới (cùng việc: đọc đơn theo người bán mặc định rồi kiểm quyền ở kho, cùng lý do thay đổi) → gộp vào `orderAt(…, perm)`; `quoteOrder` giữ tên, chữ ký và hành vi (gọi `orderAt` với `fulfillment.quote`), `TestShippingQuotesAPI` vẫn qua. Test dùng lại `newShipCancelAPIEnv` (máy chủ có bảng hãng) thay vì dựng máy chủ riêng (jscpd báo trùng với `shipment_cancel_test.go`).

## Soát thiết kế

Mục 5:
- 5.1, 5.2, 5.3: dữ liệu của fulfillment, đọc bảng của chính nó; đơn đọc qua `order.Get` (public API). Không ghi.
- 5.4, 5.5, 5.6: không áp dụng: đường chỉ đọc, không gọi ra ngoài.
- 5.7: `h.inventoryRead` chạy trong `tenancy.WithTenant` (crm_app).
- 5.8: module không nhập HTTP; handler mỏng.
- 5.9: không áp dụng: GET, không ghi; vẫn khai `x-permission`.
- 5.10, 5.11: không áp dụng.
- 5.12: theo mẫu đường đọc so phí (`handlers_quotes.go`), test ở gói `_test` trên PostgreSQL thật.

Mục 6:
- 6.1–6.11: không áp dụng: một câu SELECT không khóa, không tiền, không hàng đợi.
- 6.12: câu trả lời không có SĐT, địa chỉ lấy hàng, khóa API, cấu hình, webhookUrl; không log thêm.
- 6.13: quyền theo kho của đơn: guard đòi `fulfillment.ship` (không có ở đâu → 403); handler đọc đơn theo người bán mặc định (đơn lạ/người bán khác → 404 `not_found`), rồi `canAt(u, kho của đơn, fulfillment.ship)` → 403 `forbidden`. Chỉ trả tài khoản cùng người bán và cùng kho với đơn.

## Quyết định thay chủ shop

1. **Đơn ở kho người gọi không có quyền gửi → 403** (như `requestShippingQuotes`, `listShippingQuotes` qua `quoteOrder`), không giấu thành 404 như `createShipment`. Lý do: đường đọc, cùng khối màn với so phí; màn đã hiện câu "không có quyền ở kho này". Phương án khác: 404 như tạo vận đơn.
2. **Chỉ trả tài khoản gửi được ngay**: `active`, mode `api` (hãng có adapter) hoặc `manual` (kể cả tự giao), cùng người bán và kho với đơn — đúng điều kiện `Ship` chấp nhận. Trường `status` vẫn trả (theo yêu cầu) để sau này mở rộng mà không đổi hợp đồng. Phương án khác: trả mọi tài khoản của kho kèm trạng thái để màn giải thích "đang tạm dừng" — cần quyền xem cấu hình, để lại.
3. **Không kiểm trạng thái đơn** ở đường đọc (đơn đã gửi vẫn đọc được): màn chỉ gọi khi khối hiện; tạo vận đơn vẫn kiểm đủ.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test trước code: API chưa có thì đỏ | `TestOrderShippingAccountsAPI` | `go test -race ./internal/httpapi/ -run TestOrderShippingAccountsAPI` (sau `make gen`, trước handler) | `Bán hàng kho MAIN = 501 {"error":"not_implemented"…}, muốn 200` — FAIL |
| Bán hàng (vai sale, kho MAIN, không có `carrier_accounts.read`) thấy tài khoản kho MAIN: api có adapter, nhập tay, tự giao; không kho W2, không tạm dừng, không api thiếu adapter | `TestOrderShippingAccountsAPI` | `go test -race ./internal/httpapi/ -run 'TestOrderShippingAccountsAPI\|TestShippingQuotesAPI' -v` | `--- PASS: TestOrderShippingAccountsAPI (0.40s)`, `--- PASS: TestShippingQuotesAPI (0.56s)` |
| Không lộ khóa, cấu hình: mỗi dòng chỉ id, name, carrierKey, mode, status | `TestOrderShippingAccountsAPI` (`shipAccountRows`) | như trên | PASS |
| Không cấp thêm quyền: Bán hàng gọi `listCarrierAccounts` vẫn 403; vai sale không có `carrier_accounts.read` | `TestOrderShippingAccountsAPI` | như trên | PASS |
| Bán hàng chỉ ở kho W2 → 403; không có `fulfillment.ship` → 403 `forbidden`; đơn lạ → 404 | `TestOrderShippingAccountsAPI` | như trên | PASS |
| Test bắt được lọc sai | đột biến tay `ship_accounts.go`: bỏ điều kiện adapter; bỏ điều kiện `active` | `go test ./internal/httpapi/ -run TestOrderShippingAccountsAPI` | cả hai FAIL, đã trả tệp về bản gốc |
| Màn: vai Bán hàng thấy GHN nhập tay, tự giao của kho đơn (không thấy kho khác, tạm dừng), không còn câu "chỉ hiện với người được xem kết nối hãng", chỉ đọc `/orders/{id}/shipping-accounts`, lưu mã gửi `{carrierAccountId, trackingCode}` | `ShippingPanel.test.tsx` "vai Bán hàng (gửi hãng ở kho của đơn…)" | `cd console && npx vitest run src/screens/shipping` | `Tests 29 passed (29)` |
| Màn: không quyền gửi ở kho đơn → không đọc tài khoản, không dòng nhập tay; API lỗi → câu lỗi riêng | "không có quyền gửi hãng ở kho của đơn: không đọc…", "đọc tài khoản gửi được lỗi…" | như trên | PASS |
| Kiểu, lint màn | — | `npx tsc --noEmit`, `npx eslint src/screens/shipping` | không lỗi |
| Hợp đồng chỉ thêm | oasdiff trong cổng kiểm | `ci-task.sh` | oasdiff qua (không thay đổi phá vỡ) |

## Test bị xóa hoặc tắt

- `ShippingPanel.test.tsx` "không có quyền xem kết nối hãng: câu nói rõ tài khoản nhập tay không hiện": hành vi đó bỏ hẳn (màn không còn cần `fulfillment.carrier_accounts.read`, câu đã xóa). Thay bằng ca "vai Bán hàng (gửi hãng ở kho của đơn, không có quyền xem kết nối hãng): thấy tài khoản nhập tay…" kiểm điều ngược lại, cùng ca "không có quyền gửi hãng ở kho của đơn: không đọc tài khoản gửi được".
- `shippingTestkit.tsx`: `SENDER` bỏ `fulfillment.carrier_accounts.read` (mọi ca màn chạy với quyền như vai Bán hàng); API giả của `/api/v1/carrier-accounts` luôn 403 và ghi lại lần đọc (ca mới kiểm không có lần đọc nào).

## Khác kế hoạch

- Đường đặt theo gợi ý của điều phối viên: `GET /api/v1/orders/{id}/shipping-accounts`, operationId `listOrderShippingAccounts`. Không có lỗi mới nên không sửa `errors_fulfillment.go` (đơn lạ dùng `order.ErrNotFound`, thiếu phạm vi dùng `errForbiddenTarget` sẵn có).
- Thêm dòng `| M3 (21/11) | M3-x2 | 449 | Tài khoản hãng dùng được cho đơn |` vào `docs/superpowers/plans/danh-so-task-toan-cuc.md` (ngoài repo `apps/crm`, không nằm trong commit này).

## Việc phát hiện thêm

(không có)

## Reviewer

Vòng 1: **ĐẠT** — không có phát hiện phải sửa. Reviewer đã kiểm: hợp đồng chỉ thêm `listOrderShippingAccounts` + `ShipAccount`, `ShipAccountList`; thân trả chỉ id, name, carrierKey, mode, status; quyền: guard `fulfillment.ship`, đơn lạ/người bán khác 404, kho khác 403 (`orderAt` + `canAt`), giống `quoteOrder`; lọc gửi được khớp `Ship` (`active`, cùng kho, adapter khi có bảng hãng), `Ids` mảng rỗng đúng nhánh SQL; không lỗi mới nên không sửa `errors_fulfillment.go`; cách cũ trên màn (`listCarrierAccounts`, `canAccounts`, câu gợi ý) đã xóa hẳn; test bị thay đã ghi lý do. Ghi chú không bắt buộc: API giả của màn không mô phỏng lọc adapter (màn chỉ giữ dòng manual, test Go đã kiểm); khóa cache mới vẫn cùng `ACCOUNT_TOPIC` nên vẫn làm mới khi tài khoản đổi.

Kết luận: ĐẠT
