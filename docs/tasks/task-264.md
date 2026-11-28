# Task 264 — Hàng hoàn của đơn Nhanh cũ về kho (MO08)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 264 = M7-19). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không sửa ký hiệu có sẵn. Thêm mới ở gói `inventory`:
  `LegacyReturnInput`, `LegacyLine`, `LegacyReturn`, `LegacyReturnFilter`, `ConditionSellable`, `ConditionDamaged`,
  `ErrDuplicateReference`, `DuplicateReferenceError`, `Writer.CreateLegacyReturn`, `GetLegacyReturn`,
  `ListLegacyReturns` (0 chỗ dùng cũ). httpapi: thân ba handler khung `CreateLegacyReturn`, `ListLegacyReturns`,
  `GetLegacyReturn` (Task 260) cùng hàm riêng `legacyFilter`, `legacyPage`, `legacyOut`, `legacyGen`, `legacyAudit`,
  `vnDayStart`; trường `existingId` của `errorBody`, `inventoryDetail` (gọi từ `detailedError`), mã
  `codeLegacyReturnExists` và dòng `ErrDuplicateReference` trong `errors_inventory.go`, dòng bảng chụp `errors_snapshot_test.go`.
- Gói phụ thuộc ngược (go list): `inventory` được nhập bởi `cmd/crm`, `internal/httpapi`, `modules/order`,
  `modules/promotion`, `testkit/shiptest`, `testkit/shoptest`, `wiring/webcache`, `tools/loadkit` — chỉ thêm ký hiệu,
  không gói nào phải sửa; build/vet các gói này ở hook.
- Operation OpenAPI bị chạm, oasdiff: `createLegacyReturn`, `listLegacyReturns`, `getLegacyReturn` (hợp đồng có sẵn
  từ Task 260, không sửa `openapi.yaml`, oasdiff không đổi). Xóa ba dòng khỏi `tests/arch/testdata/m7_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn 13 "Hàng hoàn của đơn cũ" là Task 273); câu lỗi `legacy_return_exists` đã có ở
  `console/src/app/errors.ts`.
- Câu sqlc, bảng, pha migration: migration `0706_inventory_legacy_returns.sql` pha mở rộng (chỉ CREATE TABLE, RLS,
  GRANT SELECT, INSERT cho crm_app); `queries/inventory/legacy_returns.sql` (FindLegacyReturnByNorm, InsertLegacyReturn
  ON CONFLICT DO NOTHING, GetLegacyReturn, ListLegacyReturns, LegacyReturnDocumentLines); `make gen` sinh lại
  `internal/modules/inventory/internal/store` (`legacy_returns.sql.go`) và `models.go` của mọi gói store (sqlc đọc mọi
  migration: thêm kiểu `InventoryLegacyReturn`, không đổi gì khác). Bảng thứ tự khóa ADR-46 nhóm 11 đã có `legacy_returns` (Task 260).
- `documents.go`: `PostDocument` giữ nguyên chữ ký, thân chuyển vào hàm riêng `postDocument(…, moves movesFunc)`;
  `postLine` nhận thêm `movesOf` (2 chỗ gọi, cùng tệp). Hàng hoàn gọi `postDocument` với `legacyMoves` cho phiếu hỏng
  (sửa theo reviewer lần 1).
- Test thử của gói inventory: thêm helper `(*invEnv).secondWarehouse` (env_test.go) và dùng ở
  `TestAgedReservations_Warehouses` (aged_test.go) thay khối SQL lặp (jscpd).
- Test sẽ chạy: `go test -race -run 'TestLegacyReturn_.*|TestListLegacyReturns|TestGetLegacyReturn_NotFound|TestAgedReservations_Warehouses' ./internal/modules/inventory/`, `go test -race -run
  'TestLegacyReturnAPI|TestM7Contract|TestDomainErrorSnapshot' ./internal/httpapi/`; cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cursor, ngày), tìm
  `CreateDocument`, `PostDocument`, `ReplaceLines`, `canAt`, `readableWarehouses`, `idCursor`, `staffNames`,
  `refusableWrite`/`inventoryWrite`, `h.audit`.
- Dùng lại / viết mới vì: dùng lại chứng từ M1 (`CreateDocument`, `ReplaceLines`, `Writer.PostDocument`) cho phiếu nhập
  và phiếu hỏng — không viết đường đổi số dư mới; lớp HTTP dùng `inventoryWrite`, `canAt`, `readableWarehouses`,
  `idCursor`, `staffNames`, `h.audit` có sẵn. Viết mới chỉ chuẩn hóa tham chiếu (một dòng, chỉ module này dùng) và
  bảng nối `legacy_returns`. Dòng của phiếu hàng hoàn không lưu riêng: dựng lại từ dòng phiếu nhập trừ dòng phiếu hỏng
  (một nguồn sự thật).

## Soát thiết kế

- 5.1 Chủ dữ liệu: `inventory.legacy_returns` thuộc module inventory (chứng từ kho), schema `inventory`.
- 5.2, 5.3: không ghi, không đọc bảng module khác; biến thể chỉ qua khóa ngoại có sẵn của dòng phiếu. Tên người tạo đọc
  qua `identity.GetStaff` ở lớp HTTP.
- 5.4 Nguyên tử: phiếu nhập, phiếu hỏng, dòng nối, ghi sổ cả hai phiếu, audit trong **một** transaction của
  `inventoryWrite` (G05); lỗi bất kỳ → lùi hết.
- 5.5, 5.6: sự kiện `stock.changed` do `PostDocument` phát như M1; không gọi ra ngoài, không đọc Nhanh.
- 5.7: bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`), `UNIQUE (tenant_id, id)`, `UNIQUE (tenant_id,
  reference_norm)`, khóa ngoại ghép `(tenant_id, …)` tới kho và hai phiếu; mọi câu trong `tenancy.WithTenant`.
