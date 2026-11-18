# Task 29 — Chứng từ kho, nhà cung cấp

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 29 (M1-29)). Loại việc (quy trình mục 3): A

Vùng rủi ro cao: `internal/modules/inventory` (kế hoạch M1, Global Constraints) — chạy cả bộ test của gói.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn nào của `inventory` (Reserve, Release,
  StartTransit, Consume, …ByItems, Availability, DefaultWarehouse, EnsureDefaultWarehouse, Writer, Actor, Balance,
  InsufficientError giữ nguyên chữ ký, Q-X1). Thêm mới: `Supplier`, `SupplierInput`, `SupplierPatch`, `CreateSupplier`,
  `UpdateSupplier`, `ListSuppliers`, `Document`, `DocumentInput`, `DocumentLine`, `DocumentFilter`, `CreateDocument`,
  `ReplaceLines`, `(Writer).PostDocument`, `CancelDocument`, `GetDocument`, `ListDocuments`, hằng loại/trạng thái phiếu,
  sentinel `ErrSupplierNameTaken`, `ErrSupplierArchived`, `ErrSupplierNotForKind`, `ErrDocumentNotDraft`,
  `ErrVersionConflict`, `ErrNotFound`. Trong `httpapi`: `writeHandlerError` thêm nhánh lỗi inventory (một chỗ, errors.go);
  `errorBody` thêm hai trường tùy chọn `variantId`, `available` (omitempty) — mọi chỗ gọi `writeError` giữ nguyên.
  Không đổi tên ký hiệu nào nên không có chỗ tham chiếu phải sửa.
- Gói phụ thuộc ngược (go list): `internal/modules/inventory` hiện được nhập bởi `cmd/crm` (seed), `invtest`; sau task
  thêm `internal/httpapi` (handler mới). `cmd/crm` chỉ dùng `EnsureDefaultWarehouse` (không đổi).
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm (không phá vỡ): `createStockDocument`, `replaceStockDocumentLines`,
  `postStockDocument`, `cancelStockDocument`, `getStockDocument`, `listStockDocuments`, `listSuppliers`, `createSupplier`,
  `updateSupplier`; schema `Error` thêm hai thuộc tính tùy chọn. Các mã lỗi đã khai sẵn trong `x-error-codes` (M1-25).
- Chỗ dùng trên màn hình: chưa có (màn Kho là Task M1-51); client sinh `console/src/api/**` sinh lại bằng `make gen`.
- Câu sqlc, bảng, pha migration: `migrations/0116_stock_documents.sql` pha mở rộng (chỉ CREATE, GRANT): bảng
  `inventory.suppliers`, `inventory.stock_documents`, `inventory.stock_document_lines`, sequence mã phiếu, trigger chặn
  sửa dòng của phiếu không còn nháp. Câu mới ở `queries/inventory/{suppliers.sql,documents.sql}`; `balances.sql` câu
  `ApplyDelta` thêm một điều kiện cố định `on_hand` (on_hand ≥ q) cho `damage_export`. Thứ tự khóa đúng dòng nhóm 11 có
  sẵn trong `docs/adr/0046-thu-tu-khoa.md` (suppliers → stock_documents → stock_document_lines → stock_balances).
