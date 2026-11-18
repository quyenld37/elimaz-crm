# Task 38 (M1-38) — Gói xlsx dùng chung, mẫu Excel, đọc và kiểm từng dòng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`, Task 38 (M1-38). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn nào. Thêm mới: gói
  `internal/platform/xlsx` (`Limits`, `Row`, `CellError`, `Read`, `Sheet`, `Write`, `Sanitize`, `ErrNotXlsx`,
  `ErrTooLarge`, `ErrTooManyRows`, `ErrHeaderMismatch`) và gói riêng `internal/modules/catalog/internal/importer`
  (`Template`, `Validate`, `Refs`, `Plan`, `RowError`…). Chưa có chỗ dùng (Task M1-47 nối) → 0 tham chiếu cần sửa.
- Gói phụ thuộc ngược (go list): gói mới, chưa ai nhập. `tests/arch` (luật ranh giới) đổi: thêm luật (h).
- Operation OpenAPI bị chạm, oasdiff: không (route tải mẫu, nhập ở Task M1-47).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không (chỉ đọc và kiểm trong bộ nhớ; ghi database ở Task M1-47).
- Phụ thuộc: thêm `github.com/xuri/excelize/v2` v2.11.1 vào `go.mod` (kéo theo phụ thuộc gián tiếp của nó).
- Test sẽ chạy: `go test -race ./internal/platform/xlsx/... ./internal/modules/catalog/... ./tests/arch/...`;
  fuzz `FuzzRead` 60 s; `make ci` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gói đọc/ghi
  Excel, không có hàm chặn công thức; tìm `excelize`, `archive/zip`, `Sanitize`, `formula` trong mã: 0 kết quả.
  `internal/platform/textnorm` (bỏ dấu cho cột tìm) không dùng được để so tên danh mục (luật của gói: không dùng để so
  khớp định danh). `catalog/internal/barcode` có `ValidEAN13` nhưng `Writer` của catalog không bắt barcode nhập tay
  phải là EAN-13 nên không dùng (giữ đúng luật của Writer).
- Dùng lại / viết mới vì: gói `xlsx` viết mới theo kế hoạch (C22: gói chung duy nhất cho M2, M3, M6, M7). Kiểm dòng
  viết mới trong `importer`. Luật dữ liệu của catalog (mẫu mã/SKU, barcode, màu hex, độ dài tên, màu, size, mô tả,
  giá tối đa) đang là hằng không xuất của gói `catalog`; `importer` nằm dưới `catalog/internal` nên không nhập ngược
  `catalog` được (Task M1-47 sẽ cho `catalog` nhập `importer` → vòng nhập). Chọn: `importer` giữ bản sao các hằng
  và biểu thức, kèm test `package catalog` (`importrules_test.go`) so từng hằng, từng biểu thức với bản của catalog,
  lệch là đỏ. Phương án khác (dời luật ra gói `catalog/internal/rules`) là thay đổi cấu trúc ngoài task.

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 chủ dữ liệu: không tạo dữ liệu; `importer` thuộc `catalog` (chủ mẫu, biến thể). Gói `xlsx` không biết nghiệp vụ.
- 5.2, 5.3: không ghi, không đọc bảng nào. `Refs` do bên gọi (Task M1-47) đọc qua API của catalog.
- 5.4, 5.5, 5.6: không áp dụng: không có transaction, sự kiện, gọi ra ngoài.
- 5.7: không áp dụng: không truy vấn.
- 5.8: `xlsx`, `importer` không nhập `net/http`, ogen; `xlsx` không nhập `internal/modules/**` (test kiến trúc mới).
- 5.9: không áp dụng: không có route ghi.
- 5.10: không áp dụng: không dùng Valkey.
- 5.11: excelize ngoài ADR-44 → dòng trong `docs/phu-thuoc.md` (kế hoạch C22 đã chọn excelize v2); chỉ
  `internal/platform/xlsx` được nhập excelize (test kiến trúc).
- 5.12: gói chung đặt ở `internal/platform/xlsx` (tên danh từ), có doc gói và dòng "khi nào không dùng".

