# Task 86 — M2-29: Màn Khách — danh sách, hồ sơ, gộp, nhãn, ghi chú, nhập

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 86 = M2-29). Loại việc (quy trình mục 3): A (màn hình). Mockup đã duyệt (Q-X8, Task 83): `docs/mockups/m2/khach-danh-sach.html`, `khach-ho-so.html`, `khach-gop.html`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ đổi `console/` (TypeScript).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`, client sinh không đổi. Màn chỉ GỌI operation có sẵn: `listCustomers`, `createCustomer`, `getCustomer`, `updateCustomer`, `addCustomerIdentity`, `removeCustomerIdentity`, `listCustomerMergeCandidates`, `mergeCustomer`, `undoCustomerMerge`, `getCustomerTimeline`, `addCustomerNote`, `listCustomerTags`, `createCustomerTag`, `updateCustomerTag`, `addCustomerTag`, `removeCustomerTag`, `getCustomerTagRule`, `putCustomerTagRule`, `getCustomerImportTemplate`, `startCustomerImport`, `getCustomerImport`, `createUpload`, `completeUpload`, `getMe`.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/khach/` (route tự đăng ký `/khach`, `/khach/:id`, `/khach/:id/gop`; Q-T57-8, không sửa `router.tsx`). Sửa có chủ đích:
  - `console/src/app/nav.ts`: `NAV_GROUPS` thêm nhóm "Khách hàng" sau "Đơn hàng" (HV01, khung mockup M2). Chỗ dùng: `loadScreens.ts` (kiểm nhóm), `navSections`, `screens.test.tsx` (xếp theo thứ tự nhóm) — không đổi mã, chỉ thêm một nhóm.
  - `console/src/app/errors.ts`: chỉ thêm dòng nếu thiếu câu cho mã lỗi màn gặp.
  - Liên kết có sẵn `/khach/:rootId` của màn Đơn (Task 85) và cảnh báo "Xem hai hồ sơ" của Lên đơn nay có màn.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/khach src/app src/shell` (mới: `customerRules.test.ts`, `CustomerList.test.tsx`, `CustomerProfile.test.tsx`, `MergeView.test.tsx`, `TagRuleEditor.test.tsx`, `ImportDialog.test.tsx`, `route.test.tsx`), `npx tsc --noEmit`, `npx eslint .`, Playwright chỉ `e2e/khach.spec.ts` (mới, chụp 1512×767, 390×844, 360) và `e2e/shell.spec.ts` (menu có nhóm mới); ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console (useWriteAction, unwrap, errorMessage, ApiError, queryKey, hasPermission, shortTime, Dialog); `screens/staff/staffData.tsx` (`Dialog`, `RefusalBox`), `screens/len-don/orderRules.ts` (`customerQuery` — luật 4 số cuối, `displayPhone`, `shortDate`, `cleanPhone`), `screens/len-don/CustomerPicker.tsx` (`CUSTOMER_TOPIC`), `screens/don/orderData.ts` (`usePerms`), `screens/products/productRules.ts` (`formatMoney`), `screens/inventory/Balances.tsx` (`useDebounced`, `SEARCH_DELAY_MS`), `screens/imports/` (luồng tải tệp: `createUpload` → `xhrPut` → `completeUpload`; `importRules.ts`: `checkLocalFile`, `fileNameFrom`, `fmtInt`, `XLSX_TYPE`, `MAX_BYTES`), `docs/mockups/m2/m2.css`, `screens/don/don.css`.
- Dùng lại / viết mới vì: dùng lại `customerQuery` (cùng luật tìm như Lên đơn, HV26), `displayPhone`, `usePerms`, `useWriteAction` cho mọi đường ghi, `Dialog` của staff, `formatMoney`, `useDebounced`, `xhrPut`, các hàm của `importRules.ts`, `shortTime`. Viết mới: `customerRules.ts` (chữ của chỉ số quy tắc nhãn và cách đổi đơn vị — % ↔ phần vạn; màu nhãn → lớp chip có sẵn; câu lỗi dòng nhập khách; nhận biết lần gộp từ dòng thời gian) — chưa có ở đâu; `ImportDialog` là hộp thoại ba bước của khách (khác màn nhập sản phẩm: không có bước kiểm/áp dụng, kết quả là số đếm và lỗi dòng), dùng lại luồng tải lên chứ không chép trang `ImportPage`.
- Cảnh báo trùng (jscpd, ci-task lần 1: 15 đoạn mới): gộp chỗ cùng mục đích — ô "Họ tên" của Thêm khách và Sửa hồ sơ thành `NameField` (`CreateCustomerDialog.tsx`); `stubDialog` dùng lại bản của `don/donTestkit.tsx`; các bước lặp trong test thành hàm (`importFile`, `undoRow`, `type`/`saveRule`). Giữ riêng, viết lại gọn: API giả của màn Khách (bảng đường → trả lời, khác dữ liệu và đường của màn Đơn; dựng bằng `createMemoryRouter`), hộp xác nhận gộp (`back`), ba lệnh ghi tải lên/nhập khách (cùng gọi client sinh như màn nhập sản phẩm nhưng khác mục đích tệp và lệnh bắt đầu; gộp thành hook chung sẽ phải sửa màn Nhập sản phẩm — ngoài phạm vi), ô chọn nhãn và hàng "Tải thêm" (JSX khuôn mẫu). Sau sửa: `make jscpd` → "Found 0 clones".

## Soát thiết kế

- 5.1–5.8, 5.10: không áp dụng — không đổi máy chủ, database, module, sự kiện; màn chỉ gọi API có sẵn.
- 5.9 Đường ghi: mọi ghi (tạo khách, sửa, thêm/gỡ danh tính, gộp, hoàn tác, ghi chú, tạo/sửa nhãn, gắn/gỡ nhãn, đặt quy tắc, tải lên, bắt đầu nhập) qua `useWriteAction` (Idempotency-Key sinh lúc bấm, gửi lại cùng khóa); mã đối tượng và `version` nằm trong `body` của `run`.
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: màn tự đăng ký như `screens/don`; test vitest + testing-library với fetch giả như `don/donTestkit.tsx`.
- 6.1, 6.3–6.9, 6.11: không áp dụng (không chạm database, khóa, việc nền).
- 6.2 Gửi lại / đồng thời: sửa hồ sơ, sửa nhãn, đặt quy tắc gửi `version` đã đọc; 409 `version_conflict` → giữ phần đang gõ, câu báo "Hồ sơ đã thay đổi, tải lại" và nút tải lại (không tự ghi đè). Gộp cần chọn hồ sơ giữ lại và xác nhận lần hai; hoàn tác do máy chủ quyết (`merge_not_latest` kèm `blockingMergeId`, `merge_window_expired`).
- 6.10 Tiền: chỉ hiện số đồng máy chủ trả (`formatMoney`); quy tắc nhãn "Đã chi" nhập số đồng nguyên.
- 6.12 Dữ liệu cá nhân: SĐT hiện đủ cho người có `customer.read` (màn nội bộ); màn không gọi `console.*`; kết quả nhập chỉ hiện dòng, cột, mã lỗi (máy chủ không trả giá trị ô).
- 6.13 Quyền: route cần `customer.read`; nút hiện theo quyền trong `/api/v1/me` (`customer.write`, `customer.merge`, `customer.tags.manage`, `customer.import` + `files.upload`, `order.create`); máy chủ vẫn chặn (403 hiện câu của `errorMessage`).

## Bằng chứng

Thứ tự: viết `khachTestkit.tsx` và 7 tệp test trước mã; chạy `npx vitest run src/screens/khach` → FAIL "Test Files 7 failed (7)" (không nạp được `./route`, `./customerRules`, `./ImportDialog`), rồi viết mã. Chỉnh test khi chạy (không nới điều kiện): chờ bảng lần gộp nạp xong (`findAllByRole`) thay cho đọc ngay; SĐT xuất hiện ở hai thẻ nên dùng `findAllByText`; mã đơn là liên kết nên kiểm cả dòng qua `closest("li")`; thay `!`/ép kiểu bằng `nth` cho lint. Hai ca lỗi đọc (gợi ý gộp; danh sách nhãn, quy tắc) thêm khi tự soát theo "Bẫy đã gặp" (ca lỗi API có trạng thái riêng), viết cùng lúc với mã sửa, không chạy đỏ trước.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tìm "5678" hiện cả hai khách có đuôi đó (đuôi đánh dấu, cột Khớp theo); gõ 3 số nhắc 4 số cuối, không gọi tìm | `CustomerList.test.tsx` › 'tìm "5678" hiện cả hai khách có đuôi đó…', "gõ 3 số: nhắc gõ ít nhất 4 số cuối, không gọi tìm" | `cd console && npx vitest run src/screens/khach` | `Test Files 7 passed (7)`, `Tests 46 passed (46)` |
| Danh sách: SĐT đủ, tiền, mua gần nhất, khách chưa SĐT, trống, tìm không ra, lọc nhãn, nút theo quyền, Thêm khách + identity_taken có liên kết | `CustomerList.test.tsx` (8 ca) | như trên | qua |
| Gộp phải chọn hồ sơ giữ lại (không chọn sẵn, nút mờ) và xác nhận lần hai; Quay lại không gửi; gửi đúng chiều (path = giữ lại, mergedId = hồ sơ kia, matchedBy phone/staff) | `MergeView.test.tsx` › "hai hồ sơ cạnh nhau; chưa chọn…", "chọn giữ hồ sơ bên phải, bấm Gộp vào… chỉ mở hộp xác nhận…", "chưa có hồ sơ thứ hai: chọn từ gợi ý … hoặc tìm khách (… matchedBy staff)", "không có quyền gộp…" | như trên | qua |
| `merge_not_latest` → câu báo có liên kết tới lần gộp phải hoàn tác trước; `merge_window_expired` → câu quá 30 ngày; dòng quá 30 ngày không có nút; hoàn tác xong có thông báo | `MergeView.test.tsx` › "hoàn tác bị merge_not_latest…" (liên kết `#gop-m2`), "hoàn tác bị merge_window_expired…", "dòng quá 30 ngày ghi lý do…", "hoàn tác xong…"; `customerRules.test.ts` › "lấy mục merge…", "hạn hoàn tác 30 ngày…" | như trên | qua |
| Nhãn `rule` có dấu "theo quy tắc", không có nút gỡ; nhãn tay có × gửi DELETE kèm khóa; ô thêm nhãn không có nhãn đã gắn tay | `CustomerProfile.test.tsx` › "nhãn rule có dấu 'theo quy tắc' và không có nút gỡ…", "thêm nhãn: …" | như trên | qua |
| Hồ sơ theo gốc (mở bằng hồ sơ con), chỉ số nhóm, dòng thời gian cả nhóm, ghi chú, liên hệ (Facebook không gỡ), gợi ý cùng một khách (nút chỉ cho customer.merge), khách tiềm năng, 409 version_conflict giữ phần đang gõ + Tải lại hồ sơ, chỉ xem không có nút ghi | `CustomerProfile.test.tsx` (12 ca, gồm "đọc gợi ý gộp lỗi: câu lỗi riêng, hồ sơ vẫn hiện") | như trên | qua |
| SĐT hiện đủ cho customer.read; không ghi SĐT, ghi chú vào log trình duyệt | `CustomerProfile.test.tsx` › "liên hệ: SĐT hiện đủ…", "không ghi SĐT, ghi chú vào log của trình duyệt" (spy console.log/info/warn/error/debug) | như trên | qua |
| Nhập Excel: kết quả hiện số đếm và lỗi theo dòng, cột, mã (không giá trị ô); luồng createUpload customer_import → PUT → completeUpload → startCustomerImport, mọi ghi có khóa; tệp sai báo ngay; lỗi cả tệp; nhắc cột SĐT định dạng văn bản (Task 70) | `ImportDialog.test.tsx` (4 ca); `customerRules.test.ts` › "mọi mã lỗi dòng và lỗi tệp của máy chủ có câu tiếng Việt" (đọc mã từ `importer_rows.go`, `importer.go`) | như trên | qua |
| Quy tắc nhãn: % ↔ phần vạn, gửi version (có quy tắc) hoặc không (chưa có), giá trị sai chặn trước khi gửi, invalid_tag_rule giữ điều kiện; tạo/đổi tên nhãn; 10 màu đúng `TagColors()` | `TagRuleEditor.test.tsx` (6 ca, gồm "đọc danh sách nhãn hay quy tắc lỗi…"); `customerRules.test.ts` › "tỷ lệ hoàn nhập theo %…", "10 khóa màu đúng TagColors()…" | như trên | qua |
| Màn tự đăng ký `/khach`, `/khach/:id`, `/khach/:id/gop`; nhóm menu "Khách hàng" | `route.test.tsx`; `src/app/screens.test.tsx`, `src/shell/Layout.test.tsx` (có sẵn) | `npx vitest run src/screens/khach src/app src/shell` | `Test Files 15 passed (15)`, `Tests 152 passed (152)`; sau khi thêm `also` cho mục menu: `npx vitest run src/screens/khach src/app` → `12 passed`, `119 passed` |
| Typecheck, lint, định dạng | — | `npx tsc --noEmit`, `npx eslint src/screens/khach e2e/khach.spec.ts src/app`, `npx prettier --write` | không lỗi |
| Bố cục 1512×767, 390×844, 360 không tràn ngang; ảnh so mockup | `e2e/khach.spec.ts` (9 test); `e2e/shell.spec.ts` (menu) | `CRM_E2E_PORT=5186 npx playwright test e2e/khach.spec.ts e2e/shell.spec.ts --project=chromium` | `13 passed`; ảnh `console/test-results/khach-{danh-sach,tim,nhan,nhap,ho-so,gop,gop-xac-nhan}-{1512,390}.png`, `khach-{danh-sach,ho-so,gop}-360.png` |
| Cổng kiểm của task | — | ci-task (sau `git merge main`: "Already up to date"). Lần 1: đỏ jscpd (15 đoạn trùng mới, xem Tái dùng) — đã sửa. Lần 2 | "Test Files 64 passed (64) / Tests 816 passed (816)", "CI-TASK XANH (so với điểm tách nhánh 331c072…)". Sau đó chỉ thêm trạng thái lỗi đọc (gợi ý gộp, danh sách nhãn, quy tắc, tìm ở trang Gộp) và 2 ca test: chạy lại `npx vitest run src/screens/khach` (46 qua), `npx tsc --noEmit`, `npx eslint`, `make jscpd` ("Found 0 clones") — xanh |

