# Task 104 — M3-13: So phí các hãng ngay trên đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 104, M3-13, ca VC02). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM, không đổi chữ ký có sẵn. Mới trong `fulfillment`:
  `FeeQuote`, `Service.QuoteNow`, `QuoteKind()`, `QuoteKindName`, `QuoteCommand` + `NewQuoteCommand`, `QuoteRequest`,
  `QuoteAccount`, `QuoteAccepted`, `Service.RequestQuotes`, `QuoteRow`, `Service.ListQuotes`, `QuoteParcel`, năm hằng
  `QuoteErr*` (tệp `quote.go`, `quote_command.go`, `quote_request.go`; cửa test `NewQuoteCommandTimeout`,
  `CleanupQuotes` trong `export_test.go`). Thân
  `quotesHandlers()`, `quotesCleanups()` của `area_quotes.go` (khung Task M3-04, chỗ gọi duy nhất là `wire.go`) đổi.
  Hai phương thức `RequestShippingQuotes`, `ListShippingQuotes` của `httpapi.handler` (khung 501) đổi thân.
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi cmd/crm, internal/adapters/carrier/{,best,carriertest,
  fake,manual,spx,vtp}, internal/httpapi, internal/modules/cash (+ internal/formats, statement), fulfillment/internal/*,
  testkit/shiptest, tools/fakecarrier/server. Chỉ thêm ký hiệu nên không gói nào phải sửa; cổng ci-task chạy test của
  các gói này.
- Operation OpenAPI bị chạm, oasdiff: `requestShippingQuotes`, `listShippingQuotes` (đã có trong hợp đồng từ Task
  M3-04; `openapi.yaml` không đổi → oasdiff không chênh). Xóa hai dòng khỏi `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn so phí là task màn hình M3 sau); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/quotes.sql` (tài khoản báo phí của đơn, chèn dòng
  pending, đọc dòng pending, xóa pending + chèn dòng ok, ghi lỗi, danh sách theo lần báo phí, dọn quá 7 ngày) trên bảng
  `fulfillment.shipping_quotes` (có từ 0300) và đọc `fulfillment.carrier_accounts` (kèm `default_options`). Migration
  mới `0320_fulfillment_quotes_delete.sql` (pha mở rộng: chỉ `GRANT DELETE ON fulfillment.shipping_quotes TO crm_app`;
  0300 chỉ cấp SELECT, INSERT, UPDATE mà thay dòng pending và dọn 7 ngày đều cần xóa). Bảng, index, unique đã đủ.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/ -run Quote`, `go test -race ./internal/httpapi/ -run
  'ShippingQuotes|M3Routes'`, rồi cổng ci-task một lần trước review (đổi migration → toàn bộ test).
- `cmd/crm`: không sửa — lệnh báo phí vào qua `Module.CommandHandlers()` (`quotesHandlers`), hàm dọn qua
  `Module.Cleanups()` (`quotesCleanups`), cả hai đã được `wire_m3.go` đăng ký từ Task M3-04. Test khung
  `cmd/crm/wire_m3_test.go` (`TestWireM3_KeepsM1Parts`) đòi 0 hàm dọn → đổi thành đúng 1 (xem Test bị xóa hoặc tắt).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về báo
  phí); trong `fulfillment`: `InputHash` (Task M3-03), `ResolveAccount`/`account` (giải mã + địa chỉ lấy hàng),
  `LocationMap` (Task M3-18), `isTimeout`, `transientDBError`, `LoadSettings` (`QuoteValidMinutes`,
  `DefaultWeightGramsPerItem`), `publishShipment` (mẫu báo hiệu), `commands.CommandKind`, `RefreshCommand` (mẫu lệnh đọc:
  Looker luôn not_sent), `ActiveAccounts` (Task M3-09).
- Dùng lại / viết mới vì: dùng lại `InputHash`, `ResolveAccount`, `LocationMap`, `isTimeout`, `LoadSettings`,
  `commands.CommandKind`, `accountWrite`/`refusableWrite`, `canAt`, `catalog.DefaultSeller`. Viết mới truy vấn tài
  khoản báo phí của đơn vì `ActiveAccounts` không lọc theo người bán (ADR-29: đơn chỉ báo phí bằng tài khoản của người
  bán của đơn), không trả tên tài khoản (thân 202 cần `name`) và trả cả bộ khóa đã giải mã (đường yêu cầu không cần
  khóa). Phân loại lỗi báo phí viết riêng (`classifyQuoteError`) vì tập mã khác lệnh tra (`timeout | rate_limited |
  auth | address_unmapped | rejected`, không có not_sent/in_doubt).

## Soát thiết kế

Khớp kiến trúc (5.x):
1. Chủ dữ liệu: `fulfillment.shipping_quotes` thuộc fulfillment (04 mục 3), schema fulfillment.
2. Không ghi bảng module khác: chỉ ghi `shipping_quotes` và `platform.jobs` (qua `queue.Enqueue`).
3. Đọc module khác: đơn qua `order.Get` (public API; địa chỉ giao không có trong `ord.orders_v`, như Task 103 ghi), kho
   lấy hàng qua `inventory.warehouses_v` (đường `account` có sẵn).
4. Nguyên tử: một transaction ghi mọi dòng pending và mọi việc báo phí; `OnDone`/`OnTerminal` thay dòng trong tx của
   Runner.
5. Không nguyên tử: gọi hãng là lệnh `command.shipping.quote` chạy sau commit.
6. Gọi ra ngoài chỉ trong `Execute` (ngoài transaction) qua `QuoteNow`; báo phí là đọc → `Looker` luôn not_sent,
   `AutoRetryNotSent = true` (Q-3).
7. Mọi câu trong `tenancy.WithTenant`/`WriteTx`; bảng có RLS, khóa ngoại ghép `tenant_id` từ 0300.
8. Module không nhập net/http; handler mỏng.
9. `requestShippingQuotes` có `x-permission: fulfillment.quote` + `IdempotencyKey` (hợp đồng có sẵn).
10. Không dùng Valkey. 11. Không thư viện mới. 12. Theo mẫu lệnh `RefreshCommand`, `checkCommand`.

Đồng thời, an toàn (6.x):
1. Hai lần ghi kết quả cho cùng một tài khoản: `OnDone`/`OnTerminal` chỉ đổi dòng còn `pending` (câu có điều kiện
   `status = 'pending'`, 0 dòng → không làm gì); UNIQUE `(tenant_id, request_id, carrier_account_id, service)`.
2. Bấm hai lần: `Idempotency-Key` của middleware → cùng `requestId`; DedupeKey `request_id:account`.
3. Khóa: không khóa dòng nào (đọc đơn không khóa, chèn dòng mới) → không deadlock.
4. Lỗi tạm database trên đường yêu cầu: `WriteTx` sẵn có chạy lại; trong thân gán lại mọi biến kết quả (requestId sinh
   lại mỗi lần chạy).
5. READ COMMITTED mặc định.
6. Việc nền lỗi: lỗi đọc database → not_sent (thử lại tới hạn 2 phút) → `OnTerminal` ghi `error timeout`.
7. 429: chờ `Retry-After` (≤ 20 s, lượt < 3), quá thì ghi `error rate_limited`.
8. Chết giữa chừng: lệnh in_doubt → `OnTerminal` không đổi gì (Looker not_sent → lệnh về hàng chờ, chạy lại); dead hay
   expired → `error timeout`; mọi lần ghi chỉ đổi dòng còn pending.
9. Treo: mỗi lần gọi hãng có hạn (mặc định 10 s, tối đa 10 s) trong hạn lệnh 12 s.
10. Tiền: `int64` đồng; phí ≤ 0 hay vượt 2^53−1 không bao giờ thành dòng `ok` (bỏ dịch vụ đó; không còn dịch vụ nào →
    `error rejected`). `feeTotal` chỉ có ở dòng `ok` → không bao giờ trả 0 thay lỗi.
11. Quy luật: test rapid trên `QuoteNow` (mỗi tài khoản đúng một FeeQuote; có kết quả xor có mã lỗi; không kết quả nào
    phí ≤ 0).
12. Dữ liệu cá nhân: payload việc chỉ có mã (đơn, tài khoản, lần báo phí, kho, người bán), cân nặng, giá trị khai, tùy
    chọn không ghi chú; địa chỉ đọc lại từ đơn trong `Execute`; kết quả việc chỉ có phí, dịch vụ, băm.
13. Quyền: `fulfillment.quote` theo kho của đơn (`canAt`), đơn đọc theo người bán mặc định; báo hiệu
    `shipping_quote.ready` đòi `fulfillment.quote` trên `WarehouseScope(kho của đơn)`.

## Quyết định thay chủ shop

1. **Quyền DELETE cho crm_app (migration 0320).** Kế hoạch đòi `OnDone` "thay dòng pending bằng một dòng mỗi dịch vụ"
   và dọn báo phí quá 7 ngày; 0300 không cấp DELETE (cột `service` cũng không được UPDATE). Chọn thêm migration chỉ
   `GRANT DELETE` (số 0320 do điều phối viên cấp). Khác: cho UPDATE cột `service` và biến dòng pending thành dịch vụ đầu
   — vẫn cần migration và dọn 7 ngày vẫn cần DELETE. Đảo ngược: một migration REVOKE.
2. **Câu trả lời chắc chắn của hãng là `done` kèm kết quả** (cả mã lỗi `timeout`, `auth`, `rejected`, `address_unmapped`,
   `rate_limited` quá 20 s), `OnDone` ghi dòng; `OnTerminal` chỉ ghi `error timeout` khi dead/expired. Lý do:
   `TerminalHook` không nhận lý do của lệnh nên không ghi đúng mã được; báo phí là đọc, không cần thử lại khi hãng đã
   trả lời. Hãng quá hạn → `timeout` ngay (người bấm "Báo phí lại"), không chờ 8 lượt.
3. **in_doubt không đổi dòng** (khác câu chữ ban đầu ở Soát thiết kế 8): Looker luôn not_sent nên lệnh chạy lại và ghi
   kết quả thật; dòng chỉ thành `timeout` khi lệnh hết hạn 2 phút.
4. **Tài khoản chọn tường minh mà không dùng được** (không active, nhập tay, kho khác kho của đơn, hãng không báo phí)
   → 409 `carrier_account_unavailable`, không ghi gì; không có hay của người bán khác → 404. Bỏ trống → chỉ lấy tài
   khoản dùng được của người bán của đơn ở kho của đơn (có thể rỗng: 202 với `accounts: []`).
5. **Đầu vào báo phí:** cân nặng bỏ trống = `DefaultWeightGramsPerItem` × tổng số món; giá trị khai bỏ trống = 0; tùy
   chọn bỏ trống = tùy chọn mặc định của từng tài khoản; ghi chú không vào payload, không vào băm. Gói thành
   `QuoteParcel` xuất ra để task tạo vận đơn tính băm giống hệt (xem Việc phát hiện thêm).
6. **Hãng có Locations:** địa chỉ phải có cả tỉnh và phường đã khớp; thiếu một → `address_unmapped`, không gọi hãng.
7. **Khóa API không giải mã được, tài khoản đổi người bán** lúc chạy → `unavailable` (không phải `auth`).
8. **`quoteId` chỉ trả ở dòng `ok`** (dòng lỗi, pending không dùng để tạo vận đơn được).

## Bằng chứng

Lệnh A: `CRM_PG_PORT=5600 go test -race ./internal/modules/fulfillment/ -run 'TestQuote'` → `ok ... 17.6s`.
Lệnh B: `CRM_PG_PORT=5600 go test -race ./internal/httpapi/ -run 'TestShippingQuotesAPI|M3Routes'` → `ok ... 4.6s`.
Cổng ci-task lần 1 (trước khi stage): đỏ lint (gocognit 4, nolintlint 1), jscpd (2 đoạn giống), test khung wireM3,
gen-check (chỉ vì chưa stage); customer, order đỏ vì PostgreSQL chung hết chỗ nối (SQLSTATE 53300, không do task này).
Đã tách hàm, bỏ đoạn giống, sửa test khung; lần 2 (sau khi stage): cổng ci-task → `CI-TASK XANH (so với điểm tách
nhánh 1a068c8)`, test toàn bộ 401 giây, mọi gói `ok`.
Test thất bại trước: phiên trước viết `quote_test.go` khi chưa có `QuoteNow` (không biên dịch); `quote_flow_test.go`
chạy lần đầu đỏ (`RequestQuotes: không có tài khoản hãng` — lỗi so số tài khoản khi bỏ trống), sửa rồi qua.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| VC02: 3 tài khoản, A 2 ok, B 1 ok, C timeout; sắp 28.000, 31.500, 41.000, C; không phí 0 | `TestQuotes_VC02` | A | PASS |
| ok có `valid_until = now + 30'`, `input_hash = InputHash(đầu vào, dịch vụ)` | `TestQuotes_VC02` | A | PASS |
| Báo hiệu `shipping_quote.ready` đòi `fulfillment.quote` ở kho của đơn, mỗi lần ghi một | `TestQuotes_VC02` | A | PASS |
| B tạm dừng giữa chừng → unavailable, không gọi hãng | `TestQuotes_AccountPausedMidway` | A | PASS |
| Địa chỉ chưa khớp → address_unmapped, không gọi hãng | `TestQuotes_AddressUnmapped`, `TestQuoteNow_Classify` | A | PASS |
| 401 → auth + tài khoản auth_failed; 429 > 20 s → rate_limited; 429 5 s → chờ | `TestQuotes_AuthAndRateLimit` | A | PASS |
| dead/expired → timeout; in_doubt không đổi; kết quả muộn không ghi đè | `TestQuotes_OnTerminal` | A | PASS |
| Đã có vận đơn → ErrShipmentExists; tài khoản lạ/người bán khác → NotFound; không báo phí được → Unavailable | `TestQuotes_RequestRefusals` | A | PASS |
| Dọn quá 7 ngày (quyền DELETE 0320) | `TestQuotes_Cleanup` | A | PASS |
| QuoteNow A, B, C hạn 3 s → C timeout ≤ 3,5 s, không database (Service không DB) | `TestQuoteNow_ParallelTimeoutNoDB` | A | PASS |
| Hạn kẹp 10 s; phân loại lỗi; phí ≤ 0 / quá 2^53−1 bị bỏ | `TestQuoteNow_TimeoutClamped`, `TestQuoteNow_Classify` | A | PASS |
| Quy luật: mỗi tài khoản một FeeQuote, kết quả xor mã lỗi, không phí ≤ 0 | `TestQuoteNow_Property` (rapid 200) | A | PASS |
| API: 202 hai tài khoản; cùng khóa → cùng requestId, 2 việc; kho khác 403; sắp ok rồi lỗi, lỗi không feeTotal; đơn lạ 404; có vận đơn 409 | `TestShippingQuotesAPI` | B | PASS |
| Hai operation không còn 501, danh sách chờ đã xóa | `TestM3Routes_*` | B | PASS |

## Test bị xóa hoặc tắt

- Không xóa, không tắt test nào. Đổi một điều kiện: `TestWireM3_KeepsM1Parts` (`cmd/crm/wire_m3_test.go`) từng đòi
  `len(Schedules)+len(Cleanups) == 0` ("khung chưa có lịch, dọn"); vùng quotes nay có hàm dọn báo phí quá 7 ngày nên
  đòi đúng `0` lịch và `1` hàm dọn (chặt như cũ, không nới).
- `tests/arch/testdata/m3_pending.txt`: xóa `requestShippingQuotes`, `listShippingQuotes` (hai operation đã làm, đúng
  luật của tệp).

## Khác kế hoạch

- Mã nằm ở gói `fulfillment` (`quote.go`, `quote_command.go`, `quote_request.go`), không ở `internal/quote`: `QuoteNow`
  là phương thức của `Service` dùng adapter, `account`, `LocationMap`, `MarkAuthFailed` chưa xuất; tách gói con sẽ phải
  xuất các hàm nội bộ đó. Test ở `quote_test.go`, `quote_flow_test.go`.
- `QuoteKind` là hàm (`QuoteKind()`) như `CheckKind()`, `CreateCommandKind()`, không phải `var` (luật không biến toàn
  cục thay đổi được).
- Báo hiệu SSE: test kiểm tín hiệu ghi lại (`Requires`, `Scope` kho của đơn); lọc theo quyền, kho của PGHub đã có test
  ở `internal/platform/realtime/scope_test.go`, không lặp lại luồng SSE thật.
- Hãng C "treo 20 s" trong test luồng dùng hạn gọi 1 s (`NewQuoteCommandTimeout`) để test không chờ 10 s; hạn 10 s
  thật được kiểm ở `TestQuoteNow_TimeoutClamped`.
- Migration 0320 (Quyết định 1).

## Việc phát hiện thêm

- Task 103 (gửi hãng, `quote_stale`): cần tính băm bằng `fulfillment.InputHash(địa chỉ đơn, QuoteParcel(đơn, cân,
  giá trị khai, cài đặt), COD, dịch vụ, tùy chọn không ghi chú)` để khớp `input_hash` của dòng ok; tùy chọn bỏ trống
  là tùy chọn mặc định của tài khoản.
- (Ghi chú reviewer, không bắt buộc) `TestQuoteNow_TimeoutClamped` chờ 10 s thật và so khoảng 9–11 s: có thể đổi sang
  kiểm `ctx.Deadline()` adapter nhận được (luật test thời gian, Task 400). `DeleteOldQuotes` chưa có `FOR UPDATE SKIP
  LOCKED`: vận đơn đang tạo cùng lúc trỏ tới báo phí quá 7 ngày có thể làm hỏng lượt dọn của tenant đó (rủi ro thấp).

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau ci-task xanh). Phát hiện phải sửa: không có. Đủ yêu cầu, bằng chứng có
thật, ảnh hưởng khớp hai chiều, tái dùng đúng, không sót cách cũ, test khung wireM3 chỉ đổi điều kiện (vẫn chặt), luật
dự án đạt (không dữ liệu cá nhân trong payload/kết quả, gọi hãng ngoài transaction, goroutine có chủ, migration chỉ
GRANT). Ghi chú không bắt buộc (không sửa, chép sang Việc phát hiện thêm): `TestQuoteNow_TimeoutClamped` đo giờ thật
9–11 s; `TestQuotes_OnTerminal` chưa chạy ca `dead` (cùng nhánh với expired); `DeleteOldQuotes` không SKIP LOCKED;
`quoteAnswer` thừa.
