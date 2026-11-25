# Task 152 — Khách hàng — tìm-hoặc-tạo theo danh tính kênh, khóa hồ sơ đại diện, cờ không liên hệ, tên từ kênh

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 152 = M4-12). Loại việc (quy
trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** trong gói `customer`: kiểu `ChannelIdentity`,
  `ChannelCustomer`; phương thức `Writer.FindOrCreateByIdentity`, `Writer.SetDoNotContact`,
  `Writer.SetNameFromChannel`; hàm `LockRoot`, `IsDoNotContact`; hằng `NameSourcePlaceholder|Channel|Staff`. Không đổi
  chữ ký ký hiệu nào đã có (0 chỗ tham chiếu phải sửa). `Writer.Update` giữ chữ ký; hành vi thêm: đổi tên thì
  `name_source = 'staff'` (để tên nhân viên sửa không bị tên từ kênh đè). httpapi: thêm phương thức
  `(*handler).SetCustomerDoNotContact` (thay 501 mặc định của ogen).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/modules/order`,
  `internal/modules/promotion`, `internal/testkit/shiptest` (nhập `customer`; không chỗ nào phải sửa).
- Operation OpenAPI bị chạm, oasdiff: `setCustomerDoNotContact` (PUT `/api/v1/customers/{id}/do-not-contact`,
  `customer.write`, `IdempotencyKey`, 204) đã khai ở Task 143; task này hiện thực, **không sửa** `openapi.yaml` →
  oasdiff không đổi. Xóa dòng `setCustomerDoNotContact` khỏi `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn M4 ở Task 174–178); `console/src/api` không đổi vì hợp đồng không đổi.
- Câu sqlc, bảng, pha migration: `migrations/0403_customer_contact.sql` pha **mở rộng** (chỉ `ADD COLUMN` có
  DEFAULT, `CREATE OR REPLACE VIEW customer.customers_v` nối 2 cột vào cuối, `GRANT UPDATE` cột mới cho `crm_app`).
  Bảng `customer.customers` (cột mới `do_not_contact`, `do_not_contact_at`, `name_source`). Câu sqlc mới:
  `queries/customer/contact.sql` (`SetDoNotContact`, `GetDoNotContactOfRoot`, `SetNameFromChannel`),
  `channel_identity.sql` (`LockChannelIdentity` khóa tư vấn, `SetNameSource`), `lock.sql` (`LockCustomerRow`).
  Sửa câu có sẵn `UpdateCustomerProfile` (`queries/customer/write.sql`): thêm `name_source = CASE …` (không thêm
  tham số; `store.UpdateCustomerProfileParams` giữ nguyên). `docs/views.md`: hai cột mới của `customers_v`.
  `docs/adr/0046-thu-tu-khoa.md`: thêm mục khóa tư vấn của tìm-hoặc-tạo theo danh tính kênh.
- Tệp khác đổi theo: `internal/modules/*/internal/store/models.go`, `internal/platform/internal/store/models.go`,
  `internal/queries/{webcatalog,weborders}/internal/store/models.go` (sqlc sinh lại: struct `CustomerCustomer` thêm 3
  trường; không mã viết tay nào dùng); `internal/modules/customer/internal/store/write.sql.go` (câu sửa). Test có sẵn
  ghim cột `customers_v` phải nối 2 cột: `internal/modules/customer/schema_test.go` (`TestSchemaViewsAndRules`,
  `TestSchemaExtendsM1Customers`), `tests/arch/m2contract_test.go` (`TestM2Contract_ViewsDoc`). Helper test chung mới
  để tránh mã lặp (jscpd): `expectColumnsAppended` (schema_test.go), `expectOneCreated` (fororder_race_test.go).
