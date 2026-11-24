# Task 468 — Sinh nhật khách chuyển hẳn vào CRM

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 468 / M5-56; QĐ40, QĐ38). Loại việc (quy trình mục 3): A

Nguồn luật đã chép từ website (`apps/storefront-giftstatus/src/server/birthday/{rules,job,service,store}.ts`):
ngày theo lịch Việt Nam (UTC+7); 29/2 năm không nhuận → 28/2; quà đến hạn khi `sinh nhật − days_before ≤ hôm nay ≤
sinh nhật` và `ngày đăng ký ≤ sinh nhật − min_lead_days`; hạn dùng `từ hôm nay tới (ngày phát theo lịch + valid_days −
1)`; một quà mỗi năm sinh nhật; tám trạng thái `manual, due, unconfirmed, given, waiting, used, expired, stopped` theo
thứ tự ưu tiên đó, rồi theo khoảng cách tới sinh nhật.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn.
  - `customer`: thêm `BirthdayInput`, `BirthdayProfile`, `Writer.RegisterBirthday`, `StopBirthday`, `BirthdayStop`, `ValidBirthday`.
  - `promotion`: thêm `BirthdaySettings`, `GetBirthdaySettings`, `Writer.SaveBirthdaySettings`, `Writer.RunBirthdayGifts`,
    `BirthdayRun`, `LastBirthdayRun`, `BirthdayGift`, `ListBirthdayTodo`, `MarkBirthdayMessaged`, `BirthdayCustomer`,
    `BirthdayCustomerQuery`, `ListBirthdayCustomers`, `BirthdayOutbox`, các hằng trạng thái `BirthdayState*`,
    `ErrRunInProgress`, `ErrInvalidGiftSettings`; `Background` (có sẵn, 1 chỗ gọi `cmd/crm`) thêm lịch
    `promotion.birthday_run` và handler (chữ ký không đổi).
  - `identity.Permissions` thêm `promotion.birthday.manage` (ScopeShop); `identity.ServiceKeyPermissions` thêm
    `web.birthdays.write`. Chỗ dùng: `permissions_test.go`, `servicekeys_test.go` (danh sách cố định), `roles.go`
    (nhãn quyền), `docs/quyen.md`.
- Gói phụ thuộc ngược (go list): `customer` ← `httpapi`, `httpapi/web`, `order`, `promotion`, `cmd/crm`,
  `testkit/shiptest`, `wiring/webcache`; `promotion` ← `httpapi`, `httpapi/web`, `order`, `cmd/crm`; `identity` ← gần như
  mọi gói (chỉ thêm phần tử vào danh sách quyền).
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm (không phá). Website (`x-auth: service_key`, `IdempotencyKey` bắt buộc
  cho đường ghi): `webRegisterBirthday` `POST /api/v1/web/birthdays` (`web.birthdays.write`), `webBirthdayStop`
  `POST /api/v1/web/birthdays/stop` (`web.birthdays.write`), `webBirthdayOutbox` `GET /api/v1/web/birthdays/outbox`
  (`web.coupons.issue`). Nhân viên: `getBirthdaySettings` `GET /api/v1/birthday-settings` (`promotion.read`),
  `saveBirthdaySettings` `PUT /api/v1/birthday-settings`, `runBirthdayGifts` `POST /api/v1/birthday-runs`,
  `markBirthdayMessaged` `POST /api/v1/birthday-gifts/messaged`, `exportBirthdayCustomers`
  `POST /api/v1/birthday-customers/export` (no-replay) — bốn cái `promotion.birthday.manage`; `listBirthdayTodo`
  `GET /api/v1/birthday-gifts/todo`, `listBirthdayCustomers` `GET /api/v1/birthday-customers` (`promotion.read`). Mã lỗi
  mới trong `x-error-codes`: `another_run_in_progress` (409), `invalid_gift_settings` (422).
- Chỗ dùng trên màn hình: chưa có màn (M5-26/Task 209 mở rộng bốn tab sau task này). `console/src/api/**` sinh lại;
  `console/src/app/errors.ts` thêm câu tiếng Việt cho hai mã lỗi mới.