So ảnh với mockup (`docs/mockups/m2/png/khach-*.png`): đầu trang, gợi ý cùng một khách, 4 ô chỉ số, hai cột (dòng thời gian + ghi chú | nhãn, liên hệ, địa chỉ, hồ sơ đã gộp), bảng danh sách, hai thẻ so sánh, bảng lần gộp: cùng thứ tự và cỡ. Chỗ lệch có lý do — xem "Cần chủ shop xem".

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp: kế hoạch ghi `console/src/features/customers/…`; theo Q-T57-8 (như Task 84, 85) mọi tệp ở `console/src/screens/khach/`. Thêm `customerData.ts` (khóa truy vấn), `customerRules.ts` (luật thuần), `CreateCustomerDialog.tsx` (nút "Thêm khách" của mockup), `khachTestkit.tsx`, `khach.css`. Dòng thời gian (`CustomerTimeline`) nằm chung tệp `NotesPanel.tsx`; hộp quản lý nhãn (`TagsDialog`) chung tệp `TagRuleEditor.tsx`.
- Thêm đường `/khach/:id/gop` (trang Gộp của mockup khach-gop.html) ngoài `/khach`, `/khach/:id` của kế hoạch.
- `console/src/app/nav.ts`: `NAV_GROUPS` thêm nhóm "Khách hàng" (khung mockup M2, HV01). `errors.ts` không cần thêm dòng (mọi mã lỗi của API khách đã có câu).