- Test sẽ chạy: test mới `contact_test.go`, `channel_identity_test.go`, `lock_test.go`, `schema_test.go` (ca 0403)
  của `customer`; `handlers_customer_contact_test.go` của httpapi; `tests/arch` (m4contract, lockorder, viewsdoc);
  toàn bộ gói `customer` (vùng thường, nhưng đổi `Update`) và gói phụ thuộc qua `ci-task`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.Savepoint`,
  `tenancy.WithTenant`, `identity.RecordAudit`); trong `customer`: `Writer.create` (lõi của `CreateCustomer`),
  `insertIdentity`, `identityOwner`, `RootOf`, `NormalizeName`, `validName`, `Writer.audit`, `Writer.signal`,
  `fieldsAudit`, `lockOwner`/`LockCustomer`; khóa tư vấn mẫu `LockShipmentCOD`, `LockRoleNames`; httpapi
  `refusableWrite`, `isCustomerRefusal`.
- Dùng lại / viết mới vì:
  - `FindOrCreateByIdentity` gọi lại `Writer.create` (đúng đường của `CreateCustomer`: tên, code, audit
    `customer.create`, sự kiện `customer.created`, báo hiệu) và `insertIdentity` + `identityOwner` (đúng đường của
    `AddIdentity`). Không gọi thẳng `AddIdentity` vì `IdentityInput` không có `Scope` và `normalizeIdentity` từ chối
    `fb_psid` (nhân viên không được thêm danh tính kênh) — xem "Khác kế hoạch".
  - `LockRoot` viết mới (chưa có hàm khóa gốc theo hồ sơ con); dùng `RootOf` + câu khóa dòng.
  - `SetDoNotContact` dùng `LockRoot`, `audit`, `signal`, `fieldsAudit`.
  - `SetNameFromChannel` dùng `validName`, `NormalizeName`, `audit`, `signal`.
  - Handler dùng `refusableWrite` + `isCustomerRefusal` (như `customerWrite`, nhưng trả 204 không thân nên không đi
    qua `customerWrite` vốn đọc lại hồ sơ).

## Soát thiết kế

5.1 Dữ liệu thuộc `customer` (cờ, nguồn tên trên `customer.customers`). 5.2 Không ghi bảng module khác; `conversation`
sẽ gọi public API này (không ghi bảng `customer`). 5.3 Không đọc bảng module khác. 5.4 Mọi hàm chạy trong tx của bên
gọi (đường tin vào của Task 153 gọi trong transaction của nó). 5.5 Sự kiện `customer.created` qua `Events.Emit`
(outbox) như `CreateCustomer`. 5.6 không áp dụng: không gọi ra ngoài. 5.7 Mọi câu trong `tenancy.WithTenant`; khóa tư
vấn có `app.tenant_id` trong chuỗi băm. 5.8 Module không nhập `net/http`; handler mỏng. 5.9 PUT có `IdempotencyKey`,
`x-permission: customer.write` (Task 143). 5.10 không áp dụng. 5.11 Không thêm thư viện. 5.12 Theo mẫu `customer.Writer`.

6.1 Hai đường song song cùng danh tính: `pg_advisory_xact_lock(hash('customer.channel_identity:' || tenant || kind ||
scope || value))` trước khi tìm (05 mục 1); thêm savepoint: nếu danh tính vẫn bị chiếm (đường khác không lấy khóa) thì
lùi hồ sơ vừa tạo, trả chủ. Cờ "không liên hệ" đặt sau khi khóa dòng gốc (`LockRoot`), câu `UPDATE` có điều kiện; tên
từ kênh bằng một câu `UPDATE … WHERE name_source <> 'staff'` (không đọc ra Go rồi ghi lại), tăng `version` nên lần sửa
của nhân viên cầm version cũ gặp `version_conflict`. 6.2 PUT qua middleware chống trùng; bật cờ hai lần cho cùng kết
quả (không đổi thì không ghi audit, không tăng version). 6.3 Thứ tự khóa: khóa tư vấn trước mọi khóa dòng (ghi vào
ADR-46 mục "Khóa tư vấn"; chỉ đường này lấy khóa đó); `LockRoot` khóa dòng `customers` của gốc (đầu nhóm 5), đọc lại
gốc dưới khóa, lệch (gộp/hoàn tác song song) → khóa lại tối đa 3 lần. Đường gộp khóa mọi hồ sơ theo id tăng dần cũng ở
nhóm 5 → không vòng chờ khác nhóm. 6.4 Lỗi tạm chạy lại bởi `db.RetryTx` của bên gọi; hàm gán lại biến kết quả mỗi
lần. 6.5 READ COMMITTED + khóa dòng. 6.6–6.8 không áp dụng: không việc nền mới. 6.9 Khóa tư vấn chịu `lock_timeout`
của transaction. 6.10 không áp dụng: không tiền. 6.11 Quy luật "một danh tính một khách" kiểm bằng 20 goroutine song
song (không rapid: vùng `customer` không thuộc vùng rủi ro cao). 6.12 Audit chỉ ghi tên trường (`do_not_contact`,
`full_name`), không tên, PSID; PSID là `IdentityValue` (tự che trong log). 6.13 API cần `customer.write`, tenant khác →
404.

## Quyết định thay chủ shop

1. **`FindOrCreateByIdentity`, `SetDoNotContact`, `SetNameFromChannel` là phương thức của `Writer`** (kế hoạch viết
   hàm tự do). Vì: tạo khách cần `Events`, `Queue` (sự kiện `customer.created`) và đổi hồ sơ phải báo hiệu
   `customer.changed`, audit — đúng mẫu `Writer` của M2. Phương án khác: hàm tự do nhận `Writer` làm tham số. Đảo
   ngược: đổi bộ nhận thành tham số.
2. **Danh tính kênh chỉ nhận `Kind = fb_psid`** (lỗi `ErrInvalidInput` với loại khác), `Scope` bắt buộc. Vì: M4 chỉ
   làm Facebook; danh tính phone/email/zalo đi đường `AddIdentity` có chuẩn hóa riêng. Đảo ngược: thêm loại vào
   `channelKinds`.
3. **Nhân viên đổi tên (`Writer.Update` có `full_name` khác) đặt `name_source = 'staff'`.** Vì: tiêu chí "tên nhân
   viên sửa không bao giờ bị đè" — hồ sơ tạo từ kênh mang `placeholder`, sau khi nhân viên sửa phải thành `staff`.
4. **`SetNameFromChannel` cũng đổi khi `name_source = 'channel'`** (không chỉ `placeholder`): tiêu chí kế hoạch "gọi
   lại với tên khác → đổi". Tên trùng tên hiện tại → `changed = false`, không ghi gì.
5. **API trả 204** (hợp đồng Task 143), không 200 như chữ kế hoạch. Audit `customer.do_not_contact` ghi `before =
   {do_not_contact: <cũ>}`, `after = {do_not_contact: <mới>}` → `changed_fields = {do_not_contact}` đúng tiêu chí (cờ
   không phải dữ liệu cá nhân; không ghi `version` để `changed_fields` chỉ có tên trường). Bật/tắt trùng trạng thái
   hiện tại → 204, không audit, không tăng version. Tên từ kênh ghi audit `customer.name_from_channel` (người làm
   `system`, `changed_fields = {full_name}`, không giá trị).
6. **Cờ tắt thì `do_not_contact_at = NULL`**; bật thì giờ `now` của bên gọi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| R3: cột `customers_v` trước 0403 là tiền tố đúng thứ tự, hai cột cuối `do_not_contact`, `name_source`; khách có sẵn `false`/`staff` | `TestMigration0403CustomersView` | `go test -race -count=1 -v -run '^(TestFindOrCreateByIdentity\|TestFindOrCreateByIdentityInvalid\|TestFindOrCreateByIdentityRace\|TestMigration0403CustomersView\|TestDoNotContactFollowsRoot\|TestSetNameFromChannel\|TestLockRoot)$' ./internal/modules/customer/` | `--- PASS: TestMigration0403CustomersView (2.51s)` |
| 02 mục 3: cờ theo hồ sơ đại diện — bật A, B gộp vào A → B bị chặn; hoàn tác → B không; tắt qua hồ sơ con tắt gốc; bật lại không audit thêm; audit `changed_fields = {do_not_contact}` không tên; tenant khác 404 | `TestDoNotContactFollowsRoot` | như trên | `--- PASS: TestDoNotContactFollowsRoot (0.51s)` |
| CH08: `placeholder` → đổi tên, `name_source = channel`, `name_search` mới, version tăng; tên khác → đổi; cùng tên → không; hồ sơ `staff` (nhân viên tạo, hay nhân viên sửa tên qua `Update`) → không bao giờ đổi | `TestSetNameFromChannel` | như trên | `--- PASS: TestSetNameFromChannel (1.09s)` |
| CH08, 05 mục 1: lần đầu 1 khách `lead`, `placeholder`, 1 danh tính `fb_psid` phạm vi Page nguồn `chat`, `Created`; lần hai cùng khách, không tạo; Page khác là hồ sơ khác; danh tính thuộc hồ sơ đã gộp → hồ sơ đó và gốc mới; audit không chứa PSID | `TestFindOrCreateByIdentity` | như trên | `--- PASS: TestFindOrCreateByIdentity (1.09s)` |
| Đầu vào sai → `ErrInvalidInput`, không ghi; tenant B tạo hồ sơ riêng | `TestFindOrCreateByIdentityInvalid` | như trên | `--- PASS: TestFindOrCreateByIdentityInvalid (1.09s)` |
| 05 mục 1: 20 goroutine cùng `u-9` → đúng 1 khách, 1 danh tính, 1 lần `Created` | `TestFindOrCreateByIdentityRace` | như trên | `--- PASS: TestFindOrCreateByIdentityRace (1.10s)` |
| C11: `LockRoot(B)` (B gộp vào A) → A; transaction khác `FOR UPDATE NOWAIT` dòng A → 55P03; không có / tenant khác → `ErrNotFound` | `TestLockRoot` | như trên | `--- PASS: TestLockRoot (1.30s)` |
| API: không `customer.write` → 403 không đổi; có → 204, audit `changed_fields = {do_not_contact}` không tên; tắt gửi hai lần cùng khóa → 204 thân rỗng, một audit; không có → 404; thiếu khóa → 400; log không tên | `TestSetCustomerDoNotContactAPI` | `go test -race -count=1 -run '^(TestSetCustomerDoNotContactAPI)$' ./internal/httpapi/` | `ok  elimaz.com.vn/crm/internal/httpapi 2.285s` |
| Test M2 ghim cột `customers_v` đã nối; tên từ kênh, 0403 chạy lại sau sửa | `TestSchemaViewsAndRules`, `TestSchemaExtendsM1Customers`, `TestMigration0403CustomersView`, `TestSetNameFromChannel` | `go test -race -count=1 -run '^(TestSchemaViewsAndRules\|TestSchemaExtendsM1Customers\|TestMigration0403CustomersView\|TestSetNameFromChannel)$' ./internal/modules/customer/` | `ok  elimaz.com.vn/crm/internal/modules/customer 5.995s` |
| Hợp đồng M4: `setCustomerDoNotContact` không còn 501; view doc khớp; thứ tự khóa | `TestM4Contract_*`, `TestM2Contract_ViewsDoc`, `TestViewsDoc*`, `TestLockOrder*` | `go test -count=1 -run '^(TestM2Contract_ViewsDoc\|TestM4Contract_.*\|TestViewsDoc.*\|TestLockOrder.*\|TestViews.*)$' ./tests/arch/` | `ok  elimaz.com.vn/crm/tests/arch 2.794s` |
| Toàn bộ gói `customer` (M2) vẫn đạt; gói phụ thuộc; cổng tĩnh | cả gói | ci-task (lần cuối, xem dòng CI-TASK) | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`; `ok … customer 21.818s`, `ok … httpapi 26.370s`, `ok … order 56.545s`, `ok … promotion 44.824s`, `ok … httpapi/web 14.570s` |

