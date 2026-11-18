# Task 66 — M2-09: Ghi hồ sơ khách, danh tính, địa chỉ, tìm-hoặc-tạo khách

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 66 = M2-09). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `customer.Create(ctx, tx, fullName)` giữ nguyên chữ ký (Q30), thân đổi thành lớp bọc `Writer.CreateCustomer`; chỗ gọi (sau khi gộp main): `internal/httpapi/handlers_order.go:113`, `internal/testkit/shiptest/shop.go`, `internal/httpapi/handlers_inventory_test.go`, `internal/modules/order/{env_test.go,create_test.go}`, `internal/modules/order/schema_test.go` ×3 (chạy trên schema trước 0204, đã có 0201 nên vẫn chạy), `internal/modules/customer/schema_test.go` ×3 — không phải sửa, trừ `seedBefore0201` (xem "Khác kế hoạch"). Ký hiệu mới: `Writer` (phương thức `CreateCustomer`, `Update`, `AddIdentity`, `RemoveIdentity`, `AddAddress` (trả mã địa chỉ mới), `UpdateAddress`, `DeleteAddress`, `FindOrCreateForOrder`), `Actor`, `ActorStaff/System/Service`, `Source*`, `Customer`, `Stats`, `Profile`, `Identity`, `IdentityInput`, `IdentityValue`, `Address`, `AddressInput`, `AddressPatch`, `CreateInput`, `UpdateInput`, `OrderCustomerInput`, `OrderCustomer`, `Created` (payload), `Get`, `RootOf`, `GroupMembers`, hằng `TopicCustomerCreated`. Câu sqlc M1 `InsertCustomer` bị xóa (cách mới thay cách cũ; không còn ai dùng).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/order` (sau gộp main: `create.go` dùng `NormalizePhone`, `ReverseDigits` — không đổi), `internal/testkit/shiptest`.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng viết sẵn ở Task 59). Bỏ 501 cho 8 operation: `createCustomer`, `getCustomer`, `updateCustomer`, `addCustomerIdentity`, `removeCustomerIdentity`, `addCustomerAddress`, `updateCustomerAddress`, `deleteCustomerAddress`. oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có màn khách (Task M2-28 trở đi); console không gọi 8 operation này.
- Tài liệu: `docs/views.md` thêm dòng sự kiện `customer.created` (bảng "Sự kiện công bố").
- Câu sqlc, bảng, pha migration: không migration mới. Câu mới ở `queries/customer/{write.sql,identities.sql,addresses.sql}`; `customers.sql` (M1) bỏ `InsertCustomer`. Bảng chạm: `customer.customers` (INSERT, UPDATE cột đã GRANT, khóa dòng), `customer.customer_identities` (INSERT … ON CONFLICT DO NOTHING, DELETE), `customer.customer_addresses` (INSERT, UPDATE), đọc `customer.admin_units`; ghi `platform.domain_events`, `identity.audit_logs`, khóa chống trùng.
- Test sẽ chạy: `go test -race ./internal/modules/customer/... ./internal/httpapi/... ./internal/modules/order/... ./internal/testkit/... ./tests/arch/...`, rồi cổng kiểm của task (ci-task) đúng một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (events, realtime, `identity.RecordAudit`, `logging.Phone`/`logging.Address`, textnorm); `customer.NormalizePhone`, `ReverseDigits`, `NormalizeName`, `ValidateUnits` (Task 60, 61); sentinel ở `customer/errors.go` (Task 59); `customerError`/`detailedError` ở `internal/httpapi/errors.go`; `refusableWrite` (`internal/httpapi/refusal.go`); mẫu `catalog.Writer` (sự kiện, báo hiệu, audit trong tx của bên gọi).
- Dùng lại / viết mới vì: dùng lại toàn bộ các mục trên, không khai sentinel mới. Viết mới `customer.Writer` (cùng hình `catalog.Writer`/`order.Writer`: Events, Queue, Realtime) vì `events.Registry.Emit` cần sổ consumer và hàng đợi; không gộp với writer của module khác (cùng mục đích, khác lý do đổi). `customer.Actor` khai riêng đúng như kế hoạch ghi (trùng hình với `order.Actor` có chủ ý). `IdentityValue` (kiểu tự che khi log) mới vì giá trị danh tính có thể là email, Zalo, không chỉ SĐT.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: mọi bảng ghi thuộc schema `customer`, module `customer` (04 mục 3).
- 5.2 Không ghi bảng module khác: chỉ ghi `customer.*`; `platform.domain_events` qua `events.Registry.Emit`, audit qua `identity.RecordAudit` (customer → identity được phép).
- 5.3 Đọc module khác qua view: không đọc bảng module khác (`admin_units` thuộc chính `customer`).
- 5.4 Việc nguyên tử: tạo khách + danh tính + địa chỉ + sự kiện + audit trong một transaction của bên gọi; hàm nhận `tx`, không tự mở transaction. Tạo khách nằm trong một savepoint (`tx.Begin` của pgx) để lỗi giữa chừng (danh tính bị chiếm) không để lại hồ sơ.
- 5.5 Việc không cần nguyên tử: sự kiện `customer.created` ghi qua `Emit` trong tx (outbox), consumer chạy sau commit.
- 5.6 Gọi ra ngoài: không áp dụng (không có lệnh ra ngoài).
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant` của bên gọi; tenant của sự kiện, audit lấy từ cột `tenant_id` của dòng (RETURNING), không tự đoán.
- 5.8 Nghiệp vụ không biết HTTP: module không nhập `net/http`/ogen; handler mỏng ở `handlers_customer_write.go`.
- 5.9 Đường ghi: 7 operation ghi đã có `IdempotencyKey` và `x-permission: customer.write` trong `openapi.yaml`; chạy qua `WriteTx` (`refusableWrite`).
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không.
- 5.12 Module mẫu: Task 7 (module, đọc), Task 12/27 (đường ghi, Writer tự ghi sự kiện, báo hiệu, audit).