- Câu sqlc, bảng, pha migration: pha mở rộng, chỉ CREATE/GRANT/VIEW. `0525_customer_birthdays.sql`:
  `customer.birthday_profiles` + view `customer.birthday_profiles_v`; `0526_promotion_birthday_settings.sql`:
  `promotion.birthday_settings`, `promotion.birthday_runs`, `promotion.birthday_gifts`. Câu mới
  `queries/customer/birthdays.sql`, `queries/promotion/birthday.sql`. ADR-46 nhóm 10 chèn `birthday_gifts` sau
  `birthday_runs`; mục "Khóa tư vấn" thêm khóa `promotion.birthday_run:<tenant>`. `docs/views.md` thêm view mới.
- Tệp phụ phải sửa theo (thực tế): `cmd/crm/main_test.go` (danh sách lịch nền có thêm `promotion.birthday_run`),
  `tests/arch/viewsdoc_test.go` (số view 41 → 42), `internal/httpapi/errors_promotion.go` + `errors_snapshot_test.go`,
  `docs/adr/0046-thu-tu-khoa.md`, `docs/views.md`, `docs/quyen.md`, mã sinh (`internal/httpapi/gen/**`, `routes_gen.go`,
  `console/src/api/**`, `internal/**/internal/store/**`).
- Test sẽ chạy: `internal/modules/customer` (`birthdays_test.go`), `internal/modules/promotion`
  (`birthday_run_test.go`), `internal/httpapi` (`handlers_birthdays_test.go`), `internal/httpapi/web`
  (`birthdays_test.go`), `internal/modules/identity` (`permissions_test`, `servicekeys_test`), `tests/arch`; cả gói qua
  `ci-task.sh` (customer, promotion, identity là vùng rủi ro cao → cả bộ của vùng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db.Savepoint,
  tenancy.WithTenant, schedules.Schedule, queue.Kind/Policy, logging.Phone), `promotion.Writer.EnsureWebBatch` (Task 197),
  `promotion.MarkGiven` (đặt `given_at`), `customer.NormalizePhone`, `customer.NormalizeName`, `customer.Writer.create`
  (tạo khách + danh tính có savepoint chống đua), `identityOwner`, mẫu lịch theo tenant `customer.tagRulesSchedule`, mẫu
  cài đặt một dòng mỗi tenant `order.SetWebIntake`, `refusableWrite`/`auditService`/`h.audit` của httpapi.
- Dùng lại / viết mới vì: phát mã dùng lại `EnsureWebBatch` (lô theo tên, gắn SĐT, mỗi mã một lượt); "Đã nhắn" và
  "website đã gửi" cùng là `coupons.given_at` (dùng lại `MarkGiven`/`webMarkCouponGiven`, không thêm cột). Viết mới:
  luật ngày sinh nhật (không có sẵn trong CRM), bảng ghi quà theo năm (khóa tự nhiên chống phát hai lần).

## Soát thiết kế

- 5.1 Chủ dữ liệu: hồ sơ sinh nhật, đồng ý nhận tin thuộc `customer` (bảng trong schema `customer`); cài đặt quà, lượt
  phát, quà theo năm thuộc `promotion` (kế hoạch, mục "Xếp module").
- 5.2 Không ghi bảng module khác: `promotion` chỉ đọc hồ sơ; đăng ký/dừng do `customer` ghi; phát mã gọi
  `EnsureWebBatch` trong cùng module.
- 5.3 Đọc qua view: `promotion` đọc `customer.birthday_profiles_v` (có `delivered_count` của gốc), thêm vào `docs/views.md`.
- 5.4 Nguyên tử: một lượt phát (khóa tư vấn, ghi lượt, mỗi nhóm hạn một savepoint gồm lô mã + dòng quà, cập nhật số
  đếm, audit) trong một transaction.
- 5.5 Sau commit: không có việc ra ngoài; gửi Zalo/email do website làm (đọc outbox, QĐ40). Báo hiệu realtime của lô do
  `EnsureWebBatch` lo.
