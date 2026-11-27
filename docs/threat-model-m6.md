# Threat model M6 — báo cáo vận hành, chạy nền, xuất Excel, xuất danh sách khách

Bản đầu viết ở Task M6-00 (số toàn cục 223, 27/11/2026), trước khi có mã M6, theo mẫu `docs/threat-model-m5.md`. Đây
là bước 2 của "Cách làm việc ở mỗi mốc" (spec 08). Task M6-21 (244) hoàn thiện bản này: điền tên hàm test có thật vào
cột "Test", rồi đổi mỗi dòng **Chưa có** thành tên test, hoặc ghi vì sao chấp nhận.

**Phạm vi:** chỉ phần mới của M6. Ranh giới của M0 (Access, tiến trình → PostgreSQL), M1 (kho tệp, đường dẫn ký
sẵn), M2–M5 nằm ở `docs/threat-model-m{0,1,2,3,5}.md`. Chữ ký thật của hàm nhắc tới ở `docs/m6/doi-chieu-gia-dinh.md`.

**Nguồn:**

- Kế hoạch `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md`: Q8, Q12–Q14, Q16–Q18, Q22, Q23, Global
  Constraints, Review Focus.
- Spec `04-kien-truc.md` mục 7, 11, 13, 16; ADR-20, 25, 29, 36, 46.

**Cách đọc:**

- Cột "Task" ghi số toàn cục, kèm số nội bộ M6-NN. Tên test là tên tệp test mà kế hoạch giao; Task 244 điền tên hàm.
- STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu

```
 Nhân viên ──(Cloudflare Access)──▶ console ──▶ crm-api  /api/v1/reports/**   x-permission (report.* | customer.export)
                                                   │
             xem tại chỗ (B1) ─────────────────────┤  tối đa 2 chỗ mỗi tiến trình; transaction CHỈ ĐỌC
                                                   │  (transaction_read_only, statement_timeout 30 s,
                                                   │   max_parallel_workers_per_gather 0, application_name crm-report)
                                                   ├──▶ PostgreSQL: chỉ view _v đã công bố, lọc người bán own,
                                                   │    lọc Page / kho theo phạm vi, RLS theo tenant
                                                   │
             chạy nền, xuất tệp (B2) ──────────────┤  POST …/runs (Idempotency-Key) → report_runs + lệnh command.report_run
                                                   │  + audit, cùng một transaction
                                                   ▼
                                        platform.jobs ──▶ worker (tối đa 2 chỗ, mỗi câu tối đa 60 s)
                                                   │  nạp lại người tạo (LoadUserByID) + phạm vi LÚC CHẠY
                                                   ├──▶ PostgreSQL chỉ đọc (như trên)
                                                   ├──▶ tệp tạm REPORT_TMP_DIR (xóa ở mọi đường)
                                                   ├──▶ (B3) kho tệp: FileStore.Put(BucketPrivate,
                                                   │        priv/<tenant>/{reports|customers}/<runID>/<sha256>.xlsx) ngoài transaction
                                                   │        → files.Register (expires_at) trong transaction
                                                   └──▶ báo hiệu report.run_finished (chỉ mã lần chạy)
 Nhân viên ──▶ crm-api GET …/runs/{id}/file (B4) ── kiểm người tạo | report.exports.manage, quyền xuất lúc tải
                                                   → audit → files.DownloadURL (ký sẵn, hạn 5 phút) ──▶ trình duyệt tải
                                                     thẳng từ kho tệp
```

**Dữ liệu nhạy cảm mới:**

- Tệp danh sách khách: tên, SĐT chính, số liệu mua hàng (dữ liệu cá nhân).
- Số liệu kinh doanh: doanh số, tồn, tỷ lệ hoàn theo hãng (bí mật kinh doanh).
- Đường dẫn tải ký sẵn: ai cầm cũng tải được, trong 5 phút.

