# Task 265 — Sự kiện hãng và dòng bảng kê của đơn ngoài hệ thống

Task M7-20. Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 265). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `webhooks.Inbound` (struct, `internal/platform/webhooks/inbound.go`): **thêm** hai trường `Attempts int`, `LastAttempt bool` (do `inboundJobHandler` điền từ `queue.Job.Attempts` và `Kind().Policy.MaxAttempts`). Chỉ thêm, 10 chỗ dựng `webhooks.Inbound{…}` đều dùng tên trường nên không gãy.
  - `inbound.Options` (gói nội bộ của fulfillment): thêm trường `Record RecordUnmatched` (kiểu hàm mới `inbound.RecordUnmatched`, ghi bảng `unmatched_carrier_events`) và `Now func() time.Time` (nil: `time.Now`); 1 chỗ dựng (`area_inbound.go`).
  - `inbound.Handler.Handle`: đổi hành vi nhánh "không thấy vận đơn" (hai nhánh mới của kế hoạch); chữ ký không đổi.
  - Mới: `fulfillment.RecordUnmatched`, `fulfillment.ListUnmatched`, `fulfillment.UnmatchedEvent`, `fulfillment.UnmatchedPage`, `fulfillment.MetricCarrierEventUnmatched`; `inbound.MetricUnmatched`.
  - `(*handler).ListUnmatchedCarrierEvents` (httpapi): thay thân khung 501.
- Gói phụ thuộc ngược (go list): `platform/webhooks` ← cmd/crm, adapters/{carrier,facebook,fake}, modules/{cash,channel,channel/channeltest,conversation (+ internal/deps, history, inbound, live),fulfillment, fulfillment/internal/inbound}, platform/selftest. `modules/fulfillment` ← httpapi, cash, cmd/crm, … (ci-task tự lấy theo `affected.py`).
- Operation OpenAPI bị chạm, oasdiff: `listUnmatchedCarrierEvents` (đã có trong hợp đồng của Task 260; chỉ hiện thực, không sửa `openapi.yaml`). Xóa dòng `listUnmatchedCarrierEvents` khỏi `tests/arch/testdata/m7_pending.txt`. oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có màn (màn đơn ngoài hệ thống không thuộc đợt này); console không đổi.
- Câu sqlc, bảng, pha migration: migration mới `0707_fulfillment_unmatched.sql` (pha mở rộng: CREATE TABLE, RLS, GRANT; không chạm bảng cũ). Câu mới `queries/fulfillment/unmatched.sql`: `RecordUnmatchedEvent` (INSERT … ON CONFLICT DO UPDATE), `ListUnmatchedEvents`, `UnmatchedEventExists`; thêm vào `queries/fulfillment/inbound.sql`: `InboundAccountHasPending` (vận đơn của tài khoản chưa có mã hãng, chưa kết thúc; dùng index `shipments_account_tracking` có sẵn). Bảng đọc: `fulfillment.shipments`, `fulfillment.carrier_accounts` (cùng module).
- Test sẽ chạy: `go test -race -run '^(TestUnmatched_.*|TestSignal_.*)$' ./internal/modules/fulfillment/`, `./internal/modules/fulfillment/internal/inbound/` (test mới), `TestExternalLines_M7` (cash/statement), `TestNoNhanh*` (tests/arch), `TestListUnmatchedCarrierEvents*` và `TestM7Contract_StatusCodes` (httpapi), `./internal/platform/webhooks/` (test lượt thử cuối). Cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (metrics.Registry.Inc, tenancy.WithTenant, queue.Policy), mẫu danh sách có con trỏ `fulfillment.ShipmentEvents` / `handlers_shipment_events.go`, `repoRoot` của `tests/arch/lint_test.go`, môi trường test `shipEnv` (`env_test.go`, `trySignalFrom`) và `flowEnv` của `cash/internal/statement`.
- Dùng lại / viết mới vì:
  - Dùng lại `shipEnv`, `trySignalFrom`, `metricCount` cho test webhook; `flowEnv`, `e.statement`, `e.discrepancies` cho test bảng kê (nên tệp test bảng kê đặt trong `cash/internal/statement`, xem Khác kế hoạch).
  - Dùng lại `repoRoot` của `tests/arch` cho `no_nhanh_test`. Quét theo `git ls-files` (tệp repo theo dõi) thay vì duyệt thư mục: lượt ci-task đầu đỏ vì quét trúng `static.log` do CI sinh trong thư mục repo; `walkGoFiles` của `scan_test.go` chỉ đọc tệp `.go` nên không dùng được (jscpd báo đoạn duyệt thư mục lặp với nó, đã bỏ đoạn đó).
  - Dùng lại `idCursor` (httpapi, `handlers_promotion.go`) đọc con trỏ trang — jscpd báo đoạn đọc con trỏ lặp với `handlers_shipment_events.go`.
  - jscpd báo đoạn cắt trang (`len(rows) > p.Limit`) của `ListUnmatched` giống `ShipmentEvents` (`apply_public.go`): giữ riêng (ba dòng, kiểu dòng khác nhau; hàm chung kiểu generic cho hai chỗ không đáng), viết lại cho không còn lặp.
  - Dùng lại mẫu con trỏ là mã dòng (uuid) + kiểm con trỏ có thật → `ErrInvalidPage` (400 `invalid_input` đã ánh xạ ở `errors_fulfillment.go`), không thêm mã lỗi.
  - Viết mới `RecordUnmatched` (bảng mới, không có hàm tương tự) và `inbound.Options.Record` (gói `inbound` là nội bộ, không nhập được gói `fulfillment` vì vòng nhập: truyền hàm như `Enqueue`).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: sự kiện hãng thuộc `fulfillment` → bảng `fulfillment.unmatched_carrier_events`.
