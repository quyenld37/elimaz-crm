# Task 123 — M3-32: Tự khớp và chốt bảng kê; Lệch đối soát; Quá hạn chưa có tiền

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 123, M3-32). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `statement.NewParser` thêm tham số `ParsedFunc` (1 chỗ gọi:
  `cash/area_statements.go`); `statement.Parser.OnDone` gọi hàm đó khi bảng kê lần đầu thành parsed (câu `FinishParse`
  đổi `:execrows` → `:one RETURNING version`, 1 chỗ dùng). Mới ở `cash` (`area_statements_match.go`):
  `Service.ConfirmStatement`, `Service.ListDiscrepancies`, `Service.ListOverdue`, `ErrStatementChanged`,
  `TopicStatementParsed/Confirmed/Posting`, `TopicCODSettled`, `CODSettled`, `StatementsConsumerName`
  (`cash_statements`), `SignalStatementChanged`, `ConfirmedStatement`, `Discrepancy`, `Overdue`, `Discrepancy*`;
  `statementsConsumers()` trả consumer mới (Module.Consumers có thêm `cash_statements`: test của `cmd/crm`
  `TestWireM3_KeepsM1Parts`, `TestNewModulesRegistersCash` cập nhật danh sách). Mới ở `cash/internal/statement`:
  `Classify`, `Fact`, `Verdict`, `Entries`, `Entry`, `Match`, `Confirm`, `Confirmed`, `PostBatch`, `Batch`, `Posting`,
  `PostFunc`, `LockFunc`, `Settled`, `ErrChanged`, `PostBatchSize`, hằng `Status*`, `Match*`. Gói con mới
  `cash/internal/lists` (`Discrepancies`, `OverdueList`). `httpapi/errors.go`: `ErrStatementChanged` → 409
  `statement_changed`.
- Gói phụ thuộc ngược (go list): `cash` được nhập bởi `internal/httpapi`, `cmd/crm`; `cash/internal/statement`,
  `cash/internal/lists` chỉ bởi `cash`. Test dựng `cash.Deps` không có `Events` phải thêm (import_test.go,
  m3_routes_test.go) vì bảng kê đọc xong cần phát sự kiện.