## (B1) Xem báo cáo tại chỗ

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| T | Tham số chiều nhóm, cột sắp xếp, cơ sở ngày chèn SQL (`groupBy = "status; DROP TABLE x"`) | Biểu thức SQL dựng lúc chạy chỉ lấy từ danh sách trắng trong mã. Giá trị người dùng luôn là tham số `$n`, không ghép chuỗi. Tham số lạ → 400 `bad_params` trước khi gửi câu nào. SQL dựng chỉ nằm trong `*_dynsql.go`, có test trên PostgreSQL thật (`tests/arch/dynsql_test.go`) | 227 (M6-04), 228 (M6-05) | `orders_dynsql_test.go` (đếm câu bằng `pgx.QueryTracer`: 0 câu), `interactive_test.go` |
| I | Nhân viên chỉ có quyền Page P1 thấy đơn của P2, đơn web, đơn nhập tay (Review Focus 1, PQ04) | Tập đơn dựng bằng một hàm `orderSet` chung cho tổng, ô, chi tiết và tệp xuất. Không `AllPages` thì chỉ `channel_account_id = ANY($n)`, loại đơn không có Page. Phạm vi lấy từ grant (`identity.Scope{Type: page}`) và so với `identity.Can` | 227 (M6-04), 230 (M6-07) | `scope_test.go`, `scope_property_test.go` (rapid), `orders_property_test.go` |
| I | Người không có quyền nào thấy số (PQ03) | Không có dòng phạm vi là không thấy gì (`NoSales()`/`NoInventory()` → 403). Route khai `x-permission`, và `routes_test` đòi mọi route có quyền | 225 (M6-02), 227 (M6-04) | `routes_test.go`, `scope_test.go` |
| I | Lẫn số của shop khác hay người bán khác (PQ08, ADR-29) | Mọi câu chạy trong `tenancy.WithTenant` bằng `crm_app` (RLS). Điều kiện `seller_id = <own>` ở tầng dưới cùng, kể cả khi hôm nay chỉ có một người bán. `OwnSeller` theo luật của `catalog.DefaultSeller` | 227 (M6-04) | `orders_dynsql_test.go` (người bán `partner` không vào tập), `tests/arch/sellerfilter_test.go` |
| I | Kết quả báo cáo chứa SĐT, địa chỉ, tên khách | Danh sách chi tiết chỉ có mã đơn, mã khách, trạng thái, số tiền, kênh, nguồn, nhân viên. Không đọc cột `ship_phone`, `ship_address` | 230 (M6-07) | `drilldown_test.go` |
| D | Báo cáo nặng giữa ca live chiếm hết kết nối `api` và CPU database (BC05, Review Focus 3) | Tối đa 2 chỗ báo cáo mỗi tiến trình `api` (giữ 18/20 kết nối cho lên đơn). `statement_timeout` 30 s; không chạy song song trong PostgreSQL; `work_mem` 32 MB. Hết chỗ hoặc quá 30 s → `run_in_background`. Khoảng ngày tối đa 366 ngày, chi tiết tối đa 200 dòng mỗi trang | 228 (M6-05), 243 (M6-20) | `interactive_test.go`, `tests/perf/reports_perf_test.go`, `loadtest/k6/reports.js` |
| D | Câu báo cáo quét cả bảng lớn | Chỉ mục còn thiếu thêm ở `0603` (G16). `EXPLAIN` bằng role ứng dụng trên 100.000 đơn không có `Seq Scan` | 226 (M6-03), 227–234, 243 | `testkit.RequireNoSeqScan` trong `orders_dynsql_test.go` và các test `*_dynsql_test.go` |
| T | Báo cáo vô tình ghi dữ liệu | Transaction `transaction_read_only = on`; `reporting` chỉ ghi ba bảng của mình (qua sqlc), audit, `jobs`, `files` | 228 (M6-05) | `interactive_test.go` (thử ghi trong transaction báo cáo → lỗi 25006) |

## (B2) Chạy nền (`report_runs`, lệnh `command.report_run`)

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| D | Bấm xuất liên tục làm đầy hàng đợi, chiếm chỗ của worker | Tối đa 5 lần đang chờ hoặc đang chạy mỗi người (lần 6 → 429 `too_many_runs`); `Idempotency-Key` gộp 50 lần bấm thành một. Tối đa 2 chỗ mỗi worker, giữ 6/8 chỗ cho tin nhắn và vận đơn. Hết chỗ → `not_sent` có `KeepAttempt` và `RetryAfter` 15 s, không tiêu lượt (G2); trần 1 giờ (`ExpiresAfter`) → `failed`/`busy` | 235 (M6-12) | `runs_test.go`, `runs_property_test.go` |
| D | Một lần chạy quá nặng giữ kết nối lâu | Mỗi câu tối đa 60 s (`REPORT_BACKGROUND_TIMEOUT`, dưới hạn giữ chỗ 90 s của hàng đợi); 57014 hoặc kết quả quá 5 MiB → `failed`/`too_heavy`, việc `done` (không `dead`) | 235 (M6-12) | `runs_test.go` |
| E | Người bị khóa, hay vừa mất quyền, vẫn nhận kết quả từ lần chạy đã xếp hàng | Worker nạp lại người tạo bằng `LoadUserByID` và tính lại phạm vi **lúc chạy**. Bị khóa hoặc mất quyền đọc (hay mất `report.export` với `xlsx`) → `failed`/`forbidden` | 235 (M6-12) | `runs_test.go`, `internal/modules/identity/users_test.go` |
| E | Người chỉ có quyền Page tạo lần chạy với phạm vi rộng hơn | Phạm vi không phải tham số của yêu cầu; luôn tính từ grant của người tạo. `params` chỉ có ngày, mã, khóa lọc | 235 (M6-12) | `runs_test.go` |
| I | Người khác xem kết quả lần chạy, đoán được mã lần chạy tồn tại | Không phải người tạo, không có `report.exports.manage` → 404 (không lộ sự tồn tại); tenant khác → 404 | 235 (M6-12) | `handlers_reports_runs_test.go` |
| T | Worker chết giữa chừng; lần chạy bị chạy hai lần ra hai kết quả | `MarkRunning` có điều kiện trạng thái; `Lookup` đọc `report_runs` để `ResolveInDoubt` không gọi lại `Execute`; đúng một lần chuyển sang `ready` | 235 (M6-12) | `runs_test.go`, `runs_property_test.go` (rapid) |
| R | Chối đã yêu cầu báo cáo, đã xuất | `identity.RecordAudit` (`report.run_requested`) trong cùng transaction tạo lần chạy. Audit chỉ có mã, ngày, khóa lọc | 235 (M6-12) | `runs_test.go` |
| I | Số liệu, tham số lọt vào log, dấu vết, báo hiệu | Log của lần chạy chỉ có `run_id`, `report`, `rows`, `bytes`, `duration_ms`. Báo hiệu chỉ có `Entity{"report_run", id}`, `Requires` là quyền đọc của họ báo cáo | 235 (M6-12), 237 (M6-14) | `exports_test.go` (bắt log) |
| D | Kết quả và dòng lần chạy tích lại mãi | Kết quả `json` xóa sau 24 giờ; dòng `report_runs` xóa sau 90 ngày; lượt dọn mỗi lần 500 dòng, `FOR UPDATE SKIP LOCKED` | 235 (M6-12) | `cleanup_test.go` |

