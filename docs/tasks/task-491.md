# Task 491 — Sinh nhật: outbox phân trang, quà lỗi không chặn, nhập mã dừng nhận tin cũ, nhập mã còn hạn gắn SĐT

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (dòng M5-b6 ở bảng số task toàn cục). Loại việc (quy trình mục 3): A

## Nguồn việc

Website Task 213 (`apps/storefront-crm/docs/tasks/task-213.md`, nhánh `crm/m5`): việc phát hiện 2, 6, 7 và quyết định 11;
giao việc bổ sung M5 ngày 25/11 (bốn ý (1)–(4)). Kế hoạch chỉ có dòng M5-b6 ở bảng số task toàn cục; luật nghiệp vụ lấy từ
Task 468 (QĐ40), Task 197 và việc 467 (nhập dữ liệu website) trong `2026-11-18-crm-m5-website-flash-sale.md`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `promotion.BirthdayOutbox(ctx, tx, now, limit int) ([]BirthdayGift, error)` → `BirthdayOutbox(ctx, tx, now,
    OutboxQuery) (OutboxPage, error)`: 2 chỗ gọi (`internal/httpapi/handlers_web_birthdays.go`,
    `internal/modules/promotion/birthday_run_test.go` `pendingGifts`). Sửa cả hai trong task.
  - Mới: `promotion.OutboxQuery`, `promotion.OutboxPage`, `promotion.MarkBirthdaySendFailed`, `promotion.GiftSendFailure`.
  - `promotion.ImportItem` thêm trường `BoundPhone` (1 chỗ dựng: `handlers_web_coupons.go`; test dùng tên trường nên
    không gãy).
  - Mới ở customer: `Writer.ImportBirthdays`, `customer.BirthdayImport`, `customer.BirthdayImportReport` (Task 467 dùng lại).
