# Task 223 — M6-00 Chuẩn bị mốc: tiêu chí nghiệm thu, đối chiếu giả định, soát bảo mật

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 223 = M6-00). Loại việc (quy trình mục
3): A, chỉ đổi tài liệu.

**Điều kiện bắt đầu:** kế hoạch đòi "M1–M5 đóng". Chủ shop đã miễn điều kiện này: sáng 27/11/2026, qua phiên điều
phối M4, chủ shop duyệt kế hoạch M6 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ staging). Mã M5 đã gộp vào
`main` (tới Task 512), nên phần đối chiếu đọc được mã của M5.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa mã Go. Một test tạm để đọc schema
  database được tạo rồi xóa, không commit.
- Gói phụ thuộc ngược (go list): không có.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có. Chỉ đọc `migrations/*.sql` và một database thử đã migrate.
  `docs/adr/0046-thu-tu-khoa.md`, `docs/views.md`, `docs/quyen.md` chỉ được đọc, không sửa: nhóm 12a điền ở Task
  M6-02; dòng `customers_v` của `views.md` sửa ở Task M6-15.
- Tệp tạo mới: `docs/m6/doi-chieu-gia-dinh.md`, `docs/m6/tieu-chi.md`, `docs/threat-model-m6.md`, tệp task này.
- Kế hoạch M6 (ngoài repo `apps/crm`): thêm khối "Bổ sung Task 223" sau bảng giả định. Bản trước khi sửa lưu ở
  scratchpad của phiên.
- Test sẽ chạy: không có test Go nào đọc ba tệp mới. Chạy `go test ./tests/arch/` (`viewsdoc_test`,
  `lockorder_frame_test` đọc tài liệu cạnh đó) và `scripts/agent/ci-task.sh` (cổng tĩnh: gitleaks, jscpd…).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm, kiểu nào.
- Dùng lại / viết mới vì: theo mẫu có sẵn của M5-00 (Task 183): `docs/m5/doi-chieu-gia-dinh.md`,
  `docs/threat-model-m5.md` (STRIDE theo ranh giới), `docs/m5/tieu-chi-nghiem-thu.md`.

## Soát thiết kế

Không áp dụng mục 5.1–5.12, 6.1–6.13: task chỉ viết tài liệu, không chạm database, tiền, quyền, hàng đợi. Các câu của
mục 5 và 6 liên quan tới M6 đã được đối chiếu như sau:

- 5.3, đọc qua view: mọi view M6 cần đều có. G7: luật `sqlusage_test` phải mở cho `reporting`.
- 6.3, thứ tự khóa: nhóm 12a còn trống, đúng Q19.
- 6.13, quyền: G18, `ScopeShop`.

Kết quả ghi trong `docs/m6/doi-chieu-gia-dinh.md`, để task sau trả lời đúng.

## Quyết định thay chủ shop

Chủ shop vắng tới sáng và giao toàn quyền trong phạm vi kế hoạch, spec. Mỗi dòng chọn phương án đúng spec, an toàn, dễ
đảo ngược. Phần lớn là chọn nhánh "Nếu khác" cho các dòng giả định khác mã thật; task nhận áp dụng.

| # | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-223-1 | Bắt đầu M6 khi M5 chưa đóng | Bắt đầu (chủ shop đã OK 27/11 sáng) | OK có sẵn trong chat của phiên điều phối | Chờ Task 221 |
| QĐ-223-2 | Khóa tệp xuất khi `files.Register` chỉ nhận `priv/<tenant>/…` (G5) | `priv/<tenantId>/reports/<runID>/<sha256 hex>.xlsx` và `priv/<tenantId>/customers/<runID>/<sha256 hex>.xlsx` (Task M6-14, 15) | Cả hai khóa của kế hoạch (`reports/…` và nhánh "Nếu khác" `priv/reports/<tenant>/…`) đều bị `Register` từ chối (`register.go:60`). Không phải sửa luật tenant của M1, vùng rủi ro cao. Giữ `<runID>` như khóa gốc của kế hoạch (Task M6-14): `InsertServerFile` là `ON CONFLICT DO NOTHING` và `FileIDByKey` không lọc `deleted_at`, `expires_at` (`queries/platform/files.sql:26-32`), nên hai lần chạy ra cùng nội dung mà chung khóa sẽ dùng chung một dòng `files` (hạn cũ, bị lượt dọn của lần trước `MarkDeleted`) | Sửa `Register` nhận tiền tố khác: đổi luật chống ghi chéo tenant của M1, không chọn |
| QĐ-223-3 | `not_sent` vẫn tính lượt (G2) | Lượt "worker hết chỗ" trả `KeepAttempt: true` + `RetryAfter: 15s`; trần là `ExpiresAfter` 1 giờ (Task M6-12) | Đúng cách M0 dành cho lỗi tạm không do lệnh; không đổi `commands` | Không đặt `KeepAttempt`: lần chạy có thể `dead` sau 8 lượt hết chỗ trong giờ cao điểm |
| QĐ-223-4 | Không có `orderview.AllLabels`/`Domain()` (G9) | M6-07 dùng `orderview.Labels()`. Chiều ngược nhãn → điều kiện SQL: xuất `orderLabelCond` của `internal/queries` trong một commit cấu trúc riêng, rồi `reporting` gọi lại | Một nguồn duy nhất cho SQL của nhãn, khớp màn danh sách đơn (Q20). Điều kiện: đoạn SQL gắn với bí danh `o.` và `s.status` (vận đơn hiện hành), nên câu của M6-07 phải dựng đúng join, bí danh đó | Viết bảng ngược trong `reporting`: hai nguồn, dễ lệch, không chọn |
| QĐ-223-5 | `Stats.ReturnRateBP` bằng 0 khi mẫu số 0, kế hoạch nói `NULL` (G10) | `0605` tính `returned_count * 10000 / (delivered_count + returned_count)` (chia nguyên như `statsOf`), `NULL` khi mẫu số 0; ô hiện "—". Test so với `Stats` khi mẫu số > 0 | Q5, C03: mẫu số 0 hiện "—", không hiện 0% | Trả 0 như `Stats`: lẫn "chưa có đơn" với "không hoàn đơn nào" |
| QĐ-223-6 | Quyền mới tới tenant đã có (G18) | Không viết `0600_role_templates_m6.sql`: thêm quyền vào `Permissions` + `DefaultRoles`, `SyncDefaultRoles` (vai trò `migrate`) đồng bộ; số `0600` bỏ trống. `report.export`, `report.exports.manage`, `customer.export` khai `ScopeShop`; hai quyền đọc khai `ScopeByTarget` | Mã thật không còn ghi quyền bằng migration từ M2 (`docs/quyen.md:46`). `ScopeShop` là cách mã nói "chỉ có hiệu lực ở phạm vi all" (Q13, Q14) | Viết `0600` như kế hoạch: chép tay danh sách quyền lần hai, lệch với `DefaultRoles` |
| QĐ-223-7 | `reporting` chưa được nhắc view của module khác (G7) | M6-03 đặt `moduleCalls["reporting"] = {"identity"}` và thêm `viewReadExceptions["reporting"]` liệt kê đúng các view M6 đọc (danh sách ở G7), kèm cột "Mốc đọc" trong `docs/views.md` | Cùng cách M2, M3 đã làm cho `customer`, `fulfillment`; danh sách tường minh giữ ranh giới | Cho `reporting` đọc mọi view đã công bố: rộng hơn cần |
| QĐ-223-8 | `OwnSeller` khi DB không ép "một người bán `own`" (G3) | Cùng luật `catalog.DefaultSeller`: `kind = 'own' AND status = 'active'`, id nhỏ nhất; không có → `ErrNoOwnSeller` (Task M6-04) | Báo cáo đọc đúng người bán mà đơn đã ghi (đơn dùng `DefaultSeller`) | "Hơn một → lỗi" như kế hoạch: báo cáo hỏng nếu có người bán `own` thứ hai ngừng hoạt động |
| QĐ-223-9 | Tên cột tệp xuất sản phẩm khi `importer` là gói nội bộ của `catalog` (G19, Q25) | Hằng tên cột trong `products_export.go`, trùng tên cột của mẫu nhập, không có `ColCostPrice`. Một test trong gói `catalog` (được nhập `importer`) đòi mọi tiêu đề của tệp xuất có trong `importer.Columns` (Task M6-14) | `reporting` không được nhập `catalog`; test giữ hai bên không lệch. Chiều phụ thuộc: gói test `catalog_test` nhập `reporting` (test kiến trúc chỉ xét `Imports`, không xét `TestImports`) | `catalog` xuất một hàm `ImportColumns()` công khai: được, nhưng thêm API công khai chỉ cho một chỗ dùng |
| QĐ-223-10 | Chỉ mục `order_items (tenant_id, order_id)` (G16) | Không thêm ở `0603`. Mọi câu M6 lọc `seller_id`, nên `order_items_position_key (tenant_id, seller_id, order_id, position)` dùng được. Thêm khi `EXPLAIN` của M6-04/20 thấy `Seq Scan`. Ba chỉ mục còn thiếu khác thì thêm như kế hoạch | Không thêm chỉ mục thừa trên bảng ghi nhiều nhất | Thêm luôn: tốn chỗ, chậm ghi |
| QĐ-223-11 | Có chạy reviewer cho task chỉ đổi tài liệu | Có (lệnh giao việc yêu cầu), dù kế hoạch ghi task 0 không cần | Thêm một lượt soát không hại | Bỏ reviewer theo CLAUDE.md bước 7 |

