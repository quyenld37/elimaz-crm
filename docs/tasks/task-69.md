# Task 69 — M2-12: Nhãn khách, ghi chú khách, dòng thời gian

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 69 = M2-12). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** trong `customer`, không đổi chữ ký có sẵn.
  Thêm: `Tag`, `TagLink`, `TagColors`, `ListTags`, `Writer.CreateTag`, `Writer.UpdateTag`, `Writer.AddTag`,
  `Writer.RemoveTag`, hằng `TagSourceStaff`, `TagSourceRule`; `Note`, `Writer.AddNote`; `TimelineItem`,
  `TimelineCursor` (`String`, `ParseTimelineCursor`), `Timeline`, hằng `TimelineKind…`. Sentinel mới trong
  `customer/errors.go`: `ErrTagNameTaken`, `ErrTagRuleManaged` (đổi sang HTTP ở `internal/httpapi/errors.go`,
  `customerError`). Kiểu `Profile` (dựng ở `customer.Get`; dùng ở `customerOut` của httpapi, 2 nơi gọi) thêm trường
  `Tags []TagLink` — thêm trường không làm hỏng chỗ dùng (không có literal `Profile{…}` ngoài `customer.Get`). Kiểu mới
  `NoteBody` (`pii.go`, `LogValue` che nội dung ghi chú). Trong httpapi: hàm riêng mới `staffNames`, `tagOut`,
  `tagWrite`. Test có sẵn `TestM2Routes_Unimplemented` (`errors_m2_test.go`) dùng đường dòng thời gian khách làm đại
  diện "route M2 chưa làm" → đổi sang `getCustomerTagRule` (Task M2-25, cùng quyền `customer.read`), không nới kiểm.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/promotion`,
  `internal/testkit/shiptest` nhập `customer`; chỉ `internal/httpapi` phải sửa (handler mới, `customerOut` điền nhãn,
  bảng lỗi).
- Operation OpenAPI bị chạm, oasdiff: hiện thực `listCustomerTags`, `createCustomerTag`, `updateCustomerTag`,
  `addCustomerTag`, `removeCustomerTag`, `addCustomerNote`, `getCustomerTimeline` (hợp đồng có từ Task M2-02, đang 501);
  `getCustomer` và các đường ghi khách trả `tags` thật thay cho mảng rỗng. `openapi.yaml` không đổi → oasdiff trống.
- Chỗ dùng trên màn hình: `console/src` chưa dùng nhãn, ghi chú, dòng thời gian khách (grep `CustomerTag`,
  `customer-tags`, `getCustomerTimeline`, `addCustomerNote` ngoài `src/api/`: 0). Không đổi console.
- Câu sqlc, bảng, pha migration: tệp mới `queries/customer/tags.sql` (danh sách, tạo, sửa có điều kiện version, gắn
  `ON CONFLICT DO NOTHING`, gỡ dòng staff của cả nhóm, đọc nhãn của nhóm), `queries/customer/notes.sql` (chèn ghi chú),
  `queries/customer/timeline.sql` (khung `UNION ALL` + keyset, đọc `ord.orders_v`, `ord.order_status_history_v` —
  ngoại lệ chỉ đọc view Q19). Bảng: `customer_tags` (chèn, sửa `name,color,updated_at,version` — đã GRANT),
  `customer_tag_links` (chèn, xóa — đã GRANT), `customer_notes` (chỉ chèn), đọc `customer_merges`, `customers`.
  Không migration mới (bảng, trigger chỉ ghi thêm, GRANT có từ `0201`).
- Test sẽ chạy: `go test -race ./internal/modules/customer/...` (không thuộc vùng rủi ro cao), `go test -race
  ./internal/httpapi/` (cả gói, gồm `TestM2Routes_Unimplemented`), rồi cổng kiểm của task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`identity.RecordAudit`,
  `realtime.Bus`, `tenancy.WithTenant`, `identity.GetStaff`); trong `customer`: `RootOf`, `GroupMembers`, `Writer`
  (`check`, `signal`, `auditEntity`), `validText`, `clampLimit`, `ListCursor` (mẫu mã hóa con trỏ), `optUUID`; trong
  httpapi: `refusableWrite`, `customerWrite`, `catalogRead`, `isCustomerRefusal`, `customerOut`; mẫu bắt unique của
  `order.sourceNameTaken` (Task M2-15).
- Dùng lại / viết mới vì: dùng lại `RootOf`, `GroupMembers`, `Writer.signal`, `auditEntity`, `validText` (tên nhãn),
  `clampLimit`, `refusableWrite`/`customerWrite` (đường ghi nhãn trả `Customer`), `catalogRead`, `identity.GetStaff`
  (tên người làm). Viết mới: `TimelineCursor` (khóa keyset 3 phần `(at, kind, ref_id)`, khác `ListCursor` 2 phần — không
  gộp vì khác khóa và khác lý do đổi), kiểm unique tên nhãn (một hàm vài dòng theo mẫu của `order`; logic nghiệp vụ ở
  lại module), câu SQL nhãn/ghi chú/dòng thời gian (chưa có câu nào làm việc này).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1 Chủ dữ liệu: nhãn, liên kết nhãn, ghi chú thuộc `customer` (bảng có từ `0201`).
- 5.2 Không ghi bảng module khác: chỉ ghi `customer.*`; audit qua `identity.RecordAudit`.
- 5.3 Đọc module khác qua view: dòng thời gian đọc `ord.orders_v`, `ord.order_status_history_v` (ngoại lệ chỉ đọc view
  của kế hoạch G9/Q19, có sẵn trong `sqlusage_test`).
- 5.4 Nguyên tử: gắn/gỡ nhãn, thêm ghi chú cùng audit và báo hiệu trong một transaction của bên gọi.
- 5.5 Không nguyên tử: báo hiệu `customer.changed` (được phép mất); không sự kiện mới (kế hoạch không yêu cầu).
- 5.6 Gọi ra ngoài: không áp dụng: không có.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant` của bên gọi; RLS; nhãn/khách tenant khác → 404.
- 5.8 Không biết HTTP: module không nhập `net/http`; handler mỏng ở `handlers_customer_tags.go`,
  `handlers_customer_timeline.go`.