## Quyết định thay chủ shop

1. Gộp: không chọn sẵn hồ sơ giữ lại (mockup vẽ ô bên trái đã chọn); nút ghi "Chọn hồ sơ giữ lại" và mờ tới khi chọn — theo Step 1 của kế hoạch "phải chọn hồ sơ giữ lại". Dễ đảo: một giá trị khởi tạo trong `MergeView.tsx`.
2. Các lần gộp đặt ở trang Gộp (`/khach/:id/gop#lan-gop`, như mockup trạng thái C), lấy từ các mục merge/merge_undone của dòng thời gian (không có API liệt kê lần gộp). Thẻ "Hồ sơ đã gộp" ở hồ sơ có nút "Các lần gộp" trỏ tới đó. Dòng quá 30 ngày (tính theo lúc gộp, Q14) ghi lý do thay cho nút; máy chủ vẫn quyết khi bấm.
3. Mở trang Gộp không kèm `?voi=`: chọn khách thứ hai từ gợi ý trùng SĐT (matchedBy phone) hoặc ô tìm khách (matchedBy staff).
4. Màu nhãn: máy chủ có 10 khóa màu nhưng hệ thiết kế chỉ có 4 kiểu chip; ánh xạ đỏ/cam/hồng → chip đỏ, xanh lá/ngọc → chip xanh, nâu → chip đậm, còn lại → chip viền (không thêm màu mới). Ô chọn màu ghi tên màu.
5. Thêm nhãn trên hồ sơ: ô "+ Thêm nhãn" có cả nhãn đang theo quy tắc (máy chủ cho gắn tay thêm, HV35); nhãn đã gắn tay thì không có trong ô.
6. "Thêm khách" là hộp nhỏ (họ tên, SĐT tùy chọn); tạo xong mở hồ sơ mới; SĐT đã thuộc khách khác → liên kết "Mở hồ sơ khách đó".
7. Liên hệ: thêm SĐT/email/Zalo và gỡ (không gỡ Facebook, tài khoản website — máy chủ quy định); địa chỉ chỉ xem (Cần chủ shop xem 4).
8. Mục "Khách hàng" trên menu sáng cả khi ở hồ sơ, trang gộp (`also`).

