# Task 84 — M2-27: Màn Lên đơn nhanh

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 84 = M2-27). Loại việc (quy trình mục 3): A (màn hình). Mockup đã chốt: `docs/mockups/m2/len-don.html` (Q-X8, Task 83).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ đổi `console/` (TypeScript), không sửa mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`, client sinh `console/src/api/**` không đổi. Màn chỉ GỌI operation có sẵn: `createOrder` (POST, Idempotency-Key), `listCustomers`, `getCustomer`, `listAdminUnits`, `searchVariants`, `listWarehouses`, `listBalances`, `checkCoupon`, `checkOrderDuplicates`, `listTrafficSources`, `getOrderSettings`, `getMe`.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/len-don/` (route tự đăng ký `/don/moi`, Q-T57-8, không sửa `router.tsx`); `console/src/app/nav.ts` thêm nhóm menu "Đơn hàng" đứng đầu `NAV_GROUPS` (HV01 của mockup; chỗ dùng: `loadScreens.ts`, `screens.test.tsx`, `shared/index.ts` — đều đọc theo thứ tự, không ghi cứng tên nhóm); `console/src/app/errors.ts` chỉ THÊM dòng (`COUPON_REASON_MESSAGES`: câu cho 10 lý do từ chối mã; câu cảnh báo và lý do chờ của đơn nằm ở `orderRules.ts`); `console/src/app/router.tsx` không sửa nhưng `/` chuyển tới màn đầu tiên được phép, nên người có `order.create` (đủ quyền đi kèm) mở `/` sẽ vào Lên đơn thay vì Danh sách sản phẩm (đúng HV01: Đơn hàng đứng đầu); `console/src/shared/index.ts` thêm export `OrderForm` (dùng lại ở M4, Q-T57-8); e2e mới `console/e2e/len-don.spec.ts` (chụp 1512×767, 390×844). Danh mục dùng chung: `docs/danh-muc-ghi-chu.yaml` (thêm câu "khi nào không dùng" cho OrderForm vào ghi chú `console/src/shared`) và `docs/danh-muc-dung-chung.md` sinh lại bằng `make catalog`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/len-don src/app` (mới: `pricing.test.ts` chạy 200 vector của `internal/modules/order/internal/pricing/testdata/vectors.json`, `orderRules.test.ts`, `OrderForm.test.tsx`, `route.test.tsx`; có sẵn: `errors.test.ts`, `screens.test.tsx`), `npx tsc --noEmit`, `npx eslint .`, Playwright chỉ `e2e/len-don.spec.ts` (mới) và `e2e/shell.spec.ts` (menu có nhóm mới); ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console (useWriteAction, unwrap, ApiError, errorMessage, queryKey, hasPermission); `screens/inventory/Balances.tsx` (`useDebounced`, `SEARCH_DELAY_MS`); `screens/inventory/SupplierPicker.tsx` (mẫu ô gợi ý); `screens/products/productRules.ts` (`formatMoney`); `screens/products/editRules.ts` (`parseMoney`, `moneyInput`); `docs/mockups/m2/m2.css` (lớp đã duyệt).
- Dùng lại / viết mới vì: dùng lại `useWriteAction` (không viết hook gửi thứ hai, G13), `unwrap`/`errorMessage`/`ApiError`, `useDebounced`, `formatMoney`, `parseMoney`, `moneyInput`, `hasPermission`, `queryKey`. Viết mới: `pricing.ts` (bản TypeScript của `pricing.Compute`/`Allocate`/`ManualDiscountBP`; kế hoạch đòi cùng thuật toán, kiểm bằng cùng tệp vector); ô gợi ý khách và sản phẩm viết mới vì cần phím ↑ ↓ Enter và gợi ý hai dòng (SupplierPicker không có điều hướng phím; sửa nó là ngoài phạm vi); CSS: chép các lớp `m2.css` màn cần (`kbd`, `keys`, `sugg`, `picked`, `ltbl`, `sp`, `avail`, `sum`, `inline-f`, `stepper`) vào `len-don.css` như M1 làm (`inventory.css` chép từ mockup), chỉ dùng biến có sẵn. jscpd (lần chạy ci-task đầu) báo 5 đoạn trùng: 3 đoạn trong chính màn (đóng/mở, phím, giữ focus của ba ô gợi ý khách, sản phẩm, xã) → gộp vào `useCombo.ts` (cùng mục đích, cùng lý do đổi); 2 đoạn giống `inventory/documentsTestkit.tsx` và `e2e/stock-documents.spec.ts` (dựng số dư giả, lọc SKU giả) → viết lại phần của màn này (test của hai màn khác nhau, không gộp testkit chéo màn). Lần sau: 0 đoạn mới.

## Soát thiết kế

- 5.1–5.8, 5.10: không áp dụng — không đổi máy chủ, database, module, sự kiện; màn chỉ gọi API có sẵn.
- 5.9 Đường ghi: duy nhất `createOrder` qua `useWriteAction` (khóa `Idempotency-Key` giữ nguyên khi tự gửi lại 503 retry_later / 409 request_in_progress / mất mạng; bấm "Gửi lại" cùng nội dung dùng lại khóa; tạo xong thì khóa mới). `CreateOrderRequest` không có trường `idempotencyKey` (additionalProperties: false; máy chủ dựng khóa nghiệp vụ từ header, Q-T57-4) → chỉ gửi header (ghi ở "Khác kế hoạch").
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: màn tự đăng ký như `screens/products`; test vitest + testing-library với fetch giả như `inventory/documentsTestkit.tsx`.
- 6.1, 6.3–6.9, 6.11: không áp dụng (không chạm database, khóa, việc nền).
- 6.2 Gửi lại: xem 5.9; test "503 hai lần rồi 201 → ba yêu cầu cùng Idempotency-Key".
- 6.10 Tiền: số nguyên đồng (≤ 2^53−1); nhân/chia tỷ lệ trong `pricing.ts` bằng BigInt (tương đương 128 bit của Go); số máy chủ trả là số cuối, màn chỉ tính trước để hiện.
- 6.12 Dữ liệu cá nhân: màn không ghi log; SĐT trong gợi ý tìm khách che giữa (HV05) tới khi chọn.
- 6.13 Quyền: route cần `order.create` và `alsoRequires` `customer.read`, `catalog.read`, `order.read` (API màn gọi đòi); số "có thể bán" chỉ đọc khi có `inventory.read` (không có thì chỉ hiện đang bán/ngừng bán); cảnh báo vượt ngưỡng giảm giá chỉ hiện cho người thiếu `order.discount.approve` (khớp `decideApproval`); máy chủ vẫn kiểm quyền thật.

## Bằng chứng

Thứ tự: `pricing.test.ts`, `orderRules.test.ts`, `OrderForm.test.tsx` viết trước mã; chạy `npx vitest run src/screens/len-don` khi chưa có `pricing.ts` → FAIL (không nạp được mô-đun), rồi viết mã.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `pricing.ts` cho đúng từng con số như Go trên 200 vector | `pricing.test.ts` › "vector chung với Go" › "có đúng 200 bộ", "bộ %i cho đúng từng con số" (200 ca); thêm DH01, DH05, 8 ca lỗi, 5 ca `allocate`, 6 ca `manualDiscountBP` | `cd console && npx vitest run src/screens/len-don/pricing.test.ts` | `Tests 222 passed (222)` |
| Gõ "5678" ở ô khách → chọn khách bằng phím; gợi ý che giữa SĐT | `OrderForm.test.tsx` › 'gõ "5678" ở ô khách, ↓ rồi Enter chọn khách thứ hai; gợi ý che giữa SĐT'; "dưới 4 số: nhắc gõ ít nhất 4 số cuối, không gọi API" | `npx vitest run src/screens/len-don/OrderForm.test.tsx` | `Tests 22 passed (22)` |
| Phím thêm dòng → ô tìm sản phẩm có tiêu điểm | "F2 đưa tiêu điểm vào ô khách; F3 (thêm dòng) vào ô tìm sản phẩm" | như trên | qua |
| Enter trong ô số lượng không gửi form | "Enter trong ô số lượng chỉ sang ô kế (giảm dòng), không gửi đơn" (0 yêu cầu ghi); "Ctrl+Enter lưu và xác nhận; Ctrl+S lưu (mới)"; "Ctrl+Enter ngay trong ô mã (chưa rời ô): gửi đúng mã đang gõ, một lần" | như trên | qua |
| 503 hai lần rồi 201 → ba yêu cầu cùng `Idempotency-Key`, hiện đơn đã tạo; đơn sau khóa mới | "503 hai lần rồi 201: ba yêu cầu cùng Idempotency-Key, một đơn; đơn sau dùng khóa mới"; "hết lượt gửi lại (503 bốn lần): báo bận, nút Gửi lại gửi đúng đơn cũ cùng khóa" | như trên | qua |
| 409 `out_of_stock` → dòng `variantId` tô, câu "Không đủ hàng: còn {available}" | "409 out_of_stock: dòng của variantId được tô, câu 'Không đủ hàng: còn {available}', đơn chưa tạo" | như trên | qua |
| 422 `coupon_rejected` `expired` → "Mã đã hết hạn"; `checkCoupon` từ chối có câu theo lý do | "422 coupon_rejected expired: 'Mã đã hết hạn' dưới ô mã"; "checkCoupon từ chối: câu theo lý do ngay dưới ô mã, giảm theo mã 0"; `orderRules.test.ts` › "đủ 10 lý do của CouponRejectReason, câu đúng mockup" | như trên + `npx vitest run src/screens/len-don/orderRules.test.ts` | qua; `Tests 26 passed (26)` |
| `pendingReason discount_approval` → "Đơn đang chờ duyệt giảm giá" | "đơn chờ duyệt (pendingReason discount_approval): 'Đơn đang chờ duyệt giảm giá'" | như trên | qua |
| Cảnh báo `warnings` sau khi tạo; kiểm trùng trước bằng `checkOrderDuplicates` | "cảnh báo sau khi tạo: SĐT thuộc hồ sơ khác, nghi trùng"; "kiểm trùng trước khi lưu khi đã có khách và dòng (trạng thái H)" | như trên | qua |
| "Có thể bán" không báo hết hàng nhầm khi trang số dư còn trang sau (phát hiện của reviewer lượt 1) | "biến thể không có dòng số dư: trang cuối → hết hàng; còn trang sau (nextCursor) → chưa biết, không báo hết" (viết trước, FAIL, rồi sửa `VariantPicker.tsx`) | như trên | qua |
| Tiền tính trước, mã kiểm trên tạm tính sau giảm dòng + giảm đơn; cảnh báo vượt ngưỡng chỉ cho người thiếu quyền duyệt | "tổng tính trước: 2 × 690.000 …→ 1.160.000"; "giảm tay vượt ngưỡng với người không có quyền duyệt…"; "có quyền duyệt giảm giá: không cảnh báo" | như trên | qua |
| Màn tự đăng ký `/don/moi`, nhóm "Đơn hàng" | `route.test.tsx`; `src/app/screens.test.tsx` (có sẵn) | `npx vitest run` (cả console) | `Test Files 50 passed (50)`, `Tests 709 passed (709)` |
| Typecheck, lint, định dạng | — | `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check src e2e` | không lỗi; "All matched files use Prettier code style!" |
| Bố cục 1512×767, 390×844, 360 không tràn ngang; ảnh so mockup | `e2e/len-don.spec.ts` (5 test: trạng thái B, A ở hai khổ; 360 tên 120 ký tự); `e2e/shell.spec.ts` (menu có nhóm mới) | `CRM_E2E_PORT=5184 npx playwright test e2e/len-don.spec.ts e2e/shell.spec.ts --project=chromium` | `5 passed` (len-don), shell 4 qua; ảnh `console/test-results/len-don-{1512,390,360}.png`, `len-don-b-*.png` |
| Cổng kiểm của task | — | ci-task sau khi gộp main (lần 1 đỏ: catalog-check vì tệp danh mục chưa stage, jscpd 5 đoạn trùng — đã sửa, xem Tái dùng; lần 2 sau khi sửa) | "CI-TASK XANH (so với điểm tách nhánh 0924990…)" |

So ảnh với mockup (`docs/mockups/m2/png/len-don-{1512,390}.png`, trạng thái A): bố cục hai cột / một cột, thẻ Khách, Giao tới, Sản phẩm, Tiền, Thông tin đơn, nút, Phím tắt trùng thứ tự và cỡ. Chỗ lệch, có lý do:
1. Ô "Số này là của khách" không hiện khi chọn hồ sơ có sẵn (xem "Dừng để hỏi" 1).
2. Câu mã áp được: "Áp được: giảm 150.000 ₫ · còn 12 lượt" thay "giảm 10%, tối đa 150.000 ₫": `CouponCheck` chỉ trả số giảm và lượt còn, không trả điều khoản mã.
3. Nhãn khách trên thẻ khách đều là `chip chip-line` (mockup "Hay hoàn" màu đỏ): `CustomerTag.color` là chuỗi tự do của màn quản lý nhãn (Task 86/87 chưa có), chưa có bảng màu → chưa đổi màu.
4. Hộp "Có thể trùng đơn" không ghi nhãn đơn ("Mới"): bảng chữ của 16 nhãn đơn thuộc màn Danh sách đơn (Task 85); ghi giờ tạo và tổng tiền.
5. Gợi ý khách và xã/phường là danh sách đẩy nội dung xuống (như mockup vẽ đang mở), không nổi đè.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp: kế hoạch ghi `console/src/features/orders/…`; theo Q-T57-8 đọc là `console/src/screens/len-don/` (màn tự đăng ký), `OrderForm` xuất qua `console/src/shared/index.ts` cho M4. Thêm `OrderPage.tsx` (đầu trang), `orderRules.ts` (luật chữ, dựng thân), `useCombo.ts` (phím và đóng/mở của ô gợi ý), `orderTestkit.tsx`, `len-don.css`; không có `routes.tsx` riêng (route.tsx là quy ước hiện hành).
- `idempotencyKey` trong thân: `CreateOrderRequest` không có trường này (additionalProperties: false; máy chủ dựng khóa nghiệp vụ từ header `Idempotency-Key` và nhân viên, Q-T57-4). Màn chỉ gửi header; test kiểm ba lần gửi cùng header.
- Props `conversationId?`, `channelAccountId?` của `OrderForm`: hợp đồng `createOrder` chưa có trường nào nhận hai giá trị này, nên chưa khai (prop không làm gì là sai lệch); M4 thêm cùng lúc với hợp đồng.
- `pricing.ts` có thêm `manualDiscountBP` (bản của `pricing.ManualDiscountBP`) để cảnh báo vượt ngưỡng trước khi lưu (HV09, mockup trạng thái E).

## Quyết định thay chủ shop

1. Nhóm menu "Đơn hàng" thêm vào đầu `NAV_GROUPS` (HV01 của mockup M2; Khách hàng, Khuyến mãi thêm khi có màn ở Task 86/87). Phương án khác: để cuối danh sách — trái mockup. Dễ đảo: một dòng `nav.ts`.
2. Kho giữ hàng: không có ô chọn kho (mockup không vẽ); `warehouseId` bỏ trống → máy chủ dùng kho chính; "có thể bán" đọc ở kho đầu danh sách `listWarehouses` (kho chính trước). Task 411 (phạm vi kho) chưa có API "kho được lên đơn", ghi ở Việc phát hiện thêm.
3. Khách mới ("+ Khách mới"): tên khách lấy từ "Người nhận", SĐT từ "SĐT người nhận", ô "Số này là của khách" bật sẵn (HV07: hồ sơ chưa có SĐT). Chữ đã gõ ở ô tìm điền sẵn vào người nhận (số → SĐT, chữ → tên) khi ô đó trống.
4. Nút mờ kèm lý do: ngoài "chưa khách/0 dòng" (mockup B), "Lưu và xác nhận" mờ khi địa chỉ chưa đủ tỉnh, xã, số nhà (máy chủ trả `address_incomplete`); địa chỉ điền dở (thiếu người nhận/SĐT/số nhà) thì mọi nút mờ (thân `shipAddress` bắt buộc đủ ba trường); địa chỉ rỗng hẳn thì lưu mới, nháp được. Chỉ vẽ đúng điều máy chủ chặn.
5. Gửi mã đang hiện trong ô (kể cả chưa rời ô) chứ không phải mã đã kiểm trước; máy chủ kiểm lại khi lưu.
6. Biến thể ngừng bán hiện "ngừng bán", không chọn được; hết hàng hiện "hết hàng" nhưng vẫn chọn được (tồn đổi liên tục; máy chủ trả `out_of_stock` nếu vẫn thiếu, dòng được tô).
7. Cài trình duyệt chromium của Playwright 1.65 vào bộ đệm máy (`npx playwright install chromium`) để chạy e2e; chỉ thêm tệp vào `~/Library/Caches/ms-playwright`, không đổi repo.

## Dừng để hỏi

(không chặn task; ghi để chủ shop xem khi về)

1. Thay đổi so với mockup (HV07): ô "Số này là của khách (gắn vào hồ sơ)" chỉ hiện với khách mới. Lý do: `createOrder` gửi `customerId` thì hợp đồng không có chỗ cho cờ này (`phoneIsCustomers` chỉ nằm trong `customer`; `handlers_order.go` dòng 106 chỉ truyền `CustomerID`), dù module `customer.FindOrCreateForOrder` hỗ trợ. Vẽ ô mà máy chủ không nhận là sai (Bẫy đã gặp). Muốn đúng mockup cần task thêm `phoneIsCustomers` cấp đơn vào `CreateOrderRequest` (mở rộng, không phá vỡ).

## Việc phát hiện thêm

- Hợp đồng `createOrder` thiếu cờ "SĐT là của khách" khi dùng `customerId` (xem Dừng để hỏi 1).
- Task 411 (giới hạn lên đơn theo phạm vi kho): chưa có API trả "kho người này được lên đơn"; màn hiện không có ô chọn kho (máy chủ dùng kho chính). Khi 411 thêm API hay quy tắc, màn cần thêm ô chọn kho trong phạm vi.
- Liên kết "Mở đơn" (`/don/:id`), "Xem đơn trùng" (`/don/trung`), "Xem hai hồ sơ" (`/khach/:id/gop?voi=`) trỏ tới màn của Task 85–87, chưa có; tới lúc đó bấm vào là trang không tìm thấy. Task 86/87 cần đọc tham số `voi` (hồ sơ thứ hai) hoặc báo lại để đổi liên kết.
- `CouponCheck` không trả điều khoản mã (phần trăm, mức tối đa) nên câu "Áp được" chỉ nêu số giảm; muốn đúng câu mockup cần thêm trường vào hợp đồng.
- Bảng chữ cho 16 nhãn đơn (`OrderLabel`) chưa có ở console; Task 85 tạo thì hộp "Có thể trùng đơn" nên dùng lại để ghi nhãn.

## Reviewer

Lượt 1: CẦN SỬA — `VariantPicker.tsx`: biến thể không có trong một trang `listBalances` (limit 100, còn `nextCursor`) bị coi là có thể bán 0 → báo "hết hàng" nhầm. Đã sửa: chỉ coi là 0 khi trang đã đủ (không `nextCursor`), còn lại là chưa biết; thêm test. Ghi chú thêm của reviewer: `/` nay vào Lên đơn cho người có quyền (đã ghi ở Ảnh hưởng); câu errors.ts trong Ảnh hưởng chỉnh cho khớp.

Lượt 2: ĐẠT — lỗi lượt 1 đã sửa và có test thật kiểm (`VariantPicker.tsx` dòng 33–48, 67–69; test "biến thể không có dòng số dư…"); Ảnh hưởng khớp thực tế (kể cả `/` vào Lên đơn, câu errors.ts); không tái dùng sót, không cách cũ còn sót, không test bị tắt, không vi phạm luật dự án. Ghi chú không bắt buộc: người có `inventory.read` ở kho khác mà không ở kho chính thì máy chủ trả 403, màn chỉ ẩn số "có thể bán" (an toàn).

Kết luận: ĐẠT