- 5.8: module không nhập `net/http`, ogen; handler mỏng.
- 5.9: route ghi có `IdempotencyKey`, `x-permission: inventory.legacy_return.create` (theo kho) từ Task 260.
- 5.10, 5.11: không Valkey, không thư viện mới.
- 5.12: theo mẫu `inventory` (Writer, sentinel, test `_test` trên PostgreSQL thật).
- 6.1 Song song: hai yêu cầu khác khóa cùng tham chiếu → `INSERT … ON CONFLICT (tenant_id, reference_norm) DO NOTHING`
  chờ transaction kia; bên sau thấy 0 dòng → đọc id đã có (READ COMMITTED thấy bản đã commit) → `DuplicateReferenceError`,
  transaction lùi (phiếu nháp của nó mất). Kiểm trước không khóa để đỡ tốn số phiếu khi trùng thường.
- 6.2 Gửi lại: `Idempotency-Key` bắt buộc; 50 yêu cầu cùng khóa → 1 phiếu; khóa tự nhiên `reference_norm` unique.
- 6.3 Thứ tự khóa nhóm 11: stock_documents → stock_document_lines (dòng mới) → legacy_returns → stock_balances (PostDocument
  theo variant_id tăng dần; phiếu hỏng khóa lại tập con các dòng số dư đã giữ, cùng thứ tự).
- 6.4: `WriteTx` qua `db.RetryTx`; thân hàm gán lại mọi biến kết quả.
- 6.5: READ COMMITTED (mặc định), dựa vào unique index.
- 6.6–6.9: không việc nền, không gọi ngoài; giới hạn `TxAPI`.
- 6.10: không tiền.
- 6.11: quy luật tồn của M1 giữ nguyên (dùng lại `PostDocument`); thêm ca số dư cụ thể (A tốt, B hỏng).
- 6.12: ghi chú (có thể chứa tên, SĐT khách) chỉ nằm trong `note` của phiếu; audit chỉ có tham chiếu, kho, dòng; test
  đòi audit không chứa ghi chú.
- 6.13 Quyền: `inventory.legacy_return.create` theo kho, kiểm bằng `canAt` với kho của yêu cầu (tạo) hay của phiếu (đọc);
  danh sách không có `warehouseId` chỉ trả kho được giao.

## Bằng chứng