## Dừng để hỏi

**Hạn tự xóa tệp xuất trên kho tệp (G5, Q12; chặn Task M6-14 phần `bootstrap.sh`, không chặn M6-01–13).**

Kế hoạch đặt quy tắc hết hạn theo tiền tố trên `crm-private` (`reports/` 7 ngày, `customers/` 24 giờ). Mã thật và
phép đo M1 (`docs/do-luong/seaweedfs-m1.md` dòng 38–42) cho thấy cách này không chạy:

- `weed server` không chạy lifecycle worker, nên quy tắc S3 không xóa gì.
- Đường tắt TTL chỉ áp cho cả một bucket và không áp cho bucket giữ phiên bản.
- `crm-private` giữ phiên bản; ứng dụng không xóa vĩnh viễn được (ADR-36).
- `crm-private` còn được chép sang kho ngoài.

Nghĩa là tệp danh sách khách có SĐT sẽ nằm lại vô thời hạn, cả trên kho ngoài. Phía ứng dụng vẫn giữ được hạn tải:
410 sau hạn, `MarkDeleted`.

Mọi hướng sửa đều là việc hạ tầng (`docker/`, cấu hình bucket, filesync), ngoài quyền của phiên. Đề xuất cho chủ
shop, cùng điều phối viên:

- **(a, đề xuất)** Thêm bucket riêng `crm-exports`: không giữ phiên bản, không chép sang kho ngoài, đường tắt TTL 7
  ngày cả bucket. Danh sách khách hết hạn tải sau 24 giờ ở phía ứng dụng, xóa thật ở ngày 7. Cái giá: thêm một
  `Bucket` vào `files` (M1), một dòng `bootstrap.sh`, chính sách.
- **(b)** Chạy `weed worker` + `weed admin` (hoặc lịch `s3.lifecycle.run-shard`) để quy tắc theo tiền tố có hiệu
  lực. Bucket giữ phiên bản thì cần thêm quy tắc `NoncurrentVersionExpiration`, và vẫn còn bản ở kho ngoài.
- **(c)** Chấp nhận tệp nằm lại, chỉ chặn tải bằng ứng dụng: trái ý 04 mục 16 ("tự xóa theo hạn") cho dữ liệu cá
  nhân, không đề xuất.