## (B3) Dựng tệp Excel, tải lên kho

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| T | Ô chứa công thức (`=HYPERLINK(…)`, `+84…`, `@SUM`) chạy khi mở tệp (04 mục 13, PQ06) | Chỉ một đường ghi ô: bộ ghi theo luồng của `internal/platform/xlsx` gọi `xlsx.Sanitize` của M1 (đủ luật, G19). Chuỗi luôn là ô chữ. Không có hàm ghi công thức; test kiến trúc cấm `SetCellFormula`, `SetCellHyperLink`, `AddVBAProject` và cấm nhập excelize ngoài gói. Tệp không có `<f>`, `calcChain`, `vbaProject`, `externalLinks` | 236 (M6-13) | `writer_test.go`, `fuzz_test.go`, `property_test.go`, `tests/arch/xlsx_test.go` |
| D | Tệp tạm đầy đĩa của worker | Một tệp tạm mỗi lượt trong `REPORT_TMP_DIR` (bắt buộc cấu hình), xóa ở mọi đường thoát. Tối đa 200.000 dòng (`REPORT_MAX_ROWS`), quá thì cắt và ghi chú. Ghi theo luồng (`HeapInuse` ≤ 50 MB với 200.000 × 10). Tối đa 2 lần xuất cùng lúc mỗi worker. Có cảnh báo đĩa sẵn của M0 (`metrics.DiskUsedPct`) | 236 (M6-13), 237 (M6-14) | `writer_test.go` (bộ nhớ), `exports_test.go` (cắt dòng, tệp tạm bị xóa) |
| T | Thử lại giữa chừng (`RetryTx` chạy lại) ghi lặp dòng, hay khóa tệp không khớp nội dung | Mỗi lần chạy lại: cắt tệp về 0, bộ băm mới, đặt lại đếm dòng. Khóa chứa SHA-256 nội dung nên `Put` lặp lại an toàn (`ErrObjectExists` = cùng nội dung). `Register` với khóa đã có → id cũ | 237 (M6-14) | `exports_retry_test.go`, `exports_test.go` |
| I | Tệp nằm sai chỗ, người ngoài đọc được | Bucket `exports` (Task 515; migration `0606` trỏ hai mục đích `report_export`/`customer_export`, chế độ `standard`). Khóa `reports/<tenant>/…`, `customers/<tenant>/…` (`files.ExportKey`) do `files.Register` ép đúng tiền tố của mục đích (`Purpose.KeyPrefix`) và đúng tenant trong ngữ cảnh (G5). Ứng dụng không phát đường dẫn công khai nào | 226 (M6-03), 515, 237 (M6-14) | `purposes_test.go` (`TestPurposes_GoMatchesTable`), `exports_test.go` |
| I | Tệp (nhất là danh sách khách có SĐT) **nằm lại trên kho sau hạn** | Phía ứng dụng: `expires_at` của dòng `files` = hạn của lần chạy; sau hạn `DownloadURL` trả `ErrExpired` (410); lượt dọn `MarkDeleted`. Phía kho (Task 515, G5 chốt (a) 27/11): bucket riêng `crm-exports` không giữ phiên bản, không chép kho ngoài, tự xóa theo tiền tố (`reports/` 7 ngày, `customers/` 1 ngày; luật S3 + TTL fast path); `crm-app` không xóa được; hạn ứng dụng không vượt hạn kho (trần của `REPORT_FILE_TTL`, `CUSTOMER_EXPORT_TTL`) | 515, 237 (M6-14) | `tests/probe/exports_test.go` (`TestExports_*`), `tests/deploy/exports_bucket_test.go`, `TestExportObjectTTLMatchesBootstrap`, `files` `TestRegister_ExportsBucket` |
| I | Tệp xuất sản phẩm lộ giá vốn | Không cột nào đọc `variants_v.cost_price` (Q25). Bỏ mẫu đã xóa (`products_v.deleted_at`) | 237 (M6-14) | `products_export_test.go` (không có cột giá vốn) |

