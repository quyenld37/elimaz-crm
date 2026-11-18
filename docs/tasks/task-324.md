# Task 324 — Màn Sản phẩm: sửa mẫu, biến thể (M1-50b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 324, M1-50b; ảnh → 325, bài viết → 326). Mockup đã duyệt: `docs/mockups/m1/san-pham-sua.html` (+ `png/san-pham-sua-{1512,390,360}.png`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn. `catalog.GetClassification` (đã có,
  Task 34; tham chiếu: chỉ `classification_test.go`) được dùng lần đầu ở lớp HTTP. Thêm phương thức
  `(*handler).GetProductClassification` (gói `httpapi`, hiện thực `gen.Handler` sinh lại).
- Gói phụ thuộc ngược (go list): `internal/httpapi` ← `cmd/crm` (nối server; không đổi). `internal/httpapi/gen` sinh lại.
- Operation OpenAPI bị chạm, oasdiff: **thêm** `GET /api/v1/catalog/products/{id}/classification`
  (`getProductClassification`, `x-permission: catalog.read`, trả `ProductClassification` đã có) — việc treo từ Task 34,
  màn sửa cần để hiện danh mục, nhãn, thuộc tính đang gán. Chỉ thêm, oasdiff không có thay đổi phá vỡ. Màn gọi (client
  sinh): `getProduct`, `getProductClassification`, `createProduct`, `updateProduct`, `addVariant`, `updateVariant`,
  `setProductClassification`, `listCategories`, `listTags`, `listAttributes`.
- Chỗ dùng trên màn hình: tạo `console/src/features/products/{ProductEdit.tsx,VariantGrid.tsx,editRules.ts}` (+ test);
  thêm dòng `Screen` `/san-pham/:id` (ẩn, quyền `catalog.read`; `/san-pham/moi` khớp cùng mẫu) vào
  `console/src/app/screens.tsx` và bỏ `also` trùng ở dòng Danh sách nếu cần (Task 51 song song chỉ thêm dòng của nó);
  lớp chung còn thiếu của `m1.css` (`.cols`, `.grid-f`, `.label`, `.desk-only`) vào `console/src/styles.css`; lớp riêng
  của mockup vào `products.css`. Client sinh `console/src/api/**` sinh lại bằng `make gen`.