Đúng và an toàn (mục 6):
- 6.1–6.9: không áp dụng: hàm thuần trong bộ nhớ, không database, không hàng đợi, không đồng thời.
  Riêng 6.9 (treo lâu): `Read` chặn kích thước tệp (5 MiB), tổng giải nén theo header zip (50 MiB; `archive/zip`
  báo lỗi khi dữ liệu thật dài hơn header nên header là chặn trên), số dòng, độ dài ô trước khi excelize đọc; tệp
  bom zip bị từ chối mà không giải nén.
- 6.10 tiền: giá đọc thành `int64` đồng, chỉ nhận số nguyên không âm (`350000`, `350.000`), tối đa 2^53−1; không
  dùng số thực. Ô số đọc giá trị thô (không theo định dạng hiển thị) để `350,000` hiển thị kiểu Mỹ không bị đọc sai.
- 6.11 quy luật: `Sanitize` có test rapid 500 lượt (không mở đầu bằng ký tự công thức, không ký tự cấm XML,
  lặp lại không đổi, ≤ 32.767 ký tự); `Read` có fuzz không panic.
- 6.12 dữ liệu cá nhân: tệp sản phẩm không có dữ liệu cá nhân; `RowError.Message` không chép nội dung ô dài quá 50
  ký tự; gói không log. Gói `xlsx` dùng chung cho tệp khách (M2): không log, không đưa nội dung ô vào lỗi Go.
- 6.13 quyền: không áp dụng ở task này (route và quyền `catalog.import` ở Task M1-47).
- Chặn công thức khi xuất (04 mục 13): mọi ô `Write` ghi là chữ, qua `Sanitize`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `mau-200.xlsx` → 0 lỗi, 200 mẫu, 1.000 `create` | `TestValidate_Template200` | `go test -race -count=1 ./internal/modules/catalog/internal/importer/` | `ok … importer 2.544s` |
| KH01: 7 lỗi cài đúng dòng, cột, mã | `TestValidate_SevenSeededErrors` | như trên | qua: đúng 7 lỗi (5 missing_required, 9 invalid_price, 12 duplicate_sku_in_file, 20 unknown_category, 31 invalid_color_hex, 44 invalid_status, 50 inconsistent_product_fields), Plan rỗng |
| SKU của mẫu khác → `sku_belongs_to_other_product`; giá khác → `update`; giống hệt → `unchanged` | `TestValidateExistingSKUs` | như trên | qua |
| Giá: nhận `350000`, `350.000`; từ chối `350,5`, âm, > 2^53−1 | `TestValidatePrice` | như trên | qua (18 ca) |
| Mã lỗi từng cột, luật trong một mẫu | `TestValidateFieldRules`, `TestValidateGroupRules`, `TestValidateTooManyVariants`, `TestMergeCellErrors` | như trên | qua |
| `Template(refs)` rồi `Read` → tiêu đề khớp, 0 dòng | `TestTemplateRoundTrip` | như trên | qua |
| Bom zip 1 GiB → `ErrTooLarge` < 2 s, cấp thêm < 100 MB | `TestReadZipBomb`, `TestReadZipBombLyingHeader` | `go test -race -count=1 -v ./internal/platform/xlsx/` | `bom zip 1017 KB: từ chối sau 931µs, cấp thêm 6 KB`; header khai sai → `ErrNotXlsx` |
| HTML đổi đuôi → `ErrNotXlsx`; 20.001 dòng → `ErrTooManyRows`; công thức → `formula_not_allowed` | `TestReadNotXlsx`, `TestReadTooManyRows`, `TestReadFormulaNotAllowed`, `TestReadFormulaKinds` | như trên | qua |
| Dòng chỉ có ô ở cột XFD (reviewer lần 1) bị từ chối ngay; 20.000 dòng trống có định dạng vẫn đọc | `TestReadFarColumnRows` | `go test -race -count=1 ./internal/platform/xlsx/` | `ok … xlsx 27.775s` |
| Cấu trúc làm lệch trang giữa gói và excelize bị từ chối hoặc vẫn bị trần số ô chặn (reviewer lần 2): phần trùng tên (3 kiểu), sổ ở chỗ khác, kiểu quan hệ giả, `Id` trùng, tên trang trùng, trang lớn không ai trỏ tới; thẻ `<sheet>` mồi không làm lệch | `TestReadStructureMismatch` (11 ca, thêm theo reviewer lần 3: quan hệ officeDocument lồng, trang lớn không đuôi `.xml`; theo ghi chú lần 4: `<sheet>` có id của cả hai namespace quan hệ), `TestReadDecoySheetElements` | `go test -race -count=1 -v -run 'Structure\|Decoy\|FarColumn\|Shared' ./internal/platform/xlsx/` | cả 4 test PASS; mỗi ca < 2 s, < 100 MB |
| Fuzz sau sửa lần 2 | `FuzzRead` | `make fuzz-xlsx` | `execs: 44404, new interesting: 100 … PASS` |
| Công thức dùng chung không làm `Read` chậm theo bình phương | `TestReadManySharedFormulasLinear` | như trên | 7.500 ô 27 ms, 30.000 ô 125 ms (không `-race`; tỉ lệ 4,7) |
| `Write` chặn công thức; mọi ô là chữ; 1.000 dòng tiếng Việt ghi rồi đọc đúng nguyên văn | `TestWriteBlocksFormulas`, `TestWriteAllCellsAreText`, `TestReadWriteRoundTripVietnamese`, `TestWriteSheetsAndLists` | như trên | qua |
| `Sanitize` rapid 500 lượt; 40.000 `ộ` cắt đúng biên | `TestSanitizeProperties`, `TestSanitizeTruncatesOnRuneBoundary`, `TestSanitizeExamples` | như trên | `[rapid] OK, passed 500 tests` |
| Test kiến trúc: `xlsx` không nhập module; ngoài `xlsx` không nhập excelize | `TestBoundariesNotCalledModule` (7 ca mới, luật h), `TestBoundariesRealCode` | `go test -race -count=1 ./tests/arch/...` | `ok … tests/arch 19.859s` |
| Luật của importer khớp catalog | `TestImporterRulesMatchCatalog` | `go test -race -count=1 ./internal/modules/catalog/...` | `ok … catalog 35.306s` |
| Fuzz `Read` 60 s không panic | `FuzzRead` | `make fuzz-xlsx` | `execs: 34971, new interesting: 73 … PASS` |
| Cổng chung | — | `GOFLAGS=-p=4 make ci` (trước lần gộp main thứ hai) | build, vet, lint, gen-check, sqlc-vet, toàn bộ `go test -race ./...` (34 gói ok, 0 FAIL), vuln, oasdiff qua; `catalog-check` đỏ chỉ vì danh mục chưa stage (stage xong: qua) |
| Sau gộp main lần hai (b054999) | — | build, vet, `golangci-lint run ./...`, test 3 vùng trên, `make gen-check catalog-check test-guard gitleaks console-check vuln` | `0 issues`; mọi bước qua; `No vulnerabilities found`. `make jscpd` đỏ vì 2 đoạn trùng mới nằm trong mã của main (xem Việc phát hiện thêm) |