Step 2 (FAIL trước khi có mã): hai tệp test không biên dịch (`undefined: inventory.LegacyReturnInput`, `w.CreateLegacyReturn
undefined`; handler khung trả 501).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| A tốt 2 → on_hand +2, bán được +2; B hỏng 1 → on_hand +1, damaged +1, bán được +0; sổ import trỏ phiếu nhập, damage trỏ phiếu hỏng; hai phiếu posted; ghi chú phiếu "Hàng hoàn đơn cũ …" | `TestLegacyReturn_SellableAndDamaged` | `go test -race -count=1 -run '^(TestLegacyReturn_.*\|TestListLegacyReturns\|TestGetLegacyReturn_NotFound\|TestAgedReservations_Warehouses)$' ./internal/modules/inventory/` | `ok … inventory 8.228s` |
| Số bán được cũ âm (giữ 3, on_hand 1) → hàng hoàn hỏng 2 vẫn nhập: on_hand 3, held 3, damaged 2 (đỏ với mã cũ: "không đủ hàng … cần 2, có thể bán 0") | `TestLegacyReturn_DamagedWhenOversold` | như trên | `ok … inventory 2.549s` (sau gộp main) |
| Cùng biến thể tốt + hỏng gộp đúng; mọi dòng tốt → không phiếu hỏng | `TestLegacyReturn_SameVariantBothConditions` | như trên | ok |
| "vtp 1234 5678" rồi "VTP12345678" (cả tab, khoảng trắng không ngắt) → trùng kèm id cũ, không phiếu nào thêm (một kiện không nhập hai lần) | `TestLegacyReturn_DuplicateReference`, `TestLegacyReturnAPI_CreateDuplicateAudit` (409 `legacy_return_exists` + `existingId`) | như trên; `go test -race -count=1 -run '^(TestLegacyReturnAPI_.*)$' ./internal/httpapi/` | ok; `ok … httpapi 3.598s` |
| Nhiều yêu cầu cùng tham chiếu cùng lúc → 1 thành công | `TestLegacyReturn_ConcurrentSameReference` (6 luồng), `TestLegacyReturnAPI_Concurrent` (2 khóa khác → 201 + 409) | như trên | ok |
| 50 yêu cầu song song cùng khóa → 1 phiếu | `TestLegacyReturnAPI_Concurrent` | như trên | ok |
| qty 0, 1001 → 400; dữ liệu sai khác → ErrInvalidInput / 400 invalid_input; kho lạ → ErrNotFound | `TestLegacyReturn_InvalidInput`, `TestLegacyReturnAPI_InputAndScope` | như trên | ok |
| Không quyền ở kho đó → 403 (tạo, đọc, lọc); danh sách chỉ kho được giao | `TestLegacyReturnAPI_InputAndScope` | như trên | ok |
| Audit `inventory.legacy_return` có tham chiếu, 2 dòng; audit và log không chứa ghi chú | `TestLegacyReturnAPI_CreateDuplicateAudit` | như trên | ok |
| Danh sách: mới nhất trước, lọc kho, khoảng giờ/ngày VN, con trỏ; 404 phiếu lạ | `TestListLegacyReturns`, `TestGetLegacyReturn_NotFound`, `TestLegacyReturnAPI_InputAndScope` | như trên | ok |
| Ba operation không còn 501; mã lỗi mới trong bảng chụp | `TestM7Contract_StatusCodes`, `TestDomainErrorSnapshot` | `go test -race -count=1 -run '^(…\|TestM7Contract_StatusCodes\|TestDomainErrorSnapshot)$' ./internal/httpapi/` | `ok … httpapi 3.545s` |
| Cổng tĩnh + test gói bị ảnh hưởng | ci-task | ci-task.sh (chạy từ xa) | `CI-TASK XANH (từ xa trên github, so với điểm tách 48356ba9)`, run 46128983513 (sau gộp main và sửa theo reviewer): static success, test (1)–(6) success |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký `func (w Writer) CreateLegacyReturn(ctx, tx, in LegacyReturnInput, a Actor) (LegacyReturn, error)` thay cho
  `CreateLegacyReturn(ctx, tx, in, actor AuthUser, now) (id, documentID, err)`: inventory không được nhập identity
  (luật gói, audit ghi ở httpapi như mọi chứng từ kho); `PostDocument` là phương thức của `Writer`; giờ tạo lấy `now()`
  của database như phiếu kho M1. Audit `inventory.legacy_return` ghi ở handler trong cùng transaction.
