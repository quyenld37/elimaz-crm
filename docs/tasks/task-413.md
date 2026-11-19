# Task 413 — Catalog: khóa bộ đếm barcode khi áp, danh mục lưu trữ, ký tự > và ;, thứ tự từ khóa rộng (M1-r8)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù review độc lập M1, dòng 413 của `danh-so-task-toan-cuc.md`; phạm vi B-04, B-05, B-06, B-07 trong `docs/review-doc-lap-m1.md`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu nào của gói `catalog` hay `queries` đổi chữ ký. Trong gói nội bộ `catalog/internal/barcode` (chỉ `catalog` nhập): thêm `Reserve(ctx, tx, n) (Range, error)` và kiểu `Range{From, To}` có `Next()`; `formatInternal` giữ nguyên; `NextEAN13` giữ chữ ký, thân gọi `Reserve(…, 1)` (1 chỗ dùng: `variants.go` `freeBarcode`). `Writer` thêm trường không xuất `barcodes` (giá trị rỗng như cũ; mọi chỗ dựng `catalog.Writer{…}` không đổi). Hàm không xuất `insertVariant`, `freeBarcode` thành phương thức của `Writer` (2 chỗ gọi: `products.go` CreateProduct, `variants.go` AddVariant). `applyPayload` (không xuất) thêm trường tùy chọn `barcodes`.
- Hành vi không xuất đổi thêm: `classificationRefs` (`imports.go`) bỏ đường dẫn danh mục của hơn một danh mục khỏi bảng tra → dòng nhập chọn nó báo lỗi cột Danh mục thay vì gán bừa (B-06).
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `internal/modules/catalog/catalogtest`, `internal/modules/order`; `queries` ← `internal/httpapi`. Không chữ ký nào đổi nên chỉ cần chạy lại test của chúng.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Hành vi: `createCategory`/`updateCategory` từ chối tên có `>` và `addAttributeValue` từ chối giá trị có `;` bằng mã đã có `invalid_input` 400 (B-06); `listProducts` với `q` khớp hơn 1.000 mẫu lấy 1.000 mẫu mới sửa nhất (B-07). oasdiff: không chênh.
- Chỗ dùng trên màn hình: không sửa console. Màn danh mục/thuộc tính đã hiện lỗi `invalid_input`; danh sách mẫu không đổi cách gọi.
- Câu sqlc, bảng, pha migration: `queries/catalog/barcode.sql` thay `NextBarcodeNumber` bằng `ReserveBarcodeNumbers` (`INSERT … ON CONFLICT DO UPDATE SET next = next + @n`, bảng `catalog.barcode_counters`, không đổi bảng); `make gen` sinh lại `catalog/internal/store`. Migration mới `0131_catalog_search_products_order.sql`: `CREATE OR REPLACE FUNCTION catalog.search_products` cùng chữ ký, chủ, quyền — chỉ thay thân (pha mở rộng, không lệnh thu hẹp).
- Test sẽ chạy: `go test -race ./internal/modules/catalog/...` (B-04, B-06), `go test -race ./internal/queries/...` (B-07), rồi cổng kiểm `ci-task.sh` (gói đổi + gói phụ thuộc httpapi, order, cmd/crm; đổi migration → toàn bộ test theo luật của ci-task).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (barcode, cấp dải số: không có gì chung; `textnorm.Fold` chỉ cho cột tìm); tìm `NextEAN13`, `barcode_counters`, `search_products`, `CategoryPath`, `validLine`, `splitList`.
- Dùng lại / viết mới vì: B-04 dùng lại bộ đếm `catalog.barcode_counters`; `NextEAN13` thành `Reserve(1)` nên chỉ còn một câu tăng bộ đếm (xóa `NextBarcodeNumber`, không giữ hai cách); vòng tránh trùng `freeBarcode` giữ nguyên. B-06: dùng lại `validLine`, `categoryText`, lỗi `ErrInvalidInput`, chỉ thêm điều kiện ký tự. B-07: dùng lại index `products_list_updated` và ba nhánh khớp của `search_products`, chỉ thêm bước sắp trước khi cắt.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1, 5.2, 5.3: mọi bảng, hàm chạm đều của `catalog` (barcode_counters, imports, categories, attribute_values, hàm `catalog.search_products`); không đọc, ghi module khác.
- 5.4: cấp dải barcode nằm trong transaction của `ApplyImport` (cùng lúc chuyển bản nhập sang applying và xếp việc) — lùi thì dải cũng lùi.
- 5.5, 5.6: không áp dụng: không thêm sự kiện, không gọi ra ngoài.
- 5.7: câu `ReserveBarcodeNumbers` lấy tenant từ `app.tenant_id` (thiếu → NULL → lỗi NOT NULL như câu cũ); `search_products` giữ lọc `tenant_id`/`seller_id` ở mọi nhánh và ở phép nối thêm.
- 5.8: không áp dụng: không chạm lớp HTTP.
- 5.9: không thêm đường ghi; `applyImport` vẫn có `Idempotency-Key`.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo module mẫu catalog (Writer, sentinel, test ở gói `_test` trên PostgreSQL thật).

