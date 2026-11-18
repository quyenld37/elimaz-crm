# Task 412 — Màn hình khớp quyền máy chủ (M1-r7)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù của review độc lập M1, Task 404;
dòng 412 của `danh-so-task-toan-cuc.md`). Phạm vi: phát hiện E-01, E-02, E-06, E-07, E-08 trong
`docs/review-doc-lap-m1.md`. Loại việc (quy trình mục 3): B (màn vẽ khác hành vi máy chủ; test đỏ trước).

Không đổi hành vi máy chủ, không đổi `openapi.yaml`; chỉ sửa `console/` (màn, test vitest, mock e2e).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (không chạm Go).
- Gói phụ thuộc ngược (go list): không áp dụng (không chạm Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn gọi thêm (đều đã có): `listWarehouses`
  (`inventory.read`) ở Danh sách sản phẩm, chỉ khi người xem có `inventory.read`; `listPermissions`
  (`identity.roles.manage`) ở Sửa quyền nhân viên, chỉ khi người sửa có `identity.roles.manage`.
- Chỗ dùng trên màn hình:
  - E-01 `console/src/screens/inventory/DocumentEdit.tsx` (`useGrants` → quyền theo kho của phiếu; danh sách kho của
    "Phiếu mới" lọc theo `inventory.documents.create` ở kho đó, phiếu điều chỉnh thêm `inventory.documents.post` như
    `CreateStockDocument`/`ReplaceStockDocumentLines`), `DocumentList.tsx` (nút "Tạo phiếu": có quyền soạn ở ít nhất
    một kho đang dùng), `Balances.tsx` (nút đầu trang như DocumentList; nút "Tạo phiếu nhập" của kho trống theo kho
    đang xem). Dùng lại `canAt` của `stocktakeRules.ts` (3 chỗ dùng hiện có: Stocktake.tsx ×3, test).
  - E-02 `console/src/app/nav.ts` (`Screen.alsoRequires?`, hàm mới `canSee`; `allowedScreens` dùng nó),
    `router.tsx:59` (route dùng `canSee` thay `hasPermission(…, s.permission)`), `loadScreens.ts` (kiểm trường mới),
    `screens.test.tsx` (khóa trong `alsoRequires` phải có trong `docs/quyen.md`), `screens/inventory/route.tsx`
    (Kiểm kho: `alsoRequires: ["inventory.read", "catalog.read"]`), `screens/imports/ImportPage.tsx` (thiếu
    `files.upload` → câu báo thay nút/ô chọn tệp). `hasPermission` giữ nguyên chữ ký (8 chỗ dùng không đổi);
    `allowedScreens`, `navSections` giữ chữ ký (Layout.tsx, router.tsx dùng).
  - E-06 `console/src/screens/staff/staffRules.ts` `canGrant`, `canGrantSomewhere` thêm tham số loại phạm vi của quyền
    (chỗ gọi: `StaffGrants.tsx:116,177,277,297`, `staffRules.test.ts`); `staffData.tsx` `useStaffData` đọc thêm
    `listPermissions` khi được phép.
  - E-07 `console/src/screens/products/productRules.ts` `warehouseOptions` đổi tham số từ `Grant[]` sang
    `Warehouse[]` (chỗ gọi: `ProductList.tsx:55`, `productRules.test.ts`); `ProductList.tsx` đọc `listWarehouses`.
  - E-08 `console/e2e/imports.spec.ts` (bảy lỗi, lỗi áp dụng lấy nguyên văn đầu ra máy chủ),
    `console/src/screens/imports/importRules.ts` `columnText` (bỏ dấu `*` cuối tiêu đề cột bắt buộc, như mockup).
  - Mock e2e: `console/e2e/fixtures.ts` (`api.catalog()` khai thêm `listWarehouses` rỗng vì Danh sách sản phẩm của
    người có `inventory.read` gọi nó), `products.spec.ts` (`serveProducts` khai kho; ca Chủ thấy cột), `staff.spec.ts`
    (khai `listPermissions`; ca quyền `none`), `imports.spec.ts` (E-02, E-08).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/inventory src/screens/imports src/screens/staff
  src/screens/products src/app`, `npx tsc --noEmit`, `npx eslint .`; e2e Playwright của các màn chạm:
  `imports.spec.ts`, `products.spec.ts`, `staff.spec.ts`, `stock-documents.spec.ts`, `inventory.spec.ts`,
  `stocktake.phone.spec.ts`, `shell.spec.ts`; `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared`
  (chỉ có `hasPermission` — "có quyền ở bất kỳ phạm vi"); grep `canAt|scopeType|listWarehouses|listPermissions` trong
  `console/src`: `canAt` (stocktakeRules.ts, luật "theo đích: warehouse"), `canChange` (roleRules.ts, đã xử lý loại
  `none`), `useRoleData` (roleData.ts, đọc `listPermissions`), `WarehouseSelect`, truy vấn `inventory.warehouses`
  (Balances, DocumentEdit, DocumentList, Stocktake dùng chung khóa).
- Dùng lại / viết mới vì:
  - Dùng lại `canAt` cho màn phiếu, Tồn kho (không viết hàm thứ hai). Không dời nó sang `shared/`: mọi chỗ dùng đều
    trong thư mục `inventory`.
  - Dùng lại khóa truy vấn `queryKey("inventory.warehouses")` ở Danh sách sản phẩm (một lần gọi chung với màn Kho).
  - Dùng lại khóa `queryKey("identity.permissions")` của `roleData.ts` cho `listPermissions` ở màn Nhân viên.
  - `canGrant` không gộp với `canChange` (roleRules): khác câu hỏi (gán vai trò ở một phạm vi so với sửa quyền của vai
    trò, luôn ở toàn shop) và khác lý do đổi; giữ riêng, cùng ba nhánh của `identity.Can`.
  - Viết mới `canSee(grants, screen)` trong `nav.ts`: chưa có hàm kiểm nhiều quyền của một màn.

## Soát thiết kế

- 5.1–5.8, 5.10: không áp dụng — không đổi dữ liệu, module, transaction, máy chủ; chỉ màn hình.
- 5.9 Đường ghi: không thêm đường ghi; màn vẫn ghi qua `useWriteAction` có khóa chống trùng.
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: luật hiển thị trong `*Rules.ts` có test đơn vị; màn theo quy ước tự đăng ký (`loadScreens.ts`).
- 6.1–6.12: không áp dụng — không chạm database, hàng đợi, tiền, dữ liệu cá nhân.
- 6.13 Quyền: máy chủ vẫn là nơi quyết (mặc định từ chối, kiểm ở API). Màn chỉ ẩn, làm mờ đúng những gì máy chủ sẽ
  từ chối và không chặn những gì máy chủ cho: `canAt` = nhánh "theo đích" của `identity.Can`; `canGrant` = đủ ba
  nhánh (`all`; `shop` chỉ toàn shop; `none` grant bất kỳ; theo đích đúng phạm vi). Không biết loại phạm vi (người
  sửa không có `identity.roles.manage`) → giữ luật chặt cũ (chỉ làm mờ thừa, không mở thừa).

## Quyết định thay chủ shop

1. **E-06 — nguồn loại phạm vi của quyền.** Màn Nhân viên cần biết quyền nào là `none`. Chọn: đọc `listPermissions`
   (có `scopeType`, nguồn thật của máy chủ) khi người sửa có `identity.roles.manage` (Chủ luôn có); không có thì giữ
   luật chặt cũ. Vì sao: không đổi API, không chép bảng quyền lần thứ ba vào màn. Phương án khác: (a) thêm loại phạm vi
   vào câu trả lời `listRoles` (đổi API, ngoài phạm vi "chỉ console" — ghi ở Việc phát hiện thêm); (b) chép bảng loại
   phạm vi vào console và test so với `docs/quyen.md`.
2. **E-08 — dấu `*` của tên cột.** Máy chủ trả tiêu đề cột của tệp mẫu (`"Tên mẫu*"`); mockup đã duyệt hiện "Tên mẫu".
   Chọn: ô "Cột" bỏ dấu `*` ở cuối (chỉ hiển thị; câu lỗi giữ nguyên văn máy chủ). Dễ đảo: một dòng trong `columnText`.
3. **E-07 — kho nào trong ô "Kho tính có thể bán".** Chọn: kho `listWarehouses` trả (máy chủ đã lọc theo
   `inventory.read`) và đang dùng (`status = active`), như Phiếu mới, Kiểm kho; tên kho thay cho "Kho <mã>". Người
   không có `inventory.read` không gọi `listWarehouses` (sẽ 403) và không có cột, như trước. Danh sách sản phẩm chờ
   danh sách kho tải xong mới gọi `listProducts` để không gọi hai lần (không kho rồi có kho). `listWarehouses` lỗi →
   danh sách sản phẩm vẫn hiện, không có ô kho và cột (như người không có kho; không thêm trạng thái lỗi mới chưa có
   mockup — cột chỉ là phần phụ của màn này, màn Tồn kho mới là nơi báo lỗi kho).
4. **E-01 — phiếu điều chỉnh.** Máy chủ đòi thêm `inventory.documents.post` khi tạo và thay dòng phiếu điều chỉnh
   (`handlers_inventory.go` `CreateStockDocument`, `ReplaceStockDocumentLines`). Màn làm theo: Phiếu mới loại Điều
   chỉnh chỉ liệt kê kho có đủ hai quyền (không kho nào → câu báo); phiếu điều chỉnh nháp chỉ sửa được khi có đủ hai
   quyền ở kho đó. Hủy phiếu chỉ cần `inventory.documents.create` nhưng nằm chung thanh lưu; trường hợp "có soạn, không
   có ghi sổ, mở phiếu điều chỉnh nháp người khác tạo" màn không cho hủy (chặt hơn máy chủ, không thêm trạng thái mới
   chưa có mockup) — ghi ở Việc phát hiện thêm.
5. **E-01 — nút "Tạo phiếu"/"Tạo phiếu kho" đầu trang.** Hiện khi có quyền soạn ở ít nhất một kho đang dùng mà
   `listWarehouses` trả (không theo kho đang xem: người được soạn ở kho A vẫn tạo được phiếu khi đang xem kho B). Nút
   "Tạo phiếu nhập" trong ô kho trống của Tồn kho theo đúng kho đang xem.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| E-01 Phiếu mới chỉ liệt kê kho có quyền soạn ở kho đó; toàn shop thấy mọi kho | `DocumentEdit.test.tsx` "phiếu mới chỉ cho chọn kho có quyền soạn ở kho đó", "quyền soạn toàn shop: chọn được mọi kho đang dùng" | `cd console && npx vitest run src/screens/inventory` | đỏ trước sửa (5 ca E-01 hỏng); sau: `Tests 105 passed (105)` |
| E-01 Phiếu điều chỉnh cần cả ghi sổ ở kho (tạo, thay dòng) | "phiếu điều chỉnh mới cần cả quyền ghi sổ ở kho…", "phiếu điều chỉnh nháp: có soạn mà không có ghi sổ ở kho → chỉ xem…" | như trên | qua |
| E-01 Phiếu nháp: sửa, ghi sổ theo kho của phiếu | "phiếu nháp ở kho phụ: … → chỉ xem, không nút", "phiếu nháp ở kho chính: soạn toàn shop, ghi sổ chỉ ở kho phụ → Ghi sổ mờ kèm lý do" | như trên | qua |
| E-01 Nút Tạo phiếu (Phiếu kho, Tồn kho) theo kho | `DocumentList.test.tsx` "quyền soạn ở một kho thấy được → có Tạo phiếu; chỉ ở kho không thấy (hay ngừng dùng) → không"; `Balances.test.tsx` "quyền soạn chỉ ở kho chính: có Tạo phiếu kho, kho phụ trống không có Tạo phiếu nhập" | như trên | qua |
| E-02 Route Kiểm kho đòi thêm `inventory.read`, `catalog.read`; menu, route theo `canSee`; quy ước kiểm khóa | `screens.test.tsx` mục "quyền đi kèm của màn (alsoRequires)" (2 ca), "quyền đi kèm (alsoRequires) không có trong bảng quyền → lỗi như quyền chính", 2 ca `alsoRequires` của `loadScreens`; `inventory/route.test.tsx` "Kiểm kho đòi thêm inventory.read và catalog.read; màn khác không đòi thêm" | `npx vitest run src/app src/screens/inventory/route.test.tsx src/shell` | đỏ trước sửa (5 ca); sau: `Tests 107 passed (107)` |
| E-02 Nhập Excel thiếu `files.upload`: câu báo thay ô chọn tệp, không ghi gì | `e2e/imports.spec.ts` "có catalog.import mà thiếu files.upload: tải mẫu được, không có ô chọn tệp, câu báo cần quyền" (1512, 390) | `CRM_E2E_PORT=5712 npx playwright test imports.spec.ts products.spec.ts staff.spec.ts …` | qua (`95 passed`) |
| E-06 `canGrant` theo ba nhánh của `identity.Can`; không biết loại → chặt | `staffRules.test.ts` mục "canGrant theo loại phạm vi của quyền (listPermissions)" (4 ca) + 4 ca cũ chạy với `UNKNOWN` | `npx vitest run src/screens/staff` | đỏ trước sửa (`permissionKinds is not a function`); sau: `Tests 16 passed (16)` |
| E-06 Màn Sửa quyền dùng `listPermissions`: vai trò chỉ quyền `none` gán được | `e2e/staff.spec.ts` "vai trò chỉ có quyền loại none: người sửa có quyền đó ở một kho vẫn gán được ở Toàn shop" | playwright như trên | qua |
| E-07 Cột "Có thể bán" theo `listWarehouses` (kể cả quyền toàn shop), tên kho, bỏ kho đóng, không gọi thừa | `ProductList.test.tsx` "inventory.read toàn shop → ô chọn các kho đang dùng API trả (tên kho), gửi warehouseId kho đầu một lần", "có inventory.read nhưng API trả 0 kho…", "không có inventory.read → không gọi listWarehouses…"; `productRules.test.ts` "warehouseOptions"; `e2e/products.spec.ts` "inventory.read toàn shop (Chủ) → ô chọn kho có tên kho, cột Có thể bán, gửi warehouseId" | `npx vitest run src/screens/products`; playwright | đỏ trước sửa (3 ca); sau: `Tests 139 passed (139)`; e2e qua |
| E-08 Mock bảy lỗi và lỗi áp dụng chép nguyên văn máy chủ; ô Cột bỏ `*` | `e2e/imports.spec.ts` "tệp có 7 lỗi: bảng lỗi đúng dòng, cột, câu tiếng Việt…", "áp dụng thất bại vì dữ liệu vừa đổi…"; `importRules.test.ts` "cột bắt buộc: máy chủ trả tiêu đề tệp mẫu có * ở cuối…" | playwright; `npx vitest run src/screens/imports` | qua. Nguồn bảy lỗi: chạy `importer.Validate` trên `loi-tung-dong.xlsx` (test tạm, đã xóa), khớp `TestValidate_SevenSeededErrors`; lỗi áp dụng: câu của `importer.Recheck` |
| Không vỡ chỗ khác | toàn bộ vitest; toàn bộ e2e console | `npx vitest run`; `CRM_E2E_PORT=5712 npx playwright test` | `Tests 436 passed (436)`; `167 passed (1.0m)` |
| Cổng tĩnh | ci-task.sh | lần 1 đỏ: jscpd (bản trùng mới trong ProductList.test.tsx), prettier `screens.test.tsx`; sửa rồi `make jscpd`, `make console-check` | `Found 0 clones`; console-check (tsc, eslint, prettier, build) xanh; build, vet, lint, gen-check, sqlc-vet, catalog-check, gitleaks, oasdiff, test-guard, kiến trúc xanh ở lần 1 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quy ước màn tự đăng ký có thêm trường tùy chọn `alsoRequires` (E-02: route Kiểm kho cần nhiều quyền; trước đây
  một màn chỉ khai được một quyền). Không màn nào khác đổi.
- Thêm luật phiếu điều chỉnh (quyết định 4) vì cùng loại lệch "màn hiện nút máy chủ trả 403" như E-01.

## Việc phát hiện thêm

- Đề xuất (mốc sau): `listRoles` trả kèm loại phạm vi của từng quyền (hay cho `identity.users.manage` đọc
  `listPermissions`) để màn Nhân viên áp luật `none` cả khi người sửa không có `identity.roles.manage`.
- Hủy phiếu điều chỉnh nháp của người có soạn mà không có ghi sổ: máy chủ cho, màn không có nút (xem quyết định 4).
- Câu lỗi nhập Excel của máy chủ ("Thiếu Tên mẫu*") khác câu trong mockup ("Thiếu tên mẫu (cột bắt buộc)"); màn hiện
  nguyên văn máy chủ. Nếu muốn câu như mockup thì sửa ở `importer` (máy chủ), không ở màn.
- Màn Phiếu kho gọi `searchVariants` (`catalog.read`); vai trò tự định nghĩa có `inventory.documents.create` mà thiếu
  `catalog.read` sẽ không thêm được dòng (vai trò mẫu có đủ). Không sửa ở đây (ngoài E-01…E-08).

## Reviewer

Kết luận: ĐẠT (lượt 1, 19/11/2026). Không có phát hiện phải sửa. Reviewer đối chiếu `canAt`, `canEditAt` với
`handlers_inventory.go`; `canGrant` với ba nhánh của `identity.Can`/`hasAllPermissions`; API gọi thêm đều kiểm quyền
trước khi gọi (`listPermissions`, `listWarehouses`, `files.upload`); bảy lỗi và lỗi áp dụng của mock khớp nguồn Go.
Ghi chú không bắt buộc: (1) hai câu mới `TEXT_NO_UPLOAD_RIGHT`, `TEXT_NO_ADJUST_WAREHOUSE` chưa có trong mockup — nêu
trong báo cáo để chủ shop chốt câu chữ; (2) màn dựa vào `identity.me` khung đã tải; (3) phiếu nhập/xuất mới mà không
kho nào soạn được: nút Lưu nháp mờ, không câu giải thích (không có lời gọi 403). Không sửa thêm (chỉ sửa phát hiện về
tính đúng).