- Operation OpenAPI bị chạm, oasdiff: `confirmCodStatement`, `listCodDiscrepancies`, `listCodOverdue` (đã có hợp đồng từ
  Task M3-04, thay 501 bằng thân hàm; không đổi `openapi.yaml`, oasdiff không đổi). Bỏ 3 dòng khỏi
  `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn đối soát là task console sau); `console/src/api` không đổi.
- Tài liệu: `docs/views.md` (bảng sự kiện: `cod_statement.*`, `cod.settled`), ADR-46 mục khóa tư vấn (lô chốt).
- Câu sqlc, bảng, pha migration: `queries/cash/statements.sql` (FinishParse trả version; câu khớp, chốt, lô ghi tiền),
  `queries/cash/lists.sql` mới (lệch đối soát, quá hạn). Bảng `cash.cod_statements`, `cash.cod_statement_lines` (cột
  UPDATE đã GRANT ở 0303), chèn `cash.cod_transfers`/`cod_ledger_entries` qua `cash.PostWithID`, chèn
  `platform.domain_events` qua `events.Emit`. Không migration mới (0316 không dùng).
- Test sẽ chạy: `go test -race ./internal/modules/cash/...` (vùng rủi ro cao: cả bộ cash), `./internal/httpapi/ -run
  'CodStatement|M3Routes|Cod'`, `./tests/arch/...`, `./cmd/crm/...`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (events.Emit,
  realtime.Bus, db.NullUUID, tenancy.WithTenant, WriteTx/refusableWrite của httpapi); trong cash: `PostWithID`,
  `LockShipmentCOD`, `BalancesFor`, `shipment_cod_v`; fulfillment: `LoadSettings` (CODOverdueDays).
- Dùng lại / viết mới vì: ghi bút toán chỉ qua `cash.PostWithID` (truyền vào gói statement dạng hàm, tránh vòng nhập);
  khóa vận đơn dùng đúng `LockShipmentCOD` của consumer COD (ADR-46); phân loại dòng là hàm thuần mới `statement.Classify`
  dùng chung cho khớp (xem trước) và lô ghi tiền (quyết định thật), không viết hai lần. Danh sách lệch, quá hạn viết mới
  (chưa có câu nào đọc như vậy).

## Soát thiết kế

5.1 Bảng kê, dòng, bút toán thuộc `cash`. 5.2 Không ghi bảng module khác; chỉ đọc. 5.3 Đọc vận đơn qua
`fulfillment.shipments_v`, mã đơn qua `ord.orders_v` (cash được gọi order, fulfillment), cài đặt qua
`fulfillment.LoadSettings`. 5.4 Chốt (đổi trạng thái theo version) và từng lô ghi tiền là transaction riêng — xem
"Quyết định thay chủ shop" Q1: mỗi lô nguyên tử, mỗi dòng có khóa `remit:<dòng>` nên chạy lại không ghi hai lần.
5.5 Khớp dòng, ghi tiền chạy sau commit qua sự kiện nội bộ (`cod_statement.parsed`, `.confirmed`, `.posting`) do
consumer `cash.statements` xử lý; `cod.settled` phát bằng events.Emit. 5.6 Không gọi ra ngoài. 5.7 Mọi câu trong
WithTenant / tx của worker; không bảng mới. 5.8 Module không nhập net/http; handler mỏng. 5.9 `confirmCodStatement` có
`Idempotency-Key` và `x-permission` (sẵn ở hợp đồng). 5.10 không áp dụng: không Valkey. 5.11 Không thư viện mới. 5.12
Theo mẫu đường ghi Task 12 (handler → refusableWrite → Service → sentinel → errors.go).

6.1 Hai người chốt: `UPDATE … WHERE version = $2 AND status = 'matched'` → 0 dòng → `ErrStatementChanged` (409). Hai
bảng kê cùng vận đơn: lô ghi tiền lấy khóa tư vấn `cash.cod:<vận đơn>` rồi phân loại lại trên số dư mới nhất (trùng →
duplicate). 6.2 Idempotency-Key ở HTTP; khóa bút toán `remit:<dòng>`; sự kiện giao lại thấy dòng đã có
`posted_transfer_id` thì bỏ qua. 6.3 ADR-46: khóa tư vấn theo vận đơn tăng dần **trước** khóa dòng nhóm 9, rồi
`cod_statements` → `cod_statement_lines` theo `line_no` → chèn bút toán; test hai lô song song không deadlock. 6.4 Đường
HTTP dùng WriteTx (RetryTx); consumer chạy lại theo hàng đợi. 6.5 READ COMMITTED + câu có điều kiện + khóa. 6.6 Lỗi →
việc event chạy lại, quá lượt dead. 6.7 không áp dụng: không gọi ra ngoài. 6.8 tx lùi; lô sau chưa xếp thì sự kiện lô
trước chạy lại. 6.9 Lô 200 dòng (≤ 200 khóa tư vấn mỗi transaction, dưới bảng khóa 64×100) để không vượt 30 s. 6.10
int64 đồng, sổ hai vế, database kiểm tổng 0. 6.11 rapid: tổng mọi ngăn 0, mỗi dòng ≤ 1 bút toán, shop_received +
carrier_fee ≤ COD đã thu trừ dòng thừa amount_mismatch dương. 6.12 Payload sự kiện, báo hiệu, audit chỉ có mã; không
SĐT. 6.13 Quyền ở hợp đồng (`cash.statements.confirm`, `cash.cod.read`), kiểm ở guard.

## Quyết định thay chủ shop

- **Q1. Chốt không ghi tiền trong cùng transaction; ghi tiền theo lô sau commit.** Kế hoạch ghi bút toán ngay trong
  WriteTx của `confirmCodStatement`. ADR-46 bắt mọi đường ghi theo số dư lấy khóa tư vấn `cash.cod:<vận đơn>` của
  từng vận đơn; bảng kê tới 50.000 dòng sẽ giữ hàng chục nghìn khóa trong một transaction, vượt bảng khóa của
  PostgreSQL (64 × 100 = 6.400 chỗ dùng chung cả máy) và làm hỏng phiên khác. Chọn: HTTP chỉ đổi trạng thái theo
  version (TI07 giữ nguyên: 409 `statement_changed`) và phát `cod_statement.confirmed`; consumer `cash_statements` ghi
  từng lô ≤ 200 dòng (một transaction một lô, khóa `remit:<dòng>`, sự kiện giao lại bỏ qua dòng đã ghi). Dễ đảo
  ngược: muốn đồng bộ thì gọi `statement.PostBatch` lặp trong WriteTx. Phương án khác: tăng `max_locks_per_transaction`
  (đổi hạ tầng, cần chủ shop), hay chặn bảng kê lớn (trái spec: 50.000 dòng là giới hạn đã duyệt).
- **Q2. Xếp lại dòng lúc ghi tiền trên số dư mới nhất.** Hai bảng kê cùng có một mã có thể cùng khớp "matched" trước khi
  chốt (khớp là xem trước). Lô ghi tiền dưới khóa vận đơn chạy lại `Classify`: vận đơn đã được bảng kê khác trả đủ →
  `duplicate`, không ghi (không trả hai lần). Kết quả khớp có thể đổi sau khi chốt (matched → duplicate/amount_mismatch).
- **Q3. Khớp chạy qua sự kiện nội bộ thay vì loại việc `cash.statement.match` riêng.** Phần nối của cash (`wire.go`,
  `wire_m3.go`) chỉ gom lệnh, consumer, lịch; thêm loại việc giao dịch mới phải sửa ba tệp nối chung đang có task song
  song. Consumer `cash_statements` là việc giao dịch, chỉ database, chạy lại được, thứ tự nghiêm theo bảng kê — đúng
  tính chất kế hoạch đòi. Lệnh đọc tệp phát `cod_statement.parsed` trong `OnDone`.
- **Q4. `fee_only` = COD 0 và (phí + phí khác) ≠ 0** (kế hoạch: phí > 0): gồm cả hoàn phí (số âm, dấu giữ theo số);
  phí khác luôn cộng vào phí ở mọi bút toán (shop_received = C − phí − phí khác, như `net_amount` mặc định).
- **Q5. "Lệch đối soát":** dòng `amount_mismatch` chỉ hiện khi vận đơn còn `carrier_holding ≠ 0` (TI05: trả đủ phần
  còn lại thì rời danh sách); `open_remainder` chỉ cho vận đơn có tiền về mà còn lệch và không có dòng lệch nào ở trên
  (không hiện một vận đơn hai lần); `remitted_then_returned`: mọi phát hiện (bảng chưa có cột "đã xử lý"; Task M3-33
  thêm cách xử lý). Mới nhất trước theo mã mục (uuidv7), con trỏ là mã mục.
- **Q6. "Quá hạn chưa có tiền"** sắp theo mã vận đơn tăng dần (uuidv7 ≈ cũ trước), con trỏ là mã vận đơn; số ngày tròn
  xuống theo đồng hồ của module (`Deps.Now`).
- **Q7. Cảnh báo tổng khai** tính lúc chốt từ kết quả khớp (Σ `net_amount` của dòng matched, amount_mismatch,
  fee_only), không chờ lô ghi tiền (body trả ngay).
- **Q8. `cod.settled`:** aggregate `cod_statement_line` (mã dòng), Seq 1 — mỗi dòng về 0 một lần; chưa có consumer.
- **Q9.** Nhiều dòng cùng vận đơn trong một bảng kê: xét theo `line_no` trên số dư cộng dần (dòng sau thấy phần dòng
  trước đã trả).

## Bằng chứng

Lệnh chung: `ci-task.sh` của task (21/11, 04:40, sau khi gộp main 4a220ca): build, vet, lint, gen-check, sqlc-vet,
catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; test: `cmd/crm ok`, `internal/httpapi ok 140.566s`,
`internal/modules/cash ok 110.144s`, `cash/internal/statement ok 126.018s`, `tests/arch ok`; `tests/e2e` đỏ một ca
`TestIngestServesHooks` (bind: address already in use — cổng ngẫu nhiên trùng phiên khác trên máy; lần ci-task trước
xanh ca này), chạy lại `go test ./tests/e2e/ -run TestIngestServesHooks -count=1` → `ok 3.652s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TI02: FK0001 630.000 phí 32.000 → matched; chốt → V1 {0, 0, 598000, 32000, −630000}; cod.settled V1 | `TestConfirm_TI02`, `TestMatch_Classifies` | `go test -race ./internal/modules/cash/internal/statement/ -run 'TestMatch\|TestConfirm\|TestList'` | `ok 3.986s` |
| TI05/AT16: thiếu 30.000 → amount_mismatch −30000, còn mở 30.000, có trong lệch đối soát; bảng kê sau 30.000 → về 0, rời danh sách | `TestConfirm_TI05Shortfall` | như trên | ok |
| TI06: NHANH9999 → external, không bút toán, tổng sổ không đổi, có trong lệch đối soát | `TestConfirm_ExternalFeeOnlyWarning`, `TestMatch_Classifies` | như trên | ok |
| TI07/AT30: hai người chốt cùng version → 1 thành công, 1 ErrStatementChanged; remitted = số dòng khớp | `TestConfirm_Concurrent`; HTTP 409 `statement_changed`, 404, 403, 200 + audit: `TestCodStatementConfirmAPI` | trên; `go test -race ./internal/httpapi/ -run TestCodStatement` | ok; `ok 3.060s` |
| Hai bảng kê cùng FK0001 → bảng kê sau duplicate, không ghi; cùng khớp trước khi chốt → xếp lại duplicate lúc ghi | `TestConfirm_Duplicate` | như trên | ok |
| VC13: X123 của A2 (fake_b) không khớp vào bảng kê của A | `TestMatch_ByCarrierAccount` | như trên | ok |
| fee_only FK0005 0 phí 25.000 → carrier_fee +25000, shop_received −25000; cảnh báo tổng khai | `TestConfirm_ExternalFeeOnlyWarning` | như trên | ok |
| Quá hạn: 15 ngày có (days 15), 13 ngày không, đã trả đủ không, đang giao không | `TestListOverdue` | như trên | ok |
| Quy luật rapid 200 lượt: chốt song song 1–3 người, ghi tiền 3 worker song song → tổng ngăn 0, ≤ 1 bút toán mỗi dòng, external/duplicate không ghi, shop + phí ≤ đã thu trừ dòng thừa (dòng đó có trong lệch đối soát) | `TestProperty_MatchConfirm` | ci-task (`cash/internal/statement ok 126.018s`) | ok |
| ADR-46: hai bảng kê cùng vận đơn ngược thứ tự, ghi song song không deadlock, mỗi vận đơn trả một lần | `TestConfirm_ParallelStatementsNoDeadlock` | như dòng TI02 | ok |
| Lô > 200 dòng nối tiếp; sự kiện chốt giao lại không ghi thêm | `TestConfirm_ManyBatches` | như dòng TI02 | ok |
| Lệnh đọc tệp thật → sự kiện đã đọc → matched; sự kiện giao lại không khớp lại; payload hỏng lỗi | `TestMatch_AfterRealParse`, `TestMatch_RedeliveredIsNoop` | như dòng TI02 | ok |
| Đột biến tay: tắt nhánh duplicate của Classify | `TestConfirm_Duplicate`, `TestConfirm_ParallelStatementsNoDeadlock` đỏ (`bảng kê 2: amount_mismatch, mong duplicate`) | `go test ./internal/modules/cash/internal/statement/ -run 'TestProperty\|TestConfirm_Duplicate\|TestConfirm_Parallel' -rapid.checks=60` | FAIL như mong; đã trả lại mã |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ghi tiền theo lô sau commit (Q1), khớp qua consumer (Q3); không migration (0316 không dùng).
- Mã cash của task ở tệp mới `area_statements_match.go` (area_statements.go chỉ nối consumer, NewParser), để tệp
  của Task M3-31 không phình.
