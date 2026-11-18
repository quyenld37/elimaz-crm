# Task 67 — M2-10: Tìm khách dưới RLS

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 67 = M2-10). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM, không đổi ký hiệu có sẵn → 0 chỗ tham chiếu phải sửa. Mới trong `internal/modules/customer`: `SearchHit`, `Search`, `SearchTagged`, hằng `MatchedBy*`, `List`, `ListFilter`, `ListCursor` (+`String`), `ParseListCursor`, `DefaultSearchLimit`, `MaxListLimit`. `customer.ErrQueryTooShort` đã có (Task M2-02), đã ánh xạ 422 `query_too_short` ở `internal/httpapi/errors.go`. Trong `internal/httpapi`: phương thức `(*handler).ListCustomers` thay bản `UnimplementedHandler` (501).
- Gói phụ thuộc ngược (go list): gói nhập `internal/modules/customer`: `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/promotion`, `internal/testkit/shiptest` — không gói nào phải sửa (chỉ thêm ký hiệu).
- Operation OpenAPI bị chạm, oasdiff: `listCustomers` (GET /api/v1/customers, `x-permission: customer.read`) — hợp đồng đã có từ Task M2-02 (`CustomerPage`, `CustomerListItem.matchedBy`); task này chỉ hiện thực, không đổi schema → oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có màn hình khách trong `console/` (task màn hình M2 sau) → không chạm.
- Câu sqlc, bảng, pha migration: migration `0207_customer_search_fn.sql` pha mở rộng (chỉ CREATE FUNCTION, CREATE INDEX, quyền): hàm `customer.search_by_name` chủ `crm_search`; index danh sách gốc `(tenant_id, created_at DESC, id DESC) WHERE id = root_id`; `GRANT SELECT ON customer.customers TO crm_search`. Câu mới `queries/customer/search.sql`: tìm theo mã, SĐT đủ, đuôi số (bảng `customer.customers`, `customer.customer_identities`), gọi hàm tên, đọc dòng gốc + SĐT của nhóm, danh sách keyset có lọc nhãn (`customer.customer_tag_links`). Không đổi bảng.
- Test sẽ chạy: `go test -race ./internal/modules/customer/...` (search_test, search_explain_test + test có sẵn), `go test -race -run 'CustomerSearch|M2Routes|ErrorMap' ./internal/httpapi/`, rồi cổng kiểm của task (ci-task). Test có sẵn bị chạm: `TestM2Routes_Unimplemented` (dùng listCustomers làm ví dụ route 501) — xem mục Test bị xóa hoặc tắt.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (textnorm.Fold, tenancy.WithTenant, testkit.ExplainAsApp / EnableAutoExplain / RequireNoSeqScan); mẫu hàm tìm SECURITY DEFINER `catalog.search_variants` (0123/0129) và phép thử `internal/platform/tenancy/search_test.go`; con trỏ `queries.Cursor` (products_dynsql.go); `customer.NormalizePhone`, `customer.ReverseDigits`, `customer.NormalizeName`, `Stats` của customer.
- Dùng lại / viết mới vì: dùng lại `NormalizePhone` (SĐT đủ), `ReverseDigits` (đảo đuôi), `NormalizeName` → `textnorm.Fold` (tên), `ErrQueryTooShort`, `Stats` + cách tính `ReturnRateBP`, `catalogRead` (WithTenant đọc) và `statsOut` của httpapi, testkit explain. Viết mới `ListCursor` trong customer thay vì dùng `queries.Cursor`: khóa khác (created_at chứ không updated_at), module customer không nhập `internal/queries` (gói đọc chéo của lớp trên), lý do thay đổi khác nhau → giữ riêng. jscpd báo bản đầu (cùng cách mã hóa chữ) là trùng với `queries.ParseCursor`: đổi sang định dạng riêng 24 byte nhị phân (micro giây + id) base64url, ngắn hơn và kiểm độ dài chặt. jscpd cũng báo bộ đổi `*uuid.UUID → pgtype.UUID` trùng `queue.pgUUID` (gói platform riêng, không xuất ra) → viết gọn 3 dòng trong customer; và đoạn mở pool của test trùng `handlers_health_test` → mở hai pool bằng một vòng lặp. Hàm SQL viết mới theo đúng mẫu catalog (thoát `\ % _`, `NULLIF(current_setting(...))`, chủ crm_search) vì bảng khác.

