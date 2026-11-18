# Task 50 — Màn Sản phẩm: danh sách

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 50, M1-50; sửa mẫu, ảnh, bài viết đã tách sang 324, 325, 326). Mockup đã duyệt: `docs/mockups/m1/san-pham-danh-sach.html` (+ `png/san-pham-danh-sach-{1512,390,360}.png`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task chỉ chạm màn hình (`console/`), không sửa mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Chỉ gọi (đọc) `listProducts` (q, status, categoryId,
  tagId, warehouseId, cursor, limit=50), `listCategories`, `listTags`, và `getMe` (khóa `identity.me` đã có của khung,
  không gọi lại) qua client sinh `console/src/api`.
- Chỗ dùng trên màn hình: tạo `console/src/features/products/{ProductList.tsx,productRules.ts,products.css}`; thêm một
  dòng `Screen` `/san-pham` (nhóm "Sản phẩm", nhãn "Danh sách sản phẩm", quyền `catalog.read`, `also: ["/san-pham/:id"]`)
  vào `console/src/app/screens.tsx` (Task 51 chạy song song chỉ thêm dòng của nó); thêm lớp chung của `m1.css` mà màn
  cần (`.toolbar`, `.thumb`, `.more`, `.empty`, `.num`, `.money`, `.clamp2`, `.grow`) vào `console/src/styles.css`.
  Thêm `@tanstack/react-virtual` 3.14.26 (đã chọn ở Q31, `docs/chon-cong-cu-console.md`: "cài ở màn danh sách đầu tiên")
  vào `console/package.json` + dòng `docs/phu-thuoc.md`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run` (mới: `src/features/products/ProductList.test.tsx`,
  `productRules.test.ts`), `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check .`; Playwright
  `e2e/products.spec.ts` (mới) + `e2e/shell.spec.ts`, `e2e/shell.phone.spec.ts` (menu thêm mục) ở 1512 và 390 trên
  cổng 5150; `make ci` một lần trước khi stage (đổi console nên cổng console chạy trong đó).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `console/src/shared` của
  `docs/danh-muc-dung-chung.md`; `src/app/{errors,queryClient,nav,router}.ts`; màn mẫu `src/features/staff/*`; tìm
  "₫", "NumberFormat", "useSearchParams", "virtual" trong `console/src` (chưa có).
- Dùng lại / viết mới vì: dùng lại `unwrap`, `errorMessage` (lỗi), `queryKey(TOPICS.product, …)` (báo hiệu
  `catalog.product` tự tải lại danh sách đang mở), `hasPermission`/`Screen`, lớp CSS của `styles.css` (bảng thành thẻ
  dưới 760 px, chip, nút, ô nhập, hộp báo). Viết mới: định dạng tiền "350.000 ₫" và luật hiển thị của dòng mẫu (giá
  khoảng, có thể bán âm/hết hàng, nhãn trạng thái, đường dẫn danh mục) trong `productRules.ts` của module vì chưa có;
  định dạng tiền để trong feature cho tới khi màn thứ hai cần (Task 51/52 dùng thì chuyển vào `src/shared`, ghi ở
  "Việc phát hiện thêm"). Không dùng `scopeLabel` của `features/staff` (màn này không nhập feature khác); nhãn kho
  viết lại một dòng cùng quy ước "Kho <mã>".

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

Mục 5 (khớp kiến trúc):
- 5.1–5.7, 5.10: không áp dụng: không thêm bảng, transaction, sự kiện, lệnh ra ngoài hay truy vấn; màn chỉ đọc API.
- 5.8 Nghiệp vụ không ở màn hình: tìm không dấu, lọc danh mục gồm con, tính "có thể bán" đều do API
  (`catalog.ListProducts`); màn chỉ hiện số và dựng nhãn. Câu "Thiếu N so với phần giữ" chỉ đọc dấu của `available`.
- 5.9 Đường ghi: không có (danh sách chỉ đọc; nút "Thêm mẫu", "Nhập Excel" là liên kết sang màn của Task 324 và màn
  nhập Excel).
- 5.11 Thư viện: thêm `@tanstack/react-virtual` 3.14.26 — đã được chọn ở Q31/Task 48 (`docs/chon-cong-cu-console.md`,
  "cài ở task màn danh sách đầu tiên"); ghi dòng `docs/phu-thuoc.md`.
- 5.12 Theo mẫu: như màn Nhân viên (Task 54): `features/<màn>/`, một dòng `SCREENS`, test vitest + Playwright với API
  giả (`e2e/fixtures.ts`).

Mục 6 (đúng khi chạy đồng thời):
- 6.1 Người khác sửa mẫu khi đang xem: khóa truy vấn bắt đầu bằng `catalog.product` nên báo hiệu `changed` tải lại
  danh sách đang mở (`onRealtimeChanged`); không sửa gì từ màn này.
- 6.2: không áp dụng: không ghi.
- 6.3–6.6, 6.8–6.11: không áp dụng: không database, hàng đợi, tiền ghi sổ (giá chỉ hiển thị, `Money` là số nguyên
  đồng, định dạng bằng `Intl.NumberFormat("vi-VN")`, không làm tròn).
- 6.7 API bận: đọc theo `shouldRetry` (5xx, mạng 2 lần; 4xx không), lỗi hiện hộp báo bằng `errorMessage`.
- Trang theo con trỏ: `nextCursor` gửi nguyên văn; đổi bộ lọc là khóa truy vấn mới (trang về đầu), không trộn trang
  của hai bộ lọc. Gõ tìm chờ 300 ms mới gửi.
- 6.12 Dữ liệu cá nhân: không có (mẫu, giá, danh mục). Tìm kiếm nằm trong URL (`?q=`) — là mã/tên mẫu, không phải dữ
  liệu cá nhân.
- 6.13 Quyền: màn cần `catalog.read` (menu, route), API vẫn kiểm. Ô chọn kho và cột "Có thể bán" chỉ có kho người xem
  có `inventory.read` theo grant (mockup: "ô chọn kho chỉ có kho người xem có quyền"); API trả 403 nếu sai — hiện hộp
  báo.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 0 mẫu → "Chưa có mẫu nào" + Nhập Excel, Thêm mẫu (trạng thái D) | `products.spec.ts` "0 mẫu → …" (1512, 390); `ProductList.test.tsx` "0 mẫu, không lọc…" | `cd console && CRM_E2E_PORT=5150 npx playwright test e2e/products.spec.ts e2e/shell.spec.ts e2e/shell.phone.spec.ts` | `17 passed (14.6s)` |
| 1 mẫu → một dòng, "Hết danh sách.", bấm dòng sang `/san-pham/<id>` (trạng thái C) | `products.spec.ts` "1 mẫu → …"; `ProductList.test.tsx` "đúng 1 mẫu…" | như trên | qua |
| 200 mẫu: trang 50 theo con trỏ (cursor null, 50, 100, 150), cuộn tới đáy tự tải tiếp tới hết; danh sách ảo (dưới 60 dòng trong DOM, DAM0001 rời DOM khi ở đáy, quay lên lại có) | `products.spec.ts` "200 mẫu: …" (1512, 390); `ProductList.test.tsx` "200 mẫu: …" (nút Tải thêm gửi `cursor`) | như trên | qua |
| Tên 80 ký tự: cắt hai dòng, không vượt bề ngang; không cuộn ngang ở mọi ca | `products.spec.ts` "tên 80 ký tự…" (cao ≤ 2 dòng, mép phải ≤ bề ngang) + `noHorizontalScroll` mọi ca | như trên | qua |
| Có thể bán: âm đỏ + "Thiếu N so với phần giữ", 0 + "Hết hàng"; không có kho trong grant → không ô kho, không cột, không gửi warehouseId | `products.spec.ts` "tên 80 ký tự…", "không có kho trong grant…"; `ProductList.test.tsx` hai ca kho; `productRules.test.ts` `availableView`, `warehouseOptions` | như trên; `npx vitest run src/features/products` | qua; `Tests 26 passed (26)` |
| Lọc trạng thái/danh mục/nhãn gửi đúng tham số, chip bỏ lọc, Xóa bộ lọc; tìm chờ 300 ms; không thấy → câu trạng thái E | `products.spec.ts` "lọc, tìm không thấy…"; `ProductList.test.tsx` hai ca lọc, tìm | như trên | qua |
| Ảnh bìa: chưa có, mất, `<picture>` avif/webp/jpeg; lỗi API → hộp báo tiếng Việt | `ProductList.test.tsx`, `productRules.test.ts` `coverPicture` | `npx vitest run src/features/products` | qua |
| Ảnh chụp so mockup | ảnh `trang-thai-{a,d}-{1512,390}.png` do `products.spec.ts` lưu, so tay với `docs/mockups/m1/png/san-pham-danh-sach-{1512,390}.png` | `npx playwright test e2e/products.spec.ts -g "trạng thái" --output=<scratchpad>` | Khớp bố cục, cột, chip, ô ảnh, thẻ dưới 760 px. Sai khác do dữ liệu/quyết định: ô kho ghi "Kho <mã uuid>" (quyết định 1), không có dòng "Người bán ELIMAZ" (quyết định 2), menu chỉ có mục đã có màn |
| Khung: trang đầu của người có catalog.read là Danh sách sản phẩm, menu sáng mục đó | `shell.spec.ts`, `shell.phone.spec.ts` (thêm `api.catalog()`) | như dòng đầu | qua |
| Kiểu, lint, định dạng, bản dựng, toàn bộ vitest | — | `cd console && npx tsc --noEmit && npx eslint .`; `make console-check` | không lỗi; `Tests 127 passed (127)`, "All matched files use Prettier code style!", `built in 389ms`, `no leaks found` |
| Cổng `make ci` | — | `make ci CRM_PG_PORT=5600 …` (một lần, máy chung load ≈ 24) | build, vet, lint, gen-check, sqlc-vet qua; bước `test` đỏ ở 4 gói Go không bị task chạm, đều do hết thời gian chờ khi máy quá tải: `internal/httpapi`, `modules/inventory` (treo 10 phút; `statement timeout` 57014), `platform/files` (`DROP DATABASE … context deadline exceeded`), `imager` (script giả chưa kịp chạy trong 1 s). Chạy lại `go test -race ./internal/imager/...` → `ok 7.223s`. Task không đổi mã Go nào. Các cổng sau `test` chạy riêng: `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` → `EXIT 0` ("No vulnerabilities found", "Found 0 clones", `no leaks found`) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `productRules.ts` (+ test) và `products.css` ngoài mục Files (luật hiển thị test bằng vitest; lớp riêng của
  mockup), như màn Nhân viên.
- `console/src/styles.css` thêm lớp chung còn thiếu của `m1.css` (`.toolbar`, `.thumb`, `.more`, `.empty`, `.num`,
  `.money`, `.bold`, `.nowrap`, `.clamp2`, `.grow`, token `--tint`).
- `e2e/fixtures.ts` thêm `api.catalog()`; `shell.spec.ts`, `shell.phone.spec.ts` gọi nó vì "/" của Chủ giờ chuyển sang
  Danh sách sản phẩm (trước đây chưa có màn nào trong menu). Không nới điều kiện nào; thêm kiểm mục menu mới.

## Quyết định thay chủ shop

1. **Ô chọn kho, cột "Có thể bán" khi chưa có API danh sách kho.** Hợp đồng chưa có operation liệt kê kho (bảng
   `inventory.warehouses` có tên nhưng không API nào trả). Chọn: lựa chọn kho lấy từ grant `inventory.read` phạm vi kho
   của người xem (`/api/v1/me`), nhãn "Kho <mã>" như màn Nhân viên (Task 54, quyết định 1). Không có kho nào trong grant
   (kể cả Chủ có `inventory.read` toàn shop, vì không biết mã kho) → không có ô chọn kho và không có cột "Có thể bán".
   Vì sao: không đổi hợp đồng, không đụng module inventory (Task 51 song song sửa cùng lớp HTTP), dễ thay (một hàm
   `warehouseOptions`). Phương án khác: thêm `listWarehouses` (việc backend ngoài mục Files của task này). Ghi ở "Việc
   phát hiện thêm".
2. **Dòng phụ "Người bán ELIMAZ" dưới tiêu đề:** chưa có API trả tên người bán → bỏ dòng này, không viết cứng tên.
3. **Bộ lọc trạng thái mặc định "Mọi trạng thái"** (mockup trạng thái A vẽ đang lọc "Đang bán" như một ví dụ có chip
   lọc). Vì sao: mở màn thấy cả mẫu đang nhập (mẫu mới tạo là `draft`), và "Shop chưa có mẫu nào" (trạng thái D) chỉ đúng
   khi không lọc gì. Đảo ngược: đổi giá trị mặc định trong `readFilters`.
4. **Bộ lọc nằm trên URL** (`?q=&status=&category=&tag=&kho=`): quay lại từ trang sửa mẫu giữ nguyên bộ lọc; dán link
   mở đúng danh sách.
5. **Liên kết sang màn chưa có:** dòng mẫu → `/san-pham/:id`, "Thêm mẫu" → `/san-pham/moi` (Task 324), "Nhập Excel" →
   `/nhap-excel`. Tới khi các màn đó gộp, bấm vào hiện "Không có trang này…" của khung. Giữ liên kết để đúng mockup và
   Task 324 không phải sửa màn này.
6. **"Hết hàng"** chỉ hiện cạnh số 0 của mẫu đang bán (mockup: mẫu đang nhập có 0 không ghi chữ).

## Việc phát hiện thêm

- Cần operation `listWarehouses` (đọc `inventory.warehouses_v`: id, mã, tên, trạng thái, lọc theo kho người xem có
  `inventory.read`) để ô chọn kho có tên kho và Chủ (quyền toàn shop) thấy cột "Có thể bán". Màn Tồn kho (Task 51) cũng
  cần. Khi có: thay `warehouseOptions` trong `productRules.ts`.
- `formatMoney` đang ở `features/products/productRules.ts`; màn thứ hai cần định dạng tiền thì chuyển vào `src/shared`.
- Cột "Có thể bán" chỉ tự tải lại theo báo hiệu `catalog.product` (khóa truy vấn bắt đầu bằng chủ đề đó); báo hiệu
  `inventory.balance` chưa làm mới danh sách (quy ước một chủ đề đứng đầu khóa). Khi cột này bật thật (có
  `listWarehouses`), cân nhắc nối thêm chủ đề tồn cho truy vấn có `warehouseId`.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên `git diff --cached` 15 tệp; `main` không có commit mới để gộp).

- Phát hiện phải sửa: không có. Đã kiểm: khóa truy vấn theo bộ lọc, `nextCursor` gửi nguyên văn (trang đầu `""` an
  toàn vì backend không trả chuỗi rỗng); dòng đệm của danh sách ảo khớp `scrollMargin`; tự tải trang sau có chặn,
  không lặp vô hạn; đọc URL kiểm trạng thái, uuid, `q` ≤ 100, kho có quyền; liên kết chỉ hằng hoặc
  `encodeURIComponent(id)`; không `as` trên dữ liệu API; test phủ đủ tiêu chí kế hoạch ở 1512 và 390.
- Chấp nhận lỗi bước `test` của `make ci` (4 gói Go không bị chạm, hết thời gian chờ khi máy quá tải).
- Ghi chú không bắt buộc (không sửa): `scrollMargin` đo một lần khi dựng bảng, hộp báo lỗi hay toolbar xuống dòng sau
  đó làm vùng dựng lệch vài chục px, overscan 8 dòng che được; Chủ (quyền toàn shop) chưa thấy cột "Có thể bán" tới khi
  có `listWarehouses` (đã ghi ở Việc phát hiện thêm).
