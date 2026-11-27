# Task 236 — Bộ ghi XLSX theo luồng, chặn công thức, trong gói chung internal/platform/xlsx

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 236 = M6-13). Loại việc (quy trình mục 3): A
(gói chung, vùng cao theo kế hoạch).

Điều kiện "M1–M5 đóng" của M6-00 được chủ shop miễn (27/11 sáng duyệt kế hoạch M6 và cho bắt đầu khi M5 chưa đóng mốc,
Task 221 còn chờ staging).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** vào gói `internal/platform/xlsx`: `Kind` và
  `KindString`, `KindInt`, `KindMoney`, `KindDate`; `Cell`, `Str`, `Int`, `Money`, `Date`, `Empty`; `Column`; `Writer`,
  `NewWriter`, `(*Writer).Row`, `Note`, `Rows`, `Close`; `MaxDataRows`. `ErrTooManyRows` **dùng lại** sentinel có
  sẵn của `Read` (`read.go`, chỉ sửa chú thích khối lỗi), không khai lần hai. Gói con mới `xlsxtest` (`Read`, `Book`,
  `Row`, `Cell`, `Style`, `DecodeXstring`). Hành vi đổi của một ký hiệu
  có sẵn: `Sanitize` cắt theo đơn vị UTF-16 thay vì theo rune (xem Quyết định thay chủ shop QĐ-236-2); `MaxCellLength`
  giữ giá trị 32.767, đổi chú thích thành "đơn vị UTF-16". `Sanitize` chỉ được gọi trong chính gói (grep
  `xlsx.Sanitize` ngoài gói: 0 chỗ); `Write` (không sửa `write.go`) gọi qua nó. Không đổi `Read`, `Write`, `Limits`.
- Gói phụ thuộc ngược (go list): `internal/modules/cash/internal/statement`, `internal/modules/catalog`,
  `internal/modules/catalog/internal/importer`, `internal/modules/customer` (chỉ dùng `Read`/`Write`; `Write` cắt ô
  bằng `Sanitize` rồi excelize tự cắt theo UTF-16, nên kết quả ghi của `Write` không đổi với chuỗi BMP; với chuỗi
  nhiều ký tự ngoài BMP trước đây excelize cắt thêm, nay `Sanitize` cắt đúng chỗ đó — tệp ra giống hệt).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (Task M6-14, 15 sẽ dùng `Writer`).
- Câu sqlc, bảng, pha migration: không có.
- Test kiến trúc dùng chung: tách `selectorCalls` (thân vòng của `findSelectorCalls`, không đổi hành vi) và thêm
  `allGoFiles` trong `tests/arch/scan_test.go`; `findSimpleProtocol` (`sqlmode_test.go`) dùng `allGoFiles` (cùng bộ
  lọc, không đổi hành vi) — để jscpd không báo trùng với `tests/arch/xlsx_test.go`.
- Test sẽ chạy: `go test -race ./internal/platform/xlsx/...` (cả gói, gồm test cũ của M1: đọc, `Write`,
  `Sanitize`), fuzz `FuzzWriterString` 60 s, `go test ./tests/arch/ -run 'Xlsx'` và cả `./tests/arch/`; gói phụ thuộc
  ngược chạy ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục
  `internal/platform/xlsx` (`Read`, `ReadRows`, `Write`, `Sanitize`, `Limits`); grep `NewStreamWriter`, `StreamWriter`,
  `excelSerial`, `1899` trong repo: không có bộ ghi theo luồng hay hàm đổi ngày sang số Excel nào. Múi giờ VN: mỗi gói
  tự khai `time.FixedZone("ICT", 7*3600)` (observability, order, cash, fulfillment…), không có gói chung; gói xlsx
  khai riêng một biến như vậy (gói không được nhập module).
- Dùng lại / viết mới vì: dùng lại `Sanitize` của M1 cho mọi ô chữ và tiêu đề (không viết hàm thứ hai); dùng lại
  `excelize.StreamWriter` (không thêm thư viện). Viết mới `Writer` vì `Write` của M1 giữ cả bảng trong bộ nhớ
  (`Sheet.Rows [][]any`) và ghi mọi ô thành chữ — báo cáo cần ô số/tiền/ngày và 200.000 dòng. Test kiến trúc mới dùng
  lại `parseGoFiles`, `assertMentions`, `repoRoot` và `readerViolation` (luật h có sẵn) của `tests/arch`. Bộ đọc
  `xlsxtest` viết mới bằng `archive/zip` + `encoding/xml` vì kế hoạch đòi đọc lại độc lập với excelize (đọc của M1 đi
  qua excelize).

## Soát thiết kế

Mục 5:
- 5.1–5.7, 5.9, 5.10: không áp dụng: gói không chạm database, không có bảng, không gọi ra ngoài, không có đường ghi
  HTTP, không dùng Valkey.