- Test danh sách nằm trong `statement` (`TestListOverdue`, kiểm "Lệch đối soát" trong `TestConfirm_*` và test quy luật)
  thay vì `lists/lists_test.go`: cần cả luồng khớp, chốt, ghi tiền dựng sẵn ở `flowEnv`.
- Thứ tự: mã khớp, chốt được viết trước khi chạy lần đầu bộ test (không có lần chạy "thất bại trước" ghi lại); bù bằng
  kiểm đột biến tay: tắt nhánh `duplicate` của `Classify` → `TestConfirm_Duplicate`, `TestConfirm_ParallelStatementsNoDeadlock` đỏ.
- Tên consumer `cash_statements` (luật tên consumer của events không cho dấu chấm).

## Việc phát hiện thêm

- Ghi chú 1, 2 của reviewer: chặn limit ở Service (≤ 199) hoặc bỏ trần ở lists; đổi kiểm lỗi payload sang errors.Is.
- Màn đối soát trên console (chốt, lệch đối soát, quá hạn) chưa có; kết quả khớp có thể đổi sau khi chốt (Q2) — màn hình
  nên đọc lại bảng kê khi nhận báo hiệu cod_statement.changed.

## Reviewer

Kết luận: ĐẠT (lần 1, 21/11). Không có phát hiện phải sửa; Q1–Q9 trong phạm vi spec và an toàn (TI07 giữ nhờ UPDATE có
điều kiện version; chống ghi trùng ba lớp: khóa remit:<dòng>, khóa dòng, bỏ dòng đã có posted_transfer_id; thứ tự khóa
đúng ADR-46). Ghi chú không bắt buộc (chưa sửa, ghi vào Việc phát hiện thêm): (1) Service.ListDiscrepancies/ListOverdue
với limit ≥ 200 không trả next do lists chặn 200 (HTTP chặn ≤ 100 nên không xảy ra; test quy luật đọc trang đầu 200
mục); (2) TestMatch_RedeliveredIsNoop kiểm payload hỏng bằng chuỗi thay vì errors.Is(queue.ErrPermanent); (3) uuidPtr
trùng nhỏ giữa lists và statement; (4) cảnh báo tổng khai dựa trên kết quả khớp xem trước (Q7).