- 5.9 Đường ghi: 5 đường ghi có `IdempotencyKey` và `x-permission` sẵn trong `openapi.yaml` (tạo/sửa nhãn
  `customer.tags.manage`; gắn/gỡ nhãn, ghi chú `customer.write`).
- 5.10 Valkey: không áp dụng: không dùng. 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: `Writer` của customer (audit chỉ tên trường, báo hiệu), sentinel lỗi, test `_test` trên PostgreSQL thật.

Mục 6:
- 6.1 Đồng thời: sửa nhãn là `UPDATE … WHERE version = $v` (không dòng → có nhãn thì `ErrVersionConflict`, không thì
  `ErrNotFound`); tên trùng do unique `customer_tags_name_key` quyết định. Gắn nhãn `INSERT … ON CONFLICT DO NOTHING`
  theo khóa chính `(tenant, customer, tag)`: gửi hai lần hay song song → một dòng. Gỡ: `DELETE … WHERE source =
  'staff'` — không bao giờ xóa dòng `rule`.
- 6.2 Gửi lại: `Idempotency-Key` ở mọi đường ghi; gắn nhãn tự nó cũng không đổi gì lần hai.
- 6.3 Thứ tự khóa (ADR-46 nhóm 5 và mục (e)): gắn/gỡ chỉ chạm `customer_tag_links` (khóa ngầm KEY SHARE trên dòng
  `customers` qua khóa ngoại, đứng trước `customer_tag_links` đúng thứ tự); không khóa `customer_tags`. Sửa nhãn chỉ
  chạm `customer_tags` trong transaction riêng. Ghi chú chỉ chèn. Không có hai bảng bị khóa theo thứ tự ngược.
