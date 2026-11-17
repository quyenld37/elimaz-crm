# Task 01 — Repo, PostgreSQL dev, phép đo đường mua flash sale bằng Go

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, Task 1. Loại việc (quy trình mục 3): A

## Ảnh hưởng

Viết ngày 18/11/2026, trước khi viết test. Repo chưa có mã Go nào (chỉ có tệp quy trình của Task 0).

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Module gốc `elimaz.com.vn/crm` mới tạo, chưa có gói. Mã đo nằm ở module riêng `spikes/flashsale` (gói `main`, không xuất gì cho ai nhập); `cmd/`, `internal/` chưa tồn tại nên không thể nhập nó.
- Gói phụ thuộc ngược (go list): không có (`go list ./...` ở gốc không thấy gói nào; module `spikes/flashsale` không nằm trong `./...` của gốc).
- Operation OpenAPI bị chạm, oasdiff: không có (chưa có `openapi.yaml`). Điểm cuối `POST /buy` của mã đo không thuộc hợp đồng API.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có sqlc, không có migration. Mã đo dùng schema riêng `spike` trong database dev (`schema.sql` tự xóa và tạo lại mỗi lần `setup` hoặc chạy test); schema `spike` không phải dữ liệu sản phẩm.
- Hạ tầng dev mới: `docker/compose.dev.yml` (PostgreSQL 18 ghim digest, Jaeger v2 ghim digest; cổng đọc từ biến môi trường, mặc định 5442, 16686, 4318), `docker/postgres/init/01-roles.sql` (ba role của Global Constraints). `Makefile` mới với lệnh dựng database và chạy test mã đo. `tools/versions.mk`: chốt `K6_VERSION`.
- Test sẽ chạy: `cd spikes/flashsale && go test -race -count=1 ./...` trên PostgreSQL dev (6 test của Step 1). Hook Stop chỉ thấy module gốc (chưa có gói) nên không tự chạy test của `spikes/`; chạy tay và ghi vào Bằng chứng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: chưa có `docs/danh-muc-dung-chung.md` (Task 3 tạo); repo chưa có mã Go nào để dùng lại.
- Dùng lại / viết mới vì: dùng thư viện của ADR-44 (pgx v5 ≥ 5.13.0, OpenTelemetry Go v1.47.0, `otelhttp`, otelpgx v0.13.0) và thư viện chuẩn (`net/http`, `crypto/sha256`, `encoding/json`, `sort` cho phân vị độ trễ). Viết mới trong `spikes/flashsale` vì đây là mã đo, kế hoạch cấm `cmd/`, `internal/` nhập nó và nó không được thành mã dùng chung; phần khóa chống trùng thật làm ở Task 9 (`internal/platform/idempotency`), không lấy từ đây. Sinh UUID trong test bằng `crypto/rand` (vài dòng) thay vì thêm `google/uuid` cho module đo (Task 3 mới thêm thư viện đó cho module gốc).

## Soát thiết kế

