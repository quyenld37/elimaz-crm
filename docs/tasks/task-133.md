# Task 133 — M3-42: Khối Vận chuyển trên đơn — so phí, gửi hãng, trạng thái lệnh

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 133, M3-42). Loại việc (quy trình mục 3): A. Mockup đã chốt: `docs/mockups/m3/don-van-chuyen.html` trạng thái A, B, C, G (thẻ dán mã khi gửi), H; hành vi HV07–HV11, HV16 chủ shop OK 21/11.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ làm màn hình (console), không sửa Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn gọi operation có sẵn: `requestShippingQuotes`, `listShippingQuotes` (Task 104), `createShipment` (Task 103; tài khoản nhập tay Task 106), `getShipment`, `listOrderShipments`, `resendShipment`, `resolveShipmentCreate` (Task 103), `getCustomerDeliveryStats` (Task 112); thêm `listCarrierAccounts` (Task 100; chỉ khi có `fulfillment.carrier_accounts.read`, để hiện tài khoản nhập mã tay / tự giao vì so phí bỏ qua tài khoản manual) và `getFulfillmentSettings` (cân nặng mặc định mỗi món cho gợi ý). Báo hiệu: `shipping_quote.ready` (entity = đơn), `shipment.changed` (entity = vận đơn), `order.changed` (gắn/gỡ vận đơn).
- Chỗ dùng trên màn hình: tạo `console/src/screens/shipping/{ShippingPanel.tsx,QuoteTable.tsx,SendForm.tsx,CreateStateBanner.tsx,useShipment.ts,shipping.css}`, test `ShippingPanel.test.tsx` + API giả `shippingTestkit.tsx`; sửa `console/src/screens/don/OrderDetail.tsx` chỉ thêm một chỗ gắn `<ShippingPanel>` ở cột phải sau thẻ "Giao tới". Thư mục `shipping` không có `route.tsx` (không phải màn riêng). Thêm spec trình duyệt `console/e2e/van-chuyen.spec.ts`; `docs/mockups/m3/README.md` ghi "OK chủ shop 21/11" cho HV07–HV11, HV16. Test cũ của màn Đơn (`OrderDetail.test.tsx`, `route.test.tsx`) chạy lại; đơn mặc định của test là `new` nên khối không hiện.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/shipping src/screens/don`, `npx tsc --noEmit`, `npx eslint .`; ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared` (useWriteAction, unwrap, errorMessage, ERROR_MESSAGES, queryKey + meta realtimeEntity, shortTime); `formatMoney` (products/productRules); `moneyInput`/`parseMoney` (products/editRules, OrderForm cũng dùng); `canAt` (inventory/stocktakeRules — quyền theo kho); `entityMeta`/`reloadOrder`/`ORDER_TOPIC` (don/orderData); kiểu `.act/.reason` của OrderDetail; CSS `.ship/.qt/.qt-r/.unit` của `docs/mockups/m3/m3.css` chưa có trong console.
- Dùng lại / viết mới vì: dùng lại useWriteAction cho mọi lệnh ghi (khóa giữ nguyên khi tự gửi lại 503 `retry_later`; bấm hai lần nhận lại lời hứa đang chạy), errorMessage (đã có câu cho `quote_stale`, `create_in_progress`, `tracking_code_conflict`, `address_unmapped`, `carrier_account_unavailable`, `shipment_exists`), canAt, formatMoney, moneyInput/parseMoney, shortTime, reloadOrder. Viết mới: hook dữ liệu `useShipment.ts` (chỉ khối Vận chuyển; Task 134 dùng lại `useCurrentShipment`); `shipping.css` chép đúng các luật của `m3.css` mà khối dùng (chỉ token có sẵn). Không đưa vào `shared/` vì chỉ một màn dùng. Test: dùng lại `app/screenTestkit.tsx` (`reply`, `bodyOf`, `Write`, `NOT_FOUND`) và `order()` của `don/donTestkit.tsx`; jscpd lần đầu báo 4 đoạn trùng (khung fetch giả giống donTestkit/orderTestkit, một bước test lặp) → đã gộp vào screenTestkit và hàm `sendBest()`, jscpd 0 đoạn mới. Spec trình duyệt theo `e2e/fixtures.ts` (`forEachViewport`, `json`, `noHorizontalScroll`).

## Soát thiết kế

Task chỉ là màn hình gọi API có sẵn; database, khóa, hàng đợi do Task 103, 104, 106, 112 giữ.