- 6.4 Lỗi tạm thời: `WriteTx` chạy lại cả transaction; hàm ghi gán lại mọi biến trong thân, không cộng dồn.
- 6.5 Cách ly: READ COMMITTED; dòng thời gian là một câu (một ảnh chụp). Gộp chạy song song với gắn nhãn: nhãn gắn vào
  gốc lúc đọc; nếu gốc vừa bị gộp vào nhóm khác, nhãn nằm ở hồ sơ con và vẫn hiện trên gốc mới vì nhãn đọc trên **cả
  nhóm** (test "nhãn gắn vào hồ sơ con hiện trên A").
- 6.6–6.8: không áp dụng: không việc nền, không gọi ngoài; transaction tự lùi.
- 6.9 Treo: thời hạn mặc định của pool; dòng thời gian có `LIMIT` (1–100) và index sẵn (`customer_notes_customer`,
  `customer_merges_survivor`, `customers_root`).
- 6.10 Tiền: chỉ hiển thị `grand_total` (int64 đồng) dạng chuỗi trong tóm tắt đơn.
- 6.11 Quy luật: không thuộc vùng rủi ro cao; test keyset hai trang không trùng, không sót kể cả khi chèn giữa hai lần.
- 6.12 Dữ liệu cá nhân: audit ghi chú chỉ `changed_fields: ["note"]` và mã ghi chú, không nội dung (có test); tóm tắt
  dòng thời gian chỉ mã, trạng thái, tên nhãn — không SĐT, địa chỉ, tên khách; nội dung ghi chú chỉ ở trường `note`.
- 6.13 Quyền: theo `x-permission` của hợp đồng (guard mặc định từ chối); test vai trò chỉ đọc → 403 khi ghi.

## Quyết định thay chủ shop

- QĐ1: Tên nhãn trùng → 409 `already_exists` (mã có sẵn, màn hình đã có câu "Đã có mục cùng tên hoặc cùng mã"; theo
  tiền lệ Q-T72-1), sentinel mới `customer.ErrTagNameTaken`. Khác: mã mới `tag_name_taken` — phải thêm giá trị enum vào
  `Error.error`, oasdiff tính là thay đổi cho mọi operation (cần dòng duyệt). Dễ đảo: một dòng ở `errors.go`.
- QĐ2: Gỡ nhãn do quy tắc gắn → 409 `version_conflict`, sentinel mới `customer.ErrTagRuleManaged`. Lý do: màn hình chỉ
  nên cho gỡ nhãn `staff`; gặp 409 nghĩa là màn hình đang cũ, câu "Người khác vừa sửa… tải lại" đúng việc cần làm;
  không thêm mã mới vào hợp đồng (cùng lý do QĐ1). Khác: mã mới `tag_rule_managed`. Dễ đảo: một dòng.
- QĐ3: Màu nhãn kiểm trước khi ghi theo đúng 10 khóa của CHECK trong database (`TagColors`); hợp đồng nhận rộng hơn
  (`^[a-z][a-z0-9_]{0,31}$`) → khóa lạ trả 400 `invalid_input` thay vì lỗi 500 từ CHECK (ghi chú Task 60).
- QĐ4: Gỡ nhãn chưa gắn → 200 không đổi gì (DELETE lặp lại an toàn); nhãn không tồn tại → 404.
- QĐ5 (sửa sau review vòng 1): Ghi chú lưu vào **đúng hồ sơ được mở** (gốc hay hồ sơ con), theo spec 05 mục 1/B5 "gộp
  giữ nguyên customer_id ban đầu" — hoàn tác gộp thì ghi chú theo đúng hồ sơ của nó; dòng thời gian đọc theo cả nhóm;
  audit và báo hiệu theo gốc hiện tại. Khác: lưu vào gốc (như nhãn). Dễ đảo: một dòng trong `AddNote`.