Mục 6 (đúng khi chạy đồng thời):
- 6.1: hai lần cấp dải cùng lúc: `UPDATE … SET next = next + n RETURNING` dưới khóa dòng, mỗi bên một dải rời (như câu +1 cũ). Việc chạy lại dùng lại đúng dải trong payload — không cấp lần hai; số API cấp sau đó nằm sau dải.
- 6.2: `ApplyImport` chỉ chạy từ `ready` dưới khóa dòng bản nhập; bấm hai lần → lần sau `ErrImportNotReady`, không cấp dải lần hai.
- 6.3: B-04 là lỗi thứ tự khóa. Sau sửa: `ApplyImport` khóa `imports` → `barcode_counters` (đúng ADR-46 nhóm 4: bộ đếm cuối nhóm, sau đó không khóa gì thêm của nhóm 4) trong một transaction API ngắn; việc áp không khóa bộ đếm nữa. Dự phòng: hết dải (mỗi mã tự sinh trùng barcode gõ tay tốn một số) hoặc việc xếp trước bản sửa này (payload không có dải) → quay về `NextEAN13` trong transaction việc áp như trước (đúng, nhưng giữ khóa bộ đếm như cũ; hiếm). Đường tay không đổi (danh mục → mẫu → biến thể → bộ đếm).
- 6.4: lỗi tạm giữ cách cũ (RetryTx / lịch thử lại việc nền).
- 6.5: READ COMMITTED như cũ.
- 6.6: việc áp chạy lại: con trỏ dải dựng lại mỗi lần chạy `apply` từ payload (Bẫy "RetryTx chạy lại").
- 6.7, 6.8, 6.10, 6.11, 6.13: không áp dụng: không gọi ra ngoài, không tiền, không vùng rủi ro cao, không đổi quyền.
- 6.9: B-07 sắp tập khớp theo `updated_at DESC, id DESC` trước khi cắt; UNION khử trùng vốn đã tính đủ tập khớp nên chỉ thêm phép nối mẫu theo khóa chính và sắp; test thời gian của Task 390 vẫn chạy.
- 6.12: không đưa tên mẫu, barcode vào log; audit không đổi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| B-04: việc áp còn mở không khóa bộ đếm barcode; barcode không trùng | `TestApply_DoesNotHoldBarcodeCounter` | `go test -race -count=1 -run 'TestApply_DoesNotHoldBarcodeCounter' ./internal/modules/catalog/` | Trước sửa: FAIL `CreateProduct trong lúc việc áp còn mở: … tăng bộ đếm barcode: … canceling statement due to lock timeout (SQLSTATE 55P03)`. Sau sửa: ok |
| B-04: việc xếp trước bản sửa (payload không có dải) vẫn áp được | `TestApply_PayloadWithoutBarcodeRange` | `go test -race -count=1 -run 'TestApply\|TestCreateProduct_GeneratedBarcodes\|TestImport' ./internal/modules/catalog/` | ok (37,7 s), gồm các test áp, nhập, barcode tự sinh 100 luồng có sẵn |
| B-04: dải số đi đúng thứ tự, rỗng, vượt dải | `TestRangeNext` | `go test -count=1 ./internal/modules/catalog/internal/barcode/` | ok |
| B-06: tên danh mục có `>` (tạo, sửa), giá trị thuộc tính có `;` → `ErrInvalidInput`; hai danh mục cùng đường dẫn (dữ liệu cũ) → tệp nhập báo lỗi cột Danh mục, không gán bừa | `TestClassification_ImportSeparators` | `go test -race -count=1 -run 'TestClassification\|TestCategor\|TestAttribute\|TestImport\|TestTemplate' ./internal/modules/catalog/` | Trước sửa: FAIL 4 dòng (tạo/sửa có `>` được, `;` được, bản nhập `ready []`). Sau sửa: ok |
| B-07: "dam" (khớp > 1.000 mẫu) qua các trang cỡ 100 = đúng 1.000 mẫu khớp sửa gần nhất, không lặp | `TestProducts/ListBroadQNewest` | `go test -race -count=1 ./internal/queries/...` | Trước sửa: FAIL `1000 mẫu qua các trang, khác 1.000 mẫu khớp mới sửa nhất từ vị trí 2`. Sau sửa: ok (52 s), gồm `TestPlansHaveNoSeqScan` và đo thời gian tìm rộng của Task 390 |
| Cổng kiểm của task | — | `ci-task.sh` (chạy nền, sau khi gộp main) | build, vet, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard: qua. Toàn bộ `go test` (đổi migration): mọi gói ok trừ `tests/arch TestLintCleanOnRealCode` và lint — cùng một lỗi gofumpt ở `imports_apply_test.go:426` (đã sửa; `golangci-lint run ./internal/modules/catalog/... ./internal/queries/...` → `0 issues`, `go test -run TestLintCleanOnRealCode ./tests/arch/` → ok). gen-check đỏ chỉ vì lúc chạy chưa stage (so với index); sau `git add`: `make gen && git diff --exit-code -- internal console/src/api` → không chênh |

