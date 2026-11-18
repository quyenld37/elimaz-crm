# Task 52 — Màn Nhập Excel

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ đổi màn hình `console/`).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng; màn chỉ gọi operation đã có trên `main` (Task 47, 323):
  `getCatalogImportTemplate`, `createUpload` (purpose `catalog_import`), `completeUpload`, `createCatalogImport`,
  `getCatalogImport`, `applyCatalogImport`. oasdiff không chênh.
- Chỗ dùng trên màn hình: thêm thư mục `console/src/screens/imports/` (route.tsx tự đăng ký "/nhap-excel", nhóm
  "Sản phẩm", quyền `catalog.import`; ImportPage.tsx, ImportErrors.tsx, importRules.ts, imports.css, test vitest) và `console/e2e/imports.spec.ts`. Dùng lại `Dialog` của `screens/staff/staffData.tsx` (như `roles` đang
  dùng), không sửa nó. Không sửa màn khác; menu "Sản phẩm" có thêm mục "Nhập Excel" (khung đọc route.tsx tự động),
  nên thêm một dòng vào danh sách màn mong đợi của `console/src/app/screens.test.tsx` (test so cả danh sách).
- Ký hiệu TypeScript đổi: `xhrPut` (`console/src/screens/products/useUpload.ts`) thêm tham số tùy chọn thứ năm
  `onProgress` và nhận `Blob` thay vì `File` (rộng hơn). Chỗ dùng: `useUploads` (mặc định của `put`, không truyền
  onProgress — hành vi không đổi) và `ImportPage.tsx`. Vẫn gán được cho kiểu `PutFile` (`tsc` kiểm ở `put ?? xhrPut` trong `useUploads`).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/imports src/app` (luật thuần, màn, nạp màn), `npx tsc --noEmit`,
  `npx eslint .`, `npx playwright test e2e/imports.spec.ts` (1512 và 390, cổng 5152), và một lần cổng kiểm của task
  (ci-task.sh) trước khi review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console
  (`useWriteAction`, `unwrap`, `errorMessage`, `TOPICS`/`queryKey`, `useAbortOnUnmount`, `defaultSleep`); tìm
  "showModal", "XMLHttpRequest", "upload", "download", "Blob" trong `console/src`.
- Dùng lại: `useWriteAction` cho cả bốn đường ghi (createUpload, completeUpload, createCatalogImport,
  applyCatalogImport: khóa chống trùng, tự gửi lại cùng khóa); `unwrap`/`errorMessage` cho lỗi; `Dialog` của
  `screens/staff/staffData.tsx` cho hộp xác nhận áp dụng (tiền lệ: `roles` nhập từ đó); lớp CSS có sẵn trong
  `styles.css` (`page-h`, `cols`, `card`, `box-*`, `chip-*`, `tbl`, `savebar`, `dlg`).
- Dùng lại `xhrPut` của `screens/products/useUpload.ts` (PUT lên đường ký sẵn), thêm tham số tùy chọn `onProgress` cho
  thanh phần trăm (mockup trạng thái B). Bản đầu viết hàm PUT riêng; jscpd báo trùng với `xhrPut` nên gộp: cùng mục
  đích (PUT một tệp lên kho tệp) và cùng lý do đổi (giao thức PUT ký sẵn của kho).
- Không dùng `useUploads`: gắn chặt với ảnh (purpose `product_image`, chờ xử lý ảnh qua `getFile`, hàng đợi nhiều
  tệp); màn Nhập chỉ một tệp, không chờ xử lý, tiếp ngay `createCatalogImport`.
  Lớp `.steps`, `.bar`, `.drop`, `.kpis`/`.kpi` chưa có trong `styles.css`: chép từ `docs/mockups/m1/m1.css` vào
  `imports.css` của màn (như các màn khác giữ lớp riêng).

## Soát thiết kế

- 5.1–5.4, 5.7, 5.10: không áp dụng — màn hình không chạm database; dữ liệu thuộc `catalog` (bản nhập) và
  `platform/files` (tệp), đọc/ghi qua API của Task 41, 47, 323.
- 5.5–5.6: không áp dụng (không sự kiện, không gọi ra ngoài; PUT tệp lên kho bằng đường ký sẵn do máy chủ cấp).
- 5.8: không áp dụng (không mã Go).
- 5.9: mọi đường ghi qua `useWriteAction` (Idempotency-Key sinh lúc bấm, giữ nguyên khi tự gửi lại); quyền đã khai
  trong hợp đồng (`catalog.import`, `files.upload`).
- 5.11: không thêm thư viện (đọc Content-Disposition, tạo tệp tải về bằng `URL.createObjectURL` có sẵn).
- 5.12: theo khuôn màn tự đăng ký (`route.tsx`), luật thuần tách tệp `*Rules.ts` có test riêng, e2e với API giả
  `e2e/fixtures.ts`.
- 6.1: hai người cùng áp một bản nhập: gửi `version` của bản đang xem; máy chủ trả `version_conflict` hay
  `import_not_ready` → màn hiện câu lỗi và tải lại bản nhập (không tự gửi lại).
- 6.2: bấm hai lần "Áp dụng" hay mạng chập: `useWriteAction` trả lại lời hứa đang chạy, gửi lại cùng khóa; tệp cùng
  nội dung tải lại → máy chủ báo `duplicate` (kiểm theo SHA-256), màn hiện "Tệp này đã nhập lúc …".
- 6.3–6.5, 6.7, 6.10, 6.11: không áp dụng (không database, không tiền ở màn; giá chỉ do máy chủ kiểm).
- 6.6, 6.8: việc kiểm và áp chạy nền ở máy chủ; màn hỏi `getCatalogImport` mỗi 2 s khi `validating`/`applying`, mã bản
  nhập nằm trên URL (`?id=`) nên rời trang rồi quay lại vẫn thấy kết quả (mockup: "Có thể rời trang").
- 6.9: hỏi lại chỉ khi bản nhập còn chạy; dừng hỏi khi trạng thái cuối. PUT tệp bị hủy khi rời màn.
- 6.12: không dữ liệu cá nhân (tên tệp, SKU, câu lỗi do máy chủ đã cắt ≤ 50 ký tự nội dung ô).
- 6.13: màn chỉ hiện với `catalog.import`; API vẫn kiểm (`catalog.import`, `files.upload`); lỗi `forbidden` hiện câu
  chung của `errorMessage`.

## Quyết định thay chủ shop

- **Q1 — Đường dẫn `/nhap-excel`** (kế hoạch không ghi): không đặt `/san-pham/nhap` vì mục "Danh sách sản phẩm" có
  `also: ["/san-pham/:id"]` nên sẽ sáng cùng lúc với "Nhập Excel". Đổi lại chỉ là sửa một dòng route.tsx.
- **Q2 — Bỏ hai chi tiết mockup mà API không trả:** "Áp dụng bởi Lan lúc 10:02 18/11" (trạng thái E) và "Người bán:
  ELIMAZ" (trạng thái D) — `CatalogImport` không có người áp, giờ áp, người bán. Không đổi hợp đồng trong task màn hình;
  ghi vào Việc phát hiện thêm. Phương án khác: thêm trường vào API (ngoài phạm vi task 52).
- **Q3 — Giờ "Tệp này đã nhập lúc …"** lấy `createdAt` của bản nhập đã áp (`duplicateOf`, đọc bằng `getCatalogImport`)
  vì API không có giờ áp; chưa đọc được thì ghi "Tệp này đã nhập trước đây.". Lệch vài phút so với giờ áp thật.
- **Q4 — Nút "Chọn tệp khác"/"Chọn tệp đã sửa" trong các hộp trạng thái F** (đã nhập, không đọc được, áp thất bại):
  mockup không vẽ nút ở F nhưng vẽ cùng nút đó ở C và D; không có nút thì người dùng kẹt ở hộp báo. Không thêm hành vi
  mới (chỉ mở hộp chọn tệp như C, D).
- **Q5 — Kiểm tệp ngay trên máy trước khi tải lên:** đuôi khác `.xlsx`, tệp rỗng, quá 5 MB → hộp "Không đọc được tệp."
  với câu của mockup, không gọi API (máy chủ cũng từ chối, chỉ là đỡ tải lên vô ích). Tệp hỏng, quá 20.000 dòng do máy
  chủ báo (lỗi dòng 0) → cùng tiêu đề, kèm câu của máy chủ.
- **Q6 — Giữ mã bản nhập và tên tệp trên URL** (`?id=&tep=`) để "Có thể rời trang; kết quả giữ ở đây" (mockup B) đúng
  cả khi tải lại trang. Hỏi lại mỗi 2 s khi đang kiểm/đang áp (không có báo hiệu realtime cho bản nhập).
- **Q7 — Thanh tiến độ khi kiểm và khi áp** (mockup vẽ 35%, 60%): máy chủ không báo phần trăm, nên thanh chạy qua lại
  (tắt khi người dùng chọn giảm chuyển động) thay vì một con số giả. Khi tải lên thì là phần trăm thật.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tải mẫu → tệp .xlsx đúng tên, đúng byte (1512, 390) | `e2e/imports.spec.ts` "tải tệp mẫu: tệp .xlsx đúng tên và đúng nội dung máy chủ trả" | `cd console && CRM_E2E_PORT=5152 npx playwright test e2e/imports.spec.ts --project chromium` | `18 passed` (9 ca × 2 khổ) |
| Tệp lỗi → bảng 7 lỗi đúng dòng, cột, câu; không có nút áp dụng; chuỗi createUpload → PUT (header ký sẵn) → completeUpload → createCatalogImport, mỗi lần một khóa | "tệp có 7 lỗi: bảng lỗi đúng dòng, cột, câu tiếng Việt; không có nút áp dụng" | như trên | qua |
| 200 mẫu → xem trước 200 / 1.000 → hộp xác nhận → áp dụng (body `{version: 2}`, có khóa) → "Đã nhập xong."; tải lại cùng tệp → "Tệp này đã nhập lúc 10:02 18/11." | "tệp 200 mẫu hợp lệ: xem trước, áp dụng, đã nhập; tải lại cùng tệp → đã nhập lúc …" | như trên | qua |
| Rời trang rồi quay lại vẫn thấy kết quả | "rời trang rồi quay lại: kết quả kiểm vẫn ở đó" | như trên | qua |
| Tệp sai đuôi, quá 5 MB chặn ngay (không gọi API); tệp hỏng → câu của máy chủ, không bảng | "tệp không đọc được: …" | như trên | qua |
| Áp thất bại vì dữ liệu vừa đổi → "Không ghi được…", "Dòng 14: …" | "áp dụng thất bại vì dữ liệu vừa đổi: không ghi gì, nêu dòng" | như trên | qua |
| Lỗi API khi tạo, version_conflict khi áp, mở bản nhập không có | "tạo bản nhập lỗi…", "áp dụng bị từ chối vì người khác vừa đổi…", "mở bản nhập không có…" | như trên | qua |
| Không cuộn ngang ở 390 với tên tệp 75 ký tự | `noHorizontalScroll` trong các ca trên | như trên | qua |
| Luật thuần: kiểm tệp, số, giờ, cỡ, tên tệp tải về (bỏ đường dẫn), bước | `src/screens/imports/importRules.test.ts` (12 ca) | `npx vitest run` | `Test Files 25 passed, Tests 292 passed` |
| Màn tự đăng ký đúng path, nhãn, nhóm, quyền | `src/app/screens.test.tsx` | `npx vitest run` | qua |
| Màn Sản phẩm vẫn tải ảnh đúng sau khi đổi `xhrPut` | `useUpload.test.ts`, `e2e/product-images.spec.ts`, `e2e/shell.spec.ts` | `npx playwright test e2e/imports.spec.ts e2e/product-images.spec.ts e2e/shell.spec.ts --project chromium` | `30 passed` |
| Kiểu, lint | — | `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check src e2e` | sạch |
| Cổng kiểm của task (sau khi gộp main 50389d9) | ci-task.sh | ci-task.sh (lần 2) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check xanh; test kiến trúc đỏ chỉ ở `TestLintCleanOnRealCode`, `TestPackagePatternSkipsStrayDirs` với "parallel golangci-lint is running" (khóa golangci-lint dùng chung với phiên khác trên máy) |
| Hai test kiến trúc trên khi khóa lint rảnh | `TestLintCleanOnRealCode`, `TestPackagePatternSkipsStrayDirs` | `go test -count=1 -run '^(TestLintCleanOnRealCode\|TestPackagePatternSkipsStrayDirs)$' ./tests/arch` | `ok elimaz.com.vn/crm/tests/arch 25.773s` |
| So ảnh chụp với mockup (A, C, D, hộp xác nhận, E, F ở 1512 và 390) | ảnh chụp tạm, xem bằng mắt | Playwright `page.screenshot` | khớp bố cục, chữ, lớp của mockup |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bước 1 ghi "tải mẫu → tệp `.xlsx` có đủ cột": cột của tệp mẫu do máy chủ dựng và đã có test Go
  (`TestImportTemplate`, `TestTemplateRoundTrip`, Task 47). E2E màn chạy với API giả nên kiểm màn lưu đúng tên
  (`mau-nhap-san-pham.xlsx` từ Content-Disposition) và đúng từng byte máy chủ trả.
- "Tạo 200 mẫu, 1.000 mã hàng": hiện theo mockup đã duyệt (ô Mẫu 200, Biến thể tạo mới 1.000, câu "hoặc đủ 1.000
  biến thể", hộp xác nhận "Tạo 1.000 biến thể … của 200 mẫu").
- Thêm `importRules.ts` (+ test), `imports.css` ngoài hai tệp kế hoạch liệt kê, theo khuôn các màn
  khác (luật thuần tách riêng, CSS riêng của màn).

## Việc phát hiện thêm

- `CatalogImport` thiếu người áp, giờ áp và người bán để hiện đủ mockup ("Áp dụng bởi Lan lúc …", "Người bán: …",
  "Tệp này đã nhập lúc <giờ áp>"). Đề xuất task API thêm `appliedAt`, `appliedBy` (tên hiển thị), `sellerName`.
- `CatalogImport` không giữ tên tệp gốc; màn giữ tên trên URL. Có thể thêm `fileName` vào phản hồi.
- `Dialog` đang nằm trong `screens/staff/staffData.tsx` mà `roles` và `imports` cùng dùng: nên dời sang
  `console/src/shared/` (một task cấu trúc riêng). Cùng task đó dời `xhrPut` (`screens/products/useUpload.ts`), nay
  `products` và `imports` cùng dùng (luật console 6).

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1 — CẦN SỬA, chỉ tệp task: mục Ảnh hưởng ghi đường dẫn "/san-pham/nhap" thay vì "/nhap-excel"; mục Khác kế
  hoạch kể `importUpload.ts` (đã bỏ khi gộp vào `xhrPut`). Đã sửa cả hai, cùng ghi chú không bắt buộc: câu về
  `PutFile`, ghi việc dời `xhrPut` sang `console/src/shared/`, bỏ dấu chấm cuối câu máy chủ ở ca `failed` (tránh "..").
- Lượt 2 — ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc (không sửa): màn nhập mã từ màn khác (đã ghi vào Việc
  phát hiện thêm); PUT tệp nhập không tự gửi lại khi mạng chập (người dùng chọn lại tệp, có câu báo); ca `failed` không
  có lỗi nào hiện câu chung (hợp đồng khó xảy ra); Q4, Q5 cần chủ shop xác nhận theo luật console 1.