- Phiếu hỏng của hàng hoàn chuyển damaged +q điều kiện `none` (kế hoạch ghi "`available >= qty` đúng vì vừa nhập"):
  khi tồn cũ đang giữ nhiều hơn on_hand thì `available` sai dù hàng vừa nhập; số hỏng ≤ số vừa nhập cùng tx (theo reviewer lần 1).
- `qty` ngoài 1–1000 → 400 (hợp đồng Task 260 ghi "→ 400", ogen kiểm schema) thay cho 422 trong Step 1 kế hoạch.

## Quyết định thay chủ shop

- Một phiếu hàng hoàn không được có hai dòng cùng biến thể cùng tình trạng (→ 400 invalid_input); cùng biến thể khác
  tình trạng (2 tốt + 1 hỏng) được, gộp thành một dòng nhập 3 và một dòng hỏng 1. Vì: dòng phiếu M1 một biến thể một
  dòng; dựng lại dòng không mất thông tin. Phương án khác: tự gộp dòng trùng (dễ che lỗi gõ hai lần).
- Tham chiếu sau chuẩn hóa phải còn ≥ 3 ký tự (vd "a b" bị từ chối 400); lưu `reference` đã bỏ khoảng trắng hai đầu.
- Ghi chú của phiếu: "Hàng hoàn đơn cũ <tham chiếu>" và thêm " — <ghi chú>" khi có.
- Danh sách lọc theo ngày giờ Việt Nam (UTC+7, `from` 00:00 tới hết ngày `to`), mới nhất trước, con trỏ là id dòng
  cuối; `from` sau `to` → 400 invalid_input.

## Việc phát hiện thêm

- `reporting.TestDashboard_ExplainNoSeqScan` đỏ một lần trên GitHub (run 46111216398, "kế hoạch có Seq Scan" ở nhánh Hash
  Join), xanh trên máy và ở lượt sau (run 46113002102). Task này không chạm truy vấn dashboard; có vẻ kế hoạch truy
  vấn dao động theo thống kê — nên xem lại ở task của reporting.

## Reviewer

Kết luận lần 1 (28/11): CẦN SỬA.
- Phiếu hỏng ghi sổ bằng KindDamage (condAvailable, xét toàn bộ số dư): khi biến thể đã có số bán được âm (held > on_hand
  sau kiểm kho thiếu), nhập hàng hoàn có dòng hỏng bị từ chối 409 insufficient_stock. Cần: bước chuyển hỏng của hàng hoàn
  không phụ thuộc phần giữ của tồn cũ (vd điều kiện on_hand − in_transit − damaged ≥ q, hoặc không điều kiện vì qty hỏng ≤ qty
  vừa nhập cùng tx) + test có held > on_hand trước khi nhập.
- Ghi chú không bắt buộc: ca "hôm nay" của TestLegacyReturnAPI_InputAndScope so giờ máy test với now() của database.
Đã sửa (phiên 2, 28/11):
- Phiếu hỏng của hàng hoàn ghi sổ bằng `legacyMoves`: damaged +q điều kiện `none` (như `ReceiveCustomerReturn`): số
  hỏng ≤ số vừa nhập của biến thể trong cùng tx nên không xét phần giữ của tồn cũ. Phiếu hỏng M1 thường vẫn `available`.
  Test mới `TestLegacyReturn_DamagedWhenOversold` (đỏ trước khi sửa, xanh sau).
- Ca ngày của `TestLegacyReturnAPI_InputAndScope` lấy ngày từ `createdAt` do database ghi (một đồng hồ).
- Gộp `main` (fast-forward tới 48356ba9, Task 138), `make gen`, áp lại dòng bảng chụp lỗi.

Kết luận lần 2 (28/11, sau gộp main): **ĐẠT**. Hai điểm lần 1 đã sửa đúng (`PostDocument` giữ `condAvailable` cho phiếu hỏng
M1; hàng hoàn dùng `legacyMoves` điều kiện none, an toàn vì số hỏng ≤ số vừa nhập và dòng số dư đã khóa từ bước nhập;
test API lấy ngày từ `createdAt`). Không phát hiện phải sửa. Ghi chú không bắt buộc: `TestListLegacyReturns` ca
past/future so `time.Now()` ± 1 giờ với `created_at` của database (biên rộng, để nguyên); đã thêm dòng "Khác kế hoạch"
về điều kiện phiếu hỏng.