- QĐ6: Khi cả nhóm có cùng nhãn ở nhiều hồ sơ, hồ sơ hiện một dòng nhãn; nguồn là `staff` nếu có ít nhất một dòng gắn tay
  (gỡ được), không thì `rule`. Mục `tag` của dòng thời gian là nhãn **đang gắn** (lúc gắn sớm nhất trong nhóm); nhãn đã
  gỡ không còn mục (bảng liên kết không giữ lịch sử; audit vẫn ghi).
- QĐ7: Mục `order_status` bỏ dòng tạo đơn (`from_status = ''`) vì đã có mục `order_created`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ trước khi có mã | cả 4 tệp test mới | `go vet ./internal/modules/customer/ ./internal/httpapi/` | `vet: internal/modules/customer/notes_test.go:15:87: undefined: customer.Note` |
| Tên nhãn trùng → 409 | `TestCustomerTagsAPI` ("tên trùng" → 409 `already_exists`); `TestTagCreate`, `TestTagUpdate` (`ErrTagNameTaken` khi tạo và khi đổi tên) | `go test -race ./internal/httpapi/ -run 'TestCustomerTagsAPI\|TestCustomerNotesTimelineAPI\|TestCustomerWriteAPI\|TestCustomerMergeAPI'` | `ok elimaz.com.vn/crm/internal/httpapi 7.287s` |
| Màu ngoài 10 khóa của database → 400, không chạm CHECK | `TestTagCreate` (`magenta` → `ErrInvalidInput`; đủ 10 màu tạo được), `TestTagUpdate` (`neon`); `TestCustomerTagsAPI` ("màu ngoài bảng màu" → 400) | như trên và `go test -race ./internal/modules/customer/` | `ok elimaz.com.vn/crm/internal/modules/customer 71.253s` |
| Sửa nhãn có version; lệch → 409 | `TestTagUpdate`, `TestCustomerTagsAPI` ("version cũ") | như trên | ok |
| `PUT` nhãn hai lần → một dòng, 200; song song 20 lần → một dòng | `TestCustomerTagsAPI` (vòng `for range 2`, đếm 1 dòng); `TestTagLinks`; `TestTagAddConcurrent` | như trên | ok |
| Gỡ nhãn `rule` → 409, dòng còn; gỡ staff chỉ xóa dòng staff | `TestCustomerTagsAPI` ("gỡ nhãn rule" → 409 `version_conflict`, đếm còn 1); `TestTagLinks` (`ErrTagRuleManaged`, dòng rule cùng nhãn còn sau khi gỡ staff) | như trên | ok |
| Nhãn gắn vào hồ sơ con (B đã gộp vào A) → hiện trên A | `TestTagLinks` (gắn qua B → dòng ở gốc A; dòng ở B hiện khi `Get` qua A và qua B); `TestCustomerTagsAPI` (hồ sơ gốc có nhãn `rule` gắn ở hồ sơ con) | như trên | ok |
| Ghi chú `UPDATE`/`DELETE` → lỗi | `TestAddNoteAppendOnly` (crm_app lỗi; chủ bảng lỗi append-only qua `CheckOwnerAppendOnly`; nội dung không đổi) | `go test -race ./internal/modules/customer/` | ok |
| Audit ghi chú không chứa nội dung | `TestAddNoteAppendOnly` (`changed_fields = '{note}'`; không dòng audit nào chứa `0909`, `18h`); `TestCustomerNotesTimelineAPI` (log không có SĐT trong ghi chú) | như trên | ok |
| Dòng thời gian 3 đơn, 2 ghi chú, 1 gộp → 6 mục giảm dần, qua A hay B như nhau | `TestTimelineGroupKeyset` | như trên | ok |
| Hai trang 3 + 3 không trùng, không sót; thêm ghi chú giữa hai lần đọc chỉ hiện ở trang đầu khi đọc lại | `TestTimelineGroupKeyset` (trang 2 = 3 mục cũ, next nil; đọc lại trang 1 = [ghi chú mới, gộp, ghi chú 2]); `TestTimelineTiedAt` (mục cùng lúc: kind rồi ref_id giảm dần; trang 1 + 2 nối đúng thứ tự) | như trên | ok |
| Cổng kiểm của task (lần 1) | lint, gen-check, jscpd, test | cổng kiểm task (`ci-task.sh`) | ĐỎ: gocognit 2 test dài, 3 `nolint` thừa, 2 đoạn trùng (catalog `ListTags`, đoạn tạo khách tenant B trong test HTTP), gen-check vì mã chưa stage; test các gói khác đều `ok`, chỉ `tests/arch` đỏ do lint. Đã sửa: tách `TestTagCreate`/`TestTagUpdate`, `TestTimelineTiedAt`, `tagOf`, `foreignCustomer` |
| Cổng kiểm của task (lần 2, sau khi sửa) | mọi cổng | cổng kiểm task | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; test chỉ đỏ `TestM2Routes_Unimplemented` (đường đại diện 501 là dòng thời gian vừa làm — reviewer vòng 1 cũng chỉ ra). Đã đổi đường đại diện |
| Gói httpapi đầy đủ sau sửa | cả gói (gồm `TestM2Routes_Unimplemented`, các test khách) | `go test -race ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 158.000s` |
| Ghi chú giữ hồ sơ được mở; tên nhãn 51 ký tự bị chặn vì độ dài, 50 ký tự tạo được | `TestAddNoteAppendOnly` (`n.CustomerID == b.ID`), `TestTagCreate` | `go test -race ./internal/modules/customer/ -run 'TestTag\|TestAddNote\|TestTimeline'`; `golangci-lint run ./internal/modules/customer/... ./internal/httpapi/...` | `ok … 7.328s`; `0 issues.` |
| Loại order_status (bỏ dòng tạo), tag, merge_undone; không dữ liệu cá nhân trong tóm tắt; tenant khác 404; con trỏ hỏng 400 | `TestTimelineKinds`; `TestCustomerNotesTimelineAPI` (limit 1, nextCursor, mục note có nội dung và tên người viết; con trỏ hỏng 400; tenant khác 404) | như trên | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tên kiểu con trỏ là `TimelineCursor` (kế hoạch: `Cursor`) vì gói đã có `ListCursor`; `Timeline` nhận `*TimelineCursor`
  và trả `*TimelineCursor` (nil khi hết) như `List`. `TimelineItem` thêm `ActorID` và `Note` (hợp đồng có `actor`,
  `note`); `Summary` là `map[string]string` (hợp đồng: `additionalProperties: string`).