Mã đo, không phải mã sản phẩm; vẫn trả lời để phép đo chạy đúng đường mua của spec (04 mục 15 bước 4, 05 mục 7.1).

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: không áp dụng: bảng nằm trong schema `spike` riêng của mã đo; bảng thật của flash sale thuộc module đơn hàng ở M5.
2. Không ghi bảng module khác: không áp dụng: chỉ có schema `spike`.
3. Đọc qua view: không áp dụng: như trên.
4. Việc nguyên tử trong một transaction: áp dụng. Ghi giới hạn khách, lấy đơn vị, tạo đơn nháp + dòng đơn + dòng giữ hàng, đánh dấu đơn vị `held`, chuyển khóa sang `done` kèm kết quả: một transaction. Bước ghi nhận khóa (`running`) là một transaction ngắn riêng, đúng 05 mục 7.1.
5. Việc không nguyên tử đi sau commit: không áp dụng: mã đo không phát sự kiện.
6. Gọi ra ngoài: không áp dụng: không gọi bên ngoài.
7. Ngữ cảnh shop: áp dụng. Mọi bảng `spike.*` có `tenant_id NOT NULL`, `UNIQUE (tenant_id, id)`, khóa ngoại ghép `(tenant_id, …)`, `ENABLE` + `FORCE ROW LEVEL SECURITY`, policy dùng `NULLIF(current_setting('app.tenant_id', true), '')::uuid`. Mỗi transaction đặt `set_config('app.tenant_id', $1, true)` (chỉ trong transaction). Máy chủ đo chạy bằng `crm_app` (`NOBYPASSRLS`); `crm_owner` chỉ dùng cho `setup` (tạo schema, nạp suất). Có test: ngoài ngữ cảnh tenant đếm `spike.orders` ra 0, kể cả trên kết nối vừa dùng cho một transaction có tenant.
8. Nghiệp vụ không biết HTTP: không áp dụng: mã đo một tệp `main`, có chủ ý gộp HTTP và SQL để đo đúng đường đi.
9. Khóa chống trùng và quyền: khóa chống trùng áp dụng (`Idempotency-Key` bắt buộc, 1–128 ký tự `[A-Za-z0-9_-]`); quyền không áp dụng: mã đo không có người dùng.
10. PostgreSQL là nguồn sự thật: áp dụng; mã đo không có Valkey, đúng chế độ "Valkey ngừng" của ADR-40 (giới hạn số lượt đang xử lý trong tiến trình).
11. Không thêm công nghệ: pgx, OpenTelemetry Go, `otelhttp`, otelpgx nằm trong ADR-44. k6 là công cụ đo, có trong Tech Stack của kế hoạch nhưng không có trong bảng ADR-44: ghi một dòng vào `docs/phu-thuoc.md`. Jaeger v2 có trong ADR-38/44 và kế hoạch.
12. Module mẫu: không áp dụng: chưa có (Task 7, 12).

Mục 6 (đúng khi chạy đồng thời):

1. Hai yêu cầu cùng sửa: `flash_sale_claims` bằng `INSERT … ON CONFLICT DO UPDATE … WHERE quantity + $q <= $limit` (cập nhật có điều kiện, khóa dòng của khách); đơn vị lấy bằng `FOR UPDATE SKIP LOCKED`, không ai chờ ai.
2. Gửi lại cùng yêu cầu: `Idempotency-Key`; `done` trả lại kết quả đã lưu, `running` còn hạn trả 409, nội dung khác trả 422; dấu `done` ghi cùng transaction với đơn. `running` quá hạn: nhận lại bằng cập nhật có điều kiện (05 mục 7.1), chỉ một bên thắng.
3. Thứ tự khóa: đơn vị lấy theo `ORDER BY id` và `SKIP LOCKED` nên không chờ nhau; mỗi transaction chỉ khóa một dòng `flash_sale_claims` (của chính khách đó) và một dòng khóa chống trùng, luôn theo cùng thứ tự (khóa chống trùng ở bước 1 đã commit; bước 2 khóa claims → đơn vị → khóa chống trùng). Không có vòng chờ.
4. Lỗi tạm của database: theo ADR-46 (thêm vào spec và kế hoạch ngày 18/11, trong lúc task đang làm). Mọi transaction của đường mua đi qua `retryTx`, bản sao tối giản của `db.RetryTx` trong `buy.go` (mã đo không nhập `internal/`): `40P01`, `55P03`, `40001` (kể cả lúc commit) → lùi, chờ 20/40/80 ms × [0,5; 1,5], chạy lại toàn bộ, tối đa 3 lần; hết lượt → `503 retry`, xóa dòng khóa `running` của chính lượt đó (điều kiện `locked_until` = giá trị lượt này nhận), không lưu. Lỗi khác (kể cả `57014`) không chạy lại, trả 500 và xóa dòng khóa. Hàm trong transaction không gọi mạng và gán lại kết quả ở mỗi lần chạy. Đếm số lần chạy lại theo SQLSTATE và số lần hết lượt.
5. Mức cách ly: READ COMMITTED mặc định; đúng nhờ câu lệnh có điều kiện và khóa dòng.
6. Việc nền lỗi: không áp dụng: không có việc nền.
7. Bên ngoài 429: không áp dụng.
8. Tiến trình chết giữa chừng: transaction bước 2 tự lùi; dòng khóa `running` hết hạn sau 60 s và được lượt gửi lại nhận lại.
9. Treo lâu: kết nối đặt `statement_timeout` 30 s, `lock_timeout` 5 s, `idle_in_transaction_session_timeout` 60 s (ADR-26); mỗi transaction đặt `lock_timeout` 200 ms, `statement_timeout` 2 s cùng câu `set_config` với tenant (ADR-46); `-max-inflight` (mặc định 64) chặn dồn hàng trước pool.
10. Tiền: không áp dụng: mã đo không có giá.
11. Quy luật luôn đúng: không bán vượt, không khách nào vượt giới hạn, mỗi đơn vị gắn đúng một dòng đơn: kiểm bằng `verify` sau mỗi test và mỗi lần đo tải. Không dùng rapid: đây là mã đo, vùng rủi ro cao thật (idempotency) có rapid ở Task 9.
12. Dữ liệu cá nhân: không áp dụng: chỉ có mã khách dạng UUID; máy chủ đo không log yêu cầu.
13. Quyền: không áp dụng: mã đo không có người dùng.

