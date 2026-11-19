# Task 70 — M2-13: Nhập khách từ tệp Excel (lượt 2: gộp main)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-13, số toàn cục 70). Loại việc (quy trình mục 3): A

## Ảnh hưởng

**Lượt 2 (19/11, gộp `main` có Task 69 nhãn/ghi chú):** gộp không xung đột văn bản nhưng build đỏ
`maxNoteRunes redeclared` (`notes.go` của 69 và `importer_rows.go`). Thay đổi của lượt 2, chỉ trong gói `customer`
(ký hiệu không xuất ra ngoài, không gói nào khác phải sửa): xóa hằng `maxNoteRunes`, `maxTagRunes` và hàm
`noteControl` của nhập; xóa hai câu `InsertImportTagLink`, `InsertImportNote` ở `queries/customer/imports.sql` (sinh
lại `internal/store/imports.sql.go` bằng `make gen`; chỉ `importer_job.go` dùng). Không đổi migration, bảng,
OpenAPI. Test chạy lại: `./internal/modules/customer/...`, `-run CustomerImport ./internal/httpapi/`, rồi cổng kiểm
của task.

**Lượt 1:**

- Ký hiệu Go xuất ra ngoài sẽ đổi: **không sửa ký hiệu có sẵn nào** (tìm bằng grep; không đổi tên, không đổi chữ ký).
  Thêm mới trong `customer`: `ImportPurposeKey`, `ImportPurpose()`, `RegisterFilePurposes()`, `ImportHeader()`,
  `ImportTemplate()`, `CheckImportFile`, `Writer.StartImport`, `GetImport`, kiểu `Import`, `ImportError`,
  `NewImporter`/`ImporterOptions`/`Importer` (`Execute`, `OnTerminal`), `ImportKind()`, hằng trạng thái `Import*`,
  hằng giới hạn `MaxImportBytes`, `MaxImportRows`, `MaxImportErrors`, `ImportBatchSize`. Móc test riêng
  (`export_test.go`): `SetAfterBatch`, `SetBudget`, `ImportJob`. Sentinel `ErrImportTooLarge`,
  `ErrImportFileInvalid` đã khai ở Task M2-02 (dùng, không đổi; map HTTP ở `errors.go` đã có).