## Cần chủ shop xem (khác mockup vì API thiếu; không tự thêm API)

1. Danh sách khách không có cột "Nhãn", dòng phụ "2 hồ sơ đã gộp", "khách tiềm năng": `CustomerListItem` không có nhãn, số hồ sơ, lifecycle.
2. Hồ sơ đã gộp và lần gộp không ghi mã KH của hồ sơ con (mockup "KH003990 · Thu Hà (live)"): `Customer.memberIds` và summary của mục gộp chỉ có mã hồ sơ (uuid). Màn ghi "Hồ sơ đã gộp" kèm SĐT riêng của hồ sơ đó, và "hồ sơ trong nhóm"/"hồ sơ khác" trong bảng lần gộp.
3. Danh tính Facebook không có tên trang, tên người dùng ("Page ELIMAZ · Thu Hà Trần"): API chỉ có psid.
4. Địa chỉ: không có nút Sửa, "+ Thêm địa chỉ" ở hồ sơ — danh sách tệp của kế hoạch không có form địa chỉ (form địa chỉ của Lên đơn gắn với đơn); địa chỉ mới được lưu khi lên đơn. Nếu cần, một task riêng dùng `addCustomerAddress`/`updateCustomerAddress` (API đã có).
5. Ô "Mua gần nhất" không có dòng "cập nhật 14:31" (API không có lúc tính chỉ số). Nút "Lên đơn" mở /don/moi trống (form Lên đơn chưa nhận khách từ đường dẫn).
6. Gợi ý "có thể là cùng một khách" ghi "trùng SĐT với khách này" (API không nói trùng ở bao nhiêu đơn).

