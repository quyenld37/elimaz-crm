# Task 231 — Báo cáo hãng vận chuyển

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 231 = M6-08). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn. Làm theo phần "Bổ sung Task 223" của kế hoạch.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `reporting.CarriersParams` giữ nguyên (0 chỗ dùng ngoài
  `carriers.go`). Thêm mới trong `internal/modules/reporting`: `CarrierRow`, `CarriersReport` (+ `ReportRows`).
  `carriersDef()` gán thêm `JSON` (sổ `Defs()` không đổi). Không xuất ra ngoài: `carrierSet`, `carrierRows`,
  `carrierCounts`, `carriersReport`, `rateBp`, `avgHalfUp`; httpapi `carrierRowOut`. Test: thêm hàm `newBulkEnv` trong
  `orders_dynsql_test.go` (gom khối dựng 100.000 đơn, `TestOrderSet_ExplainNoSeqScan` dùng lại).
- Gói phụ thuộc ngược (go list): `reporting` được `internal/httpapi`, `cmd/crm` nhập. Sửa `reporting` kéo test của hai
  gói đó (ci-task).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Hiện thực `getSalesCarriersReport`
  (`handlers_reports_carriers.go`), xóa dòng của nó khỏi `tests/arch/testdata/m6_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn Hãng vận chuyển là Task 240).
- Câu sqlc, bảng, pha migration: không câu sqlc, không migration. Một câu dựng lúc chạy mới trong
  `carriers_dynsql.go` đọc `fulfillment.shipments_v`, `ord.orders_v`, `fulfillment.carrier_accounts_v` (đều đã có
  trong `viewReadExceptions["reporting"]` và `docs/views.md`). Chỉ mục dùng: `shipments_created (tenant_id,
  created_at)` (0300) có sẵn.
- Test sẽ chạy: `go test -race -count=1 -run '^TestCarriers' ./internal/modules/reporting/`;
  `go test -race -count=1 -run '^(TestReportsCarriers.*|TestM6Contract_StatusCodes)$' ./internal/httpapi/`;
  `go test -count=1 ./tests/arch/`; phần còn lại ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm tỷ lệ phần
  vạn hay trung bình làm tròn dùng chung trong `internal/platform`); trong `reporting`: `PeriodParams`/`ParsePeriod`,
  `DataScope`/`ScopeFor`, `OwnSeller`, `sqlBuilder` (orders_dynsql.go), `Interactive.Run`, `newDef`; httpapi
  `runReport`, `reportDay`; testkit `reportseed.NineOrders`, `ExplainAsApp`, `RequireNoSeqScan`, `Seed.Bulk`.
  `customer.Stats.ReturnRateBP` cùng kiểu công thức nhưng thuộc module `customer` (reporting không được nhập).
- Dùng lại / viết mới vì: dùng lại `sqlBuilder` (tham số `$n`), `PeriodParams`, `ScopeFor`/`OwnSeller` qua `Run`,
  `runReport`. Không dùng `orderSet` (hàm dựng tập **đơn**): tập của báo cáo này là **vận đơn** tạo trong kỳ (một đơn
  có thể có hai vận đơn, cả hai đều phải đếm), nên điều kiện người bán và phạm vi Page viết lại trong
  `carriers_dynsql.go` theo đúng luật của `filterConds` (phạm vi không `AllPages` → `channel_account_id = ANY`, mảng
  rỗng không NULL). Hai hàm nhỏ `rateBp`, `avgHalfUp` viết trong `carriers.go`: chưa có bản dùng chung; Task 229/230
  chạy song song có thể cần cùng phép tính — ghi vào Việc phát hiện thêm để gom khi gộp, không tạo tệp chung lúc này
  (tránh xung đột tệp giữa các worktree).

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới. 5.2 Không ghi: chạy trong transaction chỉ đọc của `TxReport`.
- 5.3 Đọc qua view: chỉ `fulfillment.shipments_v`, `fulfillment.carrier_accounts_v`, `ord.orders_v` (view công bố,
  có trong ngoại lệ đọc view của `reporting`; test kiến trúc kiểm).
- 5.4–5.6 Sau commit, gọi ra ngoài: không. 5.7 Ngữ cảnh shop: `Interactive.Run` chạy trong `tenancy.WithTenant`.
- 5.8 Không HTTP trong module; handler mỏng đổi kết quả sang `gen.SalesCarriersReport`. 5.9 `WriteTx`: không áp dụng
  (GET). 5.10 Valkey: không. 5.11 Thư viện mới: không. 5.12 Theo mẫu `reporting` (Def trong tệp báo cáo).
- M6: biểu thức SQL chỉ là hằng trong `carriers_dynsql.go`; giá trị (người bán, kỳ, Page) luôn là `$n`. Người bán
  (ADR-29): `sh.seller_id = $s` và `o.seller_id = $s` (nối đơn), tài khoản hãng cũng lọc `seller_id`. Phạm vi Page:
  không `AllPages` → `o.channel_account_id = ANY($n)` (đơn không gắn Page bị loại). Hàm dựng điều kiện: một hàm
  `carrierSet` dùng cho mọi cột số và dòng tổng (tổng tính trong Go bằng cộng các dòng, nên tổng = tổng các dòng).
  Danh sách chi tiết theo hãng là báo cáo đơn chiều `carrier` (Task 230), không thuộc task này.
- EXPLAIN bằng `crm_app` trên 100.000 đơn (`Seed.Bulk`) kỳ 31 ngày, phạm vi all và phạm vi Page: không Seq Scan trên
  `shipments`, `orders` (test).
- 6.1–6.4 Khóa: không khóa dòng (chỉ đọc). 6.5 READ COMMITTED: một câu duy nhất nên một ảnh chụp cho mọi cột.
- 6.6 Chạy lại: hàm `JSON` trả kết quả bằng giá trị, không cộng vào biến ngoài.
- 6.7, 6.8 Hàng đợi, in_doubt: không áp dụng. 6.9 Treo lâu: khung `Interactive` (2 chỗ, 30 s).
- 6.10 Tiền: `int64` đồng; `sum()` trên tập rỗng bọc `coalesce(…, 0)`; trung bình làm tròn nửa lên bằng số nguyên; tỷ
  lệ phần vạn `int32`, `nil` khi mẫu số 0; không `float`.
- 6.11 Quy luật: test kiểm `Created = InProgress + Returning + DeliveryFailed + Delivered + Returned` và tổng = tổng
  các dòng trên dữ liệu ngẫu nhiên lớn (`Bulk`).
- 6.12 Dữ liệu cá nhân: kết quả chỉ có khóa hãng, mã và tên tài khoản hãng, số đếm, tiền; không SĐT, tên khách.
- 6.13 Quyền: `x-permission: report.sales.read` chặn trước handler; phạm vi Page do `ScopeFor` dựng; test người chỉ có
  P2.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, chạy → FAIL | `TestCarriersNineOrders`, `TestCarriersDayBoundaryAndSeller`, `TestCarriersFeesAndStatuses` | `go test -count=1 -run '^TestCarriers' ./internal/modules/reporting/` | trước khi viết mã: lỗi biên dịch `undefined: reporting.CarriersReport`; sau khi thêm kiểu (chưa có `JSON`): 3 FAIL `báo cáo carriers không có bản xem tại chỗ: reporting: chưa làm` |
| Chín đơn ngày 02/11: VTP `Created 3, Delivered 2, Returned 1, SuccessRateBp 6666, FeeTotal 66.000, FeeAvg 22.000, COD 1.090.000`; SPX `Created 1, Cancelled 1, InProgress 1, Fee 22.000, COD 180.000, rate nil`; Best `Created 1, InProgress 1, Fee 22.000, COD 150.000`; tổng `Created 5, Cancelled 1`; dòng sắp best, spx, vtp, có tên và mã tài khoản | `TestCarriersNineOrders` | `go test -race -count=1 -run '^TestCarriers(NineOrders\|DayBoundaryAndSeller\|FeesAndStatuses)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 4.448s` |
| Người chỉ có P2 → 2 dòng: SPX chỉ `Cancelled 1`, Best `Created 1, InProgress 1`; không grant nào → 0 dòng, tổng 0, rate/avg nil; đơn tự giao không gắn Page không lọt vào P2 | `TestCarriersNineOrders` | như trên | ok |
| Đơn tự giao giao xong phí 0 → dòng `self` `Delivered 1, FeeTotal 0, FeeAvg 0`; tổng `Created 6` | `TestCarriersNineOrders` | như trên | ok |
| Mọi lần gọi: `Created` = tổng 5 nhóm chưa hủy; dòng tổng = tổng các dòng (`checkCarrierSums`) | cả ba test | như trên | ok |
| Tạo 23:59:59 giờ VN 01/11 không vào kỳ 02/11; 00:00:00 02/11 và 23:59:59 02/11 vào; vận đơn của người bán khác (partner) không vào | `TestCarriersDayBoundaryAndSeller` | như trên | ok |
| `fee_actual 25.000` + `fee 22.000` → 25.000; chỉ `fee` → 22.000; cả hai NULL → 0; `creating` → InProgress; vận đơn hủy không cộng phí, COD; trung bình 47.000/3 → 15.667 (nửa lên), 7/6 → 1; đủ chín trạng thái vào đúng cột | `TestCarriersFeesAndStatuses` | như trên | ok |
| EXPLAIN bằng `crm_app`, 100.000 đơn, kỳ 31 ngày, phạm vi all và một Page: không Seq Scan; thiếu người bán, thiếu kỳ → `ErrBadParams` | `TestCarriersExplainNoSeqScan` (+ `TestOrderSet_ExplainNoSeqScan` sau khi gom `newBulkEnv`) | `go test -race -count=1 -run '^(TestCarriers.*\|TestOrderSet_ExplainNoSeqScan)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 158.006s` |
| HTTP `GET /api/v1/reports/sales/carriers`: 200 `status ready`, kỳ; dòng vtp đủ trường, `successRateBp 6666`, có `carrierAccountId`; spx `successRateBp null`; dòng tổng `carrierKey ""`, không có `carrierAccountId`, `accountName`; người có grant `page` P2 → 2 dòng, `feeAvg null` ở SPX; from sau to → 400 `bad_date` | `TestReportsCarriers_Report` | `go test -race -count=1 -run '^(TestReportsCarriers.*\|TestM6Contract_StatusCodes)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 4.566s` |
| `getSalesCarriersReport` không còn trong `m6_pending.txt`, không trả 501; 403 khi không quyền | `TestM6Contract_StatusCodes` | như trên | ok |
| Kiến trúc (ranh giới, view, SQL, dynsql) | `tests/arch` (trừ `TestLint*`) | `go test -count=1 -timeout 500s -skip '^TestLint' ./tests/arch/` | lần đầu FAIL `carriers_dynsql.go:54: bảng thiếu tên schema: g` (CTE khai ở chuỗi khác) → đổi sang truy vấn con `AS g`; sau sửa `ok elimaz.com.vn/crm/tests/arch 10.123s`. `TestLintNotBlockedByOtherRunners` treo trên máy vì khóa lint do golangci-lint của phiên khác giữ (không phải của task này); chạy ở ci-task |
| Lint, mã lặp | golangci-lint, jscpd | `golangci-lint run ./internal/modules/reporting/... ./internal/httpapi/`; `make jscpd` | lần đầu 2 lỗi (gocognit test HTTP, S1016) + 1 bản sao (khối `Bulk` của test EXPLAIN) → tách hàm, gom `newBulkEnv`; sau sửa `0 issues`, `Found 0 clones` |
| Cổng tĩnh (gồm tests/arch, lint, jscpd, mã sinh, oasdiff) + test gói bị ảnh hưởng (race: `internal/httpapi`, `internal/modules/reporting`; thường: `cmd/crm`, `httpapi/web`, `conversation`, `platform/files`, `platform/realtime`) | ci-task | `scripts/agent/ci-task.sh` (từ xa, github; blacksmith hết phút, ubicloud tạm hỏng) | `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh fb864a8e…)`; static 15 phút 42 giây, test 4 phần xanh, 59 phút máy (run 45974310732) |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- **QĐ-231-1 — tỷ lệ thành công làm tròn xuống (2/3 → 6666, màn hình 66,66%).** Kế hoạch ghi ví dụ `SuccessRateBp
  6667` (Q5 "6667 = 66,67%"), nhưng hợp đồng đã duyệt (`ReportRateBp`: "Tỷ lệ phần vạn, làm tròn xuống"), Task 230
  ("làm tròn xuống") và `customer.Stats.ReturnRateBP` (chia nguyên) đều làm tròn xuống. Chọn theo hợp đồng để mọi báo
  cáo cùng một cách tính; test đổi số mong đợi thành 6666. Phương án khác: làm tròn nửa lên (đúng ví dụ, nhưng phải đổi
  mô tả hợp đồng và Task 230). Đảo ngược: một dòng trong `rateBp` (carriers.go) + mô tả `ReportRateBp`.