| Sau sửa theo reviewer lần 1, gộp main e863734 | — | `GOFLAGS=-p=4 make ci`, rồi chạy lại `go test -race ./tests/arch/` và `make vuln oasdiff catalog-check test-guard gitleaks console-check` | build, vet, lint, gen-check, sqlc-vet qua; `go test -race ./...` 35 gói ok, chỉ `tests/arch` đỏ vì `golangci-lint` của phiên khác giữ khóa ("parallel golangci-lint is running"), chạy lại `ok … tests/arch 27.088s`; các bước sau qua; jscpd vẫn chỉ 2 đoạn trùng của main |

| Sau sửa theo reviewer lần 2, gộp main f2b680f | — | `GOFLAGS=-p=4 make ci` | `make ci exit 0`: 38 gói `ok`, 0 FAIL (`ok … xlsx 29.390s`, `ok … importer 2.787s`, `ok … catalog 81.863s`, `ok … tests/arch`), `No vulnerabilities found.`, jscpd `Found 0 clones.` (main đã dọn 2 đoạn trùng ở Task 397), gitleaks `no leaks found` |
| **Lần cuối:** sau sửa theo reviewer lần 3, 4; gộp main 9dee868 | — | `GOFLAGS=-p=4 make ci` | `make ci exit 0`: 39 gói `ok`, 0 FAIL, `No vulnerabilities found.`, jscpd `Found 0 clones.` |