## Việc phát hiện thêm

- API thiếu cho đúng mockup: nhãn và số hồ sơ trên `CustomerListItem`; mã KH của hồ sơ con (memberIds kèm code) và của hai bên trong mục gộp; danh sách lần gộp của một nhóm (hiện đọc từ dòng thời gian, có thể phải "Xem cũ hơn").
- Form Lên đơn nhận khách từ đường dẫn (`/don/moi?khach=`) để nút "Lên đơn" ở hồ sơ điền sẵn khách.
- Máy chủ không phát báo hiệu `customer.changed`: màn khách đọc lại sau lệnh ghi của chính mình và khi quay lại cửa sổ (mặc định của TanStack Query), không tự cập nhật khi người khác sửa.

## Reviewer

Lượt 1: ĐẠT — không có phát hiện phải sửa. Đã đối chiếu quyền từng nút với `x-permission`, danh tính gỡ được (`identity.go`), nguồn nhãn (`ListGroupTags` max(source)), chiều gộp và `matchedBy` (`merge.go`), `blockingMergeId`/`ownerId` (`errors.go`), khóa summary của mục gộp (`timeline.sql`), version của quy tắc (`tagrules.go`), lỗi nhập không có giá trị ô (`importer.go`); 15 đường ghi đều qua `useWriteAction`; số ca trong bảng bằng chứng khớp mã; không test bị tắt. Ghi chú không bắt buộc (không sửa): lần gộp đang chặn nằm ngoài các trang dòng thời gian đã tải thì câu báo không có liên kết (hiếm: lần chặn mới hơn nên thường ở trang đầu); "Tải lại hồ sơ" khi version_conflict đóng hộp sửa (phần gõ còn tới khi bấm); 3 ép kiểu phần tử DOM trong test (không phải dữ liệu API).

Kết luận: ĐẠT