## (B4) Tải tệp

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| S | Người khác tải tệp của mình (đoán mã lần chạy) | `FileLink` đi qua luật xem của `GetRun`: chỉ người tạo hoặc `report.exports.manage`, không thì 404. Tenant khác → 404 | 237 (M6-14) | `handlers_reports_file_test.go` |
| E | Người đã bị gỡ quyền xuất vẫn tải tệp cũ | Kiểm lại `report.export` (hoặc `customer.export`) **lúc tải** → 403 | 237 (M6-14), 238 (M6-15) | `handlers_reports_file_test.go`, `handlers_reports_customers_test.go` |
| I | Đường dẫn tải bị chia sẻ, dùng lại về sau | Đường dẫn ký sẵn hạn 5 phút (`X-Amz-Expires=300`, `REPORT_LINK_TTL`). Mỗi lần lấy đường dẫn ghi một dòng audit. Hết hạn tệp → 410. Chấp nhận rủi ro còn lại: trong 5 phút ai cầm đường dẫn cũng tải được (ký sẵn không gắn người) | 237 (M6-14) | `handlers_reports_file_test.go` |
| R | Chối đã tải danh sách khách | `GET …/file` ghi audit (`report.file_downloaded` / `customer.export_downloaded`, mã người tải, `entity_id = runID`) trước khi trả đường dẫn (Q17) | 237 (M6-14), 238 (M6-15) | `handlers_reports_file_test.go`, `customers_export_test.go` |

## (B5) Xuất danh sách khách (PQ06)

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| E | Người có quyền theo Page xuất toàn bộ khách | `customer.export` khai `ScopeShop`, chỉ hiệu lực từ grant `all` (G18; grant theo Page → 403). Đề xuất chỉ gán Chủ và Quản lý (H11); chủ shop chọn người | 225 (M6-02), 238 (M6-15) | `permissions_test.go`, `customers_export_test.go` |
| R | Xuất danh sách khách không để lại dấu | Nhật ký ba bước: `customer.export_requested`, `customer.export_ready`, `customer.export_downloaded` | 238 (M6-15) | `customers_export_test.go` |
| I | SĐT, tên khách lọt vào nhật ký, `params`, log | Bộ lọc không nhận chữ tự do (`{"q": …}` → 400). Nhật ký và `params` chỉ có mã, ngày, số dòng. Quét chuỗi nhật ký và log, không chứa SĐT hay tên nào của dữ liệu thử | 238 (M6-15) | `customers_export_test.go` |
| I | Tệp chứa nhiều hơn mức cần | Không có địa chỉ; một dòng cho mỗi hồ sơ đại diện (đã gộp); SĐT chính là số đã xác minh mới nhất. Hạn 24 giờ | 238 (M6-15) | `customers_export_test.go`, `customers_export_property_test.go` |
| T | Tên khách là công thức | Như B3, qua `Sanitize` | 236, 238 | `customers_export_test.go` (ô `'=HYPERLINK…`) |
| T | `0605` (thêm `return_rate_bp`) làm hỏng view của module khác | `CREATE OR REPLACE VIEW` chép nguyên định nghĩa hiện hành, chỉ thêm cột sau `name_source`; test so danh sách cột trước và sau. Công thức chia nguyên đúng `customer.Stats.ReturnRateBP` (G10) | 238 (M6-15) | `customers_export_test.go` |

## Rủi ro chấp nhận

- Giới hạn 5 lần chạy mỗi người là giới hạn mềm: hai yêu cầu đến cùng lúc có thể cùng qua (Q22). Không ảnh hưởng tính
  đúng; trần cứng là số chỗ của worker.
- Trong 5 phút hiệu lực, đường dẫn ký sẵn tải được bởi bất kỳ ai cầm nó (B4).
- Tải tệp là `GET` có ghi audit, không có khóa chống trùng; tải lại chỉ thêm dòng nhật ký (Q17).
