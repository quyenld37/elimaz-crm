# Task 51 — Màn Tồn kho

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 51, M1-51; nhà cung cấp, phiếu kho, kiểm kho điện thoại đã tách sang 327, 328, 329 — không làm ở đây). Mockup đã duyệt: `docs/mockups/m1/kho-ton.html` (+ `png/kho-ton-{1512,390}.png`). Dựa trên Task 334 (`listBalances`, commit `2b97bff`, làm trước trong cùng worktree). Loại việc (quy trình mục 3): A

## Ảnh hưởng

**Lượt 2 (gộp `main` 905e1a1 — Task 324, màn sửa mẫu):** không đổi ký hiệu Go hay hợp đồng của Task 51. Xung đột:
`console/src/app/screens.tsx` (giữ cả dòng `/kho/ton` và `/san-pham/:id`; hai màn Sản phẩm đứng cạnh nhau),
`console/src/styles.css` (giữ cả `.toggle` của Task 51 và `.label`/`.grid-f`/`.cols`/`.desk-only` của Task 324),
mã sinh `oas_router_gen.go`, `sdk.gen.ts`, `index.ts` (không sửa tay: `openapi.yaml` tự gộp sạch, chạy `make gen`). Test chạy:
toàn bộ vitest, tsc, eslint, prettier, toàn bộ Playwright console (52 ca, gồm `product-edit.spec.ts` mới), `make ci`.


- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu đã có. Thêm: `inventory.Warehouse`,
  `inventory.ListWarehouses` (gói `internal/modules/inventory`); `gen.Handler` thêm `ListWarehouses` (ogen sinh),
  `*handler` hiện thực trong `handlers_inventory.go`; câu sqlc mới sinh `store.ListWarehouses`.
- Gói phụ thuộc ngược (go list): `internal/modules/inventory` ← `cmd/crm`, `internal/httpapi`, `internal/modules/order`,
  `invtest`; thêm hàm, không đổi hàm cũ nên không chỗ nào phải sửa. `internal/httpapi` ← `cmd/crm`, `tests/arch`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: thêm `GET /api/v1/inventory/warehouses` `listWarehouses` (`inventory.read`;
  chỉ trả kho người gọi có `inventory.read`), schema mới `Warehouse`, `WarehouseStatus`. Chỉ thêm → không phá vỡ.
  Dùng `listBalances` (Task 334) không đổi.
