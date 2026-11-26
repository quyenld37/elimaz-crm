# Task 510 — Sửa sau 491: quyền, nhật ký quà hết lượt, nhập sinh nhật bị trả về

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task ngoài kế hoạch, số do điều phối M5 cấp 27/11; nguồn: `docs/tasks/task-491.md` mục «Việc phát hiện thêm» 1–3). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `promotion.GiftSendFailure` thêm trường `Changed bool` (thêm, không đổi chữ ký; 2 chỗ dùng: `internal/httpapi/handlers_web_birthdays.go` WebBirthdayGiftFailed, test `internal/modules/promotion/birthday_outbox_test.go`). `customer.BirthdayImportReport` thêm trường `Rejected []BirthdayImportRejection` và kiểu mới `BirthdayImportRejection{Index int; Reason string}`, hằng `ImportRejectStopTokenTaken`; nghĩa `Conflicts` hẹp lại (chỉ "khách đã có hồ sơ khác mã"); chỗ dùng: handler WebImportBirthdays, test `internal/modules/customer/birthday_import_test.go`, `internal/httpapi/web/birthday_import_test.go`. Không đổi tên, chữ ký hàm nào.
- Gói phụ thuộc ngược (go list): customer → cmd/crm, httpapi, httpapi/web, conversation/**, order, promotion, testkit/livetest, testkit/shiptest; promotion → cmd/crm, httpapi, httpapi/web, order, wiring/webcache. Chỉ thêm trường nên các gói đó chỉ cần build.
- Operation OpenAPI bị chạm, oasdiff: `webImportBirthdays` — schema `WebBirthdayImportReport` thêm thuộc tính bắt buộc `rejected` (mảng `{index, reason}`, reason enum `stop_token_taken`) ở phía trả về: oasdiff không coi là phá vỡ. `webBirthdayGiftFailed`: hợp đồng không đổi (chỉ bỏ dòng audit thừa).
- Chỗ dùng trên màn hình: không có (hai thao tác chỉ khóa dịch vụ website gọi; `console/src/api` sinh lại theo schema mới, không màn hình nào dùng).
- Câu sqlc, bảng, pha migration: không câu SQL mới, không migration. Dùng lại `ImportBirthdayProfile`, `BirthdayProfileToken` (customer.birthday_profiles), `MarkBirthdayGiftFailed`.
- Tài liệu: `docs/quyen.md` hai dòng `web.coupons.issue`, `web.birthdays.write` (test `permissions_doc_test.go` đọc bảng; chỉ đổi cột mô tả).
- Test sẽ chạy: `TestImportBirthdays_KeepsLegacyStopToken`, `TestImportBirthdays_TakenTokenCreatesNoCustomer` (mới) ở customer; `TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock` ở promotion; `TestWebBirthday_ImportPagesFailed`, `TestWebBirthday_GiftFailedExhaustedAndTakenToken` (mới) ở httpapi/web; `TestPermissionsDoc*` ở identity; cả gói qua ci-task (customer là vùng thường; promotion là vùng rủi ro cao → cả bộ promotion).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục db: `db.Savepoint` (lùi một bước "tất cả hoặc không" trong transaction lớn); tìm "savepoint", "Rejected", "reason" trong customer, promotion.
- Dùng lại / viết mới vì: dùng lại `db.Savepoint` để lùi phần tạo khách khi mục bị từ chối (không viết SAVEPOINT tay); dùng lại `customerForPhone`, các câu sqlc có sẵn. Kiểu `BirthdayImportRejection` viết mới trong customer (báo cáo nhập của module này; promotion có báo cáo nhập mã riêng, khác lý do thay đổi → không gộp).

## Soát thiết kế

- 5.1–5.3: chủ dữ liệu không đổi (customer ghi customers, birthday_profiles; promotion ghi birthday_gifts); không đọc bảng module khác.
- 5.4: mỗi mục nhập là một savepoint trong transaction của bên gọi; mục bị từ chối lùi đúng phần của mục đó (khách, định danh, audit tạo khách), mục khác giữ nguyên; lỗi thật vẫn trả lên làm lùi cả transaction.
- 5.5, 5.6, 5.10, 5.11: không áp dụng (không sự kiện, lệnh ra ngoài, Valkey, thư viện mới).
- 5.7: mọi câu vẫn trong tx tenant của `webBirthdayWrite`/`webCouponWrite`; không bảng mới.
- 5.8, 5.9: lớp HTTP chỉ chép báo cáo; hai đường ghi đã có khóa chống trùng và `x-permission` (không đổi).
- 5.12: theo mẫu Writer của customer.
- 6.1: quà: khóa mã `FOR UPDATE` trước khi đọc số lỗi (có sẵn), nên hai lần báo cùng lúc nối tiếp nhau; `Changed` chỉ true khi câu UPDATE chạy. Nhập: unique `stop_token` + `ON CONFLICT DO NOTHING` chờ tx kia; mục thua → savepoint lùi khách vừa tạo.
- 6.2: khóa chống trùng giữ câu trả lời cũ; báo lỗi lần thứ 4 với khóa mới trả đúng trạng thái (failures 3, không retryAt), không ghi audit — idempotent theo trạng thái.
- 6.3: thứ tự khóa không đổi (ADR-46 nhóm 5 khách → birthday_profiles; nhóm 10 mã → quà); savepoint không thêm khóa mới.
- 6.4: `db.Savepoint` không bắt lỗi tạm: 40P01/55P03 trả nguyên lên RetryTx (savepoint đã lùi rồi tx lùi cả).
- 6.5–6.11: không áp dụng (không việc nền, tiền, bên ngoài).
- 6.12: lý do từ chối là mã cố định + vị trí mục, không SĐT, không mã dừng; audit không thêm trường cá nhân.
- 6.13: quyền không đổi (web.birthdays.write, web.coupons.issue); chỉ sửa tài liệu cho khớp.

## Quyết định thay chủ shop

1. Mục nhập bị trả về vì mã dừng thuộc khách khác: chọn **savepoint mỗi mục** (`db.Savepoint`) và lùi phần tạo khách, thay vì kiểm mã trước khi tạo. Vì sao: kiểm trước vẫn hở khi hai lần nhập chạy cùng lúc (mã chưa commit thì không thấy); savepoint đúng cả khi đua. Phương án khác: đọc chủ mã trước (rẻ hơn một vòng hỏi database mỗi mục nhưng hở khi đua).
2. Báo cáo nhập thêm mảng `rejected: [{index, reason}]` (reason `stop_token_taken`); mục đó **không** còn nằm trong `conflicts` (conflicts chỉ còn "khách đã có hồ sơ khác mã", vẫn có thể dừng hồ sơ của khách đó). Vì sao: hai loại khác hậu quả (một loại không ghi gì, một loại có thể dừng hồ sơ), website cần biết lý do; thêm trường trả về không phá hợp đồng. Phương án khác: giữ mục trong cả hai mảng (trùng dữ liệu) hay đổi `conflicts` thành mảng đối tượng (phá hợp đồng). Đảo ngược dễ: chỉ đổi handler và schema.
3. Quà hết lượt báo lỗi thêm: thêm `GiftSendFailure.Changed`, handler chỉ ghi audit khi `Changed`; câu trả lời như lần trước (failures 3, không retryAt, given false), vẫn 200 (không đổi thành lỗi) để website không phải xử lý ca mới.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) `docs/quyen.md` mô tả đủ `webBirthdayGiftFailed` (web.coupons.issue), `webImportBirthdays` (web.birthdays.write), khớp `x-permission` openapi | test bảng quyền `permissions_doc_test.go` (khớp `identity.Permissions`) | `go test -count=1 -run 'Doc' ./internal/modules/identity/` | `ok elimaz.com.vn/crm/internal/modules/identity 0.437s` |
| (2) Quà hết 3 lượt, báo lỗi thêm: kết quả như lần 3, `Changed` false, không ghi thêm gì; lần 1–3 `Changed` true; quà đã trao `Changed` false | `TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock` | `go test -race -count=1 -run '^TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock$' ./internal/modules/promotion/` | `--- PASS: TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock (0.21s)` |
| (2) qua HTTP: báo lỗi 5 lần (khóa khác nhau) → failures 1,2,3,3,3, retryAt chỉ ở lần 1–2; audit `send_failed` dừng ở 3 dòng | `TestWebBirthday_GiftFailedExhaustedAndTakenToken` | `go test -race -count=1 -run '^(TestWebBirthday_ImportPagesFailed\|TestWebBirthday_GiftFailedExhaustedAndTakenToken)$' ./internal/httpapi/web/` | `--- PASS: TestWebBirthday_GiftFailedExhaustedAndTakenToken (0.54s)`, `--- PASS: TestWebBirthday_ImportPagesFailed (0.57s)` |
| (3) Mã dừng thuộc khách khác: không tạo khách, không định danh, không hồ sơ, không dừng; mục hợp lệ cùng lần vẫn vào; nhập lại cùng kết quả; rejected `{index, stop_token_taken}` | `TestImportBirthdays_TakenTokenCreatesNoCustomer`, `TestImportBirthdays_KeepsLegacyStopToken` (số khách 4 → 3, conflicts `[2]`, rejected `[3]`) | `go test -race -count=1 -run '^(TestImportBirthdays_KeepsLegacyStopToken\|TestImportBirthdays_TakenTokenCreatesNoCustomer\|TestImportBirthdays_Invalid)$' ./internal/modules/customer/` | `--- PASS: TestImportBirthdays_KeepsLegacyStopToken (0.43s)`, `--- PASS: TestImportBirthdays_TakenTokenCreatesNoCustomer (0.47s)` |
| (3) qua HTTP: `rejected` có trong báo cáo (rỗng khi không có), không trong `conflicts`, số khách không đổi | `TestWebBirthday_GiftFailedExhaustedAndTakenToken`, `TestWebBirthday_ImportPagesFailed` | như trên | PASS |
| Cổng tĩnh + test gói đổi và phụ thuộc | ci-task | `ci-task.sh` (bể từ xa) | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 932917e5…)`; static success 4m13s, test (1), (2) success |
| Tự soát trước cổng | jscpd, golangci-lint, arch, tsc | `make jscpd`; `golangci-lint run ./internal/modules/customer/... ./internal/modules/promotion/... ./internal/httpapi/ ./internal/httpapi/web/`; `go test ./tests/arch/`; `npx tsc --noEmit` | `Found 0 clones`; `0 issues` (sau khi sửa revive, wrapcheck); `TestLintCleanOnRealCode` ok; tsc 0 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ca (2) và (3) qua HTTP gộp trong một test `TestWebBirthday_GiftFailedExhaustedAndTakenToken` (một env, đỡ dựng lại).
- Gộp main trước review: main không đổi từ điểm tách (932917e5), không có gì để gộp.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (agent reviewer, 27/11). Không phát hiện phải sửa. Đã soát: savepoint mỗi mục lùi sạch khách, định danh, audit tạo khách của mục bị từ chối, báo cáo chỉ cộng khi mục không lỗi; mục bị từ chối có lúc dừng không dừng nhầm hồ sơ chủ mã; `Changed` chỉ true sau câu UPDATE, handler bỏ audit khi `!Changed` (bao cả nhánh Given cũ); không dữ liệu cá nhân trong lý do từ chối. Ghi chú không bắt buộc: tên test dự kiến trong Ảnh hưởng chưa khớp tên thật → đã sửa.