- Câu sqlc, bảng, pha migration: không có (dùng câu đã có của `GetClassification`).
- Test sẽ chạy: `go test -race ./internal/httpapi/ -run TestCatalogAPI_Classification` (thêm kiểm đọc phân loại, tenant
  khác 404), `./internal/modules/catalog/...` không đổi mã; console: `npx vitest run src/features/products`,
  `npx tsc --noEmit`, `npx eslint .`, Playwright `e2e/product-edit.spec.ts` (mới, 1512 và 390) + `e2e/products.spec.ts`
  (màn danh sách cùng thư mục) trên cổng 5124; `go test -race ./tests/arch/...` (bảng route thêm một dòng); `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (mục console và
  `catalog`), `src/app/{errors,idempotency,queryClient,nav}.ts`, `features/products/productRules.ts` (Task 50),
  `features/staff/*` (màn có ghi); Go: `catalog.GetClassification`, `readAsDefaultSeller` của `handlers_catalog.go`.
- Dùng lại / viết mới vì: dùng lại `useWriteAction` (khóa chống trùng, gửi lại cùng khóa khi mạng rớt — đúng tiêu chí
  "chỉ một mẫu được tạo"), `unwrap`/`errorMessage`/`ApiError` (câu `sku_taken` "Mã hàng đã có", `version_conflict`),
  `queryKey(TOPICS.product, …)`, `formatMoney`, `categoryOptions`, `STATUS_CHIP` của `productRules.ts` (cùng feature),
  lớp CSS có sẵn (bảng thành thẻ dưới 760 px, `.tag`, `.savebar.is-sticky`, `.box-err`, `.field-err`). Go: dùng lại
  `catalog.GetClassification` và `readAsDefaultSeller` (không viết truy vấn mới). Viết mới: luật của biểu mẫu (dựng
  lưới màu × size, gợi ý SKU bỏ dấu, đọc giá "350.000", so bản nháp với bản đã lưu, thứ tự các bước lưu) trong
  `editRules.ts` vì chưa có và chỉ màn này dùng.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

Mục 5 (khớp kiến trúc):
- 5.1–5.6, 5.10: không áp dụng: không thêm bảng, transaction, sự kiện hay lệnh ra ngoài; đường đọc mới gọi public API
  `catalog.GetClassification` của module sở hữu.
- 5.7 Ngữ cảnh shop: handler mới chạy trong `readAsDefaultSeller` (= `tenancy.WithTenant` + người bán mặc định, ADR-29);
  tenant khác → 404 (test).
- 5.8 Lớp HTTP mỏng: handler chỉ đổi kiểu; màn không làm nghiệp vụ: barcode tự sinh, chuẩn hóa SKU, kiểm trùng đều ở
  API; màn chỉ gợi ý SKU và báo lỗi trước cho ô trống, giá sai dạng, SKU trùng trong chính biểu mẫu.
- 5.9 Đường ghi: mọi lần ghi qua `useWriteAction` (`Idempotency-Key` sinh khi bấm Lưu, gửi lại cùng khóa); operation
  ghi đã có `x-permission: catalog.manage`. Operation mới chỉ đọc, `x-permission: catalog.read`.
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: đường đọc như `getProduct`; màn như Task 50 / 54 (`features/<màn>/`, dòng `SCREENS`, vitest +
  Playwright với API giả).

Mục 6 (đúng khi chạy đồng thời):
- 6.1 Hai người cùng sửa: bản nháp giữ `version` của mẫu và từng biến thể lúc mở; mọi bước lưu gửi version đó (mẫu:
  `updateProduct`, `setProductClassification`, `addVariant` khóa theo version mẫu; biến thể: `updateVariant` theo version
  biến thể). Người khác vừa lưu → 409 `version_conflict` → hộp "Mẫu đã thay đổi, tải lại." + nút "Tải lại mẫu" (bỏ bản
  nháp, đọc lại). Báo hiệu realtime tải lại dữ liệu nền nhưng **không** đè bản nháp và không đổi version của bản nháp
  (nếu đổi, lần lưu sẽ đè thay đổi của người khác). Version của bản nháp = min(version của `getProduct`,
  `getProductClassification`) để hai lần đọc lệch nhau thì lần lưu báo xung đột chứ không đè.
- Lưu nhiều bước không nguyên tử (API chia operation): thứ tự `updateProduct` → `setProductClassification` →
  `addVariant` (mỗi bước version mẫu +1, biết trước vì máy chủ khóa đúng version) → `updateVariant` (chỉ kiểm version
  biến thể, tăng version mẫu). Bước lỗi thì dừng; các bước đã xong được ghi vào bản đã lưu của bản nháp (không gửi
  lại), phần còn lại vẫn là thay đổi chưa lưu, dòng lỗi tô đỏ.
- 6.2 Gửi lại: khóa chống trùng của mỗi bước; mạng rớt → `useWriteAction` gửi lại cùng khóa (tối đa 3 lần), bấm Lưu
  lại với cùng nội dung → cùng khóa. Tạo mẫu: một yêu cầu `createProduct` kèm mọi biến thể → chỉ một mẫu (test e2e).
- 6.3–6.9, 6.11: không áp dụng: không database mới, hàng đợi, việc nền.
- 6.10 Tiền: giá là số nguyên đồng; ô giá nhận "350.000" hoặc "350000", chữ khác hay số lẻ → lỗi tại ô, không làm tròn;
  vượt `Number.MAX_SAFE_INTEGER` → lỗi.
- 6.12 Dữ liệu cá nhân: không có.
- 6.13 Quyền: trang cần `catalog.read`; người không có `catalog.manage` (theo grant) thấy biểu mẫu chỉ đọc, không có
  nút Lưu; API vẫn kiểm (403 → hộp báo).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tạo mẫu 2 màu × 3 size → 6 dòng biến thể, một `createProduct` có `Idempotency-Key`, sang `/san-pham/<id>` | `product-edit.spec.ts` "tạo mẫu 2 màu × 3 size…" (1512, 390); `ProductEdit.test.tsx` "2 màu × 3 size → 6 dòng…"; `VariantGrid.test.tsx` "thêm 2 màu, 3 size…"; `editRules.test.ts` "lưới màu × size" | `cd console && CRM_E2E_PORT=5124 npx playwright test e2e/product-edit.spec.ts e2e/products.spec.ts e2e/shell.spec.ts e2e/shell.phone.spec.ts`; `npx vitest run` | `27 passed (20.5s)`; `Test Files 14 passed (14)`, `Tests 161 passed (161)` |
| SKU trùng → "Mã hàng đã có" ở đúng dòng (sửa); trang tạo → câu "Mã hàng đã có: …"; trùng ngay trong bảng → "Trùng SKU với dòng khác" | `product-edit.spec.ts` "SKU trùng khi sửa…"; `ProductEdit.test.tsx` "SKU trùng khi sửa biến thể…", "SKU đã có ở mẫu khác (sku_taken)…"; `editRules.test.ts` "thiếu mã, tên, giá; SKU sai dạng; SKU trùng…" | như trên | qua |
| Người khác vừa lưu → "Mẫu đã thay đổi, tải lại." + "Tải lại mẫu" hiện bản mới, bỏ bản nháp | `product-edit.spec.ts` "người khác vừa lưu…"; `ProductEdit.test.tsx` "người khác vừa lưu (version_conflict)…" | như trên | qua |
| Mạng rớt lúc lưu → gửi lại cùng khóa, chỉ một mẫu được tạo | `product-edit.spec.ts` "mạng rớt lúc lưu…" (máy chủ giả làm xong rồi cắt câu trả lời; kiểm 2 POST cùng khóa, 1 mẫu) | như trên | qua |
| Lưu nhiều bước theo version: `updateProduct` → `setProductClassification` → `addVariant` → `updateVariant`; bước đã xong không gửi lại; version bản nháp = min(mẫu, phân loại) | `ProductEdit.test.tsx` "đổi tên, giá, thêm nhãn, thêm size…", "SKU trùng khi sửa biến thể… bước trước đã lưu không gửi lại"; `editRules.test.ts` "savePlan", "version của bản nháp…" | `npx vitest run src/features/products` | qua |
| Tên 80 ký tự không tràn; không cuộn ngang ở mọi ca; dưới 1000 px thanh Lưu dính đáy | `product-edit.spec.ts` "trạng thái A…" + `noHorizontalScroll` mọi ca | như dòng đầu | qua |
| Ảnh chụp so mockup | `trang-thai-a-{1512,390}.png` do `product-edit.spec.ts` lưu, so tay với `docs/mockups/m1/png/san-pham-sua-{1512,390}.png` | `npx playwright test e2e/product-edit.spec.ts --output=<tmp>` | Khớp bố cục: đầu trang (crumb, tên 2 dòng, chip, nút Lưu), thẻ Thông tin, Phân loại (danh mục, nhãn có ×, + Thêm nhãn, 4 thuộc tính), Biến thể (chip màu/size, bảng 6 dòng, dòng Ngừng bán mờ, thẻ dưới 760 px). Sai khác do phạm vi/quyết định: không có thẻ Ảnh (Task 325), Bài viết (Task 326), dòng "Người bán", "Sửa lần cuối" (quyết định 3, 4); nút Lưu/Hủy mờ khi chưa có thay đổi (quyết định 5) |
| Lưu xong mà đọc lại lỗi → giữ giá trị vừa lưu, câu "Đã lưu, nhưng chưa tải lại được mẫu: …" (sửa theo ghi chú reviewer lần 1) | `ProductEdit.test.tsx` "lưu xong mà đọc lại lỗi…" (đỏ trước khi sửa `reload`, qua sau) | `npx vitest run src/features/products` | `Tests 60 passed (60)` |
| Chỉ đọc khi thiếu `catalog.manage`; mã không phải uuid, mẫu không có | `ProductEdit.test.tsx` "chỉ có catalog.read…", "mẫu không có (404)…" | `npx vitest run src/features/products` | qua |
| API đọc phân loại: người xem đọc đúng danh mục, giá trị, nhãn, version; tenant B, mẫu không có → 404 | `TestCatalogAPI_Classification` (`checkReadClassification`) | `go test -race ./internal/httpapi/ -run 'TestCatalogAPI\|TestRoute'` | trước khi có handler: `getProductClassification: 404` (FAIL); sau: `ok elimaz.com.vn/crm/internal/httpapi 39.253s` |
| Màn danh sách bấm dòng mở được trang sửa | `products.spec.ts` "1 mẫu → …" (thêm đường API giả của trang sửa và kiểm tiêu đề) | như dòng đầu | qua |
| Kiểu, lint, định dạng | — | `cd console && npx prettier --check e2e src && npx tsc --noEmit && npx eslint .` | không lỗi |
| Cổng `make ci` (bản đã gộp `main` tới `d4a1baa`: Task 320, Task 395) | — | `make ci CRM_PG_PORT=5600`, rồi chạy lại riêng phần bị hạ tầng làm đỏ | Lần chạy đầy đủ cuối: build, vet, golangci-lint (`0 issues`), gen-check, sqlc-vet qua; bước `test` đỏ ở 15 gói vì PostgreSQL chung 5600 sập giữa chừng (`FATAL: the database system is in recovery mode (SQLSTATE 57P03)`, `CREATE DATABASE … conn closed`; 215 dòng như vậy, không có lỗi khẳng định nào). Lần trước đó đỏ vì hết thời gian chờ khi tạo/migrate database thử (máy tải ≈ 24). Chạy lại khi PostgreSQL lên lại: `go test -race -count=1 ./internal/httpapi/... ./internal/modules/catalog/...` → `ok …/internal/httpapi 96.114s`, `ok …/internal/modules/catalog 51.737s` (+ 3 gói con ok); `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` → `EXIT 0` ("No vulnerabilities found", "Found 0 clones", `no leaks found`, `Tests 161 passed (161)`, `built in 489ms`); sau lần gộp thứ hai: `make build vet lint gen-check sqlc-vet` → `EXIT 0`, `0 issues`; `go test -race -count=1 ./tests/arch/...` (bảng route, theo ghi chú reviewer lần 2) → `ok elimaz.com.vn/crm/tests/arch 24.541s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm operation đọc `getProductClassification` (việc treo từ Task 34; kế hoạch không liệt kê nhưng màn sửa cần để hiện
  phân loại đang gán) cùng handler, test API; hàm `classificationOut` tách từ `SetProductClassification` để hai handler
  dùng chung (cùng commit vì chỉ phục vụ đường mới).
- Thêm `editRules.ts` (+ test) ngoài mục Files: luật biểu mẫu test bằng vitest, như `productRules.ts` của Task 50.
- `console/e2e/products.spec.ts` (Task 50): ca "1 mẫu → bấm dòng sang trang sửa" giờ mở trang sửa thật nên khai thêm
  đường API giả của trang đó và kiểm thêm tiêu đề; không nới điều kiện nào.
- `styles.css` thêm lớp chung còn thiếu của `m1.css`: `.label`, `.grid-f`, `.cols`, `.desk-only`.
- Tách `console/src/features/products/productsTestkit.tsx` (tên 80 ký tự, danh mục mẫu, `renderAt` dựng màn dưới
  QueryClient + MemoryRouter) dùng chung cho `ProductList.test.tsx` (Task 50) và `ProductEdit.test.tsx`: cổng jscpd báo
  2 đoạn trùng mới giữa hai tệp test cùng feature, cùng mục đích, cùng lý do thay đổi → gộp. Không đổi ca test nào của
  Task 50. Trong e2e, phần mở mẫu trạng thái A lặp ở ba test gộp thành `openMockup`.
- Test thất bại trước: Go và component (vitest) chạy đỏ trước khi có mã; e2e viết sau component và chạy lần đầu đã qua.

## Quyết định thay chủ shop

1. **Lưu chia nhiều bước, không nguyên tử.** API có sẵn tách sửa mẫu, phân loại, thêm và sửa biến thể. Chọn: chạy tuần
   tự theo version (mỗi bước một khóa chống trùng), bước lỗi thì dừng, bước đã xong giữ lại, phần chưa lưu vẫn là "có
   thay đổi chưa lưu" và dòng lỗi tô đỏ. Vì sao: không đổi hợp đồng ghi, mỗi bước vẫn an toàn khi gửi lại và khi hai
   người cùng sửa. Phương án khác: một operation "lưu cả mẫu" nguyên tử (việc backend lớn; ghi ở Việc phát hiện thêm).
2. **SKU trùng ở trang tạo** chỉ báo chung "Mã hàng đã có: một SKU trong bảng đã dùng cho mẫu khác…" vì lỗi
   `sku_taken` của `createProduct` không nói SKU nào. Ở trang sửa (mỗi biến thể một yêu cầu) câu nằm đúng dòng như
   mockup. Phương án khác: thêm trường `sku` vào thân lỗi (đổi hợp đồng; ghi ở Việc phát hiện thêm).
3. **Không có dòng "Người bán: ELIMAZ"** (như Task 50 quyết định 2: chưa có API trả tên người bán).
4. **Không có "Sửa lần cuối 14:02 18/11"**: `Product` không có thời điểm sửa. Đầu trang ghi "N biến thể · M ảnh".
5. **Nút Lưu mẫu / Hủy thay đổi mờ khi chưa có thay đổi** (trang sửa); "Có thay đổi chưa lưu" chỉ hiện khi có. Trang
   tạo luôn bấm được Lưu.
6. **Nút × của màu/size chỉ hiện khi không biến thể đã lưu nào dùng nó** (mockup vẽ × ở mọi chip, câu gợi ý nói "chỉ bỏ
   được biến thể chưa lưu"); không có gì chỉ hiện khi rê chuột.
7. **Mẫu mới mặc định "Đang nhập · ẩn với khách"**; mã mẫu không sửa được sau khi tạo (hợp đồng `updateProduct` không có
   `code`). Dòng biến thể mới: SKU gợi ý `<MÃ MẪU>-<MÀU>-<SIZE>` bỏ dấu, đổi theo mã mẫu tới khi sửa tay; giá để trống
   (bắt buộc nhập); cột trạng thái ghi "Mới, chưa lưu".
8. **Trang tạo lưu phân loại sau khi tạo** (`setProductClassification` với version của mẫu vừa tạo); bước này lỗi thì
   vẫn sang trang sửa và báo "Đã tạo mẫu, nhưng phân loại chưa lưu…" (không tạo lại mẫu).
9. **Chỗ cho Ảnh (Task 325) và Bài viết (Task 326)**: không vẽ thẻ tạm cho nhân viên; để chú thích trong
   `ProductEdit.tsx` ở đúng chỗ của mockup (Ảnh: cột phải trên Phân loại; Bài viết: cột trái dưới Thông tin).

## Việc phát hiện thêm

- Lỗi `sku_taken`/`barcode_taken` của `createProduct` không nói biến thể nào; thêm trường (ví dụ `sku`) vào thân lỗi
  thì trang tạo tô đỏ đúng dòng như trang sửa.
- Nếu muốn lưu mẫu nguyên tử (tên, phân loại, biến thể trong một transaction), cần một operation mới ở `catalog`.
- `Product` chưa có `updatedAt` để hiện "Sửa lần cuối" như mockup.
- Rời trang khi còn thay đổi chưa lưu chưa hỏi lại (mockup không vẽ); cân nhắc `useBlocker` khi chủ shop muốn.

## Reviewer

Lần 1 (trên `git diff --cached` trước khi gộp Task 320/395): **ĐẠT**, không có phát hiện phải sửa. Ghi chú đã sửa:
`reload` khi đọc lại lỗi dựng lại bản nháp từ dữ liệu cũ (`refetch` trả `data` cũ) → sau khi lưu xong mà đọc lại lỗi màn
hiện lại giá trị trước khi lưu. Sửa: `reload` trả câu lỗi, giữ biểu mẫu, báo "Đã lưu, nhưng chưa tải lại được mẫu: …"
(test "lưu xong mà đọc lại lỗi…"). Ghi chú khác: e2e chạy lần đầu đã qua (đã ghi ở Khác kế hoạch).

Lần 2 (trên `git diff --cached` của bản đã gộp `main` tới `d4a1baa`): **ĐẠT**, không có phát hiện phải sửa. Đã kiểm
`reload` mới, `productsTestkit.tsx` (không ca nào của Task 50 đổi nghĩa), `openMockup`, handler mới không chạm mã Task
320. Ghi chú đã làm: chạy `tests/arch` sau khi gộp (qua, ghi ở Bằng chứng) và thêm vào "Test sẽ chạy". Ghi chú không
sửa: dòng `mockImplementation` lặp trong ca 404 của `ProductEdit.test.tsx` (vô hại).