Hai lần `make ci` trước đó đỏ vì chạy chồng với hook Stop trên cùng PostgreSQL lúc máy tải 20 (lỗi hết giờ `DROP DATABASE`, mất kết nối LISTEN ở các gói không đổi); lần chạy một mình với `-p=4` qua.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm mã lỗi ngoài danh sách của kế hoạch: `invalid_code` (Mã mẫu sai mẫu), `invalid_text` (tên, mô tả, màu, size
  quá dài hay có ký tự điều khiển — Writer của catalog cũng từ chối), `too_many_variants` (quá 200 biến thể một mẫu,
  giới hạn của catalog), `cell_too_long` (ô quá 2.000 ký tự, `Limits.MaxCellChars`). Barcode trùng trong tệp hoặc đã
  thuộc SKU khác báo `invalid_barcode` (kế hoạch không có mã riêng).
- Thêm vào gói `xlsx`: `DefaultLimits`, `MaxCellLength`, hằng mã lỗi ô; `importer` thêm `RequiredColumns`, `Columns`,
  `CategoryPath`, `MergeCellErrors` (gộp lỗi ô của `xlsx.Read` với lỗi của `Validate`, bỏ lỗi trùng ô).
- Thêm tệp `internal/platform/xlsx/formulas.go` (quét công thức, Quyết định 5),
  `internal/modules/catalog/importrules_test.go` (đồng bộ luật, xem Tái dùng), lệnh `make fuzz-xlsx` (Quyết định 9).
- Luật kiến trúc mới đặt tên (h) trong `tests/arch/boundaries_test.go` (cùng chỗ với luật a–g).

## Quyết định thay chủ shop

1. **excelize v2.11.1** (bản mới nhất, 2/11/2026). OSV ngày 18/11/2026: 0 lỗ hổng cho bản này (mọi lỗ hổng đã công bố
   của excelize sửa ở các bản trước v2.11.1). Không có lỗ hổng chưa vá nên không dừng theo Q9.
2. **`header` của `Read` là danh sách cột bắt buộc**, không phải danh sách đầy đủ: cột thừa có tiêu đề vẫn được đọc.
   Lý do: thuộc tính thêm, bớt sau khi tải mẫu không làm tệp cũ hỏng; bảng kê của hãng (M3) có cột thừa. Cột thuộc
   tính lạ có giá trị báo `unknown_attribute_value`. Phương án khác: đòi đúng tập cột (chặt hơn, nhân viên phải tải
   lại mẫu mỗi lần đổi thuộc tính).
3. **`Read` bỏ dấu nháy mà `Sanitize` đã thêm** trước `= + - @ tab CR LF`, để tệp CRM xuất (M6 danh sách sản phẩm)
   nhập lại ra đúng chữ gốc (không thì mô tả mở đầu bằng "-" có thêm dấu nháy và mọi dòng thành `update`). Ô chữ mở
   bằng "=" không phải công thức nên vẫn nhận (chỉ ô có phần tử công thức bị từ chối). Phương án khác: giữ dấu nháy.
4. **`Read` giải nén thử từng phần zip trước khi đưa excelize** (tổng ≤ 50 MiB, bộ nhớ cố định): đo thấy excelize đọc
   phần có header khai sai tới đúng số byte khai rồi im lặng bỏ phần sau (tệp bị đọc thiếu dòng mà không báo);
   `archive/zip` báo lỗi số byte, CRC nên nay thành `ErrNotXlsx`. Cái giá: giải nén hai lần (≤ 50 MiB, vài chục ms).
5. **Tự quét XML trang đầu để tìm ô công thức** thay cho `excelize.GetCellFormula`: đo thấy công thức dùng chung không
   có ô gốc làm mỗi lần gọi quét lại cả trang (20.000 ô = 1 s, tăng theo bình phương số ô: tệp vài trăm nghìn ô treo
   hàng phút). Quét một lượt, tuyến tính (test `TestReadManySharedFormulasLinear`: 7.500 và 30.000 ô, so tỉ lệ).
   Cùng lượt quét đếm "số ô phải dựng" (cột lớn nhất của mỗi dòng có ô, excelize lấp chỗ trống) và từ chối
   `ErrTooLarge` khi quá 10 triệu (sửa theo reviewer lần 1: dòng chỉ có ô ở cột XFD không bị đếm vào MaxRows).
   Phương án khác: trần số cột; trần số dòng có ô (sẽ chặn nhầm tệp có nhiều dòng trống có định dạng).