Mục 6:

- 6.1 Hai yêu cầu sửa cùng thứ: `UPDATE … WHERE id AND version` (khách, địa chỉ); 0 dòng mà dòng có thật → `ErrVersionConflict`. Thêm/xóa danh tính, địa chỉ khóa dòng khách trước (`SELECT … FOR UPDATE`) nên "một địa chỉ mặc định" không đụng unique index khi song song. Tìm-hoặc-tạo cùng SĐT mới: `INSERT … ON CONFLICT DO NOTHING RETURNING` trên danh tính, bên thua lùi savepoint (hồ sơ vừa chèn biến mất, chưa ai thấy) rồi dùng hồ sơ của bên thắng.
- 6.2 Gửi lại: `Idempotency-Key` qua `WriteTx`; khóa tự nhiên `customer_identities_value_key`.
- 6.3 Thứ tự khóa: ADR-46 nhóm 5 — `customers` → `customer_identities` → `customer_addresses`; mọi đường khóa dòng khách trước rồi mới chạm danh tính/địa chỉ. Tìm-hoặc-tạo có mã khách và gắn SĐT khóa hồ sơ và gốc theo mã tăng dần (`lockPair`, cùng bảng theo khóa chính tăng dần) trước khi chèn danh tính và đổi lifecycle của gốc; có test hai đường song song không deadlock.
- 6.4 Lỗi tạm: `WriteTx` (db.RetryTx) chạy lại cả hàm; hàm gán lại mọi biến kết quả trong thân.
- 6.5 READ COMMITTED: dựa vào nó để câu đọc chủ SĐT sau khi `ON CONFLICT DO NOTHING` thấy dòng bên thắng vừa commit.
- 6.6–6.10: không áp dụng (không việc nền, không gọi ngoài, không tiền).
- 6.11 Quy luật: "không hồ sơ mồ côi, một SĐT một khách, không bao giờ tự gộp" có test song song 10 goroutine và test bảng các ca DH12, DH13.
- 6.12 Dữ liệu cá nhân: audit chỉ tên trường (`changed_fields`) và mã/số, không giá trị; payload sự kiện chỉ mã; lỗi không mang giá trị; kiểu `Name`, `Birthday`, `logging.Phone`, `logging.Address`, `IdentityValue` tự che; test quét log và span.
- 6.13 Quyền: đọc `customer.read`, ghi `customer.write` (guard mặc định từ chối); tenant khác → 404 (RLS).

## Quyết định thay chủ shop