- Gói phụ thuộc ngược (go list): `promotion` ← `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `modules/order`,
  `wiring/webcache`; `customer` ← nhiều gói (chỉ thêm ký hiệu mới, không đổi ký hiệu cũ).
- Operation OpenAPI bị chạm, oasdiff (chỉ thêm, không phá):
  - `webBirthdayOutbox`: thêm tham số tùy chọn `cursor`, trường trả `nextCursor` (tùy chọn); mô tả thêm luật bỏ qua quà
    đang chờ thử lại / đã hết lượt.
  - Mới `webBirthdayGiftFailed` `POST /api/v1/web/birthdays/gifts/{code}/failed` (web.coupons.issue, khóa chống trùng).
  - Mới `webImportBirthdays` `POST /api/v1/web/birthdays/import` (web.birthdays.write, khóa chống trùng).
  - `WebCouponImportItem` thêm `boundPhone` tùy chọn (`webImportCoupons`).
  - Không thêm mã lỗi mới (dùng `invalid_input`, `invalid_phone`, `not_found`).
- Chỗ dùng trên màn hình: không (các operation chỉ dành khóa dịch vụ của website); `console/src/api` sinh lại theo
  `make gen` nhưng không màn nào gọi.
- Câu sqlc, bảng, pha migration:
  - Migration mới `0531_promotion_birthday_gift_failures.sql` (pha mở rộng: `ADD COLUMN` có mặc định, `GRANT UPDATE` cột
    mới) trên `promotion.birthday_gifts`: `send_failures int NOT NULL DEFAULT 0`, `retry_at timestamptz`.
  - `queries/promotion/birthday.sql`: `PendingBirthdayGifts` thêm con trỏ và lọc quà đang chờ thử lại (chỉ outbox);
    mới `LockBirthdayGiftByCode`, `MarkBirthdayGiftFailed`.
  - `queries/promotion/webbatches.sql`: `InsertImportedCoupons` thêm `bound_phones`, `CouponTermsByCodes` trả `bound_phone`.
  - `queries/customer/birthdays.sql`: mới `ImportBirthdayProfile`, `BirthdayProfileToken` (mã dừng, lúc dừng theo khách), `StopBirthdayProfileOf`.
- Test sẽ chạy: `internal/modules/promotion` (vùng rủi ro cao: `-race` cả gói ở ci-task), `internal/modules/customer`
  (các test sinh nhật), `internal/httpapi/web` (test sinh nhật, mã), `internal/httpapi` (bảng lỗi, route), `tests/arch`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về con trỏ
  keyset dùng chung cho module; `ListBatches` của promotion dùng con trỏ uuid riêng), `customer.NormalizePhone`,
  `Writer.customerForPhone`, `newStopToken`, `fieldsAudit`, `auditEntity`, `promotion.MarkGiven`, `LockBirthdayCoupons`,
  `batchPhones`, `refusableWrite`/`webBirthdayWrite`/`webCouponWrite` của httpapi.
- Dùng lại / viết mới vì:
  - Nhập sinh nhật dùng lại `customerForPhone` (tìm/tạo khách theo SĐT), `birthdayEmail` (kiểm ngày, email),
    `auditEntity`; câu chèn mới vì phải giữ mã dừng, lúc đăng ký, lúc dừng, `legacy_delivered` (câu cũ sinh mã mới và
    `created_at = now`).
  - Lỗi gửi quà: khóa mã giống `LockBirthdayCoupons` nhưng cần đọc trạng thái quà (given_at, used_count, số lần lỗi) nên
    câu riêng.
  - SĐT gắn mã nhập: `customer.NormalizePhone` như `batchPhones`.
  - Đường HTTP: `webBirthdayWrite` (đăng ký, dừng), `webCouponWrite` (đánh lỗi quà).

## Soát thiết kế

5.1 Chủ dữ liệu: lỗi gửi quà là cột của `promotion.birthday_gifts` (promotion sở hữu); hồ sơ sinh nhật nhập thuộc
`customer.birthday_profiles` (customer). 5.2 Không ghi bảng module khác: promotion chỉ đọc view
`customer.birthday_profiles_v`; nhập hồ sơ do `customer.Writer`. 5.3 Đọc qua view: giữ như cũ. 5.4 Mỗi yêu cầu một
transaction tenant (`refusableWrite`). 5.5, 5.6 không áp dụng: không sự kiện, không gọi ra ngoài (website tự gửi tin).
5.7 Bảng cũ đã có RLS; cột mới không cần chính sách mới. 5.8 Module không biết HTTP; con trỏ là chuỗi mờ do promotion
mã hóa. 5.9 Hai đường ghi mới có `IdempotencyKey`, `x-permission`. 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo
mẫu Task 468/197.

6.1 Đồng thời: đánh lỗi khóa dòng mã (`FOR UPDATE`) rồi `UPDATE birthday_gifts … WHERE` theo khóa; trao (`MarkGiven`)
cũng khóa mã trước → không mất cập nhật; quà đã trao/đã dùng không tăng lỗi. Nhập hồ sơ: `INSERT … ON CONFLICT DO
NOTHING` (khóa chính theo khách và unique mã dừng), đọc lại để phân loại; dừng nhận tin bằng `UPDATE … WHERE stopped_at
IS NULL`. 6.2 Gửi lại: khóa chống trùng (cùng khóa → câu trả lời cũ, không đếm lỗi hai lần); nhập lại bằng khóa khác →
`skipped`. 6.3 Thứ tự khóa: đánh lỗi theo ADR-46 nhóm 10 `coupons → birthday_gifts`; nhập hồ sơ xử lý theo SĐT đã
chuẩn hóa tăng dần (khách nhóm 5 → `birthday_profiles`), hai lần nhập trùng SĐT không khóa ngược. 6.4 `db.RetryTx` qua
`WriteTx`; thân hàm gán lại kết quả. 6.5 READ COMMITTED. 6.6 Không việc nền mới (lượt phát giữ nguyên). 6.7, 6.8 không
áp dụng: không gọi ra ngoài. 6.9 Theo mặc định. 6.10 Tiền: mã nhập giữ điều khoản cũ; `boundPhone` là điều khoản
(khác → xung đột, không đổi mã có sẵn). 6.11 Quy luật: test bảng "quà lỗi không chặn quà mới" (quà lỗi ở đầu, quà mới
vẫn ra trong trang đầu; hết lượt → rời outbox nhưng còn ở "Cần nhắn"); con trỏ đi hết không trùng không sót. 6.12 Dữ liệu
cá nhân: audit nhập chỉ tên trường và số đếm; xung đột trả theo vị trí mục, không SĐT; mã dừng không vào log (test).
6.13 Quyền: outbox, đánh lỗi cần `web.coupons.issue`; nhập hồ sơ `web.birthdays.write`; khóa thiếu quyền → 403 (test).

## Quyết định thay chủ shop

1. **Quà gửi lỗi**: website báo `webBirthdayGiftFailed` khi không kênh nào tới khách. CRM đếm lỗi; lỗi lần 1, 2 → quà
   rời outbox 6 giờ rồi quay lại; lỗi lần 3 → không tự gửi nữa (vẫn ở tab "Cần nhắn" cho nhân viên nhắn tay). Quà đã
   trao, đã dùng: không đổi. Phương án khác: website tự bỏ qua (không bền qua lần khởi động lại), hay CRM tự ẩn sau N
   lần đọc (đọc không phải lỗi). Đổi được bằng hai hằng số trong `birthday_list.go`.
2. **Outbox phân trang** bằng con trỏ keyset (lúc phát, mã) — trang đầu không đổi với website cũ (không gửi `cursor`).
3. **Nhập mã dừng cũ**: API `webImportBirthdays` (khóa dịch vụ, 500 mục mỗi lần) nhập nguyên hồ sơ sinh nhật của website
   (SĐT, tên, ngày, kênh, email, mã dừng giữ nguyên, lúc đăng ký, lúc dừng, cờ đơn cũ), vì Task 467 (lệnh nhập) chưa làm
   và website đã có sẵn bộ khách gọi CRM. Hàm `customer.Writer.ImportBirthdays` để Task 467 dùng lại. Hồ sơ đã có (đăng
   ký lần đầu thắng): mã dừng cũ không thay mã của CRM → trả vị trí mục trong `conflicts`; nhưng nếu website ghi khách đã
   dừng thì vẫn dừng hồ sơ CRM (tôn trọng việc khách từ chối nhận tin). Phương án khác: bảng mã dừng phụ (nhiều mã cho
   một hồ sơ) — để lại nếu chủ shop thấy cần.
4. **`boundPhone` là một phần điều khoản** của mã nhập: nhập lại cùng mã mà SĐT khác (hay mã có sẵn không gắn SĐT) →
   `conflicts`, không đổi mã có sẵn (giống điều khoản tiền khác).

5. **Tiếp tục sau tạm dừng 25/11 (27/11)**: giữ commit tạm `wip: Task 491` (2d5e97df) trong lịch sử; gộp `main` (M4,
   CI mới) bằng `git merge` không rebase. Xung đột chỉ ở `openapi.yaml` (giữ đủ khối sinh nhật của task và khối kênh
   chat M4) và mã sinh (lấy bản main rồi `make gen`). Số migration 0531 chưa bị main dùng (main dừng ở 0530) nên giữ.
   Merge chưa commit riêng được (cổng commit đòi tệp task đủ mục), nên phần gộp đi chung commit hoàn thiện (commit gộp).
   Reviewer duyệt toàn bộ phần của task: `git diff main...HEAD` cộng phần đã stage (gồm cả commit wip).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) Outbox phân trang con trỏ (lúc phát, mã): đi hết không trùng không sót; con trỏ sai → ErrInvalidInput | `TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock` (promotion) | `go test -race -count=1 -run '^(TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock\|TestImportCoupons_BoundPhone)$' ./internal/modules/promotion/` | `--- PASS: TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock (0.45s)` |
| (2) Quà lỗi không chặn: lỗi 1, 2 → rời outbox 6 giờ rồi quay lại; lỗi 3 → hết lượt, còn ở "Cần nhắn"; đã trao → không đổi; mã lạ → ErrNotFound | `TestBirthdayOutbox_PagesAndFailedGiftDoesNotBlock` (`failThreeTimes`) | như trên | PASS |
| (3) Nhập hồ sơ giữ mã dừng cũ; link dừng cũ dừng hồ sơ CRM; hồ sơ có sẵn → conflicts, website đã dừng → CRM dừng; nhập lại → skipped; audit chỉ tên trường | `TestImportBirthdays_KeepsLegacyStopToken` (customer) | `go test -race -count=1 -run '^(TestImportBirthdays_KeepsLegacyStopToken\|TestImportBirthdays_Invalid)$' ./internal/modules/customer/` | `--- PASS: TestImportBirthdays_KeepsLegacyStopToken (0.24s)` |
| (3) Đầu vào sai (0/501 mục, SĐT, mã dừng trùng/sai mẫu, thiếu lúc đăng ký, ngày sai, email) → lỗi, không ghi | `TestImportBirthdays_Invalid` | như trên | `--- PASS: TestImportBirthdays_Invalid (1.96s)` |
| (4) Mã nhập gắn SĐT: đúng SĐT áp được, SĐT khác → bound_other; nhập lại cùng SĐT → skipped; SĐT khác/bỏ SĐT → conflicts; SĐT sai → ErrInvalidPhone | `TestImportCoupons_BoundPhone` (promotion), `TestWebImportCoupons_BoundPhone` (httpapi/web) | lệnh promotion ở trên; `go test -race -count=1 -run '^(TestWebBirthday_ImportPagesFailed\|TestWebImportCoupons_BoundPhone)$' ./internal/httpapi/web/` | `--- PASS: TestImportCoupons_BoundPhone (0.42s)`, `--- PASS: TestWebImportCoupons_BoundPhone (0.51s)` |
| API: webImportBirthdays, webBirthdayOutbox (cursor/nextCursor), webBirthdayGiftFailed; 403 thiếu quyền, 404 mã lạ, gửi lại cùng khóa → câu trả lời cũ; log, audit không SĐT, mã dừng | `TestWebBirthday_ImportPagesFailed` (httpapi/web) | lệnh httpapi/web ở trên | `--- PASS: TestWebBirthday_ImportPagesFailed (0.54s)` |
| Cổng tĩnh rẻ trước cổng kiểm | jscpd, vet, golangci-lint (promotion, customer, httpapi), tests/arch | `make jscpd`; `golangci-lint run ./internal/modules/promotion/... ./internal/modules/customer/... ./internal/httpapi/...`; `go test -count=1 ./tests/arch/` | `Found 0 clones.`; `0 issues.`; `ok elimaz.com.vn/crm/tests/arch 42.870s` |
| Cổng kiểm của task (bản đã gộp main 733654b1) | toàn bộ cổng tĩnh + toàn bộ test (phần gộp đổi go.mod/testkit → "all") | `ci-task.sh` | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 6f4297303ccc…)`, run 45883730823: static, test (1), test (2) success |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có e2e cho task này)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