## Quyết định thay chủ shop

- **B-04 — cấp dải barcode lúc bấm Áp, không phải lúc việc áp chạy.** Chọn: `ApplyImport` (transaction API ngắn, đã khóa dòng bản nhập) đếm biến thể mới để trống barcode trong plan, cấp một dải bằng một câu tăng bộ đếm, ghi dải vào payload của việc; việc áp dùng dải đó, không chạm bộ đếm. Vì sao: đề xuất của review "cấp dải ở đầu lần áp" trong cùng transaction việc áp vẫn giữ khóa dòng bộ đếm tới commit (khóa dòng luôn giữ tới hết transaction) và vẫn có vòng chờ với mẫu chèn cùng mã; queue chỉ đưa cho handler một transaction nên không cấp được trong transaction riêng. Dải nằm trong payload nên chạy lại dùng đúng dải đó, không hao số. Phương án khác: sequence PostgreSQL chung (không khóa) — bỏ vì một sequence cho mọi tenant làm lộ nhịp tạo mẫu giữa các shop và đổi mô hình bộ đếm theo tenant; chấp nhận và ghi vào ADR-46 — bỏ vì sửa được rẻ. Đường dự phòng (hết dải do trùng barcode gõ tay, hay việc xếp trước bản sửa) quay về cách cũ. Đảo ngược: bỏ trường `barcodes` khỏi payload.
- **B-06 — cấm ký tự ở chỗ tạo, sửa (mã lỗi có sẵn `invalid_input`) và thêm chặn đường dẫn trùng khi nhập.** Coi là sửa lỗi: tên/giá trị như vậy chưa bao giờ nhập Excel đúng được, và tên có `>` làm tệp gán mẫu vào sai danh mục mà không báo. Không đổi hợp đồng API, không đổi màn. Dữ liệu cũ đã có `>`: đường dẫn trùng bị bỏ khỏi bảng tra nên dòng chọn nó báo "Danh mục … không có" (câu báo chưa nói rõ là trùng — chấp nhận vì chỉ gặp với dữ liệu ghi trước luật này). Phương án khác: chỉ phát hiện trùng khi nhập, không cấm — bỏ vì giá trị có `;` vẫn không bao giờ khớp.
- **B-07 — sắp trong hàm tìm rồi mới cắt (migration 0131, chỉ thay thân hàm).** Tập 1.000 mẫu là 1.000 mẫu khớp sửa gần nhất, các trang nhất quán. Không thêm báo "kết quả bị cắt" lên màn (đó là thay đổi màn hình, ngoài phạm vi; ghi ở Việc phát hiện thêm). Phương án khác: hàm nhận con trỏ để trang qua hết mọi mẫu khớp — lớn hơn, đổi chữ ký hàm.

