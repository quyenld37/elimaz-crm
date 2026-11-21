# Task 443 — Test chập chờn chặn gộp 21-11: nạp dữ liệu thử đơn kẹt, mã vận đơn giả trùng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task của điều phối viên 21/11, không có
trong kế hoạch; số toàn cục 443, dòng `| chung (21/11) | x-flaky2 | 443 | Test chập chờn chặn gộp 21/11 |` thêm vào
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`). Loại việc (quy trình mục 3): A. Sửa gốc, không nới tiêu chí,
không retry test, không skip.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `testkit.StubShipment` giữ nguyên chữ ký và hợp đồng (gọi lại
  cùng mã không đổi gì, đơn không có thì không chèn), chỉ đổi cách sinh mã vận đơn; 4 chỗ gọi:
  `internal/modules/order/env_test.go:149`, `internal/modules/customer/stats_test.go:124`,
  `internal/httpapi/handlers_order_cancel_test.go:54` (và qua `orderEnv.stubShipment` trong test của order). Không ký
  hiệu xuất ra ngoài mới trong mã chạy; `testkit` là gói chỉ dùng trong test.
- Gói phụ thuộc ngược (go list): `internal/testkit/shiptest`, `internal/testkit/codtest` và 35 gói test nhập testkit
  (đổi testkit → `ci-task.sh` chạy toàn bộ).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (câu SQL của testkit, không qua sqlc; không migration).
- Tệp đổi: `internal/testkit/shipstub.go` (hằng `stubPendingSQL`, `stubCodeAlphabet`, `stubCodeAttempts`, hàm
  `stubCodeSuffix` — không xuất ra ngoài), `internal/testkit/shipstub_test.go` (mới), `internal/testkit/export_test.go`
  (mới: lộ `stubCodeSuffix` cho gói `testkit_test`), `internal/modules/fulfillment/lists_stuck_test.go`,
  `internal/modules/fulfillment/track_poll_test.go` (`clone` chạy trong `ownerBulk`),
  `internal/modules/fulfillment/env_test.go` (`bulkLoadTimeout`, `ownerBulk`). Không đổi `docs/danh-muc-dung-chung.md`
  (không có mục `StubShipment`). Ngoài repo: một dòng trong `docs/superpowers/plans/danh-so-task-toan-cuc.md` (đã thêm).
- Test sẽ chạy: `internal/testkit` (test mới), `internal/modules/fulfillment` (vùng rủi ro cao `fulfillment/status.go` →
  cả gói), `internal/modules/order` (`TestShipping_Invariants`, `TestReturns_Invariants`, cả gói vì vùng cao),
  `internal/modules/customer`, `internal/httpapi` (chỗ gọi); mỗi test đã sửa chạy lặp ≥ 30 lần dưới tải; cổng
  `ci-task.sh` (toàn bộ vì đổi testkit).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục testkit
  (`StartPostgresSeeded`/`OpenStaffSeeded` của Task 423, `FixedUUID`, `ExplainAsApp`, `RequireNoSeqScan`);
  `fulfillment.NewShipmentCode` (Crockford base32, ngẫu nhiên từ `rnd`); `trackEnv.clone` (nạp bằng
  `INSERT … SELECT generate_series`); `shiptest.NewShop`/`Shop.Order` để dựng đơn thật cho test của helper.
- Dùng lại / viết mới vì: dùng lại `clone` (đã là một câu `INSERT … SELECT generate_series` mỗi bảng), chỉ đổi giới hạn
  thời gian của transaction nạp; không dùng `StartPostgresSeeded` (xem Quyết định thay chủ shop). Mã vận đơn giả: dùng
  cùng bảng chữ Crockford base32 như `NewShipmentCode` (khớp CHECK `^E[0-9]{6}[0-9A-Z]{6}$`); không nhập
  `fulfillment` vào testkit (testkit không nhập module nghiệp vụ trừ helper test của module) — chép hằng bảng chữ 32 ký tự.
  Test của helper dùng `shiptest` để có một đơn thật, nhân bản đơn bằng SQL như `clone`.

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi helper test (`testkit`) và test; không dữ liệu, module, HTTP, quyền, tiền, thư viện mới.
- 6.1 (đồng thời): mã vận đơn giả chèn bằng `ON CONFLICT (tenant_id, code) DO NOTHING` — hai test song song cùng tenant
  không lỗi 23505 mà chuyển sang mã thử kế tiếp; xung đột mã do chỉ mục unique bắt, không phụ thuộc RLS.
- 6.2 (gọi lại): gọi lại `StubShipment` cùng mã vận đơn vẫn không đổi gì (`NOT EXISTS` theo id); có test.
- 6.9 (treo lâu): giới hạn 30 s mỗi câu giữ nguyên cho pool và cho mọi câu đang được kiểm (EXPLAIN của câu đơn kẹt chạy
  bằng `crm_app` 30 s như cũ); chỉ transaction nạp dữ liệu thử lớn đặt `SET LOCAL statement_timeout` riêng (5 phút, như
  `seedTimeout` 8 phút của Task 423 cho cùng loại việc), hết transaction là hết.
- 6.3–6.8, 6.10–6.13: không áp dụng: không khóa, tiền, việc nền, dữ liệu cá nhân, quyền mới.

## Quyết định thay chủ shop

- Bước nạp của `TestStuck_PlanUsesIndexes`: chọn **giới hạn thời gian riêng cho transaction nạp** (`SET LOCAL
  statement_timeout`) thay vì database mẫu có dữ liệu (`StartPostgresSeeded`). Vì: dữ liệu của test dựa trên shop, đơn,
  vận đơn mẫu dựng bằng mã thật (`newTrackEnv`, `te.shipped`) với mã ngẫu nhiên lúc chạy; chuyển sang `Seed` phải viết
  lại toàn bộ shop bằng SQL viết thẳng (mã cố định `FixedUUID`), lệch khỏi dữ liệu mà mã thật dựng — dễ sai, khó đảo
  ngược. Nạp đã là `INSERT … SELECT generate_series` (không lặp từng dòng); gốc lỗi là bước nạp dùng chung giới hạn 30 s
  dành cho câu của ứng dụng. Phương án khác: `Seed` (nhanh hơn ở lần chạy sau, nhưng như trên); giảm số dòng (nới tiêu
  chí, loại).
- Mã vận đơn giả: chọn **thử lần lượt tối đa 8 mã ứng viên** (băm SHA-256 của mã vận đơn + số lần thử → 6 ký tự Crockford
  base32, 30 bit) với `ON CONFLICT (tenant_id, code) DO NOTHING`, thay vì chỉ thêm bit (30–31 bit vẫn có xác suất trùng
  ~9% ở 20.000 mã theo bài toán ngày sinh). Phương án khác: dùng thẳng `NewShipmentCode` (ngẫu nhiên, vẫn trùng được).

## Gốc lỗi và cách sửa

| Test | Gốc | Sửa |
|---|---|---|
| `fulfillment` `TestStuck_PlanUsesIndexes` (57014 khi gộp, máy tải ~15) | dữ liệu thử 50.000 + 500 vận đơn (và đơn) nạp bằng 9 lần `INSERT … SELECT generate_series` cộng 5 câu chèn việc Cần xem/gọi lại, đều qua pool chủ bảng có giới hạn 30 s/câu của ứng dụng; lúc máy chung bận một câu nạp 6.250 đơn + vận đơn vượt 30 s (chạy riêng: 4,8 s) | mọi câu nạp (cả `clone`) chạy trong `ownerBulk`: `SET LOCAL statement_timeout = '5min'` chỉ cho transaction nạp; ANALYZE ở transaction riêng sau khi dữ liệu commit; năm câu EXPLAIN đang được kiểm vẫn chạy bằng `crm_app` 30 s, vẫn `RequireNoSeqScan`, số dòng giữ nguyên |
| `order` `TestShipping_Invariants`, `TestReturns_Invariants` (23505 `shipments_code_key`) | `StubShipment` sinh mã `E`+yyMMdd+6 ký tự hex của md5(id): 24 bit, một ứng viên; nhiều vận đơn giả trong một database cùng ngày trùng theo bài toán ngày sinh (20.000 mã: kỳ vọng ~12 cặp trùng) | 6 ký tự Crockford base32 (30 bit SHA-256 của id + số lần thử), chèn `ON CONFLICT (tenant_id, code) DO NOTHING`; không ra dòng mà đơn còn, vận đơn chưa có → thử ứng viên kế (tối đa 8, hết thì `t.Fatalf`); tài khoản hãng giả có rồi thì chỉ đọc (trước đây `DO UPDATE` ghi lại dòng mỗi lần gọi) |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện lỗi mã trùng trên câu cũ | `TestStubShipment_ManyCodesUnique` (với `stubShipmentSQL` cũ) | `go test -race -count=1 -run TestStubShipment ./internal/testkit/` | `FAIL … duplicate key value violates unique constraint "shipments_code_key" (SQLSTATE 23505)` |
| 20.000 vận đơn giả một database không trùng mã, đúng CHECK 0118 | `TestStubShipment_ManyCodesUnique` | `go test -race -count=3 -run TestStubShipment ./internal/testkit/` (song song với hai lệnh dưới, tải máy 10–14) | 3/3 PASS (35,9 s; 36,2 s; 41,0 s) |
| Mã ứng viên đã bị giữ → lấy ứng viên kế, không 23505 | `TestStubShipment_CodeTakenTriesNext` (ứng viên 0, 1 bị giữ → mã = ứng viên 2) | như trên | 3/3 PASS |
| Gọi lại cùng mã không đổi gì; đơn không có → không chèn | `TestStubShipment_IdempotentAndMissingOrder` | như trên | 3/3 PASS |
| Hai test của order hết 23505, lặp ≥ 30 lần dưới tải | `TestShipping_Invariants`, `TestReturns_Invariants` | `go test -race -count=30 -run 'TestShipping_Invariants$\|TestReturns_Invariants$' ./internal/modules/order/` | 30/30 + 30/30 PASS, `ok … order 1059.012s` |
| Đơn kẹt: nạp không còn 57014, vẫn kiểm không Seq Scan, lặp ≥ 30 lần dưới tải | `TestStuck_PlanUsesIndexes` | `go test -race -count=30 -run 'TestStuck_PlanUsesIndexes$' ./internal/modules/fulfillment/` | 30/30 PASS, `ok … fulfillment 176.533s` |
| Cổng của task | — | `scripts/agent/ci-task.sh` (lần 1 đỏ vì Docker Desktop tắt giữa chừng: `dial tcp 127.0.0.1:5600: connect: connection refused`; crm-shared tự lên lại ~11:54; chạy lại) | lần 2: build, vet, lint, catalog-check, jscpd, gitleaks, oasdiff, test-guard qua; test toàn bộ: mọi gói `ok` (có `internal/testkit 73.093s`) trừ `internal/platform/observability` `TestDeepHealth` (`deephealth_test.go:36: tuổi: …`), đỏ y hệt trên `main` sạch `2818f9e` (worktree tạm), không liên quan — xem Việc phát hiện thêm. `CI-TASK ĐỎ: test` chỉ vì test đó |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `internal/platform/observability/deephealth_test.go:36` `TestDeepHealth` đỏ ổn định (cả trên `main` `2818f9e`) sau
  khi Docker Desktop khởi động lại 21/11 ~11:45: test chèn `platform.backup_runs.finished_at = now() - interval '3 hours'`
  bằng đồng hồ PostgreSQL, còn `DeepHealth` (`deephealth.go:78`) tính `int64(now.Sub(backup).Hours())` bằng
  `time.Now()` của Go. Đo: đồng hồ của database chạy trước máy ~0,17 s → 2 giờ 59 phút 59,8 giây cắt xuống 2 ≠ 3. Cùng
  loại bẫy "một đồng hồ cho mỗi phép so" (Task 400, 440): test nên ghi `finished_at` bằng `time.Now()` của Go. Không
  sửa ở task này (ngoài phạm vi), cần một task riêng của điều phối viên.
- **Lượt bổ sung 21/11 (TestDeepHealth): gốc ở mã chạy, không sửa test.** Bên ghi thật của mốc sao lưu là
  `scripts/backup/runs-lib.sh` (`runs_record`, chạy trong container PostgreSQL): `INSERT INTO platform.backup_runs
  (kind, ok, duration_seconds, detail)` không đưa `finished_at`, nên mốc lấy `DEFAULT now()` (0010_ops.sql) — **đồng hồ
  PostgreSQL**. Bên đọc `DeepHealth` (`deephealth.go:78`) tính `int64(now.Sub(backup).Hours())` với `now :=
  time.Now()` — **đồng hồ của tiến trình Go**. Mã chạy trộn hai đồng hồ, nên test đang ghi mốc đúng bằng đồng hồ của bên
  ghi thật (`now() - interval '3 hours'`); đổi test sang `time.Now()` của Go là che lỗi (khác Task 440, nơi bên ghi thật
  dùng đồng hồ máy). Cùng dạng ở `walArchivedAge` (`deephealth.go:100`): `pg_stat_archiver.last_archived_time` (đồng hồ
  PostgreSQL) trừ `time.Now()` của Go. Tác động thật: trên máy chủ Linux container dùng chung đồng hồ nhân với máy nên
  lệch ~0; trên máy dev (colima) lệch vài phần mười giây tới vài phút (Task 440 gặp 12 phút) → số giờ sao lưu, giây WAL
  sai khi lệch, test của mốc giờ tròn đỏ. Đề xuất (task riêng, chọn một): (a) tuổi sao lưu và WAL tính trong SQL bằng
  `now()` của PostgreSQL (`extract(epoch FROM now() - finished_at)`), mỗi mốc so với đồng hồ của bên ghi nó — nhịp sống,
  lần chép tệp vẫn so với Go như hiện tại; (b) `runs-lib.sh` ghi `finished_at` bằng đồng hồ của máy gọi — không giải
  được vì script chạy trong container (cùng đồng hồ PostgreSQL), và `last_archived_time` không đổi được. Phương án (a)
  hợp hơn. Sau khi sửa mã chạy, test giữ nguyên `now() - interval '3 hours'` và `== 3`.

## Dừng để hỏi

- (Lượt bổ sung 21/11) `TestDeepHealth` không sửa: brief của điều phối viên nói "nếu gốc là mã chạy trộn hai đồng hồ thì
  ghi rõ vào Việc phát hiện thêm và dừng hỏi" — đúng trường hợp này (xem mục trên). Tái hiện lúc làm, không cần chèn
  lệch giả: `go test -race -count=3 -run '^TestDeepHealth$' ./internal/platform/observability/` → 3/3 `FAIL
  deephealth_test.go:36: tuổi: …`; đo đồng hồ (psql `clock_timestamp()` kẹp giữa hai mốc `time.time()` của Mac, 3 lần):
  mốc PostgreSQL lớn hơn cả mốc Mac đo **sau** nó 0,09–0,14 s. Cần OK: mở task sửa `DeepHealth` theo phương án (a)
  (vùng `internal/platform/observability`, không phải vùng rủi ro cao). Không đổi mã nào ở lượt này.

## Reviewer

Kết luận: ĐẠT (lượt 1). Phát hiện phải sửa: không có. Ảnh hưởng khớp (3 chỗ gọi `StubShipment` trực tiếp, chữ ký
không đổi; `ON CONFLICT` đúng chỉ mục `shipments_code_key`, `carrier_accounts_ref_key`; bảng chữ nằm trong CHECK 0118).
Không test nào bị tắt, nới; cách cũ (md5, `DO UPDATE SET name`) đã bỏ hẳn. Ghi chú không bắt buộc (không sửa): chú thích
"30 bit đầu" thực ra là 30 bit thấp của 4 byte đầu (đúng tính chất, chỉ lời tả); `TestStubShipment_CodeTakenTriesNext`
có thể đỏ nếu chạy vắt qua nửa đêm giờ Việt Nam (xác suất rất nhỏ); `TestDeepHealth` cần task riêng.