## Quyết định thay chủ shop

Chủ shop vắng tới sáng 19/11 và giao quyết trong phạm vi kế hoạch, spec. Các quyết định:

1. **Step 6 (chạy lại trên máy thử 13.140.64.56) bị giữ lại** (điều phối viên quyết): không vào máy chủ. Ghi ở "Việc còn lại"; làm tiếp Step 7 với kết quả đo trên máy này. Phương án khác: dừng chờ OK — bỏ vì chặn cả task chỉ vì một bước ngoài máy.
2. **Cổng dev đọc từ biến môi trường** (điều phối viên quyết): `${CRM_PG_PORT:-5442}`, `${CRM_JAEGER_UI_PORT:-16686}`, `${CRM_OTLP_PORT:-4318}`; test lấy chuỗi kết nối từ `CRM_TEST_DATABASE_URL` (mặc định cổng 5442). Lý do: nhiều phiên task chạy song song trên một máy. Mặc định giữ đúng cổng của kế hoạch.
3. **Chuỗi kết nối role chủ bảng cho test:** `CRM_TEST_OWNER_DATABASE_URL`; thiếu thì lấy `CRM_TEST_DATABASE_URL` và thay người dùng, mật khẩu bằng `crm_owner` dev. Vì: test cần hai role (`crm_owner` dựng schema, `crm_app` chạy như máy chủ thật) mà chỉ đổi một biến là đủ đổi cổng. Phương án khác: hai biến bắt buộc — dễ quên một.
4. **Kế hoạch tự mâu thuẫn ở `order_item_id UNIQUE`:** kế hoạch có test "khách mua `qty 2`" và spec 04 mục 15 nói các đơn vị gắn với *một* dòng đơn, nhưng `UNIQUE (order_item_id)` không cho hai đơn vị chung một dòng. Chọn đúng spec: một dòng đơn có `qty = q`, gắn `q` đơn vị; `order_item_id` có index thường (không unique). `verify` (a), (b) viết lại cho đúng khi `qty > 1`: (a) tổng `order_items.qty` = số đơn vị `held` + `sold`, và mỗi đơn đúng một dòng đơn; (b) mỗi dòng đơn gắn đúng `qty` đơn vị (không dòng nào gắn nhiều hay ít hơn). Với `qty 1` (mọi bài k6) hai kiểm này trùng với câu chữ của kế hoạch. Phương án khác: mỗi đơn vị một dòng đơn `qty 1` để giữ `UNIQUE` — trái spec. Dễ đảo: chỉ là mã đo.
5. **`running` quá hạn được nhận lại** (đúng spec 05 mục 7.1) thay vì luôn 409 như câu ngắn của kế hoạch; chỉ khi `request_hash` trùng. Spec đứng trên kế hoạch (quy trình mục 2).
6. **Lượt hết suất không giữ phần giới hạn đã ghi:** thứ tự theo kế hoạch là ghi `flash_sale_claims` trước rồi mới lấy đơn vị; khi hết suất (kết quả cuối, phải lưu) transaction trừ lại đúng phần vừa ghi (`quantity - q`, dòng đã bị chính transaction này khóa) rồi lưu `sold_out` cùng transaction. Có thể để lại dòng `quantity = 0`; không ảnh hưởng kiểm (c), (d). Phương án khác: `SAVEPOINT` — thêm một lượt đi về cho *mọi* lượt mua, kể cả lượt mua được.
7. **Bài `spike.js` không dựng đúng 20.000 người ảo của k6 cùng lúc:** máy có 16 GB RAM, 20.000 VU của k6 cần vài chục GB. Dùng bộ chạy `ramping-arrival-rate` (k6 tự cấp VU khi cần) lên 1.000 lượt bấm mỗi giây trong 60 s; 20.000 khách khác nhau (mã khách = số thứ tự lượt bấm chia lấy dư 20.000); thử lại với cùng khóa khi nhận `retry`. Phương án khác: 20.000 VU thật — máy treo, số đo vô nghĩa.
8. **k6:** đã có sẵn trên máy (Homebrew, v2.0.0), không cài thêm; ghim `K6_VERSION := v2.0.0` và ghi một dòng vào `docs/phu-thuoc.md` (công cụ đo, không phải thư viện của ứng dụng).
9. **Kết quả đo chỉ để tham khảo:** k6, máy chủ Go và PostgreSQL (trong máy ảo Docker) chạy chung laptop của chủ shop; ghi rõ trong `docs/do-luong/flash-sale-go.md`. Đánh giá chính thức theo ngưỡng ADR-44 chờ Step 6.
10. **Kế hoạch đổi giữa chừng:** kế hoạch và spec được sửa theo ADR-46 ngày 18/11 trong lúc task đang chạy (bản đọc lúc đầu chưa có). Reviewer phát hiện; đã làm theo bản mới (chạy lại transaction, giới hạn thời gian theo transaction, bộ đếm, số deadlock) và đo lại toàn bộ Step 5. Giữ `inTenantTx` (không chạy lại) cho `setup`, `verify` và test: đó là công cụ đo, không nằm trên đường mua.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 2: test đỏ trước khi có mã | cả bộ `buy_test.go` | `cd spikes/flashsale && go test -race -count=1 ./...` (18/11, trước khi viết `buy.go`) | `undefined: openPool, applySchema, seed, newBuyServer, verify` → `FAIL [build failed]` |
| 10 suất, 50 khách → đúng 10 bought, 40 sold_out, verify đạt | `TestTenUnitsFiftyBuyersGetExactlyTen` | `go test -race -count=1 -v ./...` trong `spikes/flashsale` | `--- PASS` |
| Cùng khóa gửi 2 lần → lần hai nhận đúng body lần một (so từng byte), 1 đơn | `TestSameKeyTwiceReturnsFirstBody` | như trên | `--- PASS` |
| Cùng khóa, body khác → 422, vẫn 1 đơn | `TestSameKeyDifferentBodyIs422` | như trên | `--- PASS` |
| Thiếu khóa, khóa sai ký tự, khóa 129 ký tự → 400, không ghi dòng khóa (thêm ngoài kế hoạch) | `TestMissingOrBadKeyIs400` | như trên | `--- PASS` |
| Một khách, `-limit 2`, 20 goroutine khác khóa → 2 bought, 18 over_limit | `TestOneCustomerLimitTwoOnlyTwoBought` | như trên | `--- PASS` |
| Một đơn vị đang bị transaction khác khóa 200 ms, mua `qty 2` → 503 retry `after_ms 500`, không còn dòng khóa, không tính vào giới hạn, không báo hết suất; thử lại cùng khóa sau khi nhả → bought | `TestLockedUnitGivesRetryNotSoldOut` | như trên | `--- PASS` |
| Ngoài ngữ cảnh tenant đếm `spike.orders` bằng `crm_app` → 0, không lỗi; cả trên kết nối vừa dùng cho transaction có tenant | `TestNoTenantContextSeesNoRows` | như trên | `--- PASS` |
| `verify` báo sai khi dữ liệu sai (thêm ngoài kế hoạch, để "verify đạt" có nghĩa) | `TestVerifyCatchesBrokenData` | như trên | `--- PASS` |
| `verify` báo (b) khi một đơn vị bị gắn sang dòng đơn khác (thêm sau review) | `TestVerifyCatchesUnitOnWrongLine` | như trên | `--- PASS` |
| ADR-46: chờ khóa quá 200 ms (`55P03`) thì chạy lại cả transaction; khóa nhả kịp → bought, bộ đếm `55P03` ≥ 1, không hết lượt | `TestLockTimeoutIsRetriedThenBought` | như trên | `--- PASS (0.33s)` |
| ADR-46: chạy lại 3 lần vẫn `55P03` → 503 retry, bộ đếm `55P03`=3, `exhausted`=1, không còn dòng khóa; gửi lại cùng khóa → bought | `TestLockTimeoutExhaustedGivesRetryNotStored` | như trên | `--- PASS (1.04s)` |
| Hai test ADR-46 đỏ khi không chạy lại | hai test trên | tạm đặt `maxTxRetries = 0`, `go test -count=1 -run LockTimeout ./...`, rồi trả lại 3 | `FAIL: ... được 503 {"after_ms":500,"result":"retry"}` và `bộ đếm chạy lại: map[exhausted:1]` |
| Không có tranh chấp dữ liệu, không chập chờn | cả bộ (11 test), 5 lần liền | `go test -race -count=5 ./...` | `ok elimaz.com.vn/crm/spikes/flashsale 10.511s` |
| Step 4: cả bộ xanh | cả bộ | `go test -race -count=1 -v ./...` | 11 `--- PASS`, `ok elimaz.com.vn/crm/spikes/flashsale 3.561s` |
| Module gốc dựng được | — | `go build ./...` ở gốc | thoát 0 (`matched no packages`: chưa có gói) |
| Định dạng, vet | — | `gofumpt -l .`, `goimports -l .`, `go vet ./...` trong `spikes/flashsale` | không in tệp nào, vet sạch |
| PostgreSQL dev có ba role đúng thuộc tính | — | `psql -U postgres -d crm -c '\du'` trong container | `crm_app` (không thuộc tính), `crm_owner`, `crm_search` "Cannot login, Bypass RLS" |
| Step 5 (cấu hình ADR-46): `spike.js` tắt và bật `-otel`, `per_customer.js`, `sustained.js`, `verify` sau mỗi lần; số lần chạy lại theo SQLSTATE, hết lượt, deadlock trước/sau | 8 lần chạy (spike tắt dấu vết 3 lần, trong đó lần 1 bị nhiễu bởi phiên Task 2; bật 2 lần; bắt đầu ở đỉnh 1 lần; per_customer; sustained) | lệnh ở cuối `docs/do-luong/flash-sale-go.md` | `verify: ĐẠT` cả 8 lần; spike p95 máy chủ 8,8–9,9 ms (tắt dấu vết), 22,9–39,5 ms (bật); 500 suất → 500 đơn; per_customer 2 bought / 98 over_limit; sustained 15.000 đơn, p95 8,3 ms; chạy lại transaction 0, hết lượt 0, deadlock 0 → 0 ở mọi lần. Bảng đầy đủ và đề xuất giá trị ADR-46 trong `docs/do-luong/flash-sale-go.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `order_item_id` không unique; `verify` (a), (b) viết cho cả `qty > 1` (Quyết định 4). Thêm hai `CHECK` ở `schema.sql`: đơn vị `held`/`sold` phải có `order_item_id` và ngược lại; khóa `done` phải có kết quả.
- Khóa `running` quá hạn được nhận lại (Quyết định 5); lượt hết suất trả lại phần giới hạn vừa ghi (Quyết định 6).
- Thêm ba test ngoài danh sách Step 1: `TestMissingOrBadKeyIs400`, `TestVerifyCatchesBrokenData`, `TestVerifyCatchesUnitOnWrongLine`; hai test cho phần ADR-46 của Interfaces: `TestLockTimeoutIsRetriedThenBought`, `TestLockTimeoutExhaustedGivesRetryNotStored`.
- `TestLockedUnitGivesRetryNotSoldOut` giữ khóa ít nhất 200 ms **và** tới khi lượt mua đã có câu trả lời (không phụ thuộc tốc độ máy; reviewer gợi ý).
- Câu trả lời 503 dùng body của kế hoạch (`{"result":"retry","after_ms":500}`), không dùng `retry_later` + `Retry-After` của ADR-46 mục 2 (đó là luật của `api` thật, Task 9).
- `spike.js` dùng bộ chạy theo tốc độ đến thay cho 20.000 VU (Quyết định 7); có biến `START_RATE` cho một lần chạy thêm bắt đầu ở đỉnh. Thêm tệp `k6/common.js` (phần chung của ba bài đo). `per_customer.js` đặt `batchPerHost: 100` (mặc định của k6 chỉ 6 yêu cầu song song mỗi máy; lần chạy đầu bị như vậy, đã chạy lại).
- `main.go` có ba lệnh `setup` (dựng lại schema, nạp suất), `serve`, `verify`; cổng `-addr 127.0.0.1:4190` (cổng `api` dev).
- `Makefile` mới có `db-up`, `db-down`, `db-reset`, `spike-test`; `CLAUDE.md` mục Lệnh thêm cách chạy song song nhiều phiên.

## Việc phát hiện thêm

- **Cổng quy trình không phủ `spikes/`:** hook Stop chỉ chạy `go list ./...` của module gốc, nên không build, vet, test module riêng `spikes/flashsale`; `.githooks/pre-commit` và `staged_code_hash` (`scripts/agent/lib.sh`) không tính `spikes/`, `docker/` là mã, nên dấu duyệt reviewer không gắn với các tệp này. Task này vẫn chạy test tay và cho reviewer xem toàn bộ phần stage. Đề xuất: thêm `spikes` vào `CODE_PATHS` và cho hook Stop chạy test của module trong `spikes/*` khi đổi (tệp quy trình bị khóa, chủ shop quyết).
- `golangci-lint` chưa cài trên máy (Task 3 cài); task này chỉ chạy `go vet`, `gofumpt`, `goimports`.
- Kế hoạch và spec đổi (ADR-46) trong lúc task đang chạy, không có thông báo cho phiên đang làm; chỉ reviewer phát hiện. Đề xuất: phiên điều phối báo các phiên đang chạy khi sửa kế hoạch.
- Phiên Task 2 chạy thử khôi phục PostgreSQL trong cùng máy ảo Docker làm nhiễu một lần đo; đo tải nên chạy khi không phiên nào khác dùng Docker.
- Bật dấu vết làm p95 tăng gấp 2,4–5 lần trên máy này (lẫn ảnh hưởng của Jaeger cùng máy); có thể phải chọn lấy mẫu dưới 100% hoặc bỏ span từng câu SQL. Chờ số của Step 6.
- Cổng 8333 (SeaweedFS của kế hoạch) đang bị container `trivietbook-s3-1` của dự án khác giữ trên máy này; Task 2 cần cổng khác hoặc đọc cổng từ biến môi trường.
- Homebrew có k6 2.4.0; máy đang dùng 2.0.0 (ghim theo bản đang có). Nâng bản là việc riêng.

## Việc còn lại

- Step 6: chạy lại Step 5 trên máy thử 13.140.64.56 (container thử, k6 từ máy khác, xóa container sau khi đo) và đánh giá theo ngưỡng ADR-44. Cần OK riêng của chủ shop; bị giữ lại theo quyết định 1.

## Reviewer

Vòng 1 (agent `reviewer`, 18/11): **CẦN SỬA**, hai phát hiện, đều đúng và đã sửa:
1. Thiếu phần chạy lại theo ADR-46 mà Interfaces của Task 1 (bản sửa 18/11) đòi; tệp task ghi sai lý do "ADR-46 chưa duyệt". Đã thêm `retryTx` (set_config ba giá trị một câu, chạy lại 3 lần có chờ ngẫu nhiên, bộ đếm theo SQLSTATE và hết lượt), hai test, sửa mục Soát thiết kế 6.4, 6.9, Quyết định 10.
2. Kết quả đo thiếu số lần chạy lại, số lần hết lượt, deadlock trước/sau, đề xuất giá trị ADR-46. Đã đo lại toàn bộ Step 5 và ghi đủ.
Gợi ý không bắt buộc đã làm: test khóa đơn vị giữ khóa tới khi có câu trả lời; `verify` test kiểm đúng tên (c), (d) và thêm ca (b). Ghi chú về `applySchema` dùng giao thức truy vấn đơn: để Task 23 loại trừ `spikes/` khi viết test kiến trúc.

Vòng 2 (agent `reviewer`, 18/11): **ĐẠT**, không phát hiện phải sửa. Đã kiểm `retryTx` (tối đa 4 lần gọi, lỗi lúc commit, `57014` không chạy lại, ctx hủy, trạng thái gán lại mỗi lần, bộ đếm), hai test ADR-46 và số đo mới. Hai ghi chú cho Task 4: ADR-46 còn đòi một dòng log mỗi lần chạy lại (mã đo chỉ có bộ đếm, kế hoạch Task 1 chỉ đòi bộ đếm); lần gặp lỗi cuối khi hết lượt chỉ đếm vào `exhausted`, giữ cùng cách đếm ở `db.RetryTx`.

Kết luận: ĐẠT