- 5.6 Gọi ra ngoài: không áp dụng (CRM không gửi tin, không gọi Nhanh).
- 5.7 Ngữ cảnh shop: ba bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`), khóa ngoại ghép `tenant_id`; mọi câu
  chạy trong `tenancy.WithTenant`/WriteTx; lịch `PerTenant`.
- 5.8 Module không nhập `net/http`; handler mỏng ở `handlers_birthdays.go`.
- 5.9 Đường ghi: mọi POST/PUT có `IdempotencyKey` (required; xuất CSV `no-replay` vì thân chứa SĐT không được lưu theo
  khóa) và `x-permission`.
- 5.10 Không dùng Valkey.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu: cài đặt như `ord.web_intake` (chưa có dòng → mặc định version 1), lịch như `customer` tag rules, lỗi
  sentinel trong module, đổi HTTP ở `errors_promotion.go`.
- 6.1 Đồng thời: hai lượt phát cùng tenant — `pg_try_advisory_xact_lock` → lượt sau `ErrRunInProgress` (409). Lưu cài
  đặt theo `version` (khóa dòng rồi so). Đăng ký trùng SĐT song song: tạo khách có savepoint (bên thua dùng hồ sơ bên
  thắng), hồ sơ sinh nhật `INSERT … ON CONFLICT DO NOTHING` rồi đọc lại.
- 6.2 Gửi lại: Idempotency-Key; khóa tự nhiên `birthday_gifts (tenant, khách, năm)` và lô theo tên (có mã lượt) nên
  chạy lại cùng ngày không phát mã thứ hai; dừng nhận tin gọi lại giữ `stopped_at` đầu tiên.
- 6.3 Thứ tự khóa: lượt phát lấy khóa tư vấn trước mọi khóa dòng, rồi nhóm 10 (`coupon_batches` → `coupons` →
  `birthday_settings` (đọc không khóa) → `birthday_runs` → `birthday_gifts`, chỉ chèn); đăng ký: khách (nhóm 5, qua
  `create`) → `birthday_profiles` (chèn). "Đã nhắn": chỉ `coupons` theo mã tăng dần (`MarkGiven` từng mã theo thứ tự).
- 6.4 Lỗi tạm: chạy trong `db.RetryTx`/WriteTx; hàm trong tx gán lại mọi biến kết quả mỗi lần chạy.
- 6.5 READ COMMITTED + khóa tư vấn + unique.
- 6.6 Việc nền lỗi: việc `task.promotion.birthday_run` MaxAttempts 3, hết hạn sau một kỳ (kỳ sau chạy lại; lượt
  idempotent theo năm).
- 6.7 không áp dụng: không gọi bên ngoài.
- 6.8 Tiến trình chết: tx lùi, khóa tư vấn theo transaction tự nhả.
- 6.9 Một lượt phát tối đa 5.000 quà mỗi nhóm hạn (giới hạn của `EnsureWebBatch`), phần còn lại lượt sau.
- 6.10 Tiền: `amount`, `min_order` là `int64` đồng, CHECK trong database (bước 1.000).
- 6.11 Quy luật "một quà mỗi khách mỗi năm" có PK; luật ngày (29/2, qua năm) có test bảng.
- 6.12 SĐT, ngày sinh, email, mã dừng không vào log/audit (audit chỉ số đếm và mã khách); lỗi không mang giá trị; test
  đọc log của lượt phát.
- 6.13 Quyền: route website chỉ khóa có `web.birthdays.write`/`web.coupons.issue` (test 403); nhân viên đọc cần
  `promotion.read`, ghi cần `promotion.birthday.manage` (test 403).

## Quyết định thay chủ shop

1. **Bảng thứ ba `promotion.birthday_gifts`** (khách, năm sinh nhật, mã) — kế hoạch chỉ nêu hai bảng nhưng đòi "chưa có
   quà năm nay" và tab Cần nhắn theo quà; cần một dòng quà theo năm làm khóa tự nhiên. Phương án khác: suy ra từ
   `coupons.bound_phone` + tên lô (không chắc theo năm, đổi SĐT là mất). Đảo ngược: bỏ bảng ở migration sau.
2. **Mã dừng lưu nguyên (`stop_token`), không lưu băm** — website gửi link dừng trong mỗi tin quà, mà tin do website soạn
   từ outbox; chỉ có băm thì CRM không đưa được link cho website. Mã dừng chỉ cho phép "ngừng nhận tin sinh nhật" của
   một khách. Phương án khác: mã hóa bằng `fieldcrypt` (phải nối keyring vào httpapi — đổi hạ tầng) hay HMAC (thêm bí
   mật cấu hình). Đảo ngược: thêm cột băm, migration sau.
3. **Hồ sơ lưu SĐT đăng ký (`phone`)** — mã gắn SĐT (`bound_phone`) và outbox cần đúng số khách đã đăng ký; khách có
   thể có nhiều SĐT. Đăng ký tìm khách theo danh tính SĐT, chưa có thì tạo khách `lead` với danh tính SĐT (nguồn web).
4. **Mặc định cài đặt: tắt (`enabled false`)**, các số như website (50.000, đơn 0, trước 7 ngày, hạn 14, đăng ký trước 30)
   — tránh CRM và website cùng phát quà trước đêm chuyển; nhân viên bật ở màn Cài đặt quà.
5. **Mã quà chỉ dùng kênh web**, giảm cố định `amount`, đơn tối thiểu `min_order`, gắn SĐT, một lượt — như mã nhập từ
   website (Task 197). Đảo ngược: đổi mẫu trong `birthday_run.go`.
6. **Hạn dùng**: từ 0:00 hôm nay (giờ VN) tới hết ngày `ngày phát + valid_days − 1`; lượt chạy trễ làm hạn ngắn hơn hôm
   nay thì kéo tới hết hôm nay (website để hạn trước ngày bắt đầu — mã vô dụng).
7. **Tên lô** `Quà sinh nhật <ngày> – hạn <ngày> · <8 ký tự mã lượt>`: mỗi lượt một lô mới cho mỗi nhóm hạn (`EnsureWebBatch`
   cùng tên mà khác SĐT là xung đột).
8. **Quyền**: đọc (cài đặt, Cần nhắn, Tất cả khách) dùng `promotion.read`; ghi (cài đặt, Tạo mã ngay, Đã nhắn, xuất CSV)
   dùng `promotion.birthday.manage` (Chủ, Quản lý mặc định). Bán hàng muốn bấm "Đã nhắn" thì chủ shop cấp quyền ở vai trò.
9. **Cần nhắn bỏ khách đã dừng nhận tin** (website vẫn liệt kê) — tôn trọng yêu cầu dừng.
10. **Xuất CSV trả trong JSON** (`{rows, filename, csv}`, có BOM UTF-8) thay vì `text/csv`, `x-idempotency: no-replay` (thân
    có SĐT không lưu theo khóa) — hợp đồng ogen chỉ dùng JSON; màn hình tải bằng Blob.
11. **Đăng ký lần đầu thắng** (như website): đã có hồ sơ thì không đổi ngày, kênh (trả `created false` kèm ngày đã lưu).
12. **Danh sách khách phân trang bằng con trỏ vị trí** (số thứ tự đã mã hóa) vì sắp theo trạng thái tính lúc đọc.
13. **Lượt phát của lịch không ghi audit** (promotion không được nhập identity, luật ranh giới module): mỗi lượt đã có dòng
    `promotion.birthday_runs` (người kích hoạt, số quà); lượt "Tạo mã ngay" của nhân viên ghi audit `promotion.birthday.run`
    ở lớp HTTP. Phương án khác: thêm identity vào danh sách được gọi của promotion (đổi bảng 04 mục 3 — ngoài phạm vi).
14. **Cần nhắn và outbox không phân trang** (tối đa 500 quà mỗi lần, `limit`): quà chưa trao chỉ là số khách tới hạn trong
    ~1–2 tuần; outbox rút dần khi website báo đã trao.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đăng ký 10 ngày trước với `min_lead_days 30` → không quà | `TestRunBirthdayGifts_OnlyEligibleOnce` (khách 0912000002), `TestBirthdayDue_Rules` ca "đăng ký muộn" | `go test -race -count=1 -run '^(TestBirthdayDue_Rules\|TestRunBirthdayGifts_OnlyEligibleOnce)$' ./internal/modules/promotion/` | `ok … promotion 2.3s` |
| Không đơn giao, không `legacy_delivered` → không quà; có cờ đơn cũ → có quà | `TestRunBirthdayGifts_OnlyEligibleOnce` (0912000003 không, 0912000004 có) | như trên | `ok` |
| Đủ điều kiện → một mã gắn SĐT (một lượt, 50.000, đơn từ 300.000, kênh web, hạn đúng); chạy lại cùng ngày → không mã thứ hai; tắt → không phát | `TestRunBirthdayGifts_OnlyEligibleOnce` | như trên | `ok` |
| Hai lượt cùng lúc → một chạy, một `ErrRunInProgress`; HTTP 409 `another_run_in_progress` | `TestRunBirthdayGifts_OneAtATime`, `TestBirthdayAPI_SettingsRunTodoExport` | `go test -race -count=1 -run '^TestRunBirthdayGifts_OneAtATime$' ./internal/modules/promotion/`; `go test -race -count=1 -run '^TestBirthdayAPI_SettingsRunTodoExport$' ./internal/httpapi/` | `ok`; `ok … httpapi 3.9s` |
| Lịch ngoài 9:00–21:00 không phát (22:00, 8:59 không; 9:00, 20:59 có), lượt đang chạy → việc xong không lỗi, kết quả không có SĐT | `TestBirthdaySchedule_Window` | `go test -race -count=1 -run '^TestBirthdaySchedule_Window$' ./internal/modules/promotion/` | `ok` |
| Luật ngày: 29/2 năm thường, qua năm, hạn không trước hôm nay | `TestBirthdayDue_Rules` (10 ca) | như dòng 1 | `ok` |
| Mệnh giá 10.500 → 422 `invalid_gift_settings`; version cũ → 409 | `TestBirthdaySettings_SaveValidate` (8 ca giới hạn), `TestBirthdayAPI_SettingsRunTodoExport` | như trên | `ok` |
| Khách đã dừng → không vào outbox; outbox chỉ khách đồng ý kênh; đã trao → rời outbox; Cần nhắn có cả khách không đồng ý; Đã nhắn ghi lúc trao | `TestBirthdayOutboxTodoMessaged`, `TestWebBirthday_RegisterOutboxStop` | `go test -race -count=1 -run '^TestBirthdayOutboxTodoMessaged$' ./internal/modules/promotion/`; `go test -race -count=1 -run '^TestWebBirthday_RegisterOutboxStop$' ./internal/httpapi/web/` | `ok`; `ok … web 2.5s` |
| Tám trạng thái như admin, đếm theo trạng thái/tháng, lọc, tìm, trang | `TestListBirthdayCustomers_States` | `go test -race -count=1 -run '^TestListBirthdayCustomers_States$' ./internal/modules/promotion/` | `ok` |
| Đăng ký lần đầu thắng, tạo khách theo SĐT, dùng khách có sẵn; đầu vào sai; dừng nhận tin giữ lúc đầu, mã lạ 404 | `TestRegisterBirthday_NewCustomerFirstWins`, `TestRegisterBirthday_ExistingCustomerByPhone`, `TestRegisterBirthday_Invalid`, `TestStopBirthday`, `TestWebBirthday_RegisterOutboxStop` | `go test -race -count=1 -run '^(TestRegisterBirthday_NewCustomerFirstWins\|TestRegisterBirthday_ExistingCustomerByPhone\|TestRegisterBirthday_Invalid\|TestStopBirthday)$' ./internal/modules/customer/` | `ok … customer 2.3s` |
| Xuất CSV có audit (số dòng), no-replay → 409 `already_processed`; quyền: bán hàng ghi/xuất → 403, khóa thiếu quyền → 403 | `TestBirthdayAPI_SettingsRunTodoExport`, `TestWebBirthday_RegisterOutboxStop` | như trên | `ok` |
| SĐT không vào log, audit (cả mã dừng, email) | `TestWebBirthday_RegisterOutboxStop` (`checkNoPII`), `TestRegisterBirthday_NewCustomerFirstWins`, `TestBirthdayAPI_SettingsRunTodoExport` | như trên | `ok` |
| Quyền mới, nhãn quyền, mã lỗi có câu tiếng Việt | `identity` test quyền và khóa dịch vụ; `errors_snapshot_test`; `console/src/app/errors.test.ts` | `go test -count=1 -run 'Permission\|ServiceKeyPermissions\|Roles\|Label' ./internal/modules/identity/`; `npx vitest run src/app/errors.test.ts` | `ok`; `17 passed` |
| Cổng tĩnh và test của gói bị ảnh hưởng | ci-task | `scripts/agent/ci-task.sh` | lần 1: mọi cổng tĩnh xanh, test kiến trúc xanh, `-race` gói sửa xanh; đỏ `cmd/crm TestNewModulesRegistersCash` (danh sách lịch nền cố định thiếu `promotion.birthday_run`) → sửa danh sách; lần 2 (sau gộp main 0e285c0): `CI-TASK XANH` — build, vet, lint, gen-check, sqlc-vet, jscpd, gitleaks, oasdiff, test-guard, console-check, kiến trúc xanh; `-race` `ok cmd/crm 6.4s`, `ok httpapi 45.9s`, `ok httpapi/web 27.3s`, `ok customer 40.7s`, `ok identity 47.1s`, `ok promotion 36.0s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `promotion.birthday_gifts`, cột `phone`, `stop_token` (thay `stop_token_hash`) — xem Quyết định 1–3.
- `exportBirthdayCustomers` là `POST …/export` trả JSON (Quyết định 10).
- Handler website đặt ở `internal/httpapi/handlers_web_birthdays.go` (như `handlers_web_coupons.go` của Task 197), không
  tạo `internal/httpapi/web/birthdays.go`; test website ở `internal/httpapi/web/birthdays_test.go` như kế hoạch.