- Chỗ dùng trên màn hình: màn mới `/kho/ton` (`console/src/features/inventory/Balances.tsx`, một dòng trong
  `src/app/screens.tsx`, nhóm menu "Kho"); lớp `toggle` của mockup thêm vào `src/styles.css` (các lớp chung khác —
  `toolbar`, `more`, `empty`, `num`, `clamp2` — Task 50 đã thêm, dùng lại); `e2e/staff.spec.ts` ca menu khai đường
  `catalog` (xem Khác kế hoạch).
  `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: thêm câu `ListWarehouses :many` trong `queries/inventory/warehouses.sql` (đọc
  `inventory.warehouses`, bảng của chính module, có RLS). Không migration.
- Test sẽ chạy: `go test -race ./internal/modules/inventory/ -run Warehouse`, `./internal/httpapi/ -run Warehouses`,
  `./tests/arch/...`; `cd console && npm test && npx tsc --noEmit && npx eslint .`; Playwright
  `e2e/inventory.spec.ts` + `e2e/shell.spec.ts` (menu) ở 1512 và 390; một lần `make ci` trước khi stage.

## Tái dùng

**Lượt 2:** chỉ gộp, không viết mã mới; hai bộ lớp CSS khác tên, không trùng nhau nên giữ cả hai.


- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `inventoryRead` (đọc trong tenant), `identity.Can` +
  `warehouseScope` (kiểm phạm vi kho, như `canAt`), mẫu sqlc của module inventory; phía màn: `unwrap`, `errorMessage`,
  `queryKey(TOPICS.balance, …)` (realtime tự tải lại khi số dư đổi), lớp `tbl`, `tbl-frame`, `select-wrap`, `box`,
  `chip`, `btn` đã có trong `styles.css`; fixture e2e `api`, `forEachViewport`. `RefusalBox`/`Dialog` của
  `features/staff` không cần (màn chỉ đọc).
- Dùng lại / viết mới vì: dùng lại tất cả. Viết mới `ListWarehouses` (chưa có hàm liệt kê kho; chủ shop giao làm ở
  task này). Lớp CSS chung chép nguyên từ `docs/mockups/m1/m1.css` vào `styles.css` (không viết lại).

## Soát thiết kế

**Lượt 2:** gộp không đổi đường đọc, quyền hay database; mã sinh lấy từ `openapi.yaml` đã gộp (có cả
`listWarehouses` và các operation của Task 324).


- 5.1–5.2: đọc kho ở module chủ (`inventory`), không ghi.
- 5.3: màn đọc qua API; `ListWarehouses` đọc bảng của chính module.
- 5.4–5.6: không áp dụng (chỉ đọc).
- 5.7: `tenancy.WithTenant` qua `inventoryRead`; bảng `inventory.warehouses` có RLS.
- 5.8: module không biết HTTP; handler lọc theo quyền.
- 5.9: không đường ghi (GET, `x-permission: inventory.read`).
- 5.10–5.11: không áp dụng; không thêm thư viện (TanStack Query `useInfiniteQuery` có sẵn).
- 5.12: theo đường đọc mẫu `listSuppliers`; màn theo `features/staff`.
- 6.1–6.4, 6.6–6.8: không áp dụng.
- 6.5: số dư có thể đổi giữa hai trang; khóa sắp `variant_id` không đổi nên không lặp dòng; báo hiệu
  `inventory.balance` làm tải lại danh sách đang mở.
- 6.9: kho ít (vài cái), không giới hạn cần thiết; danh sách tồn có `limit` 50.
- 6.10: không có tiền. 6.11: không vùng rủi ro cao.
- 6.12: không dữ liệu cá nhân.
- 6.13: `listWarehouses` chỉ trả kho có `identity.Can(u, inventory.read, kho)` (mặc định từ chối); màn chỉ hiện kho API
  trả, API `listBalances` vẫn kiểm từng kho (Task 334, test 403).

## Quyết định thay chủ shop

- **Q6 (lượt 2) — Thứ tự dòng trong `SCREENS`:** đặt "Sửa mẫu" (ẩn) ngay sau "Danh sách sản phẩm", rồi tới "Tồn kho",
  như thứ tự trên `main` + một dòng mới; menu không đổi vì dòng sửa mẫu ẩn. Phương án khác: để "Tồn kho" trước — cũng
  đúng, chỉ khác chỗ đọc.

- **Q1 — API liệt kê kho `listWarehouses`** (việc treo chủ shop giao, kế hoạch chưa có): `GET /api/v1/inventory/warehouses`,
  `x-permission: inventory.read`, chỉ trả kho người gọi có `inventory.read` (toàn shop hoặc đúng kho), kho MAIN trước
  rồi theo mã, gồm cả kho đã đóng (kèm `status`). Vì: ô chọn kho của mockup "chỉ có các kho người xem có quyền"; lọc
  ở API nên màn không phải biết luật phạm vi. Phương án khác: trả mọi kho và để màn lọc — lộ tên kho người xem không có
  quyền. Màn Nhân viên (Task 54) muốn dùng để gán phạm vi kho: người gán (Chủ, Quản lý) có `inventory.read` toàn shop
  nên thấy mọi kho; đổi màn đó là task sau (Việc phát hiện thêm).
- **Q2 — Bỏ ba nút dẫn sang màn chưa có:** "Kiểm kho", "Tạo phiếu kho" ở đầu trang và "Tạo phiếu nhập" ở trạng thái
  "kho chưa có hàng" dẫn sang màn của Task 329, 328 chưa làm. Chọn: chưa hiện (nút dẫn tới trang không có là lỗi); câu
  "Tồn kho tăng khi ghi sổ phiếu nhập." vẫn giữ. Task 328, 329 thêm nút khi có màn (ghi vào Việc phát hiện thêm).
  Phương án khác: hiện nút mờ — hành vi không có trong mockup.
- **Q3 — Câu cuối bảng khi đã hết trang:** mockup chỉ vẽ "Đang hiện 50 mã hàng đầu ở Kho chính." kèm "Tải thêm 50".
  Khi không còn trang sau: "Đang hiện cả N mã hàng ở <kho>." và không có nút. Khi tìm không ra dòng nào (mockup không
  vẽ): khung trống "Không có mã hàng nào khớp". Khi người xem không có kho nào: "Bạn chưa được xem kho nào".
- **Q4 — Tìm khi gõ:** gửi `q` sau khi ngừng gõ 300 ms (mockup không nói); chuỗi trống thì không gửi `q`.
- **Q5 — Lưới 4 cột trên điện thoại:** luật của mockup viết `.stk tr`; trong console `styles.css` nạp sau và có
  `.tbl tr` cùng độ ưu tiên nên đè mất. Viết `.tbl.stk …` để ra đúng ảnh mockup (không đổi hình).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bảng tồn bốn ngăn (tồn, lỗi, đang giữ, đang giao) và có thể bán; dòng có thể bán âm ghi rõ bằng chữ ("−2", "Thiếu 2 so với phần giữ", tô đỏ); hết hàng; ngừng bán; tên dài ≤ 2 dòng; không cuộn ngang | `inventory.spec.ts` "bảng tồn bốn ngăn…" (1512, 390); `Balances.test.tsx` "gọi tồn của kho đầu tiên…" | `cd console && CRM_E2E_PORT=5151 npx playwright test` ; `npx vitest run` | `42 passed (20.6s)`; `Tests 132 passed (132)` |
| Lọc "Thiếu so với phần giữ" gửi `onlyShort=true`, có hộp giải thích; không còn dòng thiếu → "Không có mã hàng nào thiếu so với phần giữ" + Tắt lọc | `inventory.spec.ts` "lọc Thiếu so với phần giữ…"; `Balances.test.tsx` "bật lọc…" | như trên | qua |
| Nhân viên phạm vi `w1` không thấy `w2`: API chỉ trả kho có `inventory.read`; màn chỉ có lựa chọn đó và không gọi tồn kho khác | `TestListWarehousesAPI` (phạm vi w1 → chỉ MAIN; toàn shop → MAIN, W2; tenant B; không quyền → 403); `inventory.spec.ts` "nhân viên phạm vi kho chính…"; `Balances.test.tsx` "chỉ có kho API trả…"; API tồn kho khác → 403 ở `TestListBalancesAPI` (Task 334) | `go test -race -run 'TestListWarehousesAPI' ./internal/httpapi/` ; `go test -race -run 'TestListWarehouses$' ./internal/modules/inventory/` | `ok …/internal/httpapi 5.108s`; `ok …/internal/modules/inventory 4.402s` |
| Kho chưa có hàng; tìm theo SKU gửi `q`; tải thêm theo con trỏ; menu "Tồn kho" sáng | `inventory.spec.ts` "kho chưa có hàng; tìm…", "bảng tồn…" | như trên | qua |
| 1512 và 390; ảnh chụp so với mockup đã chốt | `inventory.spec.ts` chụp `test-results/kho-ton-{1512,390}.png`, `kho-ton-thieu-{1512,390}.png` | như trên | So bằng mắt với `docs/mockups/m1/png/kho-ton-{1512,390}.png`: khớp bố cục, cột, màu dòng thiếu, chip, hộp giải thích, lưới 4 cột điện thoại. Khác do quyết định Q2 (không có ba nút sang màn chưa có), Q3 (câu cuối bảng khi đủ trang), menu trái chỉ có màn đã làm, ô người dùng là email (dữ liệu giả của fixture) |
| tsc, eslint, prettier sạch | — | `npx tsc --noEmit && npx eslint . && npx prettier --check src e2e` | thoát 0 |
| Cổng kiểm trên bản đã gộp `main` d4a1baa | `make ci` hai lần trước đó: bước test đỏ chỉ vì hạ tầng chung (lần 1: `DROP DATABASE … timeout`, `statement timeout`, load 27; lần 2: PostgreSQL chung sập vào "recovery mode" SQLSTATE 57P03, và golangci-lint báo "parallel golangci-lint is running" do phiên khác giữ khóa). Chạy lại các bước tách: | `make build vet lint gen-check sqlc-vet oasdiff catalog-check jscpd test-guard gitleaks console-check` ; `go test -race -count=1 -timeout 40m -p 3 ./internal/httpapi/ ./internal/modules/inventory/... ./internal/queries/ ./internal/modules/order/ ./tests/arch/ ./cmd/crm/` ; `CRM_E2E_PORT=5151 npx playwright test` | `EXIT 0`, `0 issues`; `ok httpapi 147.2s`, `ok inventory 298.5s`, `ok queries 199.8s`, `ok order 147.6s`, `ok tests/arch 33.3s`, `ok cmd/crm 1.6s`; `42 passed (19.3s)`. Các gói khác (files, observability, queue, imager…) không đổi trong task; lần chạy riêng trước đó: `ok files 167.9s`, `ok observability 99.7s`, `ok queue 152.4s` |
| Lượt 2: gộp `main` 905e1a1 (Task 324) giữ đủ hai màn; mã sinh khớp `openapi.yaml` đã gộp | toàn bộ vitest, Playwright console (gồm `inventory.spec.ts`, `product-edit.spec.ts`, `shell*.spec.ts`, `staff.spec.ts`), `make ci`, `make gen-check` sau khi stage | `cd console && npx vitest run && npx tsc --noEmit && npx eslint . && npx prettier --check src e2e && CRM_E2E_PORT=5151 npx playwright test` ; `make ci` ; `make gen-check` | `Tests 166 passed (166)`; lint thoát 0; `52 passed (23.4s)`; `make ci` `EXIT 0` (`ok httpapi 269.5s`, `ok inventory 386.7s`, `ok queries 260.0s`, không dòng FAIL, `No vulnerabilities found`, `no leaks found`); `gen-check` thoát 0 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `listWarehouses` (máy chủ, hợp đồng) — kế hoạch Task 51 chỉ có `listBalances`; chủ shop giao trong lời giao
  việc (Q1).
- `e2e/staff.spec.ts` ca "menu: mục Nhân viên…": thêm `await api.catalog()`. Sau khi Task 50 gộp, "/" mở Danh sách sản
  phẩm nên ca này lúc đỏ lúc xanh (đỏ 5/8 lần chạy lặp: yêu cầu `catalog/*` chưa khai). Chỉ khai đường, không nới kiểm.
- Task 334 (API tồn) làm trước trong cùng worktree, commit riêng `2b97bff` (xem `docs/tasks/task-334.md`).

## Việc phát hiện thêm

- Màn Nhân viên (Task 54): ô "Phạm vi" nên dùng `listWarehouses` thay cho danh sách kho rút từ grant đã có
  (`console/src/features/staff/staffRules.ts`, `scopeOptions`/`scopeLabel`), để shop mới gán được kho chưa ai được gán và
  hiện tên kho thay mã.
- Task 328 (phiếu kho), 329 (kiểm kho điện thoại): thêm nút "Tạo phiếu kho", "Kiểm kho", "Tạo phiếu nhập" vào màn Tồn
  kho theo mockup khi có màn đích (Q2).

## Reviewer

Lượt 2: ĐẠT (trên bản gộp `main` 905e1a1 — Task 324, phần đã stage). Không có phát hiện ảnh hưởng tính đúng. Đã kiểm:
phần stage so với HEAD đúng bằng toàn bộ Task 324; so với `main` đúng bằng toàn bộ Task 51; `screens.tsx` có cả
`/kho/ton` và `/san-pham/:id`; `styles.css` có cả `.toggle` và các lớp biểu mẫu của Task 324; mã sinh (router, routes,
SDK) có đủ `listWarehouses`, `listBalances`, `getProductClassification`; `go vet` sạch; số test khớp (52 = 42 + 10 ca
`product-edit`; 166 vitest). Ghi chú không bắt buộc: mục Ảnh hưởng thiếu `console/src/api/index.ts` — đã bổ sung.

Kết luận: ĐẠT (lần 2, trên bản đã gộp `main` d4a1baa, commit gộp dce78a5: mã sinh khớp `openapi.yaml`, không mất
operation nào của `main` — kể cả của Task 320 —, lọc kho theo quyền, màn và tệp task đúng). Lúc commit `main` đã có
thêm Task 324 (7b863a4, 905e1a1) chưa gộp vào nhánh này; nếu Task 324 đổi `openapi.yaml` thì khi gộp cần `make gen`.

Lần 1: ĐẠT (trên bản đã gộp `main` 53a9941). Không có phát hiện ảnh hưởng tính đúng. Đã kiểm: lọc kho
bằng `canAt` (mặc định từ chối) trong `inventoryRead`, RLS cách ly tenant, màn gửi đúng `warehouseId`/`q`/`onlyShort`/
`cursor`, khóa truy vấn theo chủ đề `inventory.balance`, trạng thái trống và lỗi, dấu trừ thật và câu thiếu, không
`as`/`any` trên dữ liệu API; test kiểm thật từng tiêu chí. Ghi chú không bắt buộc (không sửa): (1) `keepPreviousData`
làm bảng thoáng hiện dòng của kho/lọc cũ trong lúc chờ API — tự đúng ngay sau đó; (2) ô chọn không đánh dấu kho đã
đóng — hiện mọi shop chỉ có kho đang hoạt động, để task quản lý kho sau xử lý.