- `CreateTag`, `UpdateTag`, `AddTag`, `RemoveTag`, `AddNote` là phương thức của `Writer` (mẫu đường ghi của customer:
  audit, báo hiệu), nhận `ctx, tx, …, a Actor` như các hàm ghi khác; `UpdateTag` nhận `name, color *string` (hợp đồng
  cho sửa từng trường).

## Việc phát hiện thêm

(không có)

## Reviewer

Vòng 1: CẦN SỬA — (1) `TestM2Routes_Unimplemented` lấy `/customers/{id}/timeline` làm route chưa làm, nay ra 404 → đã
đổi sang `/customer-tags/{tagId}/rule` (M2-25). Ghi chú không bắt buộc đã làm: QĐ5 theo spec B5 (ghi chú giữ hồ sơ được
mở); ca tên 51 ký tự thật (trước là ký tự NUL) và 50 ký tự; mục Ảnh hưởng thêm `NoteBody`, `staffNames`. Để nguyên:
lúc của mục `tag` là `min(created_at)` có thể đổi khi gỡ dòng staff còn dòng rule (ca hiếm, reviewer chấp nhận).

Vòng 2: ĐẠT — không còn phát hiện phải sửa; Ảnh hưởng khớp thực tế; mọi dòng bằng chứng trỏ đúng test có kiểm điều
tiêu chí nói; QĐ1–QĐ7 không trái spec/kế hoạch (QĐ5 nay khớp 05 mục 1/B5). Ghi chú không bắt buộc: lúc của mục `tag` là
`min(created_at)` (ca hiếm, chấp nhận).

Kết luận: ĐẠT