- 5.1–5.8, 5.10: không áp dụng — không thêm dữ liệu, bảng, module, việc nền; màn chỉ gọi API.
- 5.9 Đường ghi có khóa chống trùng: mọi lệnh ghi (`requestShippingQuotes`, `createShipment`, `resendShipment`, `resolveShipmentCreate`) đi qua `useWriteAction`. Có test.
- 5.11 Không thêm thư viện npm.
- 5.12 Theo mẫu màn Đơn M2 (OrderDetail, API giả bằng fetch, meta realtimeEntity).
- 6.1 Hai người cùng gửi: máy chủ chặn (`shipment_exists`, `create_in_progress`); màn hiện câu lỗi và tải lại đơn, không tự gửi lại.
- 6.2 Bấm hai lần, mạng chập: useWriteAction (cùng khóa); test hai lần bấm nhanh = một yêu cầu; 503 hai lần rồi 202 = thành công không hiện lỗi.
- 6.3–6.7, 6.9–6.11: không áp dụng (không chạm database, hàng đợi; tiền chỉ hiện số API trả, không tự tính phí).
- 6.8 Không chắc đã gửi: `createState unknown` → chỉ hai nút "Hãng đã tạo" (đòi mã) / "Hãng chưa tạo", không có "Gửi lại" (HV11); `not_sent` → "Gửi lại" do người bấm, màn không tự gửi.
- 6.12 Dữ liệu cá nhân: màn không log; không hiện thêm SĐT, địa chỉ.
- 6.13 Quyền: nút theo `canAt(grants, "fulfillment.ship" | "fulfillment.quote", order.warehouseId)`; thiếu quyền thì nút mờ kèm lý do (mockup H); máy chủ vẫn kiểm.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| So phí: 2 giá + 1 lỗi "Hãng không trả lời", không ô nào "0 ₫", chọn rẻ nhất mặc định, nhập tay "Không báo phí" | `ShippingPanel.test.tsx` › "2 giá và 1 lỗi: không ô nào hiện 0 ₫…" | `cd console && npx vitest run src/screens/shipping src/screens/don` | `Tests 94 passed (94)` (sau sửa vòng review 1) |
| Tùy chọn gửi: không điền sẵn ghi chú (options trống = tùy chọn mặc định của tài khoản); tự điền thì cảnh báo bỏ mặc định, ô trống thôi ghi "Theo tài khoản", gửi đúng options | › "báo phí quá hạn…" (thân `{ declaredValue }`), "tự điền ghi chú cho hãng: cảnh báo bỏ tùy chọn mặc định…" | như trên | qua |
| HV07: chưa từng so phí → tự hỏi một lần; báo hiệu `shipping_quote.ready` đúng đơn thì đọc lại | › "chưa từng so phí: tự hỏi phí một lần…" | như trên | qua |
| Báo phí quá hạn → nút "Báo phí lại", gửi lại cùng thân | › "báo phí quá hạn: nút gửi thành Báo phí lại…" | như trên | qua |
| Đổi cân nặng sau so phí → "Báo phí lại" với cân nặng mới | › "đổi cân nặng sau khi so phí…" | như trên | qua |
| Bấm "Gửi hãng" hai lần nhanh → một yêu cầu, có khóa | › "gửi giá đã chọn… bấm hai lần nhanh chỉ một yêu cầu" | như trên | qua |
| 409 `quote_stale` → tự báo phí lại, giữ tài khoản đã chọn | › "409 quote_stale: tự báo phí lại, giữ tài khoản đã chọn…" | như trên | qua |
| 503 `retry_later` hai lần rồi 202 → thành công, cùng khóa, không lỗi | › "503 retry_later hai lần rồi 202…" | như trên | qua |
| Khách hay hoàn → băng ngay trên nút gửi, không chặn | › "khách hay hoàn: băng gợi ý ngay trên nút Gửi hãng…", "khách không hay hoàn: không có băng" | như trên | qua |
| `unknown` → hai nút, không "Gửi lại", "Hãng đã tạo" đòi mã + ghi chú | › "unknown: hai nút…", "unknown → Hãng chưa tạo…" | như trên | qua |
| `not_sent` → "Gửi lại" do người bấm; không quyền → mờ | › "not_sent: Chưa gửi được + Gửi lại…", "not_sent, người xem không có quyền gửi…" | như trên | qua |
| `pending`, `rejected`, đã tạo + báo hiệu `shipment.changed` | › "pending…", "hãng từ chối…", "vận đơn đã tạo…" | như trên | qua |
| HV16: nhập mã tay, mã trùng, chỉ có tài khoản tay, tự giao, thiếu quyền xem tài khoản | describe "Tài khoản nhập mã tay, tự giao" (5 ca) | như trên | qua |
| Không quyền gửi ở kho đơn → nút mờ kèm lý do (mockup H) | › "không có quyền gửi hãng ở kho của đơn…" | như trên | qua |
| Chỗ gắn trên chi tiết đơn (confirmed có khối, new không) | `OrderDetail.test.tsx` › "đơn Đã xác nhận: có khối Vận chuyển…" | như trên | qua |
| Bố cục 1512 và 390 không tràn (tên 60 ký tự, 5 dòng), ảnh so mockup | `e2e/van-chuyen.spec.ts` (A, G, C × 2 cỡ) | `cd console && CRM_E2E_PORT=5233 npx playwright test e2e/van-chuyen.spec.ts --project=chromium` | `6 passed (2.7s)` (chạy lại sau sửa vòng review 1); ảnh `console/test-results/van-chuyen-{a,g,c}-{1512,390}.png` so với `docs/mockups/m3/png/don-van-chuyen-{1512,390}.png`: cùng thứ tự khối (so phí, ô kiện, băng hay hoàn, nút đen rộng, câu thu hộ), dòng chọn có viền đỏ trái, dòng lỗi chữ đỏ; khác: các mục ở Quyết định 2–4, 8 |
| Cổng tĩnh + test console + kiến trúc | — | ci-task (lần 2, sau khi gộp đoạn trùng jscpd) | `CI-TASK XANH (so với điểm tách nhánh 501f55c…)`; console `Tests 929 passed (929)`; lần 3 sau sửa review vòng 1: XANH, console `Tests 930 passed (930)`; lần 1 đỏ ở jscpd (4 đoạn trùng, đã gộp) |
| tsc, eslint | — | `cd console && npx tsc --noEmit && npx eslint src/screens/shipping src/screens/don e2e/van-chuyen.spec.ts` | không lỗi |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp ngoài danh sách của kế hoạch: `shipping.css` (lớp của `m3.css`), `shippingTestkit.tsx` (API giả cho test), spec trình duyệt `console/e2e/van-chuyen.spec.ts` (bố cục 1512/390 và ảnh so mockup, theo mẫu `e2e/don.spec.ts`). `OrderDetail.tsx` ngoài chỗ gắn có thêm biến `canEdit` (tách điều kiện nút "Sửa đơn" có sẵn để truyền `onEdit` cho câu "Hãng từ chối … Sửa đơn" của mockup C; không đổi hành vi nút cũ).
- Bước "Test thất bại": test viết trước mã nhưng lượt chạy đầu tiên là sau khi đã có mã (2 ca đỏ do chờ dữ liệu, sửa ở test); không có lượt đỏ "chưa có mã" ghi lại.
- Khối đã gửi (`created`, `manual`) chỉ hiện tóm tắt (mã hãng, mã CRM, nhãn trạng thái, phí, thu hộ); đủ thao tác sau khi gửi, thẻ khách nhận/hoàn là Task 134.