- Đọc cài đặt, Cần nhắn, Tất cả khách cần `promotion.read` (kế hoạch chỉ nêu quyền ghi).

## Việc phát hiện thêm

- Website cần biết một SĐT đã đăng ký hay đã từ chối để hiện form (trạng thái `declined` của website không chuyển vào
  CRM): M5-30 quyết định giữ ở website hay thêm operation đọc.
- Nhập hồ sơ cũ (`legacy_delivered`, mã dừng cũ) từ `birthday.sqlite` thuộc Task 466/467 (cột đã có, nguồn `import`).

- (Reviewer, ghi chú không bắt buộc) Vai trò tự tạo chỉ có `promotion.read` sẽ thấy SĐT, email, ngày sinh qua Cần nhắn và
  Tất cả khách (vai trò mẫu đều có kèm `customer.read`): chủ shop quyết có đổi sang `customer.read` hay sửa nhãn quyền.
- (Reviewer) Tìm khách sinh nhật bằng SĐT gõ đầu `0` (vd `0912…`) không ra vì so với dạng `+84…`
  (`birthday_list.go`): nên chuẩn hóa chuỗi tìm bằng `customer.NormalizePhone` — task sau (M5-26/Task 209 làm màn).
- (Reviewer) Trạng thái `due` vẫn hiện khi đang tắt quà; Cần nhắn cắt 500 theo `created_at` trước khi sắp theo sinh nhật;
  hai hồ sơ cùng SĐT (sau gộp khách) có thể nhận hai quà; `ListBirthdayCustomers` nạp mọi hồ sơ vào bộ nhớ — đo lại khi lớn.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 24/11, trên bản đã gộp main 0e285c0). Phát hiện phải sửa: không có. Ảnh hưởng khớp
(sửa tên `BirthdayTodo` → `BirthdayGift`, thêm `customer.BirthdayStop`); bảng bằng chứng: mọi dòng có test thật kiểm
đúng điều tiêu chí nói; không test nào bị xóa, tắt, nới; 14 quyết định thay chủ shop an toàn, không trái spec. Ghi chú
không bắt buộc chép vào "Việc phát hiện thêm".