- 5.8: gói không nhập `net/http` hay module (luật h có sẵn cấm `internal/platform/xlsx` nhập `internal/modules`).
- 5.11: không thêm thư viện; excelize có sẵn (dòng của M1 trong `docs/phu-thuoc.md` thêm việc "ghi tệp báo cáo theo
  luồng").
- 5.12: theo cách của gói chung M1: lỗi sentinel trong gói (`ErrTooManyRows`, lỗi tham số), không log, không đưa nội
  dung ô vào lỗi Go (tệp có thể có dữ liệu cá nhân).

Mục 6:
- 6.1–6.9: không áp dụng: không database, không hàng đợi, không chạy đồng thời — một `Writer` chỉ một goroutine dùng
  (ghi trong chú thích kiểu). Lượt chạy lại của `RetryTx` (C19) là việc của Task M6-14: Writer không giữ trạng thái
  ngoài chính nó, mỗi lần chạy tạo Writer mới.
- 6.10: tiền là `int64` đồng, ghi nguyên giá trị vào ô số (`strconv.FormatInt`, không qua float), định dạng `#,##0`.
- 6.11: test thuộc tính rapid 500 lượt cho `Sanitize` (đã có của M1, thêm điều kiện đơn vị UTF-16) và fuzz cho bộ ghi:
  chuỗi bất kỳ → XML đọc được, giá trị đọc lại bằng `Sanitize(s)`.
- 6.12: gói không log; lỗi không chứa nội dung ô.
- 6.13: không áp dụng: không có quyền.

## Quyết định thay chủ shop

| Mã | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-236-1 | Kế hoạch đặt tên hằng `Kind` là `String`, `Int`, `Money`, `Date` và hàm dựng ô `Int`, `Money`, `Date` — Go không cho hai tên trùng trong một gói | Hằng `KindString`, `KindInt`, `KindMoney`, `KindDate`; hàm giữ `Str`, `Int`, `Money`, `Date`, `Empty` như kế hoạch. Giá trị 0 của `Kind` là ô trống (`Empty()` = `Cell{}`) | Bên gọi (M6-14, 15) dùng hàm dựng ô nhiều nhất; tên hằng có tiền tố là cách thường gặp trong Go; dễ đổi tên bằng gopls | Đổi tên hàm (`IntCell`…): dài hơn ở mọi chỗ gọi |
| QĐ-236-2 | Giới hạn 32.767 "ký tự" của ô tính theo gì | `Sanitize` đếm đơn vị UTF-16 (ký tự ngoài BMP như emoji tính 2), vẫn không cắt giữa một ký tự | Excel và excelize đếm theo UTF-16 (`trimCellValue` của excelize cắt thêm theo UTF-16); đếm theo rune thì chuỗi nhiều emoji bị excelize cắt lần nữa, giá trị trong tệp khác `Sanitize(s)` (fuzz của kế hoạch đòi bằng nhau). Đây là "luật thiếu" thêm vào đúng hàm của M1 (G19); test cũ của M1 dùng ký tự BMP nên chạy nguyên | Giữ đếm rune: tệp có thể khác `Sanitize(s)` với emoji |
| QĐ-236-3 | Ngày ngoài khoảng Excel và ngày rỗng | `Date(t)` với `t.IsZero()` là ô trống; ngày trước 01/03/1900 hoặc từ năm 10000 (giờ VN) → `Row` trả lỗi | Số ngày trước 01/03/1900 lệch một ngày do lỗi năm nhuận 1900 của Excel; ngoài khoảng Excel không hiện được | Ghi số âm: Excel hiện `####` |
| QĐ-236-4 | LibreOffice để mở thử | Máy không có `soffice`: **chưa kiểm**; Task 21 kiểm bằng tay như kế hoạch | Không cài phần mềm ngoài trên máy khi chủ shop vắng | Cài LibreOffice: việc ngoài phạm vi |
| QĐ-236-5 | Kế hoạch: test kiến trúc "cấm nhập excelize ngoài `internal/platform/xlsx`" — nhưng `internal/modules/customer/importer_test.go` (M2) nhập excelize để dựng tệp thù địch có ô `<f>` cho test bộ đọc | Cấm gọi `SetCellFormula`, `SetCellHyperLink`, `AddVBAProject` ở **mọi** tệp (cả `_test.go`); luật nhập excelize áp cho mã chạy thật (như luật h có sẵn), tệp `_test.go` được nhập | Test bộ đọc cần tệp có công thức thật mà gói xlsx không ghi được (đúng mục tiêu task). Không sửa test của module customer (ngoài phạm vi). Dễ siết lại khi test đó chuyển sang tệp mẫu `testdata` | Cấm cả `_test.go`: phải viết lại test M2 |
| QĐ-236-6 | Fuzz tìm ra: excelize thoát dãy `_xHHHH_` (Excel đọc thành một ký tự) bằng các lần khớp không chồng nhau, nên hai dãy dùng chung dấu `_` (vd `_x0056_x0041_`) bị thoát thiếu, Excel sẽ hiện `_x0056A` | Writer tách chuỗi như vậy thành nhiều đoạn rich text (không định dạng) ngay trước dấu `_` dùng chung (`textValue`, `xstringChains`); chuỗi thường vẫn là ô chữ một đoạn | Giữ đúng chữ gốc mà không sửa excelize, không đổi dữ liệu; chỉ chạm chuỗi hiếm có dạng này. Không ảnh hưởng chặn công thức (ô vẫn là chữ, qua `Sanitize`) | Bỏ qua (chữ hiện sai); thay ký tự (đổi dữ liệu) |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Ô `=HYPERLINK…`, `+849…`, `-5`, `@SUM`, tab/CR/LF đầu → thêm `'`; "Bình thường" giữ nguyên; không `<f>` ở mọi phần, không phần/phần tử siêu liên kết; tiền `150000` ô số `#,##0`; ngày 18/11/2026 10:30 VN = `46344.4375`, `dd/mm/yyyy hh:mm` | `TestWriterBlocksFormulas` (+ `checkFormulaRow`) | `go test -race -count=1 ./internal/platform/xlsx/...` | `ok elimaz.com.vn/crm/internal/platform/xlsx 77.413s`; ci-task từ xa: `test (1): success`, `test (2): success` |
| Tiêu đề đậm, cố định (`ySplit=1`, `frozen`, `A2`), độ rộng cột đúng, `<col>` tăng dần, ghi chú nghiêng, ô trống không ghi, `Date(zero)` trống, tên trang | `TestWriterHeaderBoldFrozenAndNoteItalic` | như trên | ok |
| Số nguyên và tiền lớn ghi đúng nguyên int64 (không qua float) | `TestWriterIntAndBigMoneyExact` | như trên | ok |
| Số ngày Excel tự tính theo giờ VN (cùng thời điểm ở UTC ra cùng số), 01/03/1900 = 61; ngoài khoảng → lỗi (QĐ-236-3) | `TestWriterDateSerial` | như trên | ok |
| `\x01` bị bỏ, XML đọc được; 40.000 "ệ" → 32.767 ký tự UTF-8 hợp lệ; emoji cắt theo UTF-16 (16.383); `_x0041_` đọc lại nguyên chữ | `TestWriterStripsControlAndTruncates` | như trên | ok |
| Dãy `_xHHHH_` dùng chung `_` đọc lại đúng (QĐ-236-6) | `TestWriterChainedXstringEscapes`; hạt giống fuzz `cb2a6363a7edc3b4` | như trên | ok |
| `Row` sai số ô / loại lạ → lỗi, dòng lỗi không đếm; `Row`/`Close` sau `Close` → lỗi; tệp mở được bằng `excelize.OpenReader` và `xlsxtest`; không `xl/calcChain.xml`, `xl/vbaProject.bin`, `xl/externalLinks/`; không cột, cột không loại, tên trang sai → lỗi | `TestWriterRejectsBadUse` | như trên | ok |
| `ErrTooManyRows` khi vượt trần (Row và Note); `MaxDataRows = 1.048.575` | `TestWriterTooManyRows` (trần hạ bằng `SetMaxDataRowsForTest`) | như trên | ok |
| 200.000 dòng × 10 cột: HeapInuse tăng ≤ 50 MB; thời gian (chỉ in ra) | `TestWriterStreamsLargeTable` | `go test -count=1 -run TestWriterStreamsLargeTable -v ./internal/platform/xlsx/` | `200.000 dòng: 3.022093375s, tệp 8750232 byte, HeapInuse trước 3 MB, đỉnh 41 MB, lúc đẩy tệp 41 MB` (với -race: 21,8 s, đỉnh 41 MB) |
| Fuzz 60 s: chuỗi bất kỳ (tiêu đề và ô) → sheet1.xml đọc được bằng `encoding/xml`, không `<f>`, đọc lại = `Sanitize(s)` | `FuzzWriterString` (hạt giống `testdata/fuzz/FuzzWriterString`) | `go test -count=1 -run '^$' -fuzz '^FuzzWriterString$' -fuzztime 60s ./internal/platform/xlsx/` | `fuzz: elapsed: 1m0s, execs: 44715 … PASS` (lượt đầu tìm ra `_x0056_x0041_`, đã sửa theo QĐ-236-6) |
| Thuộc tính rapid 500 lượt: bảng ngẫu nhiên đọc lại đúng (chữ = `Sanitize`, số đúng int64, ô trống vắng), không `<f>`, `Rows()` đúng | `TestWriterRoundTripProperty` | `go test -race -count=1 ./internal/platform/xlsx/...` | ok |
| `Sanitize` (500 lượt): không mở đầu bằng `= + - @` tab CR LF, bất biến, ≤ 32.767 (thêm: ≤ 32.767 đơn vị UTF-16); ví dụ emoji | `TestSanitizeProperties` (M1, thêm một điều kiện), `TestSanitizeTruncatesUTF16Units` (mới) | như trên | ok |
| Test cũ M1 của bộ đọc, `Write`, `Sanitize` chạy nguyên vẹn | `TestSanitizeExamples`, `TestSanitizeTruncatesOnRuneBoundary`, `read_test.go`, `write_test.go`, `read_rows_test.go`, `FuzzRead` (hạt giống) | như trên | ok |
| Test kiến trúc với tệp mẫu: gọi `SetCellFormula`/`SetCellHyperLink`/`AddVBAProject` → báo tên tệp (cả `_test.go`); nhập excelize ngoài gói xlsx → báo; trong gói xlsx và trong `_test.go` không báo; mã thật 0 vi phạm | `TestXlsxRulesReportSample`, `TestXlsxRulesProductCode` | `go test -count=1 -v ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 233.294s`; `--- PASS: TestXlsxRulesProductCode (14.04s)` |
| Cổng tĩnh (lint, vet, gen, jscpd, test-guard, danh mục, gitleaks) + test gói đổi và phụ thuộc ngược | — | `scripts/agent/ci-task.sh` | `REMOTE-TEST XANH …/runs/45945101912`; `CI-TASK XANH (từ xa trên blacksmith, …)`. Lượt trước đỏ ở test-guard (`t.Skip` khi `-short` trong `TestWriterStreamsLargeTable`), đã bỏ dòng đó |
| Mở bằng LibreOffice không giao diện | — | `which soffice libreoffice` | không có: **chưa kiểm** (QĐ-236-4), Task 21 kiểm bằng tay |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tên hằng `Kind` có tiền tố `Kind` (QĐ-236-1).
- `ErrTooManyRows` là sentinel có sẵn của `Read` (cùng nghĩa), không khai lần hai.
- Đo bộ nhớ 200.000 dòng: HeapInuse sau `runtime.GC()` ở mỗi 20.000 dòng và đúng lúc tệp được đẩy ra đích (đỉnh thật,
  không lẫn rác chưa dọn); thời gian chỉ in ra (Task 400: không lấy giờ thật làm tiêu chí đúng/sai).
- Luật nhập excelize của test kiến trúc mới không áp cho `_test.go` (QĐ-236-5).
- Kế hoạch G19 ghi "M6-13 không sửa `sanitize.go`"; task có sửa (đếm UTF-16, QĐ-236-2), trong phạm vi mục Files
  ("chỉ khi `Sanitize` thiếu luật"); `write.go` không sửa.

## Việc phát hiện thêm

- excelize v2.11.1 `File.WriteTo` dựng cả tệp zip trong `bytes.Buffer` rồi mới ghi ra đích: `Writer.Close` giữ tệp nén
  trong bộ nhớ một lúc (200.000 dòng × 10 cột ≈ 9 MB). Đủ cho trần 200.000 dòng của M6; tệp lớn hơn nhiều thì phải
  tính lại.
- Bộ đọc của excelize (`GetCellValue`, nên cả `xlsx.Read` của M1) giải mã chuỗi đã nối của các đoạn rich text, nên đọc
  sai dạng hiếm `_xHHHH_` dùng chung `_` (QĐ-236-6): tệp CRM xuất có chuỗi như vậy rồi nhập lại có thể ra chữ khác.
  Chưa sửa (bộ đọc M1, ngoài phạm vi).
- `internal/modules/customer/importer_test.go` nhập excelize trong test (QĐ-236-5): có thể chuyển sang tệp mẫu
  `testdata` để siết luật cho cả `_test.go`.

## Reviewer

Kết luận: ĐẠT (lượt 1, 27/11/2026). Phát hiện phải sửa: không có. Ảnh hưởng dự kiến khớp thực tế, không chỗ gọi nào
bị sót; mọi dòng bảng bằng chứng trỏ tới test có thật và kiểm đúng tiêu chí; không test bị tắt hay nới.
Ghi chú không bắt buộc: (1) G19 nói không sửa `sanitize.go` — đã thêm dòng vào "Khác kế hoạch"; (2) `xlsxtest` là gói
thường, chú thích đã ghi không dùng trong mã chạy thật, hiện không ai nhập; (3) đoạn rich text (dạng `_xHHHH_` hiếm) có
thể mất khoảng trắng đầu/cuối đoạn trong Excel tùy excelize ghi `xml:space` — rủi ro thấp, không ảnh hưởng chặn
công thức.