- 5.2 Không ghi bảng module khác: chỉ ghi bảng của fulfillment. Phần bảng kê (TI06) chỉ là test kiểm lại, không đổi mã cash.
- 5.3 Đọc module khác qua view: không đọc module khác.
- 5.4 Nguyên tử: ghi `unmatched` trong chính transaction của việc vào (cùng tx với `done` của việc).
- 5.5 Sau commit: chỉ số tăng trong handler như các chỉ số `carrier_signal_*` có sẵn (đếm trong bộ nhớ, không gửi mạng trong tx). Không cảnh báo (Q17).
- 5.6 Gọi ra ngoài: không có; nhánh mới không tra hãng (test đếm lời gọi hãng giả = 0).
- 5.7 Tenant: bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`), `UNIQUE (tenant_id, id)`, `UNIQUE (tenant_id, carrier_key, tracking_code)`, FK ghép `(tenant_id, carrier_account_id)` tới `carrier_accounts`; mọi câu chạy trong tx của việc vào (WithTenant) hoặc `inventoryRead` (WithTenant).
- 5.8 HTTP mỏng: handler chỉ đọc tham số, gọi `fulfillment.ListUnmatched`, đổi kết quả.
- 5.9 Đường ghi: không có route ghi mới (chỉ GET). Quyền `fulfillment.unmatched.read` khai sẵn ở `x-permission` (Task 260).
- 5.10 PostgreSQL là nguồn sự thật: đúng.
- 5.11 Thư viện mới: không.
- 5.12 Theo module mẫu: sqlc trong `queries/fulfillment`, test trên PostgreSQL thật, sentinel có sẵn.

Mục 6:
- 6.1 Hai việc vào cùng mã cùng lúc: `INSERT … ON CONFLICT (tenant_id, carrier_key, tracking_code) DO UPDATE SET event_count = event_count + 1` là một câu nguyên tử, không đọc ra Go rồi ghi lại.
- 6.2 Gửi lại: việc vào đã chống trùng ở tầng 1 (dedupe của webhooks). Webhook hãng gửi lại cùng sự kiện với khóa khác thì đếm thêm (đúng ý "số lần"). Việc vào chạy lại sau lỗi tạm: tx lùi nên không đếm hai lần.
- 6.3 Thứ tự khóa: ADR-46 nhóm 8, `unmatched_carrier_events` cuối nhóm (đã có dòng từ Task 260). Handler chỉ đọc `carrier_accounts`, `shipments` (không khóa) rồi ghi bảng mới; không khóa bảng nào khác sau nó.
- 6.4 Lỗi tạm trên đường yêu cầu: GET đọc trong `WithTenant` có sẵn.
- 6.5 Cách ly: READ COMMITTED + câu upsert.
- 6.6 Việc nền lỗi: đường cũ giữ nguyên (lỗi tạm `ErrShipmentNotYetKnown` khi tài khoản có vận đơn chờ mã); lượt cuối ghi `unmatched` và `done` thay vì `dead` (kế hoạch nhánh b). Lượt cuối tính theo `Kind().Policy.MaxAttempts` của việc vào (`InboundKind`, mặc định 8).
- 6.7 429: không áp dụng: không gọi ra ngoài.
- 6.8 Chết giữa chừng: tx lùi, việc về hàng chờ; upsert chạy lại được.
- 6.9 Treo lâu: một câu upsert + một câu `EXISTS … LIMIT 1` trên index `shipments_account_tracking`.
- 6.10 Tiền: không áp dụng (TI06 chỉ kiểm lại không bút toán).
- 6.11 Quy luật: `event_count` = số lần ghi, test 1.000 sự kiện liên tiếp. Không phải vùng rủi ro cao mới.
- 6.12 Dữ liệu cá nhân: bảng và API chỉ có mã vận đơn, khóa hãng, số lần, thời điểm; không SĐT, tên, địa chỉ.
- 6.13 Quyền: route có `x-permission: fulfillment.unmatched.read` (ScopeNone), guard kiểm; test 403 khi thiếu quyền.

## Quyết định thay chủ shop

1. **Biết "lượt thử cuối" thế nào:** thêm `Attempts`, `LastAttempt` vào `webhooks.Inbound`, điền ở `inboundJobHandler` từ `queue.Job.Attempts >= Kind().Policy.MaxAttempts` (chính sách của việc vào, M0). Phương án khác: thêm `MaxAttempts` vào `queue.Job` (chạm worker của hàng đợi, rộng hơn). Chọn cách chỉ thêm trường, dễ đảo ngược.
2. **`last_raw_status`:** tín hiệu hãng của M3 cố ý không mang trạng thái (B10: trạng thái chỉ đọc qua API có xác thực), và nhánh mới không tra hãng → cột để NULL. Giữ cột như kế hoạch để sau này hãng nào đưa trạng thái trong webhook thì ghi.
3. **Chỉ số `carrier_event_unmatched`:** tăng trong handler (như `carrier_signal_*` có sẵn) sau khi `RecordUnmatched` không lỗi, nhãn là khóa hãng. `RecordUnmatched` chỉ ghi database (không nhận registry) để dùng được từ nơi khác.
4. **"Mã vận đơn của CRM":** đúng mẫu `NewShipmentCode`: `E` + 6 số + 6 ký tự base32 Crockford (`^E[0-9]{6}[0-9A-HJKMNP-TV-Z]{6}$`). Mã vận đơn ghi vào bảng: mã hãng nếu có, không thì mã trong tín hiệu.
5. **Lọc `from`/`to` của danh sách:** ngày theo giờ Việt Nam; một dòng được lấy khi có sự kiện trong khoảng (`last_seen_at >= from 00:00` và `first_seen_at < to+1 00:00`). Sắp mới thấy trước theo `first_seen_at DESC, id DESC` (khóa không đổi khi dòng được cập nhật, con trỏ ổn định). `from > to` → 400 `invalid_input`.
6. **Test M3 `TestSignal_ShipmentNotYetKnown`, ca "tuyến tài khoản khác":** tài khoản đối tác không có vận đơn chờ mã nên theo kế hoạch (nhánh a) tín hiệu giờ được ghi `unmatched` và việc xong, không còn lỗi tạm. Đổi khẳng định của ca đó: không lỗi, vẫn **không** có lệnh tra cho vận đơn của tài khoản kia (Q-7 giữ nguyên) và có đúng một dòng `unmatched` của tài khoản đối tác. Ca VC10 chính (tài khoản có vận đơn chờ mã → lỗi tạm) giữ nguyên.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| MO07: webhook đơn cũ `VTP0000OLD1` qua ingest thật → việc vào `done` lần đầu, `event_count` 1 rồi 2 khi gửi lại, 0 lệnh tra, 0 `dead`, 0 `alert_events`, chỉ số +2; vận đơn CRM cùng tài khoản vẫn có lệnh tra (VC05) | `TestWebhookRouted_OldOrderUnmatched` (adapters/carrier) | `go test -race -count=1 -run '^(TestWebhookRouted_.*)$' -v ./internal/adapters/carrier/` | `--- PASS: TestWebhookRouted_OldOrderUnmatched (0.39s)`; `ok … adapters/carrier 17.068s` |
| Nhánh a mức module: ghi ngay, gửi lại +1, máy hãng giả 0 lời gọi, chỉ số `carrier_event_unmatched{fake}` = 2 | `TestUnmatched_OldOrderRecordedAtOnce` | `go test -race -count=1 -run '^(TestUnmatched_.*\|TestSignal_.*)$' -v ./internal/modules/fulfillment/` | `--- PASS: TestUnmatched_OldOrderRecordedAtOnce (4.17s)` |
| VC10 giữ nguyên + nhánh b: tài khoản có vận đơn chờ mã → lượt 1–7 lỗi tạm, không ghi; lượt 8 (cuối) ghi `unmatched`, không lỗi; mã CRM `E…` lạ → lỗi tạm, lượt cuối ghi | `TestUnmatched_PendingAccountLastAttempt`, `TestSignal_ShipmentNotYetKnown` (M3, ca tài khoản khác đổi theo Quyết định 6) | như trên | `--- PASS: TestUnmatched_PendingAccountLastAttempt (4.62s)`, `--- PASS: TestSignal_ShipmentNotYetKnown (4.17s)` |
| Kiểm lại M3 (tín hiệu có vận đơn → chỉ lệnh tra; hai tài khoản cùng mã; tài khoản bị bỏ; payload sai) | `TestSignal_EnqueuesRefreshOnly`, `TestSignal_SameTrackingTwoAccounts`, `TestSignal_AccountDropped`, `TestSignal_BadPayload`, `TestWebhookRouted_SignalOnly`, `TestWebhookRouted_EarlyAndRevoked` | hai lệnh trên | đều `--- PASS` |
| 1.000 sự kiện ngoài hệ thống liên tiếp → xử lý hết, 0 dead, 0 cảnh báo | `TestWebhookRouted_ThousandOldEvents`, `TestUnmatched_ThousandEvents` | hai lệnh trên | `--- PASS: TestWebhookRouted_ThousandOldEvents (14.09s)`, `--- PASS: TestUnmatched_ThousandEvents (3.63s)` |
| `RecordUnmatched`/`ListUnmatched`: upsert đếm, mốc không lùi, lọc hãng/khoảng, con trỏ, cỡ trang sai → `ErrInvalidPage`, tenant khác 0 dòng | `TestUnmatched_RecordAndList`, `TestUnmatched_ListFilters` | `go test -race -count=1 -run '^(TestUnmatched_.*)$' -v ./internal/modules/fulfillment/` (chạy lại sau khi tách test cho gocognit) | `--- PASS: TestUnmatched_RecordAndList (0.81s)`, `--- PASS: TestUnmatched_ListFilters (0.85s)`; `ok … fulfillment 4.231s` |
| Lượt thử cuối: `Inbound.Attempts`, `LastAttempt` theo `Policy.MaxAttempts` | `TestInboundLastAttempt` (platform/webhooks) | `go test -race -count=1 ./internal/modules/fulfillment/internal/inbound/ ./internal/platform/webhooks/` | `ok … webhooks 8.119s` |
| Nhận dạng mã vận đơn CRM | `TestIsCRMShipmentCode` | như trên | `ok … fulfillment/internal/inbound 1.527s` |
| TI06 kiểm lại: bảng kê 3 dòng (2 khớp, 1 mã lạ) → dòng lạ `external`, không bút toán, tổng COD không đổi, hiện trong "Lệch đối soát" | `TestExternalLines_M7` | `go test -race -count=1 -run '^TestExternalLines_M7$' -v ./internal/modules/cash/internal/statement/` | `--- PASS: TestExternalLines_M7 (1.64s)` |
| `listUnmatchedCarrierEvents`: lọc, con trỏ, 400, 403 thiếu quyền, không dữ liệu khách; route không còn 501 | `TestListUnmatchedCarrierEvents`, `TestListUnmatchedCarrierEvents_Filters`, `TestM7Contract_StatusCodes` | `go test -race -count=1 -run '^(TestListUnmatchedCarrierEvents.*\|TestM7Contract_StatusCodes)$' -v ./internal/httpapi/` | `--- PASS: TestListUnmatchedCarrierEvents (0.99s)`, `--- PASS: TestListUnmatchedCarrierEvents_Filters (0.94s)`, `--- PASS: TestM7Contract_StatusCodes (1.11s)` |
| C20: không mã gọi Nhanh trên mã thật (cả `tests/arch/`), testdata → nêu `sample.go` | `TestNoNhanh_RealCode`, `TestNoNhanh_ArchDir`, `TestNoNhanh_FindsSample` | `go test -count=1 -run '^TestNoNhanh' -v ./tests/arch/` (sau khi đổi sang quét tệp git theo dõi) | `--- PASS` cả ba; `ok … tests/arch 2.458s` |
| Cổng kiểm của task (cổng tĩnh + test gói đổi và gói phụ thuộc) | ci-task | `ci-task.sh` lượt 3 (lượt 1–2 đỏ ở cổng tĩnh: gocognit, gosec G122, nolintlint, gochecknoglobals, jscpd, no-Nhanh quét trúng `static.log` của CI — đã sửa) | run 46130961612: `static: success`, test (1,3,4,5,6) success; test (2) đỏ duy nhất `TestOrderSet_ExplainNoSeqScan` (reporting): `reportseed: chèn đơn hàng loạt … statement timeout (SQLSTATE 57014)` khi nạp 100.000 đơn mẫu trên máy CI; gói reporting chỉ có `models.go` sinh lại. Chạy lại riêng gói (`remote-test.sh <tree> 020b5403 <tệp: ./internal/modules/reporting/>`, không cổng tĩnh) → run 46131617918 `REMOTE-TEST XANH` |

## E2E viết sẵn, chưa chạy

(kế hoạch của task không có e2e)

## Test bị xóa hoặc tắt

- Không xóa, không tắt test nào. Đổi khẳng định của một ca: `TestSignal_ShipmentNotYetKnown` (M3), ca "tuyến tài khoản khác": trước muốn `ErrShipmentNotYetKnown`, giờ muốn không lỗi + 0 lệnh tra cho vận đơn tài khoản kia + 1 dòng `unmatched` của tài khoản đối tác (hành vi mới theo kế hoạch nhánh a; xem Quyết định 6). Ca VC10 chính của test đó giữ nguyên.

## Khác kế hoạch

- Test bảng kê đặt ở `internal/modules/cash/internal/statement/external_lines_m7_test.go` thay vì `internal/modules/cash/external_lines_m7_test.go`: môi trường bảng kê (`flowEnv`, nạp bảng kê, chốt, "Lệch đối soát") nằm trong gói test `statement_test`; đặt ở gói `cash_test` phải chép lại cả môi trường.
- Test webhook mức module ở `internal/modules/fulfillment/unmatched_test.go` (dùng `shipEnv`); `inbound_m7_test.go` trong gói `inbound` kiểm nhận dạng mã CRM và luật chọn nhánh (không database).

## Việc phát hiện thêm

- (Reviewer, ghi chú) Vận đơn kẹt ở `creating` (lệnh tạo `rejected`/`not_sent` mà không ai hủy) làm tài khoản bị coi là "có vận đơn chờ mã" mãi: webhook của đơn cũ trên tài khoản đó đi đủ 8 lượt thử theo lịch lùi rồi mới ghi `unmatched` (không dead, không cảnh báo — MO07 vẫn đạt, chỉ chậm). Nếu M3 không tự hủy các vận đơn này thì cân nhắc thu hẹp `InboundAccountHasPending` theo `create_state`.
- (Reviewer, ghi chú) `LastAttempt` lấy `Kind().Policy.MaxAttempts`, worker dừng theo `max_attempts` lưu trên dòng việc; hiện bằng nhau. Đổi chính sách số lượt của việc vào thì xét đưa `max_attempts` của dòng vào `queue.Job`.

## Reviewer

Kết luận: ĐẠT (lượt 2)

- Lượt 1: CẦN SỬA — mục Ảnh hưởng ghi sai tên câu sqlc (`UnmatchedCursorExists` → `UnmatchedEventExists`), thiếu `inbound.Options.Now` và kiểu `inbound.RecordUnmatched`. Đã sửa; kèm sửa chú thích `ErrShipmentNotYetKnown` ở `webhook_source.go` (hết lượt không còn dead) và ghi hai ghi chú của reviewer vào "Việc phát hiện thêm".
- Lượt 2: ĐẠT, không phát hiện phải sửa. Đủ Interface của kế hoạch; bảng bằng chứng trỏ tới test có thật, kiểm database thật; ca test M3 đổi khẳng định đã ghi tên và lý do; không SQL ngoài sqlc, không dữ liệu cá nhân, migration chỉ mở rộng, ADR-46 nhóm 8 có bảng mới ở cuối.
- Ghi chú không bắt buộc (không làm): tín hiệu mang mã dài hơn 128 ký tự sẽ lỗi CHECK, việc vào hết lượt thành dead (như hành vi M3 cũ với tín hiệu lạ); hãng thực tế không gửi mã dài như vậy.

## Gộp vào main

- Điểm tách 020b5403; main có thêm 139, 219, 261, 264. Gộp `--no-ff` lên 7be0cb3d.
- Xung đột duy nhất: `tests/arch/testdata/m7_pending.txt` — main (Task 264) bỏ `createLegacyReturn`, `listLegacyReturns`, `getLegacyReturn`; nhánh bỏ `listUnmatchedCarrierEvents`. Bản gộp bỏ cả bốn; 25 dòng còn lại đều là stub `ht.ErrNotImplemented` (reviewer đối chiếu từng dòng).
- Mã sinh: 15 tệp sqlc `models.go` tự gộp; `make gen` sinh thêm `FulfillmentUnmatchedCarrierEvent` cho `internal/queries/flashverify/internal/store/models.go` (gói mới của Task 219 trên main). Sau đó `make gen` không chênh.
- Migration `0707_fulfillment_unmatched.sql` không trùng số (main mới nhất 0706).
- Kiểm trên máy: `go build ./...` qua; `go vet` các gói fulfillment, httpapi, platform/webhooks, adapters/carrier, cash, tests/arch, cmd/crm qua.
- Test từ xa: `CRM_CI_POOL_SLOTS=vps=0 CRM_CI_LABEL=integrate-main-265 merge-test.sh b5bbefb5 HEAD <gói> <race>` — gói: cmd/crm, httpapi, fulfillment, cash, platform/webhooks, adapters/carrier, queries/flashverify, tests/arch; -race: fulfillment, platform/webhooks, tests/arch. Kết quả: `MERGE-TEST XANH (github)`, run 46133761318 (static + 6 phần test success, 61 phút máy). Không có test chập chờn.
- Reviewer (phần gộp): **ĐẠT**, không phát hiện phải sửa. Đã kiểm: tương tác với trigger `cash.cod_open_holdings` (Task 139) — dòng external không ghi bút toán nên không chạm trigger; thứ tự khóa ADR-46 không chồng; `no_nhanh_test.go` không bắt nhầm mã legacy return của Task 264. Ghi chú không bắt buộc: `handlers_fulfillment_unmatched.go` tự dựng đầu ngày giờ VN, trùng việc với `vnDayStart` (`handlers_inventory_legacy.go`) — để task sau.