## Quyết định thay chủ shop

1. **Tài khoản nhập mã tay, tự giao lấy từ `listCarrierAccounts`** (so phí bỏ qua tài khoản manual nên không có trong bảng so phí). Operation này cần `fulfillment.carrier_accounts.read` (Chủ, Quản lý, Kho — không có Bán hàng). Người thiếu quyền thấy câu "Tài khoản nhập mã tay chỉ hiện với người được xem kết nối hãng". Phương án khác: thêm operation "tài khoản gửi được của đơn" theo `fulfillment.ship` (đổi hợp đồng, việc của backend) — ghi ở Việc phát hiện thêm. Lý do chọn: không đổi API trong task màn hình, dễ đảo khi có operation mới.
2. **Cho xem hàng chỉ có "Theo tài khoản" / "Không cho xem" (`none`)**, không có "Cho xem", "Cho thử" như mockup: hợp đồng chưa định mã cho hai lựa chọn đó (adapter VTP chỉ nhận rỗng hoặc `none`); tự đặt mã sẽ làm hãng báo lỗi. Thêm lại khi hợp đồng có mã.
3. **Ô "Thu phí khi khách từ chối" để trống mặc định** (mockup điền 30.000): VTP từ chối mọi phí khi từ chối > 0, điền sẵn sẽ làm báo phí VTP lỗi. Trống cả ba ô tùy chọn = theo tài khoản hãng (xem Quyết định 5).
4. **Không có ô kích thước kiện** (mockup không vẽ; `requestShippingQuotes` không nhận kích thước nên nhập kích thước khi gửi luôn ra `quote_stale`). Câu khi đổi ô sau so phí: "… so phí lại để có giá đúng (muốn tính theo kích thước kiện cũng phải so phí lại)".
5. **Khai giá điền sẵn = tổng đơn** (theo mockup), cân nặng trống = theo cài đặt (gợi ý "n món × g"), ghi chú cho hãng **không** điền sẵn (mockup điền): máy chủ coi `options` là trọn bộ, có một ô (kể cả ghi chú) là bỏ toàn bộ tùy chọn mặc định của tài khoản (bên trả phí, lấy hàng, cho xem, phí khi từ chối) — điền sẵn sẽ âm thầm đổi tiền phí ship với mọi đơn có ghi chú hãng (reviewer vòng 1). Màn hiện ghi chú hãng của đơn ở dòng gợi ý ("chỉ gửi khi chép vào ô này"); người dùng tự điền tùy chọn nào thì có băng nói rõ đã bỏ tùy chọn mặc định, ô trống ghi "Không"/"Không thu". Phương án khác: backend gộp options với mặc định (Việc phát hiện thêm). Cân nặng, khai giá, tùy chọn gửi y hệt ở so phí và gửi hãng để `input_hash` khớp.
6. **Không giá nào mà chỉ một dòng chọn được** (ví dụ kho chỉ có một tài khoản nhập tay — tình hình hiện nay) thì chọn sẵn dòng đó; nhiều dòng không giá thì để người xem chọn.
7. **"Hãng từ chối: <mã>"** lấy `subStatus` của vận đơn đã hủy (`cancelKind = create_rejected`, vận đơn mới nhất của đơn); API chưa trả lý do từ chối riêng nên thiếu mã thì chỉ hiện "Hãng từ chối vận đơn."
8. Khối hiện khi đơn `confirmed` hoặc đã có vận đơn hiện hành; đặt ở cột phải sau thẻ "Giao tới" (trang M2 để "Giao tới" ở cột phải, mockup để ở cột trái — không dời phần cũ).

