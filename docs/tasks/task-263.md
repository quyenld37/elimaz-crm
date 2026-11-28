# Task 263 — Đếm nhiều người cùng lúc và nhập số đếm bằng Excel

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 263 = M7-18). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm ký hiệu mới ở `inventory` (`IncrementLine`, `CodeCandidate`, `AmbiguousCodeError`, `ErrUnknownCode`, `ErrAmbiguousCode`, `ErrNegativeCount`, `ErrImportAlreadyApplied`, `CountImportPurposeKey`, `CountImportPurpose`, `CountImportFile`, `PrepareCountImport`, `CreateCountImport`, `GetCountImport`, `CountImport`, `CountImportRowError`, `CountImportKind`). Đổi khung của Task 260: `CountImportDeps` bỏ trường `DB`, `Files`, `Queue` (việc nền chạy trong tx của hàng đợi, không đọc kho tệp) — 1 chỗ dùng `cmd/crm/wire_m7.go:25`; `RegisterCountImportPurpose` 1 chỗ gọi `cmd/crm/main.go:661` (chữ ký giữ nguyên); `CountImportJobs` 1 chỗ `wire_m7.go:25` (chữ ký giữ nguyên). `RecordCount`, `PostOpening`, `ApplyStocktake` không đổi.
- Gói phụ thuộc ngược (go list): `inventory` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/promotion`, `internal/testkit/shiptest`, `internal/testkit/shoptest`, `internal/wiring/webcache`, `tools/loadkit`, `inventory/invtest` (chỉ thêm ký hiệu nên chỉ `cmd/crm`, `httpapi` thật sự đổi); test của `internal/platform/files` và `internal/modules/conversation` thêm mục đích mới vào danh sách mục đích có dòng trong bảng.
- Operation OpenAPI bị chạm, oasdiff: `incrementStocktakeLine`, `createStocktakeCountImport`, `getStocktakeCountImport` — hợp đồng đã có từ Task 260, không sửa `openapi.yaml` (oasdiff không đổi); chỉ thay thân handler 501 và xóa 3 dòng khỏi `tests/arch/testdata/m7_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn kiểm kho mở sổ là Task 273); mã lỗi đã có câu tiếng Việt ở `console/src/app/errors.ts` (Task 260).
- Câu sqlc, bảng, pha migration: migration mới `0705_inventory_count_imports.sql` pha mở rộng (bảng mới `inventory.count_imports` có RLS, khóa ngoại ghép tenant; một dòng `platform.file_purposes` `inventory_count_import`/`private`). Câu mới ở `queries/inventory/count.sql`: khóa chia sẻ phiên, tra mã quét, cộng/trừ dòng đếm, upsert lô dòng đếm, ghi/đọc/kết thúc bản nhập. Bảng chạm: `inventory.stocktakes` (FOR SHARE), `inventory.stocktake_lines` (INSERT … ON CONFLICT ON CONSTRAINT stocktake_lines_variant_key, UPDATE), `inventory.count_imports`, đọc `catalog.variants_v`, `catalog.products_v`, `inventory.stock_balances` (qua `Availability`).
- Đổi lỗi sang HTTP: `internal/httpapi/errors.go` thêm trường `Candidates` vào thân lỗi; `errors_inventory.go` thêm 4 dòng `errorMapping` (`ambiguous_code`, `unknown_code`, `negative_count` 422; `import_already_applied` 409), kiểu `ambiguousCandidateBody` và nhánh `ambiguous_code` của `inventoryDetail`; mã đã có trong `x-error-codes` và `console/src/app/errors.ts` từ Task 260.
- Mã sinh: `make gen` sinh `internal/modules/inventory/internal/store/count.sql.go` và cập nhật `models.go` của mọi gói store (thêm `InventoryCountImport`; sau khi gộp main có cả `internal/queries/flashverify/internal/store/models.go`).
- Test helper: `opening_count_test.go` thêm `countedOpening`, `duplicateSKU`, `checkAmbiguous` (dùng ở `count_import_test.go`); `conversation/filepurposes_test.go` dựng danh sách mục đích bằng `slices.Concat` (tránh mã lặp với `files/purpose_test.go`, jscpd).
- Test sẽ chạy: `go test -race -run` các test mới của `internal/modules/inventory` (opening_count_test.go, count_import_test.go), `internal/httpapi` (handlers_inventory_count_test.go, errors_snapshot_test.go), `internal/platform/files` (`TestPurposes_GoMatchesTable`), `internal/modules/conversation` (test mục đích); cả gói chạy ở ci-task (inventory là vùng rủi ro cao → cả bộ inventory).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `xlsx` (`xlsx.Read`, `Limits`, `CellError`, `CodeFormulaNotAllowed`), `files` (`ReadContent`, `RegisterPurpose`, `Purpose`), `queue` (`Enqueue`, `Handler`, `TerminalHook`, `ErrPermanent`); tìm "sha256" (cash `PrepareStatementImport` băm nội dung tệp làm khóa chống nhập trùng), "Import" (customer `importer.go`, catalog `imports_validate.go`); trong inventory: `Availability`, `lockOpen`, `RecordCount`, `actorUUID`.
- Dùng lại / viết mới vì: dùng lại `files.ReadContent` + `xlsx.Read` (đúng như customer import), `Availability` để chụp `expected_on_shelf = on_hand − in_transit` như `RecordCount` (một phép trừ, không tách hàm riêng nên không cần commit cấu trúc), `queue.Enqueue` với DedupeKey = mã bản nhập, ràng buộc `stocktake_lines_variant_key` của M1 làm đích ON CONFLICT, trigger `stocktake_lines_open_only` của M1 là lớp sau. Viết mới: câu upsert cộng dồn (RecordCount ghi đè theo version, không cộng được), tra mã quét SKU/barcode trên mọi người bán (tìm sản phẩm của catalog chỉ trong một người bán, theo tiền tố — không dùng được cho "đúng một biến thể"), bảng bản nhập riêng của inventory (bản nhập của customer/catalog thuộc module đó, khác lý do thay đổi).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: dòng đếm và bản nhập số đếm thuộc `inventory` (schema `inventory`).
- 5.2 Không ghi bảng module khác: chỉ ghi `inventory.*`; tệp qua public API `files`.
- 5.3 Đọc module khác qua view: mã quét tra trên `catalog.variants_v`, `catalog.products_v`.
- 5.4 Nguyên tử: cộng một lần quét = một transaction; áp một bản nhập (mọi dòng hợp lệ + đặt `done`) = một transaction của việc nền.
- 5.5 Không cần nguyên tử đi sau commit: bản nhập chạy bằng việc nền `inventory.count_import` ghi cùng transaction tạo bản nhập (Enqueue).
- 5.6 Gọi ra ngoài: không áp dụng (không gọi dịch vụ ngoài; đọc kho tệp ở handler, ngoài transaction).
- 5.7 Ngữ cảnh shop: bảng mới có `tenant_id`, RLS (`enable_tenant_rls`), unique và khóa ngoại ghép `tenant_id` (stocktakes, files); mọi câu trong `tenancy.WithTenant` (handler) hoặc tx tenant của hàng đợi.
- 5.8 Không biết HTTP: module không nhập `net/http`; handler mỏng ở `handlers_inventory_count.go`.
- 5.9 Chống trùng, quyền: ba operation đã có `x-permission: inventory.stocktake` và `IdempotencyKey` (hai đường ghi) từ Task 260; handler kiểm quyền ở kho của phiên (`canAt`).
- 5.10 PostgreSQL là nguồn sự thật: không dùng Valkey.
- 5.11 Thư viện: không thêm (excelize qua `xlsx` có sẵn).
- 5.12 Module mẫu: lỗi sentinel trong `inventory/errors.go`, đổi HTTP ở `errors_inventory.go`, test `_test` trên PostgreSQL thật (`testkit.OpenStaff`, `t.Parallel()`).
- 6.1 Đồng thời: cộng dồn bằng một câu `INSERT … ON CONFLICT DO UPDATE SET counted = counted + by WHERE counted + by BETWEEN 0 AND 1 000 000` (không đọc ra Go rồi ghi); trừ (`by < 0`) bằng `UPDATE … WHERE counted + by >= 0`. Test 3 người × 100 lần `+1` song song → 300.
- 6.2 Gửi lại: khóa chống trùng của middleware; bản nhập: unique `(tenant_id, stocktake_id, file_sha256, mode)` — `replace` lần hai trả bản cũ, `add` lần hai 409; việc nền DedupeKey = mã bản nhập; đặt `done` có điều kiện `status = 'queued'`.
- 6.3 Thứ tự khóa (ADR-46 nhóm 11): `stocktakes` (FOR SHARE) → `stocktake_lines` (theo variant_id tăng dần trong một câu ORDER BY) → `count_imports`. `PostOpening` khóa FOR UPDATE phiên nên chờ mọi lần cộng đang chạy và ngược lại; test chạy song song cộng với đăng.
- 6.4 Lỗi tạm: đường yêu cầu qua `WriteTx`/RetryTx có sẵn; việc nền dùng lịch thử lại của hàng đợi. Thân hàm gán lại mọi biến kết quả.
- 6.5 Mức cách ly: READ COMMITTED + câu có điều kiện + khóa dòng phiên.
- 6.6 Việc nền lỗi: lỗi tạm → thử lại; payload hỏng → ErrPermanent; hết lượt/dead → `OnTerminal` đặt bản nhập `failed` (không treo `queued`).
- 6.7 Quá tải bên ngoài: không áp dụng (không gọi ra ngoài).
- 6.8 Chết giữa chừng: việc transactional, tx tự lùi, chạy lại từ đầu (bản nhập vẫn `queued`).
- 6.9 Treo lâu: việc nền Timeout 30 s; tệp tối đa 5 MiB/20 000 dòng; một câu upsert theo lô.
- 6.10 Tiền: không áp dụng (số lượng hàng, không tiền).
- 6.11 Quy luật: test rapid — chuỗi cộng/trừ ngẫu nhiên song song, số đếm cuối = tổng các lần được nhận, không bao giờ âm.
- 6.12 Dữ liệu cá nhân: không có; `error_rows` chỉ số dòng + mã lỗi, không nội dung ô; log không ghi nội dung ô (test).
- 6.13 Quyền: `inventory.stocktake` ở kho của phiên; người không có quyền ở kho đó → 403; tenant khác → 404.

