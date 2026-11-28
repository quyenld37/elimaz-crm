# Task 531 — Lọc người bán ở các danh sách tiền (cash) và đường đọc M3

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task điều phối giao, không có trong tệp PLAN; số toàn cục 531; nguồn: QĐ-527-3 ở `docs/tasks/task-527.md`); nền: kế hoạch M3 `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task M3-29–M3-32, M3-48). Loại việc (quy trình mục 3): B (lỗ bảo mật tìm thấy: test đỏ trước, rồi sửa)

## Chẩn đoán

API chạy với người bán mặc định (spec 04 mục 14 bước 0, QĐ-527-1). Task 527 chặn route vận đơn; các đường của module
cash không kiểm người bán: danh sách bảng kê, chi tiết bảng kê, lệch đối soát, quá hạn chưa có tiền, phát hiện đối
chiếu, nghĩa vụ hoàn, phiếu của đơn đọc được tiền của người bán khác; nhập bảng kê bằng tài khoản hãng của người bán
khác, chốt bảng kê, ghi phiếu thu, chi/miễn hoàn của người bán khác cũng không bị chặn (QĐ-527-3 ghi "nhập bảng kê đã
chặn" — rà lại mã: `statement.Import` chỉ đọc `carrier_accounts_v` theo mã, không so người bán).

Rà các đường M3 khác (vận chuyển, kho; mọi `handlers_*.go` ngoài cash): thêm hai lỗ cùng loại 527 — phiếu nhặt hàng
(`GET /picking-list?shipmentIds=`: vận đơn theo mã chỉ kiểm phạm vi kho, lộ mã đơn, SKU, tiền thu hộ) và giữ hàng quá hạn
(`GET /inventory/aged-reservations`: trả mã, số đơn của người bán khác). Đã lọc sẵn: báo giá, tài khoản hãng của đơn,
phiếu trả, đóng gói, báo cáo tồn/hãng, mọi route vận đơn (527). Không có chiều người bán theo thiết kế: cài đặt vận
chuyển, kho, địa giới, phiếu kho, kiểm kê, hàng hoàn cũ (theo kho). Tài khoản hãng và cài đặt hãng cho website: xem
QĐ-531-4.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep): thêm `cash.CheckSeller`, kiểu `cash.ScopeKind`, 3 hằng
  `cash.ScopeStatement`, `cash.ScopeCarrierAccount`, `cash.ScopeRefundObligation` (mới). Thêm tham số `seller uuid.UUID`
  (sau `tx`) vào `Service.ListDiscrepancies` (chỗ gọi: `handlers_cod_statements.go`, `statement/match_test.go:300`),
  `Service.ListOverdue` (`handlers_cod_statements.go`, `match_test.go:483`), `Service.ListRefundObligations`
  (`handlers_payments.go`, `refunds_test.go:51, 136`), `Service.ListReconcileFindings` (`handlers_cod_adjust.go`,
  `reconcile/nightly_test.go:71, 351`); thêm trường bắt buộc `SellerID` vào `cash.StatementListFilter`
  (= `statement.ListFilter`; chỗ dựng: `handlers_cod_statements.go`, 4 chỗ `import_test.go:607–618`). Gói con nội bộ
  `lists.Discrepancies`, `lists.OverdueList` thêm tham số seller (chỉ cash gọi). httpapi: helper riêng
  `cashSellerOwns` (seller_scope.go), gọi ở `ImportCodStatement`, `GetCodStatement`, `ConfirmCodStatement`,
  `PayRefundObligation`, `WaiveRefundObligation`; `sellerOrder` (527) gọi thêm ở `RecordPayment`, `ListOrderPayments`.
  Kho: thêm trường bắt buộc `SellerID` vào `inventory.AgedPage` (chỗ dựng: `handlers_inventory_aged.go`; test:
  helper `invEnv.aged` + 3 ca lỗi `aged_test.go:222–224` (lỗi trước khi đọc), `aged_explain_test.go` 5 ca qua một chỗ
  gọi); `GetPickingList` gọi `sellerShipment` (527) cho từng mã vận đơn. Test: hàm `uploadCSV` tách từ
  `m3Env.uploadStatement` (dùng chung với test mới), trường `files` của `sellerEnv`.
- Gói phụ thuộc ngược (go list): nhập `internal/modules/cash`: cmd/crm, internal/httpapi, internal/testkit/codtest và
  test của các gói cash — chỉ httpapi và test cash gọi các hàm đổi chữ ký. `inventory.AgedPage`: chỉ httpapi và test
  inventory dựng.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng (người bán khác → 404 như mã lạ đã có trong mô tả; danh
  sách chỉ ít dòng hơn). Hành vi đổi ở: listCodStatements, getCodStatement, importCodStatement, confirmCodStatement,
  listCodDiscrepancies, listCodOverdue, listReconcileFindings, listRefundObligations, payRefundObligation,
  waiveRefundObligation, recordPayment, listOrderPayments, getPickingList, listAgedReservations.
- Chỗ dùng trên màn hình: không đổi console (cùng hợp đồng); hôm nay chỉ một người bán thật nên dữ liệu thật không đổi.
- Câu sqlc, bảng, pha migration: tệp mới `queries/cash/seller_scope.sql` (3 câu đọc người bán theo khóa chính); thêm
  điều kiện người bán vào `ListStatements`, `ListDiscrepancies` (cả ba nhánh), `ListOverdue`, `ListRefundObligations`,
  `ListRunFindings` (phát hiện gắn vận đơn của người bán khác bị ẩn; phát hiện cấp tenant, không vận đơn, vẫn hiện —
  QĐ-531-2), `AgedReservations` (queries/inventory/aged.sql: người bán của số dư mà dòng giữ, câu con theo khóa chính
  của `stock_balances`). Không migration, không bảng mới.
- Test kế hoạch truy vấn: `TestM3ListPlans_Large` (cash) đổi tham số theo câu (thêm seller); `TestAgedReservations_Plan`
  (inventory): dữ liệu mẫu thêm người bán thứ hai 10.000 biến thể có số dư (với 100 dòng số dư, planner quét cả bảng
  nhỏ cho câu con — Seq Scan; số dư thật lớn hơn nhiều; ca kiểm không đổi).
- Test sẽ chạy: mới `TestCashRoutes_OtherSellerNotFound`, `TestCashLists_OtherSellerHidden` (internal/httpapi),
  `TestCheckSeller` (cash), `TestAgedReservations_OtherSellerHidden` (httpapi), thêm 2 ca phiếu nhặt hàng vào
  `TestShipmentRoutes_OtherSellerNotFound` (+1 đối chứng 200); sửa và chạy lại các test cash, inventory có chỗ gọi đổi;
  ci-task theo gói đổi (cash, inventory là vùng rủi ro cao: cả bộ hai vùng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm người bán
  chung); `fulfillment.CheckSeller`, `httpapi.sellerOrder`, `sellerShipmentCOD`, `catalog.DefaultSeller` (Task 527).
- Dùng lại / viết mới vì: dùng lại `catalog.DefaultSeller`, `sellerOrder` (đường theo mã đơn), mẫu `CheckSeller` của
  527. Viết `cash.CheckSeller` riêng trong cash vì đối tượng (bảng kê, nghĩa vụ hoàn) là bảng của cash, câu SQL phải
  nằm trong module sở hữu (luật Go 7, 8); không gộp với `fulfillment.CheckSeller` (khác module, khác sentinel, khác lý
  do thay đổi). Tài khoản hãng của lần nhập kiểm qua `cash.CheckSeller` (cash đã đọc `carrier_accounts_v` cho nhập
  bảng kê) thay vì thêm loại vào fulfillment để sentinel 404 là của cash (`carrier_account_not_found` như cũ).

## Soát thiết kế

- 5.1 (đúng module): SQL người bán trong `queries/cash`, hàm công khai ở cash; httpapi chỉ lấy người bán mặc định.
- 5.2 (quyền): người bán là trục thứ hai cạnh quyền `cash.*`; khác → cùng 404 như mã lạ; danh sách lọc trong SQL.
- 5.3 (tenant): mọi câu trong tx tenant (RLS); `seller_id` đọc qua view công bố `carrier_accounts_v`, `shipments_v`,
  `orders_v` (cash đã đọc các view này).
- 5.4 (lỗi): sentinel có sẵn `ErrStatementNotFound`, `ErrCarrierAccountNotFound`, `ErrObligationNotFound`,
  `ErrOrderNotFound`; không mã lỗi mới, không đổi errors*.go.
- 5.5 (hiệu năng): điều kiện người bán đi theo khóa chính (câu con vô hướng ở chỗ chưa nối bảng có seller, như 527
  tránh Hash Join quét bảng lớn); `TestM3ListPlans_Large` giữ "không Seq Scan".
- 5.6–5.9, 5.11: không áp dụng: không bảng, migration, thư viện, lệnh nền, cấu hình mới.
- 5.10: không áp dụng: không đổi giới hạn tốc độ.
- 5.12: test theo mẫu `seller_scope_test.go` (527).
- 6.1 (transaction): kiểm người bán trong cùng tx với thao tác, trước khóa; `seller_id` của tài khoản hãng, đơn không
  đổi nên không có khe giữa kiểm và khóa.
- 6.2 (gửi lại): 404 là lỗi nghiệp vụ → không lưu khóa chống trùng (refusableWrite / isCashRefusal / lỗi refund đã có).
- 6.3 (RetryTx): helper không giữ biến ngoài.
- 6.4–6.11, 6.13: không áp dụng: không thêm khóa, tiền, hàng chờ, gọi ra ngoài, đồng hồ.
- 6.12 (dữ liệu cá nhân): không log gì mới.

## Quyết định thay chủ shop

- QĐ-531-1: làm ở lớp API với người bán mặc định như QĐ-527-1 (không RLS theo người bán, không cột seller mới trên
  bảng cash). Danh sách: tham số seller bắt buộc trong hàm của cash, lọc trong SQL. Phương án khác: thêm cột
  `seller_id` vào `cod_statements`, `refund_obligations` (migration, khóa ngoại ghép) — để bước 2 (tài khoản người bán).
- QĐ-531-2: "Phát hiện đối chiếu" đêm chạy cho cả tenant: phát hiện gắn vận đơn chỉ hiện cho người bán của vận đơn;
  phát hiện không gắn vận đơn (tổng sổ cả tenant `ledger_nonzero`, `skipped`…) vẫn hiện — đó là kiểm toàn vẹn sổ của
  shop vận hành tenant, ẩn đi sẽ giấu cảnh báo khỏi người duy nhất xử lý. Phương án khác: ẩn hết phát hiện không vận
  đơn (mất cảnh báo) hay chạy đối chiếu theo người bán (đổi lệnh nền, ngoài phạm vi).
- QĐ-531-4: tài khoản hãng (`/carrier-accounts`: danh sách, sửa, khóa API, kiểm kết nối, đồng bộ địa giới) và cài đặt
  hãng cho website (`/web-carriers`) giữ nguyên, không lọc người bán: đây là cấu hình cấp shop mà hợp đồng chủ ý cho
  chủ shop quản cho mọi người bán (`createCarrierAccount` nhận `sellerId`; spec 04 mục 14 bước 1: nhân viên ELIMAZ vận
  hành cả hàng đối tác; website là một cửa hàng chung); không trả khóa API ra ngoài. Đổi ở đây là đổi hợp đồng.
  Phương án khác: chỉ cho tài khoản của người bán mặc định (bỏ `sellerId`) — để bước 2 (cổng người bán, phạm vi người
  bán ADR-18), ghi ở Việc phát hiện thêm.
- QĐ-531-3: đường ghi của cash (nhập, chốt bảng kê, ghi phiếu, chi/miễn hoàn) cũng chặn người bán khác (404) dù tên
  task nói "đường đọc": cùng lỗ, cùng cách sửa, một dòng mỗi handler. Phương án khác: để task sau (bỏ ngỏ lỗ ghi tiền).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Bước 2 (tài khoản người bán): tài khoản hãng, cài đặt hãng cho website, sự kiện hãng không khớp
  (`/carrier-events/unmatched`, có `carrier_account_id`), dòng phiếu kho, kiểm kê theo biến thể của mọi người bán cần
  phạm vi người bán (QĐ-531-4); hôm nay là cấu hình/vận hành cấp shop.
- `GET /warehouses/{id}/pickup` đọc qua `catalogRead` không kiểm phạm vi kho (agent rà phát hiện, không thuộc chiều
  người bán, chưa kiểm sâu) — để điều phối xem.

## Reviewer

Kết luận: CHƯA REVIEW