11. **Từ chối tệp có cấu trúc làm hai bên đọc lệch trang** (sửa theo reviewer lần 2): tên phần trùng trong zip (sau
    khi đổi `\` thành `/`, không phân biệt hoa thường); sổ không ở `xl/workbook.xml` theo `_rels/.rels`; tên trang trùng;
    `Id` quan hệ của trang đầu xuất hiện hơn một lần; thẻ `<sheet>` mang `id` của cả namespace transitional lẫn strict; đích quan hệ có `\`; tên trang đầu excelize trả khác trang đã
    quét. Thẻ quan hệ, thẻ `<sheet>` chỉ nhận ở đúng vị trí con trực tiếp như encoding/xml giải vào kiểu của excelize
    (sửa theo reviewer lần 3: thẻ lồng làm hai bên chọn hai sổ khác nhau). Trần số ô tính trên **mọi** phần của zip
    (cả phần không có đuôi `.xml`), nên dù có cách lệch nào chưa nghĩ tới thì excelize cũng không
    dựng quá 10 triệu ô. Cái giá: tệp do công cụ lạ ghi sổ ở chỗ khác `xl/workbook.xml` bị từ chối (Excel, LibreOffice,
    Google Sheets đều ghi đúng chỗ này). Phương án khác: chép đủ mọi quy tắc tìm phần của excelize (dễ lệch khi nâng bản).
6. **`Read` bắt panic của excelize thành `ErrNotXlsx`** (excelize có nhiều báo cáo panic với tệp hỏng trong 2026); lỗi
   trả về không chép chuỗi lỗi của excelize (có thể chứa giá trị trong tệp khách).
7. **`Row.Cells` chỉ có ô có giá trị** (đọc khóa thiếu ra ""), để bộ nhớ theo số ô thật, không theo số cột × số dòng.
8. **Có lỗi thì `Plan` rỗng** (nhập tất cả hoặc không, Task M1-47). Biến thể có sẵn: ô Barcode trống giữ barcode cũ
   (barcode chỉ tự sinh lúc tạo, `UpdateVariant` không cho xóa); ô Mã màu, Giá nhập trống là xóa (đúng như Writer:
   `ColorHex ""` bỏ màu, `ClearCostPrice`) nên thành `update`. Trạng thái nhận không phân biệt hoa thường; mã nhãn đổi
   về chữ thường (như catalog); giá trị thuộc tính so đúng chữ (database phân biệt hoa thường). Lỗi xếp theo dòng
   rồi thứ tự cột của mẫu; mỗi ô tối đa một lỗi; cột mẫu so với dòng đầu tiên của mẫu mà ô đó không lỗi.
9. **Fuzz 60 s là lệnh riêng `make fuzz-xlsx`, không thêm vào `make ci`** (mỗi lần `make ci` của mọi task thêm 60 s);
   `make ci` vẫn chạy các hạt giống của `FuzzRead`. Phương án khác: thêm vào `ci`.
10. **Danh sách chọn của mẫu nằm trên trang ẩn `_lists`** (luật danh sách viết thẳng chỉ chứa 255 ký tự, không đủ cho
    cây danh mục); tên `_lists` dành riêng, `Write` từ chối trang trùng tên.

## Việc phát hiện thêm

- `Refs` (theo kế hoạch) không có trường mẫu đã có (tên, danh mục, nhãn…) nên `Plan` không biết mẫu có sẵn bị đổi tên
  hay phân loại; Task M1-47 cần so lại khi áp (hoặc thêm `ExistingProducts` vào `Refs`).
- Biến thể mới trùng màu, size với biến thể có sẵn **không có trong tệp** không bị `Validate` bắt (Refs chỉ theo SKU);
  Task M1-47 sẽ gặp `ErrDuplicateOption` lúc áp, cần đổi thành lỗi dòng.
- Bộ fuzz của Go đứng hàng chục giây khi thu gọn một mẫu mới (mặc định `-fuzzminimizetime` 60 s), trông như treo;
  `make fuzz-xlsx` đặt 5 s.
- Task M1-47 phải gộp lỗi ô của `xlsx.Read` bằng `MergeCellErrors` trước khi áp `Plan` (ô công thức đọc thành rỗng;
  ô Giá nhập rỗng của biến thể có sẵn nghĩa là xóa giá nhập). Đã ghi trên doc của `Validate`.
- Lượt quét công thức của gói vẫn có thể đánh số ô khác excelize với tệp dựng cố ý (số dòng `r` giảm dần, trùng):
  một ô công thức có thể lọt qua và giá trị đã tính sẵn được nhận như chữ. Không công thức nào được chạy nên mức ảnh
  hưởng thấp; trần số ô không phụ thuộc chỗ này. Tên phần trùng, sổ ở chỗ khác đã bị từ chối (Quyết định 11).
- Lúc gộp main f2b680f: `git stash apply --index` gặp xung đột ở `docs/danh-muc-ghi-chu.yaml`, lệnh vẫn bỏ mục stash
  (lỗi thao tác của phiên này). Đã khôi phục bằng SHA của commit stash, giữ cả hai dòng (`urlerr` của main, `xlsx` của
  task), đặt lại `docs/tasks/hien-tai.md` bằng `new-task.sh 38`; đã kiểm lại đủ tệp và nội dung sửa lần 2.
- Test "id của hai namespace quan hệ" (`TestReadStructureMismatch`) nên thêm `<Relationship Id="rId2" …
  Target="worksheets/sheet2.xml"/>` để bắt đúng chỗ lệch khi bỏ `relID` (ghi chú lần 5 của reviewer; không làm để
  khỏi phải duyệt lại).
- `maxCellSlots` (10 triệu) là hằng không xuất; nếu M2, M3 cần bảng rộng hơn thì đưa vào `Limits`.
- Sau khi gộp main b054999 (Task 19, 319), `make jscpd` đỏ vì 2 đoạn trùng mới không thuộc task này:
  `internal/platform/metrics/flusher_test.go:23` ↔ `internal/platform/observability/helpers_test.go:29`, và
  `internal/platform/observability/alerter.go:186` ↔ `scripts/external-probe/probe.go:196`. Không sửa ở đây (ngoài
  phạm vi); chủ của Task 19/319 cần gộp hoặc thêm vào mốc nền.

## Reviewer

Kết luận: ĐẠT (lần 5, agent `reviewer`, trên `git diff --cached` sau khi gộp main 9dee868).

- Lần 1 — CẦN SỬA: dòng chỉ có ô ở cột XFD không bị đếm vào MaxRows, `Read` tốn hàng phút CPU. Sửa: trần 10 triệu ô
  phải dựng trong lượt quét XML, `dataRow` dừng ở cột tiêu đề cuối, test `TestReadFarColumnRows`. Ghi chú không bắt buộc
  đã làm: doc `Validate` về `MergeCellErrors`, sửa tên test trong tệp task.
- Lần 2 — CẦN SỬA: gói và excelize có thể đọc hai trang khác nhau (sổ theo `_rels/.rels`, tên phần trùng), vượt trần
  số ô và việc tìm công thức. Sửa: Quyết định 11, test `TestReadStructureMismatch`, `TestReadDecoySheetElements`.
- Lần 3 — CẦN SỬA: thẻ `<Relationship>` lồng (gói nhận, excelize bỏ); trần số ô bỏ qua phần không đuôi `.xml`. Sửa:
  `elems` chỉ nhận con trực tiếp như encoding/xml của excelize; trần tính trên mọi phần; thêm 2 ca test.
- Lần 4 — ĐẠT. Ghi chú không bắt buộc đã làm: `<sheet>` có id của hai namespace quan hệ bị từ chối (`relID`).
  Sau đó gộp đoạn trùng jscpd trong test (`evilBook`).
- Lần 5 — ĐẠT. Ghi chú không bắt buộc chưa làm: ca test "id của hai namespace quan hệ" nên thêm quan hệ `rId2` trong
  `xl/_rels/workbook.xml.rels` để test bắt đúng chỗ lệch nếu bỏ `relID` (hiện vẫn xanh nhờ excelize báo lỗi khác).
  Ghi vào Việc phát hiện thêm.