## Test bị xóa hoặc tắt

Không test nào bị xóa hay tắt. Sửa assertion có sẵn (không nới quy luật):
- `TestSchemaViewsAndRules`, `TestM2Contract_ViewsDoc`: danh sách cột `customers_v` nối `do_not_contact, name_source`
  (đúng R3/Q-X2: mốc sau nối cuối, như các mốc trước đã làm với `orders_v`).
- `TestSchemaExtendsM1Customers`: kiểm 4 cột M1 là tiền tố và 11 cột M2 theo sau trong **15 cột đầu** (trước đòi đúng
  15 cột tổng; sau 0403 view có 17 cột, phần nối của 0403 kiểm ở `TestMigration0403CustomersView`).

## Khác kế hoạch

- Không gọi `AddIdentity` của M2 cho danh tính kênh mà dùng lõi `insertIdentity` (có `scope`); `AddIdentity` không
  nhận `scope` và từ chối `fb_psid` (đúng ý M2: nhân viên không thêm danh tính kênh). Gắn danh tính `fb_psid` thứ hai
  cho khách có sẵn: ghi ở "Việc phát hiện thêm".
- Phương thức của `Writer` thay hàm tự do (Quyết định 1); API 204 (Quyết định 5).

## Việc phát hiện thêm

- Cờ "không liên hệ" chỉ đọc ở hồ sơ gốc: nếu khách B đã bật cờ rồi bị gộp vào A chưa bật, sau gộp B lại nhận tin tự
  động (spec 02 mục 3: "phải dừng ngay khi khách yêu cầu"). Đúng chữ kế hoạch ("theo `root_id`"), nhưng cần chủ shop
  chọn: khi gộp mang cờ sang gốc (OR cờ của cả nhóm) hay giữ như hiện tại. Đường gộp ngoài phạm vi task này.