- Gói phụ thuộc ngược (`go list`): `customer` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/order`,
  `internal/modules/promotion`, `internal/testkit/shiptest` — chỉ thêm ký hiệu; chỉ `cmd/crm` (nối lệnh, đăng ký mục
  đích) và `httpapi` (3 handler) phải sửa. `internal/platform/files` (chỉ tệp test `purpose_test.go`, gói `files_test`)
  sẽ nhập `customer` để so sổ mục đích có `customer_import` với bảng (không tạo vòng nhập; test kiến trúc chỉ xét nhập
  không-test).
- Operation OpenAPI: `getCustomerImportTemplate`, `startCustomerImport`, `getCustomerImport` đã có trong hợp đồng từ
  Task M2-02 (đang trả 501) — **không sửa `openapi.yaml`**, oasdiff không đổi. Chỉ thay stub bằng handler thật.
- Chỗ dùng trên màn hình: chưa có màn nhập khách trong `console` (task này không giao màn hình) → không chạm.
- Câu sqlc, bảng, pha migration: migration mới `0208_files_customer_import.sql` (pha mở rộng: một dòng INSERT vào
  `platform.file_purposes`). Câu mới `queries/customer/imports.sql`: chèn/đọc/cập nhật `customer.customer_imports`
  (bảng có từ 0201), tra SĐT đã có (`customer_identities`), tìm-hoặc-tạo nhãn theo tên (`customer_tags`), gắn nhãn
  (`customer_tag_links`), chèn ghi chú (`customer_notes`), đọc cả danh mục `admin_units`. Dùng lại câu có sẵn:
  `InsertCustomerProfile`, `InsertAddress`, hàm `insertIdentity` của gói. Không đổi cột; GRANT của 0201 đủ.
- Test sẽ chạy: khi viết `go test -race` cho `./internal/modules/customer/...`, `./internal/httpapi/...`,
  `./internal/platform/files/...`, `./cmd/crm/...`; cổng kiểm của task một lần trước review (chạm migration → toàn bộ
  test theo luật của cổng).

## Tái dùng

**Lượt 2:** Task 69 đã vào main, nên dùng lại phần cùng nghĩa của 69 và xóa bản riêng của nhập:
- Ghi chú: `validNote` (`notes.go`, giới hạn `maxNoteRunes` 4000, cấm ký tự điều khiển ngoài xuống dòng/tab) thay cho
  kiểm tay + `noteControl`; câu `InsertNote` (`notes.sql`) thay `InsertImportNote` (cùng câu INSERT).
- Nhãn: hằng `maxTagNameRunes` (`tags.go`) thay `maxTagRunes`; câu `InsertTagLink` (`tags.sql`, nguồn
  `TagSourceStaff`, `ON CONFLICT DO NOTHING` → không dòng = đã gắn, bỏ qua `pgx.ErrNoRows`) thay `InsertImportTagLink`.
- **Giữ riêng** (khác nghĩa): `InsertTagIfAbsent` + `TagIDsByName` — nhập tìm-hoặc-tạo nhãn theo **tên**, hàng loạt,
  trùng tên thì dùng nhãn có sẵn; `InsertTag` của 69 báo lỗi khi trùng tên (một lỗi 23505 hỏng cả transaction của lô),
  và `Writer.CreateTag`/`AddTag`/`AddNote` ghi audit + báo hiệu **từng** nhãn/khách — kế hoạch cấm audit từng dòng.
  `parseTags` (tách theo `;`, bỏ trùng, ≤ 20 nhãn) không có bản tương đương ở 69.

**Lượt 1:**

- Đã kiểm `docs/danh-muc-dung-chung.md` và tìm theo việc: đọc Excel có giới hạn (`internal/platform/xlsx.Read`, G10:
  gói đã ở `internal/platform/xlsx` từ M1 Task 38 → **không cần tách gói**, không có commit cấu trúc); ghi mẫu
  (`xlsx.Write`); đọc tệp đã tải ngoài transaction (`files.ReadContent`); kiểm tệp trong tx (`files.Status`); mục đích
  tệp (`files.RegisterPurpose`, bảng `platform.file_purposes`, `TestPurposes_GoMatchesTable`, G11); lệnh chạy ngoài
  transaction (`commands.Handler` + `queue.Kind`, mẫu `catalog.ImportValidator`); chuẩn hóa SĐT (`NormalizePhone`),
  tên (`validName`, `NormalizeName`, `validText`); savepoint (`db.Savepoint`); chèn danh tính (`insertIdentity`), hồ sơ
  (`InsertCustomerProfile`), địa chỉ (`InsertAddress`, `insertAddressParams`), audit (`Writer.auditEntity`), sự kiện
  (`Events.Emit`), lỗi HTTP (`customerError` có sẵn hai mã).
- Viết mới vì: (1) `Writer.CreateCustomer` ghi audit và báo hiệu **từng khách** — kế hoạch cấm audit từng dòng, nên
  dòng nhập dùng các khối nhỏ ở trên trong hàm riêng `importRow`, không chép lại `createIn`; (2) `ValidateUnits` chỉ
  nhận **mã** tỉnh/xã, tệp Excel ghi **tên**: đọc cả danh mục một lần vào bộ nhớ để đổi tên (bỏ dấu, có/không tiền tố
  "Tỉnh/Thành phố/Phường/Xã") hoặc mã sang mã; (3) nhãn, ghi chú: Task 69 (M2-12, chạy song song, chưa gộp) mới viết
  `AddTag`/`AddNote`; nhập cần tìm-hoặc-tạo nhãn theo **tên** hàng loạt, nên câu SQL riêng của nhập đặt trong
  `imports.sql` (không trùng tệp với Task 69). Ghi vào "Việc phát hiện thêm" để xem gộp khi Task 69 vào main.

## Soát thiết kế

Khớp kiến trúc (mục 5):
1. Chủ dữ liệu: `customer_imports`, khách, danh tính, địa chỉ, nhãn, ghi chú đều thuộc schema `customer`. Mục đích tệp
   là một dòng của `platform.file_purposes` theo cơ chế M1 (R5), không sửa CHECK.
2. Không ghi bảng module khác: chỉ ghi `customer.*`; `platform.files` chỉ đọc qua `files.Status`/`ReadContent`;
   `platform.jobs` qua `queue.Enqueue`; audit qua `identity.RecordAudit` (bên trong `auditEntity`).
3. Đọc module khác qua API công khai: `files.*` (platform). Không đọc bảng module khác.
4. Nguyên tử: tạo `customer_imports` + xếp lệnh trong một transaction (WriteTx). Mỗi lô 500 dòng một transaction kèm
   cập nhật số đếm của lần nhập (tiến độ và dữ liệu cùng commit → chạy lại tiếp đúng chỗ).
5. Không nguyên tử: sự kiện `customer.created` qua `Events.Emit` trong transaction của lô (outbox). Không báo hiệu
   realtime từng khách (được phép mất; tránh 20.000 báo hiệu).
6. Gọi ra ngoài: chỉ đọc kho tệp — trong `Execute` của lệnh, ngoài transaction (ADR-46). Lệnh chỉ đọc kho và ghi có
   điều kiện, chạy lại an toàn, nên hết giữ chỗ là chạy lại (`LeaseRetry`, như `inDoubtResolver`), không `in_doubt`.
7. Tenant: mọi câu trong `tenancy.WithTenant`; bảng có sẵn RLS; tệp tenant khác → `files.Status` không thấy → 422.
8. Module không nhập `net/http`; handler mỏng ở `handlers_customer_import.go`.
9. Đường ghi `startCustomerImport` đã có `IdempotencyKey` và `x-permission: customer.import` (M2-02).
10. Valkey: không áp dụng (không dùng).
11. Không thêm thư viện (excelize đã có, chỉ qua `internal/platform/xlsx`).
12. Theo mẫu: `catalog.ImportValidator` (lệnh ngoài transaction đọc tệp), `customer.Writer` (ghi + sự kiện + audit).

Đúng khi chạy đồng thời (mục 6):
1. Hai đường cùng sửa: SĐT là khóa (`customer_identities_value_key`); mỗi dòng trong savepoint, SĐT vừa bị chiếm bởi
   giao dịch song song → lùi savepoint, tính `skipped`. Hai lượt chạy cùng một lần nhập (giữ chỗ hết hạn khi lượt cũ
   còn chạy): cuối mỗi lô khóa dòng `customer_imports` và so số dòng đã xử lý với vị trí bắt đầu của lô; lệch → lùi cả
   lô (không cộng đôi số đếm).
2. Gửi lại: `Idempotency-Key` ở HTTP; lệnh `DedupeKey = importId`; nhập lại cùng tệp không trùng nhờ SĐT.
3. Thứ tự khóa (ADR-46 nhóm 5): lô chèn khách → danh tính → địa chỉ → liên kết nhãn rồi mới khóa `customer_imports`
   (cuối nhóm); không khóa dòng khách có sẵn. `customer_tags` chỉ chèn (`ON CONFLICT DO NOTHING`), "không khóa".
4. Lỗi tạm của database: lô chạy trong `db.RetryTx` (qua `WithTenant`); thân hàm gán lại mọi kết quả mỗi lần chạy.
   Lỗi khác trong `Execute` → `not_sent` (chạy lại theo lịch), không `in_doubt`.
5. READ COMMITTED + ràng buộc unique + khóa dòng lần nhập.
6. Việc nền lỗi: lệnh tự hẹn lại; hết lượt (`dead`) hoặc hết hạn → `OnTerminal` đặt lần nhập `failed`.
7. 429: không áp dụng (không gọi dịch vụ ngoài ngoài kho tệp).
8. Chết giữa chừng: số đếm commit cùng lô; chạy lại bỏ qua `ok+skipped+error` dòng đầu (cùng tệp, cùng thứ tự đọc);
   dù vị trí lệch thì SĐT đã có vẫn chặn trùng.
9. Treo lâu: `Execute` tự dừng sau ngân sách 20 s (hạn lệnh 30 s, ADR-26) và trả `not_sent` sau 1 s để lượt sau làm
   tiếp; mỗi lô 500 dòng là một transaction ngắn.
10. Tiền: không áp dụng (không có tiền).
11. Quy luật: "nhập lại không bao giờ tạo trùng; ok+skipped+error = số dòng" — test chết giữa chừng 1.200 dòng.
12. Dữ liệu cá nhân: lỗi dòng chỉ `{row, column, code}`; audit chỉ số đếm; `last_error` của lệnh chỉ mã ngắn; không
    log giá trị ô.
13. Quyền: `customer.import` cho cả 3 route (guard mặc định từ chối; `sale` → 403); tải tệp dùng `files.upload` có sẵn.

## Quyết định thay chủ shop

1. **Việc `customer.import` là lệnh (`commands.Handler`) nối trong `cmd/crm/newCommands`, không phải
   `queue.Handler` trong `module.go`/`JobHandlers`.** Vì: `queue.Handler.Handle` chạy trong **một** transaction và cấm
   gọi ra ngoài database, trong khi kế hoạch đòi đọc kho **ngoài** transaction (ADR-46) và **mỗi lô 500 dòng một
   transaction** — chỉ chạy được trong `Execute` của lệnh (đúng mẫu `catalog.import.validate` của M1); `background.Deps`
   cũng không có kho tệp. Chính sách: Mode command, hết giữ chỗ → chạy lại (`LeaseRetry`), 60 lượt (mỗi lượt tự dừng sau 20 s), lỗi tạm chờ 30 s, hạn 30 s.
   Phương án khác: thêm kho tệp vào `background.Deps` và tách thành nhiều việc theo lô (phức tạp hơn, đổi gói chung).
2. **Đăng ký mục đích `customer_import`** bằng hàm `customer.RegisterFilePurposes()` (trong `module.go`, gói
   `customer`), `cmd/crm` gọi trước `files.CheckPurposes`. Không dùng `init()` (luật Go của dự án cấm).
3. **Kiểm số dòng ngay khi bắt đầu:** kế hoạch đòi "tệp 20.001 dòng → 422 `import_too_large` trước khi xử lý dòng
   nào" (mã HTTP), nên `startCustomerImport` đọc và kiểm tệp (`files.ReadContent` + `xlsx.Read`, ngoài transaction)
   trước khi tạo lần nhập; tệp không phải xlsx, sai tiêu đề → 422 `import_file_invalid`. Lệnh vẫn kiểm lại (phòng hờ)
   và đặt `failed` với lỗi `{row: 0, code}` nếu gặp.
4. **Tỉnh/thành, Xã/phường nhận tên** (có dấu hay không, có hay không tiền tố "Tỉnh/Thành phố/Phường/Xã/Đặc khu") hoặc
   mã của danh mục. Ba cột địa chỉ: trống cả ba → không tạo địa chỉ; có một mà thiếu cột khác → lỗi `required` ở cột
   thiếu. Địa chỉ tạo ra là mặc định, người nhận = họ tên, SĐT = SĐT của dòng.
5. **Ngày sinh** nhận `dd/mm/yyyy` (cả `d/m/yyyy`) hoặc số ngày kiểu Excel (ô định dạng ngày); sai → `invalid_date`.
6. **Nhãn**: tách theo `;`, bỏ khoảng trắng, bỏ trùng; nhãn chưa có thì tạo màu `gray`; nhãn dài quá 50 ký tự hay quá
   20 nhãn một dòng → lỗi dòng. Liên kết nguồn `staff`. **Ghi chú**: một ghi chú, tác giả là người bắt đầu nhập.
7. **Mã lỗi dòng** (không giá trị): `required`, `invalid_name`, `invalid_phone`, `invalid_date`, `unknown_province`,
   `unknown_ward`, `ward_not_in_province`, `invalid_text`, `tag_too_long`, `too_many_tags`, và mã ô của
   `internal/platform/xlsx` (`formula_not_allowed`, `cell_too_long`). Lưu tối đa 1.000 lỗi đầu (như M1), `error_rows`
   đếm đủ. Một dòng lỗi thì không tạo gì cho dòng đó.
8. **Ô công thức thật** (phần tử `<f>` trong xlsx) vẫn bị `xlsx.Read` từ chối (`formula_not_allowed`, luật của gói
   chung, chặn chèn công thức); ô **chữ** bắt đầu bằng `=` (ví dụ `=HYPERLINK("x")` gõ dạng văn bản) được lưu nguyên
   chữ — đúng ý "đọc như chữ, không tính công thức".
9. **Audit duy nhất** `customer.import` (entity `customer.import`, mã lần nhập) ghi lúc lần nhập `done`, actor là nhân
   viên đã bắt đầu, `after` chỉ số đếm. Lúc bắt đầu không audit.

10. **(Lượt 2) Ghi chú nhập theo luật `validNote` của Task 69**: nay chấp nhận cả `\r` (xuống dòng kiểu Windows) như
    ghi chú gõ tay — một luật cho mọi ghi chú. Phương án khác: giữ luật riêng chặt hơn của nhập (hai luật cho cùng một
    cột, dễ lệch).

11. **(Lượt 2) Gộp main thành commit gộp riêng `36a6059`** (đúng bản git tự trộn, cổng commit cho qua như mọi commit
    gộp), rồi sửa lỗi build `maxNoteRunes redeclared` ở commit sau có reviewer. Vì: reviewer duyệt đúng phần sửa nhỏ
    trên nền đã gộp, không phải cả diff của main. Đổi lại: riêng commit gộp không build (chỉ lỗi trùng hằng, sửa ngay ở
    commit kế). Phương án khác: một commit gộp kèm sửa (reviewer phải duyệt cả phần của Task 69, 73, 412).

12. **(Lượt 2) Sửa test `TestImport_ResumeAfterCrash` cho khỏi phụ thuộc giờ thật** (luật Task 400): trên máy tải ~47–60,
    test đỏ hai lần (dừng ở `running` 500 rồi 1000 dòng) vì lượt chạy qua worker tự dừng theo ngân sách 20 s hoặc bị
    hạn 30 s của lệnh cắt giữa lô, rồi hẹn lượt sau mà `drain` không chờ. Sửa: `newImportEnv` đặt ngân sách 1 giờ
    (`SetBudget`, test tự dừng vẫn đặt 0) và phần chạy lại gọi thẳng `Execute` tới khi `done`. Không nới tiêu chí:
    vẫn đòi đúng 1.200 khách, 1.200 SĐT, ok 1200, một audit. Mã chạy thật không đổi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (Lượt 2) Sau khi gộp main và dùng lại `validNote`, `InsertNote`, `InsertTagLink`, `maxTagNameRunes` của Task 69: nhập vẫn tạo nhãn nguồn staff, ghi chú tác giả đúng; mọi test nhập, nhãn, ghi chú, dòng thời gian | `TestImport_*`, `TestTag*`, `TestNote*`, `TestTimeline*`; `TestCustomerImportAPI` | `go test -race -run 'Import\|Tag\|Note\|Timeline' ./internal/modules/customer/`; `go test -race -run CustomerImport ./internal/httpapi/` | httpapi `ok 16.3s`; customer: chỉ `TestImport_ResumeAfterCrash` đỏ do giờ thật (Quyết định 12), sau sửa `ok 101.3s` (chạy riêng) — kết quả cả gói ở cổng kiểm bên dưới |
| `TestPurposes_GoMatchesTable` xanh với `customer_import`; bỏ dòng INSERT của 0208 → đỏ | `TestPurposes_GoMatchesTable` (files) | `go test -race ./internal/platform/files/ -run TestPurposes` | `ok`; khi thay INSERT bằng `SELECT 1`: `FAIL … customer_import: đã RegisterPurpose, thiếu dòng trong bảng` (đã trả lại) |
| Tệp 5 dòng, dòng 3 SĐT `123`, dòng 5 phường không thuộc tỉnh → ok 3, error 2, lỗi row 3 và 5, JSON lỗi không chứa giá trị ô | `TestImport_RowErrorsAndReimport` | `go test -race ./internal/modules/customer/ -run TestImport` | `--- PASS: TestImport_RowErrorsAndReimport` |
| Nhập lại → ok 0, skipped 3, không thêm khách | `TestImport_RowErrorsAndReimport` (phần `again`) | `go test -race ./internal/modules/customer/ -run TestImport` | PASS |
| Ô `=HYPERLINK("x")` ở cột tên → lưu nguyên chữ | `TestImport_RowErrorsAndReimport` (đếm `full_name = '=HYPERLINK("x")'`) | `go test -race ./internal/modules/customer/ -run TestImport` | PASS |
| Audit đúng 1 dòng mỗi lần nhập, không audit từng dòng | `TestImport_RowErrorsAndReimport`, `TestImport_ResumeAfterCrash` | `go test -race ./internal/modules/customer/ -run TestImport` | PASS (`customer.import` = 1 rồi 2 sau lần nhập thứ hai; `customer.create` = 0) |
| Giết việc sau lô 1 của tệp 1.200 dòng rồi chạy lại → 1.200 khách, không trùng | `TestImport_ResumeAfterCrash` | `go test -race ./internal/modules/customer/ -run TestImport` | `--- PASS: TestImport_ResumeAfterCrash` (500 khách sau lô 1; 1.200 khách, 1.200 SĐT khác nhau, ok 1200 sau khi chạy lại) |
| Tệp 20.001 dòng → 422 `import_too_large` trước khi xử lý dòng nào (không tạo lần nhập) | `TestCustomerImportAPI`, `TestImport_FileChecks`, `TestImport_JobFailsOnTooLarge` | `go test -race ./internal/httpapi/ -run TestCustomerImportAPI`; `go test -race ./internal/modules/customer/ -run TestImport` | PASS |
| `fileId` là ảnh sản phẩm hoặc của tenant khác → 422 `import_file_invalid` | `TestCustomerImportAPI`, `TestImport_FileChecks` | `go test -race ./internal/httpapi/ -run TestCustomerImportAPI`; `go test -race ./internal/modules/customer/ -run TestImport` | PASS |
| `createUpload` `purpose: customer_import`, 6 MiB → 422 `file_too_large` | `TestCustomerImportAPI` | `go test -race ./internal/httpapi/ -run TestCustomerImportAPI` | PASS |
| Vai trò `sale` → 403 (cả ba route) | `TestCustomerImportAPI` | `go test -race ./internal/httpapi/ -run TestCustomerImportAPI` | PASS |
| Mẫu tệp 8 cột đúng thứ tự, tải về được | `TestImportTemplate`, `TestCustomerImportAPI` | `go test -race ./internal/modules/customer/ -run TestImport`; `go test -race ./internal/httpapi/ -run TestCustomerImportAPI` | PASS |
| Mã lỗi từng cột, ô công thức thật, SĐT trùng trong tệp → skipped, ngày dạng số Excel | `TestImport_RowCodes` | `go test -race ./internal/modules/customer/ -run TestImport` | PASS |
| Lượt tự dừng sau ngân sách, lượt sau làm tiếp đúng chỗ | `TestImport_YieldsAndResumes` | `go test -race ./internal/modules/customer/ -run TestImport` | PASS (`[not_sent/yield not_sent/yield done/]`) |
| Lệnh hết lượt → lần nhập failed; lần nhập done giữ nguyên | `TestImport_TerminalMarksFailed` | `go test -race ./internal/modules/customer/ -run TestImport` | PASS |

## Test bị xóa hoặc tắt

- Không test nào bị xóa, tắt hay nới ngưỡng. (Lượt 2) `TestImport_ResumeAfterCrash`: phần chạy lại đổi từ `e.drain()`
  (qua worker, có hạn 30 s giờ thật) sang gọi thẳng `Execute` tới khi `done`; mọi assertion giữ nguyên. Lý do:
  Quyết định 12. Đường qua worker vẫn được `TestImport_RowErrorsAndReimport` (qua `run`/`drain`) kiểm.

## Khác kế hoạch

- Việc `customer.import` là lệnh nối ở `cmd/crm`, không nằm trong `module.go` `JobHandlers` (Quyết định 1).
- Tệp Excel thử không lưu trong `internal/modules/customer/testdata/*.xlsx`: test sinh trong bộ nhớ bằng excelize
  (`book` trong `importer_test.go`) và `xlsx.Write` (test HTTP) — cùng ý G10 (sinh trong test), khỏi commit tệp nhị phân.
- Mã chia ba tệp `importer.go` (mục đích, mẫu, bắt đầu/đọc), `importer_rows.go` (kiểm dòng), `importer_job.go`
  (lệnh) thay cho một tệp `importer.go`, để mỗi hàm dưới ngưỡng gocognit.
- Thêm ngoài danh sách test của kế hoạch: tự dừng/tiếp tục theo ngân sách thời gian, `OnTerminal` → failed, lệnh gặp
  tệp quá dòng → failed, mã lỗi từng loại cột, ô công thức thật, ngày dạng số Excel.

## Việc phát hiện thêm

- Sắp các dòng của lô theo SĐT trước khi chèn (ADR-46: cùng bảng theo khóa tăng dần) để khử deadlock giữa hai lần nhập
  song song (reviewer, ghi chú không bắt buộc).
- Ô SĐT dạng số trong Excel mất số 0 đầu: màn nhập khách (sau này) nên nhắc định dạng cột SĐT là văn bản, hoặc nhập
  thêm số 0 khi ô là số 9 chữ số.
- Một lô 500 dòng phải xong trong hạn 30 s của lệnh: trên máy rất tải (load ~47) một lô gần chạm hạn; nếu máy chủ thật
  chậm như vậy thì lần nhập không tiến (mỗi lượt bị cắt ở lô đầu). Nên đo trên máy chủ và cân nhắc lô nhỏ hơn hoặc hạn
  dài hơn (Task 400 / đo hiệu năng).
- `files.TestPublicVariants` chập chờn trên `main` (hạn 200 ms khi máy tải nặng).

- ~~Khi Task 69 vào main: gộp câu nhãn/ghi chú~~ — đã làm ở lượt 2 (xem Tái dùng).

## Cổng kiểm của task

- **Lượt 2** (sau khi gộp main, đã stage; chạy một lần): build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd,
  gitleaks, oasdiff, test-guard **xanh**. Bước test toàn bộ (vì nhánh có migration 0208) ĐỎ do quá 10 phút hoặc hạn giờ
  thật khi máy tải ~44: `catalog` (TestApply_* chờ worker 30 s), `identity`, `inventory` (57014 statement timeout),
  `promotion`, `customer` — chỉ `customer` bị lượt 2 chạm. Chạy lại riêng với `-timeout 60m`:
  `go test -race -timeout 60m ./internal/modules/customer/... ./internal/modules/promotion/...` →
  `ok customer 636.3s`, `ok promotion 700.8s`. `catalog`, `identity`, `inventory` không nhập gói nào lượt 2 đổi;
  điều phối viên chạy toàn bộ test trên `main`.

**Lượt 1:**

- Lần 1 (trước khi stage): ĐỎ ở `gen-check` (tệp mới chưa stage — đúng luật so với git), `jscpd` (một đoạn lặp 6 dòng
  trong `importer_test.go`, đã gộp thành `startRows`), và `test` (log bị cắt; chạy riêng mọi gói bị cắt —
  `cmd/...`, `internal/adapters/...`, `internal/httpapi/...`, `catalog`, `cash`, `customer`, `imager` — đều `ok`).
- Lần 2 (sau khi stage): build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard
  **xanh**. `test` toàn bộ ĐỎ vì: `identity`, `inventory` quá 10 phút (lần 1 cùng hai gói qua trong 239 s, 255 s; DB
  chung nhiều phiên đang tải) và `files.TestPublicVariants` — test phụ thuộc thời gian (hạn xử lý 200 ms), **đỏ cả trên
  `main`** (worktree tạm của `main`, `-count=3`: PASS, PASS, FAIL), không liên quan task này. Chạy lại riêng
  `identity`, `inventory` với `-timeout 30m`: lệnh bị dừng sau 40 phút vì hết giờ chạy nền (máy đang tải rất nặng, load
  ~62), **chưa có kết quả lần 2**. Bằng chứng còn lại: hai gói này không nhập gói nào task đổi (`go list`), và đã `ok` ở
  lần 1 khi đã có migration 0208. Điều phối viên chạy toàn bộ test trên `main` sau khi gộp.

## Reviewer

**Lượt 2:** Kết luận **ĐẠT** (lần 1, trên phần đã stage sau commit gộp `36a6059`). Không có phát hiện phải sửa. 7 mục
đều đạt: bốn ký hiệu của Task 69 dùng lại có thật; không còn chỗ dùng tên cũ (`maxTagRunes`, `noteControl`,
`InsertImportNote`, `InsertImportTagLink`); mã sinh do `make gen`; test không bị nới (sau vòng `Execute` vẫn kiểm
done và số đếm). Ghi chú không bắt buộc: ghi chú nhập nay nhận `\r` (Quyết định 10); `created_at` liên kết nhãn lấy
giờ của lô; nhánh bỏ qua `pgx.ErrNoRows` khi gắn nhãn chỉ phòng hờ.

**Lượt 1:**

Kết luận: **ĐẠT** (lần 1, trên phần đã stage sau khi gộp main). Không có phát hiện phải sửa. Ghi chú không bắt buộc
(không ảnh hưởng tính đúng, giữ lại làm việc sau):
- Chèn danh tính SĐT theo thứ tự dòng, không theo khóa tăng dần: hai lần nhập song song có SĐT trùng khác thứ tự có thể
  deadlock 40P01; tự hồi phục (`RetryTx` chạy lại lô, hết lượt thì chờ 30 s). Có thể sắp `fresh` theo SĐT.
- Nhánh `errStaleProgress` (hai lượt cùng chạy một lần nhập) chưa có test riêng.
- Tên xã trùng trong cùng tỉnh (mơ hồ) báo `ward_not_in_province` thay vì một mã riêng.
- Ô SĐT kiểu số trong Excel mất số 0 đầu → `invalid_phone`; nên ghi chú ở mẫu tệp/màn hình sau này.