- Test sẽ chạy: `go test -race ./internal/modules/inventory/...` (cả bộ vùng cao, gồm thuộc tính và khóa chéo),
  `go test -race ./internal/httpapi/...`, `./tests/arch/...`, `./cmd/crm/...`; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db`, `tenancy`,
  `events`, `realtime`, `logging`, `textnorm`, `idempotency`); grep `applyDelta`, `EnsureBalance`, `InsufficientError`,
  `compareUUID`, `ScopesFor`, `WarehouseScope`, `catalogWrite`, `RecordAudit`, `logging.Phone`.
- Dùng lại / viết mới vì:
  - `applyDelta` + `EnsureBalance` + `Writer.changed` (Task M1-28) cho mọi thay đổi số dư khi ghi sổ phiếu: không viết câu
    UPDATE số dư mới; chỉ thêm một điều kiện cố định `on_hand` vào câu `ApplyDelta` có sẵn.
  - `InsufficientError`/`ErrInsufficient`, `Availability`, `compareUUID`, `Actor` dùng lại nguyên.
  - `textnorm.Fold` cho cột tìm nhà cung cấp (như `catalog.products.name_search`); `logging.Phone` cho SĐT.
  - HTTP: `WriteTx` (chống trùng), `identity.Can`/`ScopesFor` cho quyền theo kho, `identity.RecordAudit` ở handler
    (inventory không được nhập identity — bảng "Được gọi"). jscpd báo `inventoryWrite` trùng `catalogWrite` (khung
    WriteTx + lỗi nghiệp vụ không lưu): gộp phần khung thành `refusableWrite` (`internal/httpapi/refusal.go`, cùng mục
    đích, cùng lý do thay đổi là luật chống trùng ADR-41); mỗi module giữ hàm mỏng dựng Writer và nhận lỗi của mình.
    `catalogWrite` đổi thân, không đổi hành vi (test catalog HTTP vẫn qua).
  - `pgUUID`/`fromPgUUID` giống helper riêng của `queue/pgstore.go`: giữ riêng (hai dòng, mỗi gói tự đổi kiểu pgtype;
    tạo gói platform chỉ cho việc này không đáng), viết dạng một biểu thức.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: nhà cung cấp, chứng từ kho thuộc `inventory` (04 mục 3, Q33/Q-X6) — bảng trong schema `inventory`.
2. Không ghi bảng module khác: chỉ ghi `inventory.*`; audit qua `identity.RecordAudit` ở handler (public API).
3. Đọc module khác qua view: biến thể đọc qua `catalog.variants_v` (lấy `seller_id`, kiểm có tồn tại); khóa ngoại ghép
   `(tenant_id, seller_id, variant_id) → catalog.variants` như `stock_balances`.
4. Nguyên tử: ghi sổ một phiếu (đổi trạng thái + mọi dòng số dư + sổ + sự kiện) trong một transaction; dòng nào thiếu thì
   cả phiếu lùi.
5. Không nguyên tử đi sau commit: `stock.changed` ghi qua `events.Emit` trong tx (outbox), báo hiệu realtime.
6. Gọi ra ngoài: không áp dụng (không gọi mạng).
7. Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant`/`WriteTx`; ba bảng mới bật RLS (`platform.enable_tenant_rls`),
   `UNIQUE (tenant_id, id)`, khóa ngoại ghép `tenant_id`.
8. Nghiệp vụ không biết HTTP: `inventory` không nhập `net/http`; handler mỏng ở `handlers_inventory.go`.
9. Mọi đường ghi có `IdempotencyKey` và `x-permission`; quyền theo kho kiểm ở handler bằng `identity.Can` với
   `Scope{warehouse, id}`.
10. Valkey: không áp dụng.
11. Thư viện mới: không.
12. Theo module mẫu: catalog (Task 27: sửa có `version`, khóa dòng rồi so), đường ghi Task 12 (`WriteTx`, audit, lỗi ở
    `errors.go`).

Mục 6 (đúng khi chạy đồng thời):

1. Sửa cùng thứ: ghi sổ là `UPDATE … WHERE status = 'draft' AND version = $v` (một người thắng); số dư chỉ đổi bằng câu
   có điều kiện; nhà cung cấp và phiếu nháp có `version`, khóa dòng rồi so.
2. Gửi lại: `Idempotency-Key` ở mọi POST/PUT/PATCH; sổ có unique `(doc_type, doc_id, variant, warehouse, bucket, step)`
   nên một phiếu không áp hai lần kể cả khi lọt qua lớp trên.