- **QĐ1 — Đường ghi là phương thức của `customer.Writer{Events, Queue, Realtime}`** thay vì hàm gói như kế hoạch viết (`CreateCustomer(ctx, tx, …)`). Vì sao: `events.Registry.Emit` cần sổ consumer và hàng đợi; đúng mẫu Q-T57-1 (order, inventory) và Task 27 (catalog). Phương án khác: biến toàn cục giữ sổ (cấm, `gochecknoglobals`). Đảo ngược: đổi phương thức thành hàm nhận `Writer`.
- **QĐ2 — Lớp bọc M1 `customer.Create(ctx, tx, fullName)` phát sự kiện bằng sổ rỗng** (`events.NewRegistry()`, `queue.NewQueue()`, không báo hiệu): dòng `customer.created` vẫn vào `platform.domain_events`, chỉ không có việc consumer. Vì sao: giữ chữ ký M1 (Q30), hôm nay không consumer nào nghe `customer.created` (đã tìm trong cả kế hoạch M2–M7 và `docs/views.md`). Rủi ro ghi ở "Việc phát hiện thêm": khi có consumer, người gọi lớp bọc phải chuyển sang `Writer.CreateCustomer` (Task M2-16 sẽ thay đường gọi duy nhất trong mã chạy thật ở `handlers_order.go`).
- **QĐ3 — Bên thua cuộc đua SĐT lùi bằng savepoint** thay vì `DELETE` hồ sơ vừa chèn: `crm_app` không có quyền DELETE trên `customer.customers` (QĐ4 của Task 60); lùi savepoint cho cùng kết quả (không còn hồ sơ, không còn sự kiện, audit của lần chèn đó) mà không mở thêm quyền xóa. Phương án khác: migration GRANT DELETE (mở rộng quyền xóa khách, kém an toàn).
- **QĐ4 — Không `CustomerID` mà `PhoneIsCustomers = false`:** không tra chủ SĐT, tạo khách mới không danh tính. Vì sao: DH12 / 05 mục 1 — SĐT người nhận không phải của khách (đặt hộ mẹ) thì không được gắn đơn vào hồ sơ người khác. Kế hoạch ghi "có SĐT đã là danh tính → dùng hồ sơ đó" không nói cờ; ca test của kế hoạch ("SĐT của B → trả B") chạy với cờ bật. Phương án khác: luôn tra (gắn nhầm đơn của người đặt hộ vào hồ sơ chủ số).
- **QĐ5 — `RemoveIdentity` từ chối `fb_psid` (và `web_account`) bằng `ErrInvalidInput` (400 `invalid_input`)**: không khai sentinel mới (sentinel M2 chỉ khai ở Task 59); hợp đồng cũng chỉ cho nhân viên thêm `phone`, `email`, `zalo_uid`. Danh tính được gỡ khi thuộc bất kỳ hồ sơ nào trong nhóm của `{id}` (màn hình hiện danh tính cả nhóm).
- **QĐ6 — Đổi `lifecycle lead → customer`** đặt trên hồ sơ gốc (màn hình đọc theo gốc), tăng `version` của gốc, chỉ khi thật sự đổi; khách mới tạo từ đơn được tạo thẳng với `lifecycle = customer`.
- **QĐ8 — `AddIdentity` với giá trị đã thuộc chính nhóm** cũng trả 409 `identity_taken` (ownerId là gốc của chính nhóm): một giá trị một chủ, màn hình biết là đã có.
- **QĐ7 — `getCustomer` trả `tags` rỗng**: nhãn là Task M2-12 (69); trường bắt buộc của hợp đồng nên trả mảng rỗng.

## Bằng chứng

