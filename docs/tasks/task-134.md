# Task 134 — M3-43: Khối Vận chuyển — sau khi gửi: dòng thời gian, hủy, đổi hãng, đổi địa chỉ, cập nhật tay, COD

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 134). Loại việc (quy trình mục 3): A

Mockup đã chốt: `docs/mockups/m3/don-van-chuyen.html` trạng thái D, E, F, G, H và `the-khach.html` (HV12–HV16, chủ
shop OK 21/11).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** `fulfillment.OpenAddressChanges` (hàm đọc mới) và
  phương thức `(*handler).ListShipmentAddressChanges` (ogen sinh interface); không ký hiệu sẵn có nào đổi chữ ký, 0 chỗ
  tham chiếu phải sửa. `changeView` (riêng của gói) không đổi.
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/adapters/carrier/**`,
  `internal/modules/cash/**`, `internal/testkit/{codtest,shiptest}`, `tools/fakecarrier/server`; chỉ thêm hàm nên các gói
  này không đổi hành vi (ci-task chạy test của chúng).
- Operation OpenAPI bị chạm, oasdiff: **thêm** `GET /api/v1/shipments/{id}/address-changes` (`listShipmentAddressChanges`,
  `x-permission: fulfillment.read`, schema mới `AddressChangeList`) — thay đổi mở rộng, không phá vỡ. Dùng (không đổi):
  `getShipment`, `listShipmentEvents`, `listOrderShipments`, `syncShipment`, `cancelShipment`, `cancelOrder`,
  `requestAddressChange`, `resolveAddressChange`, `setShipmentManualStatus`, `getShipmentCod`,
  `getCustomerDeliveryStats`, `getFulfillmentSettings`, `listAdminUnits`.
- Chỗ dùng trên màn hình: `console/src/screens/shipping/` (ShippingPanel nhánh "đã có vận đơn": thay khối tóm tắt `Sent`
  của CreateStateBanner bằng `ShipmentCard`; mới Timeline, CancelDialog, AddressChangeDialog, ManualStatusDialog, CodBox),
  `console/src/screens/don/OrderDetail.tsx` (gắn thẻ Dòng thời gian vận đơn ở cột trái, huy hiệu nhận/hoàn ở thẻ Khách),
  `console/src/screens/len-don/CustomerPicker.tsx` (một chỗ hiện `DeliveryStatsBadge` ở khách đã chọn),
  `len-don/AddressFields.tsx` (commit cấu trúc: tách `ProvinceField`, xuất `WardField` để hộp đổi địa chỉ dùng lại), mới
  `len-don/DeliveryStatsBadge.tsx`, `shipping/{shipmentRules.ts,dialogParts.tsx}`; API giả của test
  (`shippingTestkit.tsx`, `don/donTestkit.tsx`, `e2e/{don,len-don,van-chuyen}.spec.ts`) thêm các đường đọc mới. Không chạm phần chọn tài
  khoản/gửi hãng (`Compose`, QuoteTable, SendForm — Task 449).
- Câu sqlc, bảng, pha migration: thêm câu `OpenAddressChanges :many` trong `queries/fulfillment/address.sql` (đọc
  `fulfillment.address_change_requests`, cột không có địa chỉ; dùng index `address_change_requests_open`). Không migration.
- Test sẽ chạy: `go test -race ./internal/httpapi/ -run 'TestAddressChangeAPI'`, `./internal/modules/fulfillment/ -run
  AddressChange`; `cd console && npx vitest run src/screens/shipping src/screens/len-don src/screens/don`, `tsc`,
  `eslint`; ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared`
  (useWriteAction, errorMessage, queryKey, meta realtimeEntity); trong màn: `Dialog` (staff/staffData), `Act`,
  `TEXT_NO_SHIP_PERM` (shipping/CreateStateBanner), `useGrants`, `useCurrentShipment`, `useDeliveryStats`,
  `useFulfillmentSettings`, `shipmentKey` (shipping/useShipment), `canAt` (inventory/stocktakeRules), `shortTime`
  (app/time), `formatMoney`, `parseMoney`, `reloadOrder`, `entityMeta`, `WardField` + `listAdminUnits` (len-don),
  `.tl` (don.css), `carrierName` (QuoteTable). Phía Go: `fulfillment.Get` (kho của vận đơn để kiểm quyền), `canAt` của
  httpapi, `addressChangeOut`, `inventoryRead`.
- Dùng lại / viết mới vì: viết mới các thành phần trong danh sách Files của kế hoạch; lớp CSS `.cod`, `.cod-c`, `.src`,
  `.tl li.is-ign`, `.risk` chép từ `docs/mockups/m3/m3.css` vào `shipping.css` (chưa có trong console). Hủy đơn đi
  `cancelOrder` của M2 (Q-38), không viết đường thứ hai. Đường đọc yêu cầu đổi địa chỉ đang mở là mới (xem Quyết định).

## Soát thiết kế

- 5.1–5.3: đường đọc mới ở module `fulfillment`, đọc bảng của chính nó; màn hình chỉ gọi API.
- 5.4–5.6: không áp dụng: không thêm đường ghi; màn dùng đường ghi có sẵn.
- 5.7: câu mới chạy trong `inventoryRead` (transaction tenant, RLS); test tenant khác không thấy.
- 5.8: handler mỏng ở `internal/httpapi`, module không nhập HTTP.
- 5.9: GET không cần khóa chống trùng; khai `x-permission: fulfillment.read`. Mọi nút ghi trên màn dùng `useWriteAction`
  (Idempotency-Key sinh khi bấm, giữ khi tự thử lại).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: theo mẫu `listShipmentEvents` (kiểm kho của vận đơn, thiếu quyền → 404).
- 6.1: các lệnh ghi có sẵn đã kiểm version/khóa ở máy chủ; `setShipmentManualStatus` gửi `version` của vận đơn đang xem,
  409 thì đọc lại; hủy đơn gửi `version` của đơn.
- 6.2: Idempotency-Key qua useWriteAction; bấm hai lần không tạo hai lệnh.
- 6.3–6.9: không áp dụng: đường đọc một câu SELECT không khóa.
- 6.10: tiền chỉ hiển thị (int64 từ API, `formatMoney`); "Tiền hãng đã thu" đọc bằng `parseMoney` (số nguyên đồng).
- 6.11: không áp dụng (không chạm vùng rủi ro cao).
- 6.12: đường đọc không trả địa chỉ (chỉ id, chế độ, trạng thái, ghi chú đã che khi lưu); màn không log.
- 6.13: quyền theo kho: GET thiếu `fulfillment.read` ở kho → 404 (test); nút trên màn theo quyền ở kho của đơn
  (`fulfillment.ship`, `fulfillment.cancel`, `fulfillment.manual_status`, `order.cancel`, `cash.cod.read`), máy chủ vẫn kiểm.

## Quyết định thay chủ shop

1. **Thêm đường đọc `listShipmentAddressChanges`** (GET, chỉ yêu cầu đang mở). Lý do: hợp đồng chỉ có POST tạo và POST
   xác nhận; yêu cầu nhập tay (hãng không sửa qua API) cần nút "Hãng đã nhận" mang id yêu cầu, tải lại trang là mất id và
   yêu cầu mở chặn mọi yêu cầu mới (409) — đơn kẹt. Phương án khác: giữ id trong bộ nhớ màn (mất khi tải lại), hay thêm
   trường vào `Shipment` (chạm mọi câu đọc vận đơn). Thêm mới, không phá vỡ, dễ bỏ.
2. **Hộp "Hủy / đổi hãng" chọn "Hủy đơn"** gọi `cancelOrder` (M2, Q-38) với lý do `other` và ghi chú là lý do gõ
   (mockup một ô lý do tự do); "Đổi hãng khác" gọi `cancelShipment`. Phương án khác: mở hộp Hủy đơn của M2 (thêm một
   bước); giữ một hộp đúng mockup.
3. **Thẻ dòng thời gian và "Vận đơn của đơn này"** đặt ở cột trái chi tiết đơn (mockup D), dưới "Sản phẩm", trên "Lịch sử thao tác";
   COD và yêu cầu đổi địa chỉ nằm ngay dưới thẻ vận đơn ở cột phải.
4. **Câu trước khi gửi yêu cầu đổi địa chỉ** không nói trước hãng có sửa qua API hay không (máy chủ mới biết, trả
   `mode`); câu chung "Hãng sửa được qua CRM thì gửi lệnh; không thì CRM ghi yêu cầu, bạn gọi hãng rồi bấm Hãng đã
   nhận", kết quả theo `mode` hiện ngay sau khi gửi.
5. **Người làm trên dòng thời gian:** `ShipmentEvent` không có người (Task 119 chỉ thêm `actorType` ở dòng thời gian
   của đơn); chip nguồn lấy theo `source` (carrier_api/webhook → "Hãng (API)", staff → "Nhân viên", system → "Hệ
   thống"), không hiện tên người.
6. **Nút "Hủy / đổi hãng" khi hãng đã lấy hàng** (đang chuyển, thất bại, đang hoàn): hiện mờ kèm lý do "Hãng đã lấy
   hàng: không hủy được…" thay vì bấm được như ảnh mockup D. Lý do: máy chủ trả 409 `shipment_not_cancellable` cho đổi
   hãng, còn hủy đơn lúc này chỉ tạo việc `cancel_conflict` cho người xem (kế hoạch Task 105) — để nhân viên gọi hãng là
   an toàn hơn. Đảo lại chỉ cần bỏ một nhánh trong `cancelReason`.
7. **"In nhãn"** trên thẻ vận đơn là liên kết sang màn Đóng gói (`/warehouse/pack`, Task 136 in nhãn theo lô), không
   in một nhãn riêng (không có đường in một vận đơn ngoài lô); chỉ hiện với quyền `fulfillment.print` và vận đơn nối API.
8. **"Hủy để đổi hãng"** cho vận đơn đang tạo có lệnh tạo đã kết thúc (`not_sent`, `unknown`; Việc để lại của Task 105):
   nút nhỏ dưới băng kết quả lệnh tạo; với "Không rõ" đòi tick "Đã tra trang hãng: hãng không có vận đơn này" và gửi
   `carrierChecked: true`; không có lựa chọn hủy đơn ở đây (hủy đơn vẫn ở nút Hủy đơn của chi tiết đơn).
9. **Băng VC12 "Hãng từ chối hủy"** của mockup E chưa vẽ: `Shipment` không có trường cho biết lần hủy vừa bị hãng từ
   chối (vận đơn chỉ quay lại "Đang chuyển"); ghi ở Việc phát hiện thêm, không đoán từ dòng thời gian.

## Bằng chứng

Thứ tự: đường đọc API viết test trước (`TestAddressChangeAPI_ListOpen` đỏ 403 vì người dùng kho khác chưa có trong môi
trường, sửa phần dựng test rồi xanh); phần màn hình viết mã cùng lúc với test (không có lần chạy đỏ riêng được ghi lại).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đường đọc yêu cầu đổi địa chỉ đang mở: rỗng / một yêu cầu / rỗng sau khi đóng; không trả địa chỉ; kho khác 404, chỉ order.read 403, vận đơn lạ 404 | `TestAddressChangeAPI_ListOpen` | `go test -race ./internal/httpapi/ -run TestAddressChangeAPI -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 2.818s` |
| Thẻ đã gửi: mã hãng, nhãn, "Cập nhật lần cuối lúc 14:20 22/11 (hãng) · CRM hỏi lúc 14:21 · lần hỏi gần nhất lỗi: …", ba nút (Đồng bộ lại, Đổi địa chỉ, Hủy / đổi hãng) và liên kết "In nhãn" tới `/warehouse/pack`, phí, cân nặng, thu hộ; đã lấy hàng → hủy mờ; thiếu `fulfillment.print` → không có In nhãn | `ShipmentCard.test.tsx` › "mã hãng, nhãn, cập nhật lần cuối…", "thiếu fulfillment.print: không có In nhãn" | `npx vitest run src/screens/shipping` (sau khi gộp main có Task 449, sau vòng review 1) | `Tests 58 passed (58)` |
| VC18 kẹt: "Hãng chưa cập nhật 80 giờ (ngưỡng 72 giờ)" kèm lần hỏi lỗi | "VC18 vận đơn kẹt…" | như trên | qua |
| Đồng bộ lại: một yêu cầu có Idempotency-Key | "Đồng bộ lại: một yêu cầu có khóa…" | như trên | qua |
| TD03 đang yêu cầu hủy: "Đang chờ hãng xác nhận hủy — hàng chưa được trả về kho", nút hủy mờ | "TD03 đang yêu cầu hủy…" | như trên | qua |
| VC11 đổi hãng: lý do bắt buộc, `cancelShipment {reason}`; hủy đơn → `cancelOrder {version, reason: other, note}`; thiếu `order.cancel` → lựa chọn mờ; "Không rõ" → `carrierChecked: true` | 4 ca trong "Hủy / đổi hãng (VC11, TD03, HV12)" | như trên | qua |
| "Hủy để đổi hãng" khi "Chưa gửi được": hộp chỉ có đổi hãng (không lựa chọn hủy đơn, không ô tick), gửi `cancelShipment {reason}` | "'Chưa gửi được': hộp chỉ có đổi hãng…" | như trên | qua |
| DH15 đổi địa chỉ: gửi đúng địa chỉ (người nhận, SĐT giữ), hãng không sửa qua API → "Gọi hãng đổi địa chỉ, sau đó bấm "Hãng đã nhận"", "Địa chỉ trên đơn vẫn là địa chỉ cũ tới khi bấm"; tải lại vẫn xác nhận được; yêu cầu API còn chờ không có nút | 3 ca "Đổi địa chỉ sau khi gửi (DH15, HV13)" | như trên | qua |
| VC14 cập nhật tay: đòi lý do; Thành công gửi `codCollected`, `at` theo giờ VN (16:30 → 09:30Z), `version`; không có "Đã hoàn về kho"; thiếu quyền → mờ | 3 ca "Cập nhật trạng thái tay (VC14, HV16)" | như trên | qua |
| Khung COD bốn ngăn đúng số (112.450.000 ₫ ở "Hãng đang giữ", viền đậm); thiếu `cash.cod.read` → câu riêng | 2 ca "Khung COD bốn ngăn (HV15)" | như trên | qua |
| Dòng thời gian 12 sự kiện, nguồn Hãng (API) / Nhân viên / Hệ thống, bỏ qua hiện mờ kèm lý do; còn trang → "2+ sự kiện", "Xem thêm"; vận đơn cũ "Đã hủy (đổi hãng)" | 3 ca "Dòng thời gian vận đơn (HV14)" | như trên | qua |
| Thẻ khách "Nhận 3 · Hoàn 2 (40%)" + "Hay hoàn"; "Có hoàn"; "Chưa có đơn giao xong" | "Huy hiệu nhận/hoàn của khách (DH03)" | như trên | qua |
| Tên khách ≥ 60 ký tự trong thẻ khách (clamp2), huy hiệu cùng thẻ | `OrderDetail.test.tsx` › "tên khách 60+ ký tự…" | `npx vitest run src/screens/don src/screens/len-don src/screens/shipping` | `Test Files 15 passed (15)`, `Tests 417 passed (417)` (sau khi gộp main, sau vòng review 1) |
| Luật thuần: giờ VN hai chiều, ngày sai → null, giờ kẹt, câu lỗi lạ không lộ chữ thô | `shipmentRules.test.ts` (5 ca) | như trên | qua |
| Bố cục 1512 và 390 không tràn, không cuộn ngang (tên khách 60 ký tự, 12 sự kiện, 112.450.000 ₫, hộp hủy, đổi địa chỉ, cập nhật tay); ảnh so mockup | `e2e/van-chuyen-sau-gui.spec.ts` (D, E kẹt, E đang hủy, F, G × 2 cỡ) | `cd console && CRM_E2E_PORT=5234 npx playwright test e2e/van-chuyen-sau-gui.spec.ts --project=chromium` | `10 passed (5.2s)`; ảnh `console/test-results/van-chuyen-sau-gui-*-{1512,390}.png` so `docs/mockups/m3/png/don-van-chuyen-{1512,390}.png` trạng thái D: cùng bố cục hai cột, cùng thứ tự (dòng thời gian, vận đơn của đơn ở trái; thẻ vận đơn, COD ở phải), chip nguồn, sự kiện bỏ qua viền đứt; khác: không có dòng mô tả phụ dưới sự kiện và tên người (API không có), nút hủy mờ khi đã lấy hàng (Quyết định 6), không có liên kết "Sổ của vận đơn" (chưa có màn) |
| Cổng tĩnh (build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console-check) và test gói đổi + phụ thuộc | ci-task | `scripts/agent/ci-task.sh` (sau khi gộp main, toàn bộ đã stage) | `CI-TASK XANH (so với điểm tách nhánh a6131971…)` |
| Spec cũ của màn Đơn, Lên đơn, Task 133 vẫn qua (thêm đường API giả mới) | `e2e/don.spec.ts`, `e2e/len-don.spec.ts`, `e2e/van-chuyen.spec.ts` | `CRM_E2E_PORT=5234 npx playwright test e2e/len-don.spec.ts e2e/don.spec.ts e2e/van-chuyen.spec.ts --project=chromium` | `16 passed` (don, len-don) + `6 passed` (van-chuyen) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm một đường đọc API (Quyết định 1): `openapi.yaml`, `queries/fulfillment/address.sql`, `fulfillment.OpenAddressChanges`,
  handler, test HTTP; mã sinh lại bằng `make gen`.
- Tệp ngoài danh sách: `shipmentRules.ts` (+ test, luật thuần), `dialogParts.tsx` (chân hộp chung của ba hộp thoại, gỡ
  trùng jscpd), `e2e/van-chuyen-sau-gui.spec.ts`; `OrderDetail.tsx` thêm thẻ dòng thời gian (cột trái) và huy hiệu ở
  thẻ Khách; API giả của `e2e/{don,len-don,van-chuyen}.spec.ts`, `donTestkit.tsx`, `shippingTestkit.tsx` thêm đường mới.
- `CreateStateBanner.tsx`: bỏ khối tóm tắt `Sent` (thay bằng `ShipmentCard`); `ShippingPanel.tsx`: nhánh có vận đơn
  dựng `ShipmentCard` / băng lệnh tạo trong cùng một `<section>` (không dựng lại khối khi gửi xong); không đổi `Compose`.
- Commit cấu trúc riêng: `AddressFields.tsx` tách `ProvinceField`, xuất `WardField` (không đổi hành vi) để hộp đổi địa
  chỉ dùng lại.

## Việc phát hiện thêm

- VC12 "Hãng từ chối hủy: đã lấy hàng" cần một trường trên `Shipment` (ví dụ `cancelRejectedAt`) để màn hiện băng; hiện
  vận đơn chỉ quay lại "Đang chuyển" (Quyết định 9).
- `ShipmentEvent` không có người làm, không có mô tả phụ của hãng (mockup D có "Phạm Thu Trang", "Bưu tá đang đi phát").
- Nút "Giao một phần" (Task 117) chưa có ở khối Vận chuyển; mockup đơn vận chuyển chưa vẽ.
- `console/e2e/van-chuyen.spec.ts` ca A và G (1512, 390) đỏ ngay trên `main` sau khi gộp Task 449: API giả chưa khai
  `GET /api/v1/orders/{id}/shipping-accounts` (đường mới của Task 449) — phần chọn tài khoản không thuộc task này nên
  không sửa; các ca C của spec đó và spec của task này qua.
- Liên kết "Sổ của vận đơn" ở khung COD chờ màn tiền trên đơn (mockup `tien-tren-don.html`).

## Reviewer

Commit 1 (cấu trúc, `AddressFields.tsx`): **ĐẠT** vòng 1 — không phát hiện phải sửa; đổi tỉnh vẫn xóa xã, cùng khóa
truy vấn danh mục tỉnh, cùng nhãn "Tỉnh/thành *", không biến thừa; `WardField` chỉ thêm `export`.

Commit 2 (hành vi): vòng 1 **CẦN SỬA** — bảng bằng chứng ghi "bốn nút" mà test chỉ kiểm ba (thiếu "In nhãn"). Đã sửa:
thêm kiểm liên kết "In nhãn" (`/warehouse/pack`) và ca thiếu `fulfillment.print`. Kèm hai ghi chú không bắt buộc đã sửa
vì ảnh hưởng câu hiện cho nhân viên: hộp mở từ "Hủy để đổi hãng" không còn hiện lựa chọn "Hủy đơn" (`replaceOnly`, đúng
Quyết định 8, có test); khung COD hiện "Đang tải" thay vì câu "không có quyền" khi quyền chưa tải. Ghi chú còn để lại:
"Vận đơn của đơn này" tải lại theo báo hiệu của đơn; người thiếu `fulfillment.read` thấy hộp lỗi ở thẻ dòng thời gian;
409 khi hủy đơn chỉ hiện câu lỗi (đơn đọc lại theo báo hiệu).

Commit 2 vòng 2: **ĐẠT** — phát hiện vòng 1 đã sửa (In nhãn có test href và ca thiếu quyền); `replaceOnly` chỉ truyền từ
"Hủy để đổi hãng"; `CodBox` không gọi API khi quyền chưa tải; không test nào bị xóa, tắt, nới. ci-task chạy lại sau
sửa: `CI-TASK XANH`.
