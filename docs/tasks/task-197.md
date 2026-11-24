# Task 197 — Đợt mã và quà sinh nhật trong promotion; nhập mã còn hạn từ website

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 197 = M5-14). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `promotion.Batch` thêm trường `Purpose` (chỉ thêm; dùng ở `handlers_promotion.go` `batchOut`, `GetCouponBatch`, `CreateCouponBatch`, test `batch_test.go`).
  - `promotion.Coupon` thêm trường `GivenAt *time.Time` (chỉ thêm; `couponOut` của `handlers_promotion.go` đọc).
  - Mới: `WebBatchInput`, `(Writer).EnsureWebBatch`, `MarkGiven`, `ImportItem`, `ImportReport`, `ImportCoupons`, `ListBatches`, `BatchStats`, `GetBatchStats`, `ErrBatchConflict`, hằng `PurposeManual`, `PurposeBirthday`, `PurposeImported`.
  - `CreateBatch`, `BatchInput`, `GetBatch` của M2 giữ nguyên chữ ký; `GetBatch` đọc thêm cột `purpose`.
- Gói phụ thuộc ngược (go list): `promotion` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/modules/order`, `internal/wiring/webcache`. Chỉ thêm ký hiệu nên chỉ `httpapi` (handler mới, `batchOut`, `couponOut`) và `httpapi/web` (test mới) đổi có chủ ý; `order` gọi `Redeem`/`Check` không đổi.
- Operation OpenAPI bị chạm, oasdiff (chỉ thêm):
  - Mới, website (`x-auth: service_key`, `x-permission: web.coupons.issue`, `IdempotencyKey` bắt buộc): `webCreateBirthdayBatch` `POST /api/v1/web/birthday-batches`, `webMarkCouponGiven` `POST /api/v1/web/coupons/{code}/given`, `webImportCoupons` `POST /api/v1/web/coupons/import`.
  - Mới, nhân viên: `listCouponBatches` `GET /api/v1/coupon-batches` (`promotion.read`).
  - Schema `CouponBatch` (`getCouponBatch`, `createCouponBatch`): thêm trường tùy chọn `purpose`, `givenCount`, `usedCount`, `expiredCount`; schema `Coupon` (listCoupons, getCoupon…) thêm `givenAt` tùy chọn. Mã lỗi mới `batch_conflict` (422) vào `x-error-codes`.
- Chỗ dùng trên màn hình: `console/src/screens/ma-giam-gia/*` dùng `CouponBatch`, `Coupon` — chỉ thêm trường tùy chọn; client sinh lại (`console/src/api/**` qua `make gen`), màn không đổi. Màn "Quà sinh nhật" là task khác (M5-26). `console/src/app/errors.ts` thêm câu cho mã lỗi mới `batch_conflict` (test `errors.test.ts` đòi mọi mã trong `x-error-codes` có câu).
- Câu sqlc, bảng, pha migration:
  - `migrations/0505_coupon_batches_web.sql` pha **mở rộng**: `coupon_batches.purpose` (mặc định `manual`, CHECK), unique index một phần `(tenant_id, purpose, name) WHERE purpose <> 'manual'`, index `(tenant_id, purpose, id)` cho danh sách; `coupons.given_at`, `coupons.origin` (mặc định `crm`, CHECK); GRANT UPDATE cột `coupon_batches(purpose, quantity)`, `coupons(given_at)` cho `crm_app`. Không CREATE bảng, không sửa view.
  - `queries/promotion/manage.sql`, `coupons.sql`: 7 câu đọc/ghi mã chọn thêm `given_at` (cùng cột để `couponFrom` đổi kiểu dòng được); `GetBatch`, `InsertBatch` trả thêm `purpose`.
  - Mới `queries/promotion/webbatches.sql`: tìm lô theo (purpose, tên), mã của lô, đặt purpose, chèn lô nhập (ON CONFLICT DO NOTHING), khóa lô theo id, chèn mã nhập hàng loạt (ON CONFLICT DO NOTHING), đọc mã có sẵn theo danh sách, đếm lại quantity, đánh dấu đã tặng, danh sách lô, số đếm của lô.
  - Bảng: `promotion.coupon_batches`, `promotion.coupons` (M2 là chủ, M5 chỉ ALTER ADD). Thứ tự khóa ADR-46 nhóm 10 giữ: lô → mã.
- Test sẽ chạy: `go test -race -run` từng test mới trong `internal/modules/promotion`, `internal/httpapi` (`TestCouponAPI_*`, `TestWebCouponsAPI_*`), `internal/httpapi/web` (`TestWebBirthdayCoupon_*`); cả gói qua `scripts/agent/ci-task.sh` (migration mới chỉ thêm → gói dùng đối tượng nó chạm).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (promotion: `CreateBatch`, `NewCode`, `GetBatch`, `Check`, `Redeem`, `validateTerms`); `httpapi`: `refusableWrite`, `auditService`, `promotionWrite`, `catalogRead`, `couponCursor`; testkit `OpenStaff`, env `orderEnv`/`serviceKey` của test `httpapi/web`.
- Dùng lại / viết mới vì:
  - Lô sinh nhật **gọi `Writer.CreateBatch` của M2** (kiểm điều khoản, bộ sinh mã `NewCode`, chèn theo lô, `max_uses = 1`, báo hiệu) trong một savepoint (`db.Savepoint` có sẵn; ADR-46 cấm tự `tx.Begin`) rồi đặt `purpose` — không chép phần kiểm và sinh mã.
  - Nhập mã cũ viết mới (`ImportCoupons`): mã do website đặt sẵn, mỗi mã một điều khoản, có `used_count`/`given_at`/`origin` → không khớp `CreateBatch` (một mẫu, mã sinh) hay `CreateCoupon` (một mã, trả `ErrCodeTaken`). Dùng lại `validateTerms` để kiểm từng mã.
  - Đường ghi website dùng `refusableWrite` + `auditService` (mẫu Task 195); đọc lô của nhân viên dùng `catalogRead` như `GetCouponBatch`.
  - Lượt dùng mã ở đơn web: không viết gì, dùng `promotion.Redeem` trong `order.CreateFromWeb` (M2/M5-11).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: đợt mã, mã thuộc `promotion` (04 mục 3; kế hoạch M5 bảng chuyển: quà sinh nhật = `customer` + `promotion`); chỉ thêm cột vào bảng của `promotion`.
- 5.2 Không ghi bảng module khác: chỉ ghi `promotion.*`; audit qua `identity.RecordAudit` (public API) ở lớp HTTP.
- 5.3 Đọc module khác qua view: không đọc bảng module khác (`CreateBatch` dùng hàm thuần `customer.NormalizePhone`).
- 5.4 Nguyên tử: lô + mã + audit một transaction (`WriteTx`); dùng mã khi tạo đơn web là `Redeem` trong transaction tạo đơn (M2/M5-11, không đổi).
- 5.5 Sau commit: báo hiệu realtime của `CreateBatch` ghi trong tx như M2; không sự kiện mới.
- 5.6 Gọi ra ngoài: không áp dụng (không gọi dịch vụ ngoài; CRM không gọi Nhanh — mã cũ do website gửi).
- 5.7 Ngữ cảnh shop: mọi câu trong `WithTenant`/`WriteTx`; bảng đã có RLS; index mới đứng đầu bằng `tenant_id`.
- 5.8 Không HTTP trong module: đúng; handler mỏng ở `internal/httpapi/handlers_web_coupons.go`.
- 5.9 Mọi đường ghi: ba operation website có `IdempotencyKey` (required) và `x-permission: web.coupons.issue` (quyền khóa dịch vụ đã có từ Task 189); đọc nhân viên `promotion.read`.
- 5.10 PostgreSQL là nguồn: không dùng Valkey.
- 5.11 Không thư viện mới.
- 5.12 Theo mẫu: `Writer`, sentinel lỗi ở `promotion`, đổi HTTP ở `errors_promotion.go`; test trên PostgreSQL thật.

Mục 6:
- 6.1 Hai yêu cầu cùng lúc: (a) hai lần tạo lô sinh nhật cùng tên — unique index một phần `(tenant_id, purpose, name)`: lần sau gặp 23505 khi đặt `purpose` → lùi savepoint, đọc lại lô đã commit (READ COMMITTED) → trả lô cũ hoặc `ErrBatchConflict`; có test song song. (b) `MarkGiven` là một UPDATE có điều kiện `WHERE given_at IS NULL` (lần sau chờ khóa dòng rồi thấy đã có → 0 dòng, đọc lại lúc trao đầu) → lần đầu thắng. (c) Nhập mã: `INSERT … ON CONFLICT (tenant_id, code) DO NOTHING`; lô nhập khóa `FOR UPDATE` theo id tăng dần trước khi chèn mã → hai lần nhập cùng lô chạy lần lượt. (d) Dùng mã: `Redeem` câu có điều kiện `used_count < max_uses` của M2 (test [WB11] cho mã sinh nhật).
- 6.2 Gửi lại: `Idempotency-Key` bắt buộc; thêm khóa tự nhiên (purpose, tên lô) và (tenant, mã) nên gửi lại với khóa khác cũng không nhân đôi.
- 6.3 Thứ tự khóa: ADR-46 nhóm 10 `coupon_batches → coupons`: lô sinh nhật ghi lô trước mã (CreateBatch) rồi UPDATE lô vừa tạo (dòng của chính tx); lô nhập khóa lô (id tăng dần) trước khi chèn mã, đếm lại quantity trên dòng đã giữ. Ghi chú reviewer: dòng lô nhập **mới** được chèn theo thứ tự tên gặp trong lần nhập (trước bước khóa theo id), nên hai lần nhập đồng thời với hai lô mới tên xếp ngược nhau có thể gặp 40P01 trên unique index; `RetryTx` chạy lại nên kết quả vẫn đúng (website chỉ nhập một lần, QĐ42). Lần chờ duy nhất ngoài thứ tự là chờ unique index của lô cùng tên do tx kia giữ — tx kia không chờ gì của tx này nên không thành vòng.
- 6.4 Lỗi tạm: `WriteTx`/`RetryTx` có sẵn (thân hàm gán lại biến kết quả mỗi lần chạy).
- 6.5 Mức cách ly: READ COMMITTED (đọc lại lô sau khi lùi savepoint cần thấy bản đã commit).
- 6.6–6.8 Việc nền: không áp dụng (không việc nền mới).
- 6.9 Treo lâu: tối đa 5.000 mã/lô sinh nhật, 500 mã/lần nhập → câu chèn theo lô 1.000 của M2, một câu chèn cho lần nhập; trong `statement_timeout` của TxAPI.
- 6.10 Tiền: `int64` đồng; phần trăm website (1–100) đổi sang phần vạn (×100) như M2.
- 6.11 Quy luật: không thêm test rapid (promotion không trong `HIGH_RISK_DIRS`; "mỗi mã dùng đúng một lần" là quy luật của `Redeem` M2 đã có test); thêm test song song cho lô cùng tên và [WB11].
- 6.12 Dữ liệu cá nhân: SĐT gắn mã không vào audit, lỗi, log (audit chỉ số lượng SĐT); `bound_phone` không ra API.
- 6.13 Quyền: route website chỉ khóa có `web.coupons.issue`; khóa thiếu quyền → 403 (có test); `listCouponBatches` cần `promotion.read` (test 403).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Cổng kiểm của task: build, vet, lint, gen-check, sqlc-vet, jscpd, gitleaks, oasdiff (chỉ thêm), test-guard, console-check, test -race gói sửa + `tests/arch` (gồm `TestTxUsageProductCode`: savepoint qua `db.Savepoint`), test gói phụ thuộc | (toàn bộ) | `ci-task.sh` (trên phần đã stage) | `CI-TASK XANH (so với điểm tách nhánh 5a9ec786…)` |
| Lô sinh nhật 30 mã khác nhau, `max_uses = 1`, `purpose = birthday`; gọi lại cùng tên → cùng 30 mã, không thêm dòng; cùng tên khác giá trị/số lượng → `ErrBatchConflict`; lô manual cùng tên không bị ảnh hưởng | `TestEnsureWebBatch_SameNameSameBatch` | `go test -race -count=1 -run '^(TestEnsureWebBatch_.*|TestMarkGiven_KeepsFirst|TestImportCoupons_.*|TestBatchStats|TestListBatches_PurposeAndCursor|TestMigration0505_ExistingM2Rows|TestCreateBatch_.*)$' ./internal/modules/promotion/` | `ok elimaz.com.vn/crm/internal/modules/promotion 4.152s` |
| Lô gắn SĐT: mã theo thứ tự SĐT gửi lên, gọi lại khác thứ tự vẫn đúng, SĐT khác → conflict | `TestEnsureWebBatch_BoundPhonesOrder` | như trên | ok |
| Hai lần tạo lô cùng tên song song → cùng một lô, 1 lô, 5 mã | `TestEnsureWebBatch_ConcurrentSameName` | như trên | ok |
| Quá 5.000 mã, loại manual/imported, tên rỗng → `ErrInvalidInput`, không ghi | `TestEnsureWebBatch_Invalid` | như trên | ok |
| Migration 0505 trên database có lô và mã của M2 → không lỗi, lô cũ `manual`, mã cũ `crm`, `given_at` rỗng | `TestMigration0505_ExistingM2Rows` | như trên | ok |
| `MarkGiven` hai lần → `given_at` giữ lần đầu; mã lô manual, mã không có → `ErrNotFound` | `TestMarkGiven_KeepsFirst` | như trên | ok |
| Nhập 4 mã (đã dùng, trùng mã có sẵn khác giá trị, hết hạn) → Inserted 3, Conflicts [TRUNG1]; đã dùng → `exhausted`, hết hạn → `expired`, 10% → phần vạn 1000; nhập lại → Skipped 3 | `TestImportCoupons_Report` | như trên | ok |
| 501 mục, 0 mục, mã trùng, 101%, hạn ngược, sai mẫu → `ErrInvalidInput`; 500 mục được; lô mặc định | `TestImportCoupons_Invalid` | như trên | ok |
| Số đếm lô: trao 4, dùng 3 (Redeem thật), hết hạn 2 | `TestBatchStats` | như trên | ok |
| `ListBatches` lọc purpose, phân trang | `TestListBatches_PurposeAndCursor` | như trên | ok |
| `getCouponBatch` trả purpose, givenCount 10, usedCount 3, expiredCount 0; listCoupons có `givenAt`; `listCouponBatches?purpose=birthday` chỉ lô sinh nhật; purpose lạ → 400; không `promotion.read` → 403 | `TestCouponAPI_BatchStatsAndList` | `go test -race -count=1 -run '^(TestCouponAPI_.*)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 4.225s` |
| `createCouponBatch` M2 không đổi hành vi (sau khi tách `templateIn`, `phonesIn`, `batchAuditFields`) | `TestCouponAPI_Batch` | như trên | ok |
| `batch_conflict` → 422 trong bảng đổi lỗi | `TestDomainErrorSnapshot*`, `TestErrorCodes_*` | `go test -race -count=1 -run '^(TestDomainErrorSnapshot.*\|TestErrorCodes_.*\|TestServiceKey.*)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.538s` |
| [WB05] Website xin lô → 200 created; gọi lại → cùng lô; trao mã (gọi lại giữ lần đầu); đơn web dùng mã → lượt dùng active, `used_count 1`; đơn thứ hai → 422 `coupon_invalid` `exhausted`; audit service một dòng mỗi việc | `TestWebBirthdayCoupon_IssueGiveRedeem` | `go test -race -count=1 -run '^(TestWebBirthdayCoupon_.*|TestWebCoupons_Refusals|TestWebImportCoupons_API)$' ./internal/httpapi/web/` | `ok elimaz.com.vn/crm/internal/httpapi/web 2.809s` |
| [WB11] Hai đơn web cùng lúc dùng cùng mã sinh nhật → đúng một đơn có mã | `TestWebBirthdayCoupon_ParallelOrdersOneWins` | như trên | ok |
| 422 `batch_conflict` không lưu theo khóa; khóa thiếu quyền → 403; mã không có → 404; 5001 mã → 400 | `TestWebCoupons_Refusals` | như trên | ok |
| Nhập qua API: inserted 3, conflicts [TRUNG1], audit; báo giá web mã nhập 10% giảm 35.000; mã đã dùng → `exhausted`; 501 mục → 400; khóa thiếu quyền → 403 | `TestWebImportCoupons_API` | như trên | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Handler website đặt ở `internal/httpapi/handlers_web_coupons.go` (gói `httpapi`), không ở `internal/httpapi/web/coupons.go`: đường ghi cần `WriteTx`/`refusableWrite`/`auditService` của gói `httpapi` (mẫu Task 195 `handlers_web_requests.go`); gói `web` chỉ có đường đọc có bộ đệm. Test website vẫn ở `internal/httpapi/web/coupons_test.go` như kế hoạch (dùng env đơn web sẵn có).
- `webCreateBirthdayBatch` chỉ trả **200** kèm `created` (kế hoạch ghi 200/201): một mã thành công để câu trả lời lưu theo khóa chống trùng luôn khớp mã mà ogen ghi.
- `MarkGiven` trả `GivenMark{CouponID, At, First}` thay vì chỉ `error`: lớp HTTP cần id mã để audit và chỉ audit lần trao đầu.
- `getCouponBatch` không có mảng `codes`; "`codes[].givenAt`" của kế hoạch làm bằng trường tùy chọn `givenAt` của schema `Coupon` (danh sách mã của lô là `listCoupons?batchId=`).
- `EnsureWebBatch` là phương thức `Writer` (đúng dòng M5-14 của bảng đối chiếu M5-00) với thứ tự tham số theo kế hoạch `(ctx, tx, in, a)`; `ListBatches` nhận con trỏ `*uuid.UUID` như `ListCoupons`; số đếm là hàm riêng `GetBatchStats` mà `GetCouponBatch` gọi cùng transaction.
- Tách nhỏ trong `handlers_promotion.go` để dùng chung với đường website: `templateIn`, `phonesIn`, `batchAuditFields` (audit `createCouponBatch` thêm khóa `purpose`).

## Quyết định thay chủ shop

- **Mã nhập từ website chỉ dùng trên kênh web** (`channels = {web}`), mỗi mã một lượt: mã sống trên website từ trước; phương án khác là mọi kênh (`{}`). Nhân viên mở rộng được bằng `updateCoupon` khi mã chưa dùng. Dễ đảo ngược.
- **Phần trăm của mã nhập là phần trăm nguyên 1–100** (×100 thành phần vạn) theo kế hoạch "10% → 1000"; phần trăm lẻ của website (nếu có) bị từ chối 400 để không làm tròn sai.
- **Lô nhập không tên → "Mã nhập từ website"**; số lượng của lô nhập được đếm lại sau mỗi lần nhập (cột `quantity` NOT NULL của M2).
- **Nhập lại**: mã đã có cùng điều khoản (kiểu, giá trị, trần, đơn tối thiểu, hạn) tính `Skipped`, không đổi `used_count`/`given_at` của mã có sẵn; khác → `Conflicts`. Mã trùng trong cùng lần nhập → 400 cả lần (không nhập một phần).
- **`webMarkCouponGiven` không nhận thân**, lấy giờ máy chủ; chỉ mã của lô website (birthday, imported), mã nhân viên tạo → 404.
- **Lô sinh nhật cùng tên**: so cả số lượng, điều khoản và bộ SĐT; khác bất kỳ → 422 `batch_conflict`.

## Dừng để hỏi

(không có)

## Việc phát hiện thêm

- `docs/threat-model-m5.md` dòng 58 ghi test `TestWebCoupon_GuessRateLimited` cho M5-14 ("giới hạn tần suất theo khách và IP" khi dò mã). Kế hoạch Task 197 không có Interface nào cho việc này, và hợp đồng báo giá `/web/quote` không mang IP của khách; hiện chỉ có giới hạn theo khóa dịch vụ (`web_key`, 1.200/phút) và theo SĐT khi tạo đơn (`web_order_customer`). Mã CRM sinh có ≈ 1,1 × 10^12 khả năng nên dò không khả thi; nhưng **mã nhập từ website có thể ngắn, dễ đoán**. Cần một task riêng (thêm giới hạn theo khách/IP cho bước kiểm mã ở báo giá, hoặc sửa dòng threat model) — chưa làm ở đây.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên phần đã stage sau khi ci-task XANH).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế; `models.go` của sqlc ở 10 gói đổi do sinh lại theo schema mới (không ai gọi tới, không cần sửa). Chữ ký `CreateBatch`, `BatchInput`, `GetBatch` giữ nguyên; 7 câu mã cùng cột nên `couponFrom` dùng chung kiểu dòng.
- Bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí ([WB05], [WB11], migration trên dữ liệu M2, song song cùng tên, 422 không lưu theo khóa, 403/404/400).
- Ghi chú không bắt buộc: (1) lô nhập mới chèn theo thứ tự tên gặp, có thể 40P01 khi hai lần nhập đồng thời — `RetryTx` chạy lại, đã ghi ở 6.3; (2) mã nhập trùng mã nhân viên cùng điều khoản nhưng khác kênh/`max_uses` tính `Skipped` — đúng quyết định đã ghi; (3) ghi thêm `models.go` sinh lại vào Ảnh hưởng (đã ghi ở đây).