Lệnh chung: `go test -race -count=1 ./internal/modules/customer/... ./internal/httpapi/... ./internal/modules/order/... ./internal/testkit/... ./tests/arch/...` → tất cả `ok` (customer 50 s, httpapi 125 s, order 91 s, testkit 26 s, arch 35 s); `golangci-lint run ./internal/modules/customer/... ./internal/httpapi/...` → `0 issues`. Cổng kiểm của task (ci-task) chạy hai lần: lần 1 đỏ vì PostgreSQL chung bị khởi động lại giữa chừng (57P01, 57P03, EOF ở nhiều gói không liên quan), gen-check (thay đổi chưa stage) và jscpd (2 đoạn trùng mới — đã sửa: đổi dạng `Writer.check`, dữ liệu mẫu danh mục trong test HTTP). Lần 2: build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; test đỏ đúng hai test do môi trường dùng chung: `testkit.TestCleanupStaleDropsOnlyIdleOldDatabases` (hết giờ chờ khóa advisory dọn database thử do phiên khác giữ) và `tests/arch.TestLintCatchesPlantedMistakes` ("parallel golangci-lint is running" — hook Stop chạy lint cùng lúc); mọi gói khác `ok`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| POST tạo khách → 201, code `^KH\d{6,}$`, danh tính `+84912345678`, `name_search`, sự kiện `customer.created` Seq 1, audit `changed_fields` ⊇ {phone, full_name} không chứa `912345678` | `TestCustomerWriteAPI` (`checkCreate`), `TestCreateCustomerWithPhone` | lệnh chung | ok |
| Tạo thêm khách `"0912 345 678"` → 409 `identity_taken` có `ownerId`; không hồ sơ mồ côi | `checkCreate`, `TestCreateCustomerPhoneTaken` | lệnh chung | ok |
| Lớp bọc M1 `customer.Create(ctx, tx, "Lan")`: code KH…, root_id = id, name_search "lan", sự kiện `customer.created`; test M1 vẫn đạt | `TestCreateM1Wrapper`; `TestSchemaExtendsM1Customers` (`schema_test.go`), test của `internal/modules/order` | lệnh chung | ok |
| PATCH version cũ → 409 `version_conflict`; hai PATCH song song → một 200, một 409 | `checkUpdate`, `TestUpdateCustomer`, `TestUpdateCustomerConcurrent` | lệnh chung | ok |
| DH12: `{CustomerID: A, Phone, PhoneIsCustomers: false}` → không danh tính mới, không khách mới, `PhoneConflictWith` nil | `TestFindOrCreateForOrder/DH12…` | lệnh chung | ok |
| DH13: SĐT của B, cờ bật → A không đổi, `PhoneConflictWith = B`, không dòng `customer_merges` | `TestFindOrCreateForOrder/DH13…` | lệnh chung | ok |
| Không CustomerID, SĐT của B → B, `Created = false`; SĐT mới, cờ tắt → khách mới không danh tính phone | `TestFindOrCreateForOrder/không mã khách…` (3 ca) | lệnh chung | ok |
| 10 goroutine cùng SĐT mới `0901000001` → 1 khách, 1 danh tính, cùng CustomerID, 1 sự kiện, 1 audit | `TestFindOrCreateForOrderRace` | lệnh chung | ok |
| Phường không thuộc tỉnh → 422 `ward_not_in_province`; địa chỉ mặc định thứ hai → cũ hết mặc định; song song vẫn một mặc định | `checkAddresses`, `TestAddAddress`, `TestAddAddressConcurrentDefault`, `TestUpdateDeleteAddress` | lệnh chung | ok |
| PQ08: GET (và PATCH) khách của tenant B → 404 `not_found` | `checkGet` | lệnh chung | ok |
| Log và dấu vết của bộ test HTTP không có `0912345678`, `912345678`, `Hồng Nhung`, `Lê Lợi` | `TestCustomerWriteAPI` (cuối hàm, `testkit.CaptureLog` + `CaptureSpans`) | lệnh chung | ok |
| Nhân viên thiếu `customer.write` → 403 | `checkCreate`, `checkIdentities` | lệnh chung | ok |
| Danh tính: thêm/gỡ, không gỡ fb_psid, không gỡ của nhóm khác, không bao giờ gộp | `TestAddIdentity`, `TestRemoveIdentity`, `checkIdentities` | lệnh chung | ok |
| Tìm-hoặc-tạo (có mã khách) và AddIdentity cùng khách, cùng SĐT song song 20 vòng: không deadlock, đúng một danh tính | `TestFindOrCreateForOrderVsAddIdentityNoDeadlock` (đếm `pg_stat_database.deadlocks`) | `go test -race -count=3 -run TestFindOrCreate ./internal/modules/customer/` | ok; bỏ `lockPair` thì đỏ `deadlock: 0 → 6` (đã thử rồi trả lại) |
| Không mở transaction ngoài RetryTx (savepoint qua `db.Savepoint`), không SQL ngoài sqlc | `tests/arch` `TestTxUsageProductCode`, `TestDynSQLProductCode` | lệnh chung | ok (lần đầu đỏ, đã sửa: xem "Khác kế hoạch") |