## Dừng để hỏi

- **B-05 (mẫu nháp trong danh mục đã lưu trữ vẫn chuyển sang Đang bán được) — chưa sửa, chờ chủ shop chọn.** Sửa đúng nghĩa là `UpdateProduct` từ chối đổi sang `active` khi danh mục của mẫu đã lưu trữ — nhân viên sẽ thấy một lần chặn mới khi bấm "Đang bán" (và dòng nhập Excel đổi trạng thái mẫu như vậy sẽ lỗi), cần một mã lỗi mới trong hợp đồng API (đề xuất 409 `category_archived`, theo mẫu `role_archived`) và câu báo trên màn. Đây là đổi hành vi người dùng thấy nên theo lời giao không tự làm. Hai lựa chọn: (1) chặn như trên — khóa chia sẻ danh mục trước khi khóa mẫu (đúng ADR-46 nhóm 4), có test đua lưu trữ/đổi trạng thái; (2) giữ luật một chiều và ghi rõ "lưu trữ danh mục chỉ chặn khi còn mẫu đang bán; mẫu nháp đổi sang đang bán vẫn được, M5 phải tự bỏ danh mục đã lưu trữ khi dựng kệ". Đề xuất: (1), làm thành một task riêng.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- B-05 không làm trong task này (xem Dừng để hỏi).

## Việc phát hiện thêm

- ADR-46 (`docs/adr/0046-thu-tu-khoa.md`, đoạn đường áp): nên ghi thêm rằng bộ đếm barcode nay khóa trong transaction ngắn của `ApplyImport` (sau `imports`), việc áp chỉ khóa nó ở đường dự phòng.
- Màn danh sách mẫu không báo khi tìm theo từ khóa khớp hơn 1.000 mẫu (kết quả bị cắt ở 1.000 mẫu sửa gần nhất). Cần trường "đã cắt" trong `listProducts` và một dòng báo trên màn — đổi hợp đồng và màn, để task sau.
- Câu báo khi tệp chọn đường dẫn danh mục trùng (dữ liệu cũ có `>`) là "không có"; nếu cần rõ hơn thì thêm mã lỗi `ambiguous_category` cho bước kiểm.

## Reviewer

Kết luận: ĐẠT (agent reviewer, một vòng, trên phần đã stage sau khi gộp main).

- Phát hiện phải sửa: không có. Đã kiểm: thứ tự khóa ADR-46 nhóm 4 (bản nhập → bộ đếm trong ApplyImport; đường dự phòng vẫn mẫu → biến thể → bộ đếm); việc nền chạy lại dùng lại dải từ đầu (payload giải mã lại mỗi lần, `ap := *a` không sửa applier dùng chung); số đếm khớp đúng các biến thể ghi với barcode trống; ý nghĩa bộ đếm cũ/mới nhất quán (không cấp lại số); `search_products` giữ chủ, quyền, lọc tenant/seller ở mọi nhánh; B-06 không còn đường ghi giá trị thuộc tính nào khác.
- Ghi chú không bắt buộc: (1) thêm dòng `classificationRefs` vào Ảnh hưởng — đã thêm; (2) có thể ghi vào ADR-46 rằng bộ đếm nay khóa trong `ApplyImport` — chưa làm (ADR do chủ shop duyệt; ghi ở Việc phát hiện thêm); (3) chưa có test ca hết dải giữa chừng rồi quay về `NextEAN13` — logic đơn giản, không bắt buộc.