- **QĐ-231-2 — một dòng mỗi tài khoản hãng** (nhóm theo `carrier_key`, `carrier_account_id`), sắp theo khóa hãng, tên
  tài khoản, mã tài khoản; tên tài khoản là `carrier_accounts_v.name` (rỗng nếu không đọc được). Kiểu dòng của kế hoạch
  có `CarrierAccountID`, `AccountName` nên mỗi tài khoản một dòng; shop có hai tài khoản cùng hãng thì thấy hai dòng.
  Đảo ngược: bỏ `carrier_account_id` khỏi `GROUP BY`.

## Khác kế hoạch

- `CarriersReport` thêm trường `Period` (hợp đồng đòi `period`). Tỷ lệ 6666 thay 6667 (QĐ-231-1).
- Gom khối dựng 100.000 đơn của test EXPLAIN thành `newBulkEnv` trong `orders_dynsql_test.go` (jscpd), dùng ở cả
  `TestOrderSet_ExplainNoSeqScan` và `TestCarriersExplainNoSeqScan`. Test EXPLAIN kiểm phần tập vận đơn (nối
  `shipments_v`, `orders_v`); phần nối `carrier_accounts_v` chạy trên vài dòng đã gộp.

## Việc phát hiện thêm

- `rateBp` (phần vạn làm tròn xuống) và `avgHalfUp` (trung bình nửa lên) nằm trong `carriers.go`; Task 229/230 song
  song có thể viết hàm cùng việc. Khi gộp: gom về một tệp của `reporting` (ví dụ `numbers.go`).
- Ví dụ `6667` trong kế hoạch M6 (Q5, Task 230, Task 241 `rate(6667)`) lệch với hợp đồng "làm tròn xuống" (QĐ-231-1);
  điều phối viên thống nhất khi gộp Task 230.
- `TestLintNotBlockedByOtherRunners` (tests/arch) treo khi máy có golangci-lint của phiên khác đang giữ khóa lâu.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên phần đã stage).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp; `add`, `derive` (phương thức không xuất) và tệp `handlers_reports_carriers_test.go` chỉ nêu qua tên
  test — không ảnh hưởng tính đúng.
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí; EXPLAIN chỉ phủ phần tập vận đơn (đã ghi ở
  Khác kế hoạch), chấp nhận được.
- Ghi chú không bắt buộc: QĐ-231-1 (6666 thay 6667) cần chốt khi gộp Task 230, 241; `carrierRowOut` coi `uuid.Nil` là
  dòng tổng — an toàn vì `shipments.carrier_account_id` NOT NULL; gom `rateBp`, `avgHalfUp` khi gộp với 229/230.