Ghi chú trung thực: test và mã viết xen nhau trong cùng phiên; không chụp riêng bước "đỏ" cho từng test (lần chạy đầu của gói `customer` đã xanh). Bước đỏ có ghi nhận: hai test kiến trúc ở dòng cuối, và `TestCustomerWriteAPI` đỏ vì thân 409 có thêm `ownerId` (sửa assertion cho đúng hợp đồng).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi test `customer_test.go (M1, phải vẫn đạt)`: gói `customer` không có tệp này (test M1 Task 30 của khách nằm ở `schema_test.go`, `internal/modules/order/create_test.go`); task này tạo `customer_test.go` cho lớp bọc.
- Lớp bọc M1 lấy giờ bằng câu sqlc `TxNow` (`SELECT now()`), không gọi `tx.QueryRow` trong Go (luật SQL tĩnh); savepoint qua `db.Savepoint` có sẵn (luật ADR-46 cấm tự gọi `Begin`).
- `schema_test.go` `seedBefore0201`: hàm tạo khách trên database **chưa có 0201** — trước đây gọi `customer.Create`; nay `Create` ghi cột M2 nên không chạy được trên schema M1. Đổi sang chèn bằng đúng câu SQL của M1 (`InsertCustomer` cũ) qua `crm_app`; ý của test (dòng M1 có sẵn trước migrate được điền đúng) giữ nguyên; `checkM1CreateAfter` vẫn gọi `customer.Create` sau migrate.

## Việc phát hiện thêm

- Task gộp khách (M2-11, Task 68): `customer.lockPair` khi gốc đổi giữa lúc đọc và lúc khóa thì thử lại mà vẫn giữ khóa cũ, có thể khóa gốc mới ngược thứ tự tăng dần; task gộp cần xét thứ tự khóa của mình với đường này (reviewer vòng 2).

- Lớp bọc `customer.Create` phát `customer.created` với sổ rỗng (QĐ2): khi một mốc sau đăng ký consumer cho `customer.created`, phải đổi người gọi còn lại sang `Writer.CreateCustomer` (Task M2-16 thay `handlers_order.go`).
- Phần phụ thuộc dữ liệu danh mục thật: kiểm phường/tỉnh của địa chỉ dùng `ValidateUnits`; test tự nạp đơn vị mẫu mã `T…`. Khi chưa nạp danh mục thật (chờ chủ shop OK), API thêm địa chỉ trên máy thật trả 422 `unknown_admin_unit` cho mọi mã.

## Reviewer

Vòng 1: CẦN SỬA — `fororder.go` `useExisting` chèn danh tính trước khi khóa dòng khách và đổi lifecycle của gốc sau đó (ngược ADR-46 nhóm 5; deadlock được với `AddIdentity` cùng khách, cùng SĐT). Đã sửa: `lockPair` khóa hồ sơ và gốc theo mã tăng dần trước khi chèn danh tính; thêm `TestFindOrCreateForOrderVsAddIdentityNoDeadlock` (đỏ khi bỏ khóa). Ghi chú không bắt buộc của vòng 1 (IP trống trong audit — handler khác cũng không điền; `GetCustomer` dùng lại `catalogRead`; xóa địa chỉ mặc định không tự chọn địa chỉ khác) giữ nguyên.

Vòng 2: ĐẠT — không phát hiện phải sửa. Đã kiểm: thứ tự khóa (lockPair theo mã tăng dần, khớp thứ tự uuid của PostgreSQL; mọi đường khác khóa một dòng khách), không vòng chờ ở unique index danh tính, không tự gộp, không hồ sơ mồ côi (db.Savepoint), dữ liệu cá nhân, SQL qua sqlc. Ghi chú không bắt buộc: số đếm deadlock của phiên khác có thể đến trễ (test vẫn đỏ khi bỏ khóa: 0 → 6); `lockPair` thử lại khi gốc đổi giữa chừng có thể khóa gốc mới ngược thứ tự (chỉ xảy ra khi có đường gộp — ghi ở "Việc phát hiện thêm"); chưa có test song song cho hồ sơ con.

Kết luận: ĐẠT