Trong lúc chờ, Task M6-14, 15 viết theo QĐ-223-2. Hạn tải ở phía ứng dụng làm như kế hoạch. Phần `bootstrap.sh` để
trống, chờ hướng được chọn.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Cổng tĩnh (gitleaks, jscpd, danh mục, lint…) qua với ba tệp tài liệu mới, trên bản đã gộp `main` `b256382b` | `static-gates.sh` (job `static`) | `scripts/agent/ci-task.sh` | `static: success, 1 phút 6 giây` … `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh b256382b…)` (run 45930563344) |
| Không gói Go nào bị ảnh hưởng (task chỉ đổi tài liệu) | — | `scripts/agent/ci-task.sh` | `== test từ xa: không gói nào` |
| `tieu-chi.md` có đủ sáu ca của mốc | — (đếm) | `grep -c 'BC0[1-5]\*\*\|PQ06\*\*' docs/m6/tieu-chi.md` | `6` |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

(không có: task chỉ viết tài liệu)

## Khác kế hoạch

- `go test ./tests/arch/` không chạy riêng: lượt chạy trên Mac của phiên trước quá 600 s (hết giờ, không phải đỏ),
  và Mac không chạy test nặng (chủ shop 26/11). Task không sửa `docs/views.md`, ADR-46 hay mã nào mà test kiến trúc
  đọc; ci-task xác định không gói nào cần test.
- Đối chiếu view bằng database thử của `testkit.StartPostgres`, đọc qua `pg_catalog` và `information_schema`, thay
  cho `psql \d+` trên "database dev đã migrate". Database `crm` dùng chung (cổng 5600) chưa migrate. Không được chạy
  `make db-up`/`db-reset` trên bộ chung.
- Tên tệp task theo lệnh giao việc, thay cho "Chuẩn bị M6" của Step 0.
- Kế hoạch M6 (ngoài repo) được thêm khối "Bổ sung Task 223" sau bảng giả định, trỏ tới `doi-chieu-gia-dinh.md`.

## Việc phát hiện thêm

- `customer`: công thức tỷ lệ hoàn chép ở hai chỗ (`customer.go:181` `statsOf` và `search.go:183`). Gộp thì làm ở
  một task dọn của `customer`, không làm ở M6.
- Mục "Cấu trúc file" của kế hoạch M6 ghi `console/src/features/reports/**`, nhưng console thật dùng
  `console/src/screens/<tên>/`. Task M6-16–18 theo mã thật (G6).
- Kế hoạch ghi `testkit.MemFileStore`, `testkit.SignAccessJWT`. Tên thật là `filestest.MemFileStore` và
  `cfaccesstest.AccessKeys` + `serveAs` (G1, G5).
- G8: cột bảng `ord.order_status_history.from_status` cho phép `NULL`; giá trị `''` là do view `COALESCE` mà có.
  `reportseed` (chèn bảng gốc) chèn `NULL` hay `''` đều được; mọi câu báo cáo và câu so đọc qua view.

## Reviewer

Kết luận: **ĐẠT** (lượt 2).

- Lượt 1: CẦN SỬA, hai phát hiện. (1) Khóa tệp xuất của QĐ-223-2 bỏ mất `<runID>`: hai lần chạy cùng nội dung dùng
  chung một dòng `files` (`ON CONFLICT DO NOTHING`, `FileIDByKey` không lọc `deleted_at`/`expires_at`) → đã thêm
  `<runID>` ở task, G5, threat model, khối bổ sung của kế hoạch. (2) G14 hướng dẫn `ScopesFor` + lọc dạng uuid làm
  lọt grant theo kho → đổi thành lọc theo `Grant.Scope.Type`. Thêm hai ghi chú điều kiện cho QĐ-223-4, QĐ-223-9.
- Lượt 2: ĐẠT, không còn phát hiện phải sửa; đối chiếu lại `register.go:60`, `files.sql:26-32`,
  `permissions.go:269-281`, `orders_dynsql.go:208-239`, `boundaries_test.go:49`.
- Ghi chú không bắt buộc (để task sau): thân Task M6-14 (kế hoạch dòng 742, 744) còn ghi khóa và quy tắc hết hạn cũ;
  khối "Bổ sung Task 223" đã nói task sau theo mã thật. M6-04 nhận phạm vi toàn shop theo `isAll`
  (`Type == ScopeAll && ID == ""`, `permissions.go:265`).