- Kế hoạch ghi "danh tính `fb_psid` thứ hai cho cùng khách dùng `AddIdentity` của M2", nhưng `AddIdentity` không có
  `scope` và từ chối `fb_psid`. Task cần việc này (nhắn riêng từ comment, Task 158/155) phải thêm một đường gắn danh
  tính kênh cho khách có sẵn (lõi `insertIdentity` với `scope`, cùng khóa tư vấn của task này).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, 26/11/2026, trên phần đã stage sau ci-task XANH).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế (mọi tệp, ký hiệu); bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí.
- Ghi chú không bắt buộc: (1) `TestFindOrCreateByIdentityRace` chứng minh quy luật "một danh tính một khách", không
  riêng khóa tư vấn (savepoint + `ON CONFLICT` cũng giữ quy luật); (2) cờ "không liên hệ" khi gộp — ghi ở "Việc phát
  hiện thêm"; (3) tên câu `LockCustomerRow` trùng chữ với kiểu kết quả `store.LockCustomerRow` của câu `LockCustomer`
  (biên dịch được, chỉ dễ đọc nhầm; không đổi); (4) Task 153 gọi `Writer.FindOrCreateByIdentity` (phương thức) thay
  vì hàm tự do trong chữ kế hoạch (Quyết định 1).