3. Thứ tự khóa (ADR-46 nhóm 11): suppliers (FOR SHARE khi tạo phiếu nhập) → stock_documents (dòng phiếu) →
   stock_document_lines → stock_balances theo `(variant_id, warehouse_id)` tăng dần (dòng phiếu đọc theo `variant_id`
   tăng dần, một kho mỗi phiếu). `Reserve` chỉ khóa số dư, không có đường nào khóa số dư trước phiếu.
4. Lỗi tạm: `WriteTx` (RetryTx) chạy lại; hàm không giữ trạng thái giữa các lần.
5. READ COMMITTED + câu có điều kiện; không đọc số dư ra Go rồi ghi lại.
6–8. Việc nền: không áp dụng (không có việc nền mới; sự kiện qua outbox có sẵn).
9. Treo lâu: giới hạn của `db.TxAPI`/`WriteTx`; tối đa 500 dòng mỗi phiếu.
10. Tiền: `unit_cost` là `bigint` đồng, CHECK 0…9007199254740991; không cộng nhân tiền trong task này.
11. Quy luật: test thuộc tính `TestInventoryInvariants` thêm thao tác ghi sổ phiếu ngẫu nhiên các loại; thêm bất biến
    `damaged <= on_hand`.
12. Dữ liệu cá nhân: SĐT nhà cung cấp kiểu `logging.Phone`; audit chỉ ghi `hasPhone`, không ghi số.
13. Quyền: mặc định từ chối; `inventory.documents.create` soạn nháp (theo kho), `inventory.documents.post` ghi sổ (theo
    kho), `inventory.read` đọc (theo kho; danh sách lọc theo kho được giao), `inventory.suppliers.manage` không theo đích.

## Quyết định thay chủ shop

1. **Mã phiếu**: sinh từ một sequence `inventory.stock_document_code_seq` (không khóa dòng, như mã đơn), dạng
   `<tiền tố>-<số 6 chữ số>`: NK nhập, XK xuất, HL hàng lỗi, XL xuất hàng lỗi, DC điều chỉnh. Số chạy chung mọi tenant
   (có thể nhảy số). Khác: bộ đếm theo tenant (thêm bảng và khóa dòng). Đổi sau: chỉ đổi hàm sinh mã.
2. **Phiếu `adjust` cần `inventory.documents.post`** (bảng của kế hoạch): hiểu là soạn nháp phiếu điều chỉnh (tạo và sửa
   dòng) cần cả `inventory.documents.post` trên kho, vì ghi sổ mọi phiếu đã cần quyền này. Khác: chỉ là điều kiện ghi
   sổ (trùng luật chung, không thêm gì).
3. **Hủy phiếu** (`cancelStockDocument`, kế hoạch không ghi quyền): `inventory.documents.create` theo kho, chỉ hủy phiếu
   nháp. Khác: cần `post`.