## Bằng chứng

Lệnh chung (trên bản đã gộp main, 28/11): `go test -race -count=1 -run '^(TestIncrementLine_.*|TestCountImport_.*)$' ./internal/modules/inventory/` → `ok … 9.737s`; `go test -race -count=1 -run '^(TestCountAPI_.*|TestDomainErrorSnapshot.*|TestM7Contract.*)$' ./internal/httpapi/` → `ok … 3.211s`; `go test -count=1 -run 'TestPurposes_GoMatchesTable|TestFilePurposes_MatchTable' ./internal/platform/files/ ./internal/modules/conversation/` → `ok`; `go test -count=1 -run TestM7 ./tests/arch/` → `ok`. Cả gói ở ci-task (xem dòng cuối).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 3 người × 100 lần `+1` song song → 300 | `TestIncrementLine_ConcurrentCounters` (module); `TestCountAPI_Increment` (3 × 30 qua API, mỗi lần một khóa chống trùng → 90, 91 dòng audit) | lệnh chung | `--- PASS: TestIncrementLine_ConcurrentCounters (2.55s)`, `--- PASS: TestCountAPI_Increment (0.91s)` |
| Quét barcode đúng biến thể; trùng giữa hai người bán → 422 `ambiguous_code` kèm ứng viên | `TestIncrementLine_CodesAndBounds`, `TestCountAPI_Increment` (barcode `BC000001`), `TestCountAPI_AmbiguousBody` (thân lỗi có `candidates`) | lệnh chung | PASS |
| `counted = 2`, `by = -3` → 422 `negative_count`, vẫn 2; `by` ngoài ±1000 hay 0 → 400 | `TestIncrementLine_CodesAndBounds`, `TestCountAPI_Increment` (trừ 93 khi có 92) | lệnh chung | PASS |
| Phiên `applied` → 409 `stocktake_not_open`; `+1` song song với đăng: được đếm trước hoặc 409, không dòng nào đổi sau `applied` | `TestIncrementLine_RacesPost`, `TestCountAPI_Increment` (sau khi đăng) | lệnh chung | `--- PASS: TestIncrementLine_RacesPost (2.45s)` |
| Phiên kiểm thường không cộng dồn | `TestIncrementLine_RegularStocktakeRejected`, `TestCountAPI_Increment` (400) | lệnh chung | PASS |
| Quy luật: chuỗi cộng/trừ ngẫu nhiên → số cuối = tổng các lần được nhận, không âm | `TestIncrementLine_Property` (rapid) | lệnh chung | `[rapid] OK, passed 300 tests` |
| Tệp 6 dòng → replace A = 7, B = 10, lỗi `unknown_code`, `not_integer`, `formula`; replace lần hai → bản cũ, không đổi số; add hai lần → lần hai `import_already_applied` | `TestCountImport_ReplaceAddAndRowErrors`, `TestCountAPI_Import` (202 queued → done, 409 lần hai) | lệnh chung | PASS |
| Bản nhập chạy sau khi phiên đã đăng → `failed`, không đổi dòng; số âm → `negative`; mã trống → `unknown_code`; mã trùng → `ambiguous_code` | `TestCountImport_ClosedStocktakeAndCodes` | lệnh chung | PASS |
| Tệp 3.000 dòng dưới 30 s (ghi thời gian đo) | `TestCountImport_ThreeThousandRows` | lệnh chung | `3.000 dòng: chuẩn bị + áp 546.748333ms` (máy Mac, PostgreSQL chung) |
| Mục đích tệp R5: dòng `inventory_count_import`/`private`; Go khớp bảng; `.xlsx` 6 MiB → quá lớn; `.csv` → sai loại; không phải xlsx / sai tiêu đề → 400 | `TestCountImport_PurposeAndBadFiles`, `TestPurposes_GoMatchesTable`, `TestFilePurposes_MatchTable` | lệnh chung | PASS |
| `error_rows` và log không chứa nội dung ô | `TestCountImport_ReplaceAddAndRowErrors` (dòng thô không có `KHONGCO`, `abc`, `1+1`), `TestCountAPI_Import` (thân và log không có `BIMAT`) | lệnh chung | PASS |
| Quyền ở kho của phiên → 403; tenant khác → 404 | `TestCountAPI_Increment`, `TestCountAPI_Import` | lệnh chung | PASS |
| Mã lỗi mới đổi HTTP đúng | `TestDomainErrorSnapshot`, `TestDomainErrorSnapshot_TableMembership` | lệnh chung | PASS |
| Cổng kiểm của task (cổng tĩnh + toàn bộ test vì bản gộp main đổi go.mod/testkit) | ci-task | ci-task (chạy từ xa) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46163756158`, `CI-TASK XANH (từ xa trên github …)`; static success, test (1)–(6) success. Các lượt trước đỏ ở lint (gocognit, nolint thừa), jscpd (helper test lặp), `TestXlsxRulesProductCode` (SetCellFormula trong test), `TestIncrementLine_RacesPost` (so giờ hai đồng hồ) — đã sửa |
| Ba operation không còn 501 | `TestM7Contract_StatusCodes` (tests/arch, httpapi) với `m7_pending.txt` bỏ 3 dòng | lệnh chung | PASS |

## Test bị xóa hoặc tắt

- Không xóa test có sẵn. Trong test mới `TestIncrementLine_RacesPost` (của task này) bỏ phép so `counted_at > applied_at` trước khi commit: hai mốc lấy từ đồng hồ Go lúc bắt đầu mỗi lệnh, `PostOpening` lấy giờ trước khi chờ khóa nên lần cộng hợp lệ (commit trước khi đăng) có thể mang giờ muộn hơn — đỏ chập chờn ở ci-task lượt 3 (bẫy "một đồng hồ"). Điều kiện "không dòng nào đổi sau khi applied" vẫn được kiểm bằng `counted = on_hand = số lần cộng thành công` và lần cộng sau khi đăng → `ErrStocktakeNotOpen`.

## Khác kế hoạch

- Đọc tệp ở handler, không ở việc nền: kế hoạch ghi việc nền đọc tệp bằng `FileStore.Open`. Làm: `PrepareCountImport` đọc, băm, phân tích tệp ở handler **ngoài** transaction (ADR-46, cùng cách nhập khách của M2), lưu dòng số đã đọc vào cột `pending_rows` (chỉ số dòng, mã SKU/barcode, số; không dữ liệu khách), việc nền chạy transactional theo tenant không cần kho tệp. Lý do: băm SHA-256 phải có trước khi ghi dòng (khóa chống nhập trùng `(stocktake, sha256, mode)`), nên handler đằng nào cũng đọc tệp; tệp hỏng/sai tiêu đề báo 400 ngay. Vì thế `CountImportDeps` bỏ `DB`, `Files`, `Queue` (khung Task 260), thêm cột `pending_rows` vào bảng.
- Số dòng trong `errorRows` là số dòng Excel (tiêu đề là dòng 1), nên tệp 6 dòng của kế hoạch báo `{4 unknown_code}, {5 not_integer}, {6 formula}` thay vì 3, 4, 5: người dùng mở Excel thấy đúng số dòng đó; cùng cách đánh số của `xlsx.Row.Number` và bản nhập khách.
- Chữ ký `IncrementLine(ctx, tx, stocktakeID, code, by int32, a Actor, now) (variantID uuid.UUID, counted int32, err)`: `Actor` của inventory thay `AuthUser` (inventory không nhập identity), `int32` khớp kiểu sinh từ openapi, trả thêm mã biến thể cho thân phản hồi `StocktakeIncrementResult`.
- Phần chụp `expected_on_shelf` dùng `Availability` (một phép trừ `on_hand − in_transit` như `RecordCount`), không tách hàm riêng nên không có commit cấu trúc.
- Cộng dồn chỉ cho phiên mở sổ (`kind = opening`): phiên kiểm thường của M1 áp theo version đã xem trước, cộng dồn giữ khóa chia sẻ không tăng version phiên nên sẽ phá luật đó; phiên thường vẫn đếm bằng `recordStocktakeCount`.
- Gộp main (28/11) trong cùng commit này: giải xung đột `errors.go`, `errors_inventory.go` (giữ `ExistingID`/`legacy_return_exists` của Task 264 cùng phần của 262, 263), `m7_pending.txt` (bỏ dòng các task đã làm), mã sinh `models.go` (sinh lại bằng `make gen`); xóa `secondWarehouse` lặp ở `opening_test.go` (Task 262) vì main đã có bản giống hệt ở `env_test.go`.

## Việc phát hiện thêm

(Từ ghi chú không bắt buộc của reviewer, chưa làm vì ngoài phạm vi hoặc không ảnh hưởng tính đúng.)
- `CreateStocktakeCountImport` đọc và phân tích tệp (≤ 5 MiB) trước khi kiểm quyền ở kho của phiên: người thiếu quyền ở kho đó vẫn làm máy chủ đọc tệp rồi mới nhận 403 (không lộ dữ liệu: tệp phải cùng tenant, đúng mục đích).
- Mỗi lần quét, handler gọi `stocktakeAt` (dùng `GetStocktake`, nạp mọi dòng của phiên): phiên vài nghìn biến thể thì mỗi lần quét đọc cả bảng dòng đếm — đúng nhưng chậm; nên có bản kiểm quyền chỉ đọc đầu phiên.
- `Availability` (`inventory.go`, mã M1) tìm bằng `IndexFunc` trong vòng lặp (O(n²)); bản nhập 20 000 biến thể có thể chậm so với hạn 30 s của việc nền.
- Đường `OnTerminal` (việc hết lượt → bản nhập `failed`) chưa có test riêng.

## Quyết định thay chủ shop

- Đọc tệp ở handler, lưu `pending_rows` (xem Khác kế hoạch). Phương án khác: việc nền đọc tệp (đúng chữ kế hoạch) nhưng phải đọc tệp hai lần (handler để băm, worker để áp) và việc nền phải không transactional. Dễ đảo ngược: cột và việc nền tách riêng.
- Đánh số lỗi theo dòng Excel (xem Khác kế hoạch). Phương án khác: số thứ tự dòng dữ liệu.
- Tệp `add` đã nhập → 409 dù lần đầu còn `queued`/`failed`; muốn nhập lại tệp lỗi thì sửa tệp (băm khác). Phương án khác: cho nhập lại khi lần trước `failed` — để màn hình (Task 273) xin nếu cần.

## Reviewer

Kết luận: ĐẠT (reviewer chạy trên phần đã stage của merge commit gộp main + Task 263, sau ci-task XANH run 46163756158). Không có phát hiện phải sửa. Đã đối chiếu: đủ Interface và test của kế hoạch; bảng bằng chứng trỏ đúng test có thật và kiểm đúng điều tiêu chí nói; giải xung đột giữ đủ phần của Task 262, 264, 263; không test nào bị tắt hay nới ngoài phép so giờ đã ghi lý do; không vi phạm luật dự án. Ghi chú không bắt buộc chép vào "Việc phát hiện thêm"; theo gợi ý đã ghi thêm `errors.go`, `errors_inventory.go` vào mục Ảnh hưởng.

## Gộp vào main

Phiên gộp tuần tự 28/11 (nhánh `task-263` vào `main` sau Task 540). Nhánh đã chứa phần Task 262 (đã gộp ở cbcb88e5) nên phần đó không đổi.

- Xung đột `internal/httpapi/errors.go` (trường `errorBody`): main có `ExistingID` (264) + `Count` (262); nhánh có cả hai cộng `Candidates` (263). Giữ bản nhánh (đủ ba trường).
- Xung đột `internal/httpapi/errors_inventory.go`: hằng mã — giữ đủ `legacy_return_exists`, `opening_exists`, `warehouse_not_empty`, `uncounted_lines` cùng bốn mã mới của 263; `inventoryDetail` — giữ ba nhánh (`AmbiguousCodeError`, `UncountedLinesError`, `DuplicateReferenceError`), mỗi kiểu lỗi riêng nên thứ tự không đổi kết quả. Bảng `errorMapping` tự gộp, giữ thứ tự của nhánh.
- Xung đột `tests/arch/testdata/m7_pending.txt`: main còn `incrementStocktakeLine`, `createStocktakeCountImport`, `getStocktakeCountImport`; nhánh đã làm thật cả ba → bỏ.
- Mã sinh `models.go` tự gộp; `make gen` sau gộp không chênh. Migration `0705` không trùng số.
- Bằng chứng máy: `go build ./...`, `go vet ./internal/httpapi/ ./internal/modules/inventory/... ./cmd/crm/ ./tests/arch/` qua.
- Test từ xa: `merge-test.sh` cây `573f87ea`, gói `cmd/crm, internal/httpapi, internal/modules/inventory/..., internal/modules/conversation, internal/platform/files, tests/arch` (race: httpapi, inventory): MERGE-TEST XANH (github, run 46166951925: static + 6 phần test đều success, 45 phút máy). Không có test chập chờn.
- Reviewer (gộp): ĐẠT — giải xung đột giữ đủ phần Task 262, 263, 264; `errorMapping` đúng thứ tự; `m7_pending.txt` còn 16 dòng khớp 16 stub còn `ErrNotImplemented`; mã sinh, migration 0705, snapshot lỗi, `x-error-codes` khớp. Không có phát hiện phải sửa.