## Soát thiết kế

- 5.1 Chủ dữ liệu: bảng khách thuộc module customer; hàm, câu, index đều trong schema `customer`.
- 5.2 Không ghi bảng module khác: chỉ đọc.
- 5.3 Đọc module khác qua view: không đọc module khác (nhãn là bảng của chính customer).
- 5.4, 5.5, 5.6: không áp dụng (đường đọc, không transaction nghiệp vụ, không sự kiện, không gọi ra ngoài).
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant` (handler qua `catalogRead`); hàm SECURITY DEFINER chạy bằng `crm_search` (BYPASSRLS) nên TỰ lọc `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid` (ngoài ngữ cảnh → NULL → 0 dòng), `search_path` cố định, mọi tên viết đủ schema; `REVOKE ALL … FROM PUBLIC`, chỉ `crm_app` EXECUTE. Index mới bắt đầu bằng `tenant_id`. Test: tenant B không thấy khách của A; gọi ngoài ngữ cảnh 0 dòng không lỗi.
- 5.8 Nghiệp vụ không biết HTTP: customer không nhập net/http; handler mỏng.
- 5.9 Đường ghi: không có (GET).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12 Module mẫu: theo identity/catalog (store sqlc, test gói `_test` trên PostgreSQL thật).
- 6.1–6.8, 6.10, 6.11: không áp dụng (chỉ đọc, không khóa, không tiền, không việc nền). 6.9: câu đọc chịu giới hạn 30 s chung; test kế hoạch chứng minh không Seq Scan trên 100.000 khách.
- 6.12 Dữ liệu cá nhân: không log `q` (có thể là SĐT hay tên); lỗi trả về không chứa `q`; `SearchHit.FullName` kiểu `Name`, `Phones` kiểu `logging.Phone` (tự che). Test HTTP kiểm log/span không chứa SĐT, tên.
- 6.13 Quyền: `x-permission: customer.read` (guard mặc định từ chối); test nhân viên không có `customer.read` → 403.
- Ghi chú Task 60 (khách tạo bằng hàm M1 chưa có name_search): đã kiểm lại — đường chèn duy nhất của mã chạy là `InsertCustomerProfile` (queries/customer/write.sql) đặt `name_search`; `customer.Create` của M1 bọc `Writer.CreateCustomer` từ Task 66; dòng có sẵn được 0201 điền. Chỉ còn fixture test của inventory chèn thẳng (không tìm theo tên ở đó).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| "5678" → A, B (phone_tail); "345678" → A; "0912 345 678", "+84912345678" → A (phone); "nhung", "NHUNG", "Nhưng", "hong nhun" → A (name); "hong" → A, C; mã của B (hoa, thường) → B (code) | `TestSearchClassifies` (23 ca con) | `go test -race ./internal/modules/customer/` | `ok elimaz.com.vn/crm/internal/modules/customer 36.358s` |
| "50%_", "%%", "__", "n_m", `a\` không khớp ai (đã thoát ký tự đặc biệt) | `TestSearchClassifies` các ca `50%_`, `%%`, `__`, `n_m`, `a\` | như trên | ok |
| "12", "123", "1.2", "a", "%", rỗng → `ErrQueryTooShort` | `TestSearchTooShort` | như trên | ok |
| Gộp: root_id của B = A → "5678" một dòng gốc A, SĐT của cả nhóm; mã/tên của hồ sơ con trả gốc A | `TestSearchGroupsByRoot` | như trên | ok |
| Tenant: trong WithTenant(B) không ra khách của A; hàm gọi ngoài ngữ cảnh → 0 dòng, không lỗi | `TestSearchTenantIsolation` | như trên | ok |
| Hàm `SECURITY DEFINER`, chủ `crm_search`, `search_path=pg_catalog, pg_temp`, PUBLIC không EXECUTE, crm_app có | `TestSearchByNameFunctionDefinition` | như trên | ok |
| Thứ tự mua gần nhất trước, limit theo nhóm | `TestSearchOrderAndLimit` | như trên | ok |
| Danh sách khi q rỗng: khách gốc mới nhất, keyset (created_at, id), con trỏ hỏng → ErrInvalidInput, tenant riêng | `TestListCustomers` | như trên | ok |
| Hồ sơ con không hiện; lọc tagId theo nhóm (cả List và SearchTagged) | `TestListCustomersMergedAndTagged` | như trên | ok |
| 100.000 khách, ~130.000 danh tính, ANALYZE: câu đuôi số, SĐT đủ, mã, dòng gốc, SĐT nhóm, danh sách (có/không nhãn) bằng crm_app không Seq Scan; thân hàm tên (auto_explain) dùng Bitmap Index Scan GIN, không Seq Scan; LIKE thẳng bằng crm_app là Seq Scan; hàm và câu thẳng cùng số dòng | `TestSearchPlans` (11 ca con) | `go test -race ./internal/modules/customer/ -run TestSearchPlans -v` | `--- PASS: TestSearchPlans (9.36s)`, mọi ca con PASS |
| M2-02: route M2 chưa làm vẫn 501/403/401/400 (đổi ví dụ sang timeline) | `TestM2Routes_Unimplemented` | `go test -race ./internal/httpapi/ -run 'TestCustomerSearchAPI\|TestM2Routes\|TestErrorMap\|TestCustomerWrite'` | `ok elimaz.com.vn/crm/internal/httpapi 3.659s` |
| HTTP: các ca tìm, "12" → 422 `query_too_short`, không có `customer.read` → 403, con trỏ hỏng → 400, trang 2 dòng có nextCursor, tagId (có/không q); log và span không chứa SĐT, tên, chuỗi tìm | `TestCustomerSearchAPI` | `go test -race ./internal/httpapi/ -run 'TestCustomerSearchAPI\|TestErrorMap'` | `ok elimaz.com.vn/crm/internal/httpapi 2.146s` |
| Hợp đồng listCustomers (đường, quyền) không đổi | `tests/arch` `m2Operations` | `go test ./tests/arch/ -run M2` | `ok elimaz.com.vn/crm/tests/arch 0.528s` |
| Lint hai gói | — | `golangci-lint run ./internal/modules/customer/... ./internal/httpapi/...` | `0 issues.` |
| Cổng kiểm task: build, vet, lint, gen-check (sqlc/ogen không chênh), sqlc vet, catalog-check, jscpd (0 clone), gitleaks, oasdiff, test-guard, test toàn bộ (đổi migration) | — | ci-task lần 2 (lần 1 ĐỎ ở jscpd và `TestM2Routes_Unimplemented`, đã sửa như mục Tái dùng và Test bị xóa hoặc tắt) | `CI-TASK XANH (so với điểm tách nhánh 811a0251247baabecf857cbe518389708daf6f38)` |

## Test bị xóa hoặc tắt

- Không xóa, không tắt, không nới. Sửa một test có sẵn: `TestM2Routes_Unimplemented` (`internal/httpapi/errors_m2_test.go`, Task M2-02) dùng `GET /api/v1/customers` làm ví dụ route "chưa làm → 501"; route này nay đã làm (Task M2-10) nên test đổi sang `GET /api/v1/customers/{id}/timeline` (Task M2-12, vẫn 501, cùng quyền `customer.read`). Giữ nguyên mọi kiểm: 501 + request_id, không grant 403, không thẻ 401, thiếu Idempotency-Key 400.

## Khác kế hoạch

- `search_path` của hàm là `pg_catalog, pg_temp` (kế hoạch ghi `pg_catalog, customer`): xem QĐ1.
- Phép đo thân hàm tên dùng `testkit.ExplainAsApp` + auto_explain (đọc kế hoạch câu lồng trong hàm khi crm_app gọi thật) thay cho `SET LOCAL ROLE crm_search`: xem QĐ2.
- Thêm `SearchTagged` (ngoài Interface) để `q` + `tagId` lọc nhãn trước khi cắt limit: xem QĐ3.
- Thêm index `customers_roots_created` trong 0207 cho danh sách keyset: xem QĐ5.

## Quyết định thay chủ shop

- QĐ1 — `search_path` của hàm: chọn `pg_catalog, pg_temp` (như hàm tìm của catalog 0123/0129) thay vì `pg_catalog, customer`. Vì: khi `pg_temp` không ghi rõ, PostgreSQL tìm bảng trong `pg_temp` TRƯỚC (khuyến cáo của tài liệu PostgreSQL cho hàm SECURITY DEFINER là đặt `pg_temp` cuối); mọi tên trong thân hàm đã viết đủ schema nên không cần `customer` trong đường tìm. Phương án khác: đúng chữ kế hoạch `pg_catalog, customer` (an toàn nhờ tên đủ schema nhưng để hở `pg_temp` đứng đầu). Đảo ngược: `CREATE OR REPLACE FUNCTION` ở migration sau.
- QĐ2 — Đo kế hoạch thân hàm bằng auto_explain (công cụ chung đã có, `TestSearchPlans` của tenancy/queries) thay cho chạy lại thân hàm bằng `SET LOCAL ROLE crm_search`: đo đúng câu trong hàm khi crm_app gọi thật (cùng tham số, cùng role chủ), không phải bản chép tay của thân hàm. Phương án khác: chép thân hàm vào test và chạy dưới `SET LOCAL ROLE` (dễ lệch với hàm thật).
- QĐ3 — `q` kèm `tagId`: lọc nhãn trong câu đọc dòng gốc (trước khi cắt limit), nhóm có nhãn khi BẤT KỲ hồ sơ nào trong nhóm có nhãn (nhãn gắn trên hồ sơ con trước khi gộp vẫn tính). Cũng áp cho danh sách. Phương án khác: chỉ xét nhãn trên hồ sơ gốc (task nhãn sau có thể đổi nếu chọn gắn nhãn chỉ ở gốc; khi đó hai cách cho cùng kết quả).
- QĐ4 — Số dòng mặc định khi tìm qua HTTP là 50 (mặc định `limit` của hợp đồng, ogen tự điền) chứ không 20; `customer.Search` vẫn dùng 20 khi `limit ≤ 0`. Vì: không đổi hợp đồng (oasdiff), ogen không cho biết tham số có được gửi hay không. Phương án khác: thêm tham số riêng cho tìm trong openapi (đổi hợp đồng — ngoài phạm vi).
- QĐ5 — Index `customer.customers (tenant_id, created_at DESC, id DESC) WHERE id = root_id` trong 0207: danh sách keyset theo spec 04 mục 7 cần index; không có thì đọc cả bảng. Pha mở rộng, xóa được ở migration sau.
- QĐ6 — Chuỗi toàn chữ số dài ≥ 10 nhưng không chuẩn hóa được thành SĐT (ví dụ 11 số lạ) đi đường đuôi số (khớp khi đó là phần cuối của một số đã lưu); kế hoạch chỉ nói 4–9 chữ số. Phương án khác: trả `ErrInvalidPhone` — khó dùng hơn cho nhân viên dán số lạ.
- QĐ7 — Đọc tối đa 500 hồ sơ khớp (hàm tên cắt `least(lim, 500)`, đuôi số `LIMIT 500` gốc) rồi mới gom theo gốc và cắt `limit`; với tên quá rộng (khớp > 500 hồ sơ) kết quả là các nhóm mua gần đây nhất trong 500 hồ sơ đó. Đủ cho ô tìm (tối đa 100 dòng).

## Việc phát hiện thêm

- Mô tả `listCustomers` trong `openapi.yaml` vẫn ghi "Chưa làm ở Task M2-02 (trả 501…)" như mọi operation M2 khác đã làm (createCustomer…); không sửa để không đổi hợp đồng và tệp sinh của console trong task này — nên dọn đồng loạt ở một task tài liệu.

## Reviewer

Kết luận: ĐẠT (lần 2).

- Lần 1: CẦN SỬA — chỉ ở tệp task: dòng "Cổng kiểm task" chưa có kết quả thật; `TestSearchClassifies` ghi 22 ca trong khi mã có 23. Mã đúng; Step R đạt (hàm `customer.search_by_name` có `search_path` cố định `pg_catalog, pg_temp`, tên đủ schema, tự lọc `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`, chủ `crm_search`, PUBLIC bị thu quyền, chỉ `crm_app` EXECUTE). Đã sửa cả hai, kèm chú thích "rồi theo id" của `TestSearchOrderAndLimit`.
- Lần 2: ĐẠT, không phát hiện phải sửa. QĐ1–QĐ7 hợp lý (QĐ1 an toàn hơn chữ của kế hoạch).
- Ghi chú không bắt buộc (không sửa, không ảnh hưởng tính đúng): `q` chỉ gồm khoảng trắng trả 422 còn `q` rỗng trả danh sách; từ tìm 1–2 ký tự làm index trigram quét rộng (đúng nhưng chậm hơn).