4. **Chi tiết `insufficient_stock`**: thân lỗi thêm hai trường tùy chọn `variantId`, `available` (kế hoạch: "kèm
   `variantId`, `available`"); mọi lỗi khác giữ đúng `{error, request_id}`; không có vết lỗi. Khác: bỏ chi tiết.
5. **Danh sách phiếu không có `warehouseId`**: trả phiếu của mọi kho người gọi có `inventory.read` (grant `all` → mọi kho).
   Có `warehouseId` mà không có quyền ở kho đó → 403.
6. **Xuất hàng lỗi (`damage_export`)**: `damaged −q` (điều kiện `damaged ≥ q`) rồi `on_hand −q` với điều kiện cố định mới
   `on_hand ≥ q` thêm vào câu `ApplyDelta` (thay vì `none`), để số dư lệch do kiểm kho trả `insufficient_stock` chứ không
   lỗi CHECK 500.
7. **Giới hạn**: tối đa 500 dòng mỗi phiếu, |số lượng| ≤ 1.000.000 mỗi dòng (CHECK ở database), ghi chú ≤ 1000 ký tự.
8. **Tìm nhà cung cấp**: cột `name_search` (bỏ dấu bằng `textnorm.Fold`), tìm chứa chuỗi; trả tối đa 200 dòng theo tên.
9. **Trigger bảo vệ dòng phiếu**: chèn, xóa, sửa dòng của phiếu không còn `draft` bị database từ chối (lớp thứ hai sau
   kiểm trong Go).

10. **Phạm vi Task 332 (M1-29b, nhà cung cấp) làm luôn ở đây.** Kế hoạch được chia lại ngày 18/11 trong lúc task đang
    chạy: API nhà cung cấp (`CreateSupplier`, `UpdateSupplier`, `ListSuppliers`, 3 operation, `TestSupplier_OnImportOnly`)
    sang Task 332. Phiên này được giao rõ "Chứng từ kho, nhà cung cấp" và mã đã viết, test, review ĐẠT với đúng Interface
    của Task 332 (tên hàm, kiểu, operation, quyền, mã lỗi khớp từng chữ), nên giữ trong commit này. Việc còn lại: điều
    phối viên đánh dấu Task 332 đã xong theo commit của Task 29 (không tạo `task-332.md`). Khác: gỡ phần nhà cung cấp
    khỏi commit và để Task 332 làm lại (tốn công, cùng kết quả).

## Bằng chứng

Lệnh chung: `go test -race ./internal/modules/inventory/... ./internal/httpapi/...` (crm-t29, PostgreSQL 5529); lần cuối
trong `make ci` trên bản đã gộp main `b054999`: `ok inventory 133.034s`, `ok httpapi 136.816s`, `ok identity`, `ok tenancy`
(luật schema), `ok tests/arch`; trước khi viết mã: cả hai gói FAIL (undefined: inventory.DocumentLine…;
createSupplier 404).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| KH03: phiếu nhập 10 cái, giá nhập 180000, NCC "Xưởng Hoa" → on_hand 10, 1 dòng sổ import +10; nháp không đổi số dư; ghi sổ lần hai, sửa dòng sau ghi sổ bị chặn (Go và trigger) | `TestPostImport_IncreasesOnHandAndLedger`; `TestInventoryAPI_SupplierAndImport` | `go test -race ./internal/modules/inventory/... ./internal/httpapi/...` | ok |
| C5: trùng tên 409 `supplier_name_taken`; phiếu xuất có NCC 422 `supplier_not_for_kind` + CHECK 23514 khi chèn thẳng; NCC lưu trữ 422 `supplier_archived`; phiếu cũ đọc ra tên; log, audit không chứa SĐT | `TestSupplier_OnImportOnly`; `TestInventoryAPI_SupplierAndImport` | như trên | ok |
| KH10: on_hand 5, held 3 → xuất 3 → 409 `insufficient_stock` kèm `variantId`, `available: 2`, số dư không đổi; xuất 2 được | `TestPostExport_CannotEatHeld`; `TestInventoryAPI_InsufficientBody` | như trên | ok |
| KH08: hàng lỗi 2 → damaged 2, available 8; Reserve 9 → ErrInsufficient; damage_export 2 → on_hand 8, damaged 0 | `TestPostDamage_ReducesAvailable` | như trên | ok |
| Phiếu 3 dòng, dòng giữa thiếu → không dòng nào ghi | `TestPost_AllOrNothing` | như trên | ok |
| 50 lần cùng khóa → áp 1 lần (200 hoặc 409 request_in_progress); hai khóa song song → một 200, một 409 `document_not_draft`; sổ một lần | `TestInventoryAPI_PostIdempotent`; `TestPost_ConcurrentOnce` | như trên | ok |
| Quyền theo kho: post phạm vi w1 ghi sổ phiếu w2 → 403; chỉ `documents.create` → 403 ở post; đọc, sửa dòng, hủy, liệt kê kho khác → 403; danh sách chỉ phiếu kho được giao | `TestInventoryAPI_WarehouseScope` | như trên | ok |
| adjust −4 khi available 3 → thiếu; adjust +2 → on_hand +2 | `TestPostAdjust` | như trên | ok |
| Luật dòng, hủy, tenant khác 404, danh sách phân trang | `TestReplaceLines_Rules`, `TestCancelDocument`, `TestListDocuments` | như trên | ok |
| Thuộc tính (rapid 300 lượt, 1–4 goroutine): thêm ghi sổ phiếu ngẫu nhiên 5 loại → bất biến Task M1-28 vẫn đúng; damaged ≤ on_hand | `TestInventoryInvariants` | như trên | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Gồm cả phạm vi Task 332 (M1-29b) — xem Quyết định 10. Test KH03 dùng API tạo nhà cung cấp thay vì chèn thẳng.

- `PostDocument` là phương thức của `inventory.Writer` (không phải hàm gói như kế hoạch ghi) vì phải phát `stock.changed`
  và báo hiệu như mọi đường đổi tồn của Task M1-28 (`Writer.Reserve`). Các hàm còn lại là hàm gói như kế hoạch.
- Unique của dòng phiếu là `(tenant_id, seller_id, document_id, variant_id)` thay vì `(tenant_id, document_id,
  variant_id)`: luật schema ADR-29 (`tenancy.CheckSchemaRules`) đòi `seller_id` ngay sau `tenant_id` ở bảng có
  `seller_id`; biến thể chỉ thuộc một người bán nên ràng buộc tương đương.
- Thêm `ListDocuments` (kế hoạch có operation `listStockDocuments` nhưng không ghi hàm) và sentinel `ErrNotFound`,
  `ErrVersionConflict` của inventory.

## Việc phát hiện thêm

- Kho `closed` vẫn tạo và ghi sổ phiếu được (kế hoạch không ghi luật); nên chặn khi có màn quản lý kho (mốc sau).
- `make ci` trên bản đã gộp main `b054999`: mọi bước qua trừ jscpd, báo 2 đoạn trùng mới **có sẵn trên main** (không
  phải tệp của task): `internal/platform/metrics/flusher_test.go` ↔ `internal/platform/observability/helpers_test.go`,
  `internal/platform/observability/alerter.go` ↔ `scripts/external-probe/probe.go` (từ Task 19/319). Không sửa ở đây.
- Khi máy tải cao (load ~20, nhiều phiên song song), `make ci` từng đỏ ngẫu nhiên ở `imager` (script giả chưa chạy),
  `commands` (DROP DATABASE quá hạn), `files` (TestPublicVariants); chạy riêng đều qua.
- (reviewer, không chặn) `postLine`: phiếu xuất hàng lỗi, khi `damaged > on_hand` (số dư lệch do kiểm kho) bước trừ
  `damaged` qua nhưng `on_hand ≥ q` không qua, con số `available` trả kèm `insufficient_stock` đọc `damaged` sau khi đã
  trừ trong cùng transaction nên thấp hơn thực tế; transaction vẫn lùi đúng. Sửa khi làm kiểm kho (Task M1-35).
- Mã phiếu dùng sequence chung mọi tenant nên số trên một shop có thể nhảy (xem Quyết định 1).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, trên `git diff --cached` sau khi gộp main `b054999`).

- Không có phát hiện phải sửa. Đã kiểm: chống bán vượt (`condAvailable`, `condOnHand`), ghi sổ một lần (UPDATE có điều
  kiện + unique sổ + `WriteTx`), thứ tự khóa ADR-46 nhóm 11 ở cả ghi sổ, tạo phiếu, thay dòng, trigger; tất cả-hoặc-không
  (`final = false` lùi tx); quyền theo kho trong tx, `warehouse_id` không sửa được; RLS ba bảng; SĐT không vào log, audit;
  lỗi đổi HTTP một chỗ; migration 0116 chạy được nhờ `WithAllowOutofOrder`.
- Ghi chú không bắt buộc: (1) phạm vi Task 332 — đã ghi Quyết định 10, Khác kế hoạch; (2) số `available` trong ca hiếm
  xuất hàng lỗi khi số dư lệch — ghi ở Việc phát hiện thêm; (3) kho `closed` — đã có ở Việc phát hiện thêm.