## Việc phát hiện thêm

- Bán hàng (`sale`) có `fulfillment.ship` nhưng không đọc được tài khoản nhập tay (thiếu `fulfillment.carrier_accounts.read`): luồng chạy thật bây giờ (nhập mã tay) không làm được với vai Bán hàng. Cần operation liệt kê tài khoản gửi được của đơn theo `fulfillment.ship`, hoặc trả tài khoản manual trong `requestShippingQuotes` với status riêng.
- `CarrierOptions.inspection`: hợp đồng chưa định mã "cho xem", "cho thử".
- Backend: tạo vận đơn có `options` chỉ gồm `note` thì `options != {}` nên bỏ tùy chọn mặc định của tài khoản (`area_create.go` shipPlan, dòng `p.options == (Options{})`); so phí cũng vậy (`quote_request.go` requestOptions: `in != nil` là bỏ `default_options`). Nên gộp từng trường với mặc định của tài khoản; khi đó màn có thể điền sẵn ghi chú hãng của đơn như mockup.
- API không trả lý do hãng từ chối tạo vận đơn (chỉ `cancelKind`), màn chỉ hiện được `subStatus` nếu có.

## Reviewer

Vòng 1: **CẦN SỬA** — ô "Ghi chú cho hãng" điền sẵn ghi chú của đơn làm `options` khác rỗng, máy chủ (so phí `requestOptions`, gửi hãng `shipPlan`) bỏ toàn bộ tùy chọn mặc định của tài khoản (`payer`, `pickup`, …) một cách im lặng, trong khi màn ghi "Theo tài khoản". Đã sửa: không điền sẵn; băng cảnh báo khi tự điền tùy chọn; ô trống đổi chữ; test "tự điền ghi chú cho hãng…", thân so phí ca quá hạn thành `{ declaredValue }`. Ghi chú không bắt buộc đã làm: lý do nút "Hãng chưa tạo" sửa thành 'Vận đơn về "Chưa gửi được", rồi bấm Gửi lại' (đúng `resolveNotCreated` → not_sent); lúc đang tải tài khoản nút ghi "Đang tải tài khoản hãng". Chưa làm (không ảnh hưởng tính đúng): `listCarrierAccounts` một trang 100; lỗi đọc `listOrderShipments` không hiện câu.

Vòng 2: **ĐẠT** — phát hiện vòng 1 đã sửa đúng: `initialForm` không điền sẵn tùy chọn, không ô nào điền thì không gửi `options` (máy chủ dùng mặc định ở cả `requestOptions` và `shipPlan`); chữ "Theo tài khoản" chỉ hiện khi cả ba ô trống, tự điền thì có băng cảnh báo; thân so phí và gửi hãng vẫn giống nhau nên `input_hash` khớp; `isLoading` không kẹt với truy vấn tắt. Không phát hiện mới. Ghi chú không bắt buộc: câu Quyết định 3 đã sửa cho khớp Quyết định 5; `listCarrierAccounts` một trang 100 và lỗi đọc `listOrderShipments` không hiện câu — để lại, không ảnh hưởng tính đúng.

Kết luận: ĐẠT