1. `docs/quyen.md` (dòng 146–147): mô tả `web.coupons.issue`, `web.birthdays.write` chưa nhắc báo quà gửi lỗi và nhập
   hồ sơ sinh nhật cũ (lúc đăng ký lùi ngày, cờ `legacyDelivered`).
2. `WebBirthdayGiftFailed`: khi quà đã hết lượt (3 lần) lần báo thêm không đổi gì nhưng vẫn ghi audit `send_failed`;
   có thể bỏ audit khi số lỗi không tăng.
3. `ImportBirthdays`: mục conflict vì mã dừng thuộc khách khác vẫn tạo khách (lead) theo SĐT dù không có hồ sơ.

## Reviewer

Kết luận: ĐẠT (27/11, duyệt toàn bộ phần của task: commit wip 2d5e97df + phần đã stage, so `git diff main --cached`
trong lúc gộp main 733654b1; kiểm cả phần giải xung đột openapi.yaml và mã sinh). Không phát hiện phải sửa. Đã kiểm
thêm: thứ tự khóa coupons → birthday_gifts (ADR-46 nhóm 10), con trỏ micro giây khớp timestamptz và so bằng citext như
ORDER BY, thân hàm trong RetryTx dựng lại kết quả, quyền cột của crm_app, ErrInvalidPhone có mapping. Ghi chú không bắt
buộc (không sửa, ghi ở Việc phát hiện thêm): audit send_failed vẫn ghi khi quà đã hết lượt; mục conflict vì mã dừng
thuộc khách khác vẫn tạo khách theo SĐT; `docs/quyen.md` chưa nhắc hai thao tác mới.
