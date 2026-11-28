# Task 219 — Thử tải flash sale và lượt đọc của website (M5-36)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 219 / M5-36). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu xuất ra ngoài nào. Thêm mới: gói `internal/queries/flashverify` (`Verify`, `Report`, `Failure`, `ItemCount`, hằng `Check*`, `ErrSaleNotFound`) — 0 chỗ tham chiếu trước task; lệnh `crm verify-flash --sale <id>` trong `cmd/crm` (`verifyFlashCommand`, `isVerifyFlashCommand`, `runVerifyFlash` — chưa xuất); lệnh con `loadkit m5key`, `m5sale`, `m5close`, `m5stats`, `m5report` trong `tools/loadkit` (gói main). Trong `tools/loadkit` đổi chữ ký `seedProduct` (trả thêm mã mẫu): 5 chỗ gọi (`fb.go` seedFB, `m2.go` seedM2 hai chỗ — của Task 88 vừa gộp, `m5.go` seedM5Sale, seedM5Catalog), sửa cả năm. `cmd/crm/main.go`: `run` gom năm lệnh chạy một lần vào `isOneOffCommand` (`import_website.go`, cạnh `runOneOffCommand`; gocognit của `run` 21 > 20), `verify-flash` đi qua `runOneOffCommand`, mã đợt sai dạng → `runVerifyFlash` in cách gọi, thoát 2 (như trước). `tools/loadkit`: `seedFlags.check` thay bằng `seedFlags.prepare` (đọc cờ, đòi -out/-tokens, `prepareShop`; 4 chỗ gọi fbseed, m2seed, m5key, m5sale), `prepareShop` nhận `extra func(context.Context, *db.DB) error`, hàm mới `openOwnerFrom` (m2stats, m5stats), bỏ `seedM5AdminUnits` (dùng `seedAdminUnits`). `tests/arch`: `sqlusage_test.go` thêm `flashverifyPkg` (chỉ view `_v` của promotion, inventory, ord, như `weborders`), `viewsdoc_test.go` 53 → 56 view. `inventory.Reconcile`, `customer.EnsureFixtureAdminUnits` chỉ được gọi thêm (không đổi chữ ký).
- Gói phụ thuộc ngược (go list): `internal/queries/flashverify` mới, chỉ `cmd/crm` nhập. `cmd/crm`, `tools/loadkit` là gói main, không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: không có (k6 và loadkit gọi operation sẵn có: `webFlashBuy`, `webFlashComplete`, `webFlashCurrent`, `webCatalogProducts`, `webAvailability`, các operation flash sale và khóa dịch vụ của nhân viên).
- Chỗ dùng trên màn hình: không có.
- Mã sinh: `make gen` thêm kiểu của ba view mới vào `models.go` của 13 store (sqlc đọc cả thư mục migrations); không sửa tay. Tài liệu: `loadtest/README.md` thêm mục "Bộ thử tải M5", `docs/do-luong/thu-tai-m5.md` mới.
- Câu sqlc, bảng, pha migration: migration mới `0543_flash_verify_views.sql`, pha mở rộng (chỉ `CREATE VIEW` + `GRANT SELECT`): `inventory.flash_sale_units_v`, `promotion.flash_sale_claims_v`, `ord.order_flash_lines_v` (security_invoker, RLS của bảng gốc áp). Số 0543: số trống kế tiếp sau 0542 (Task 88) trên main và mọi nhánh task-* (đã kiểm `git ls-tree` từng nhánh). Câu sqlc mới `queries/flashverify/verify.sql` (khối mới trong `sqlc.yaml` → `internal/queries/flashverify/internal/store`). Không bảng nào đổi.
- Hạ tầng thử: `loadtest/m5_run.sh` (mới, dùng `fb_lib.sh`), `loadtest/k6/m5/*.js` (mới), job `loadtest` của `.github/workflows/test-remote.yml` (nhận `m5:…`, dựng Valkey của compose trong job, hạn 60 phút như job cũ (trần của test kiến trúc)), `scripts/ci/remote-test.sh` (`CRM_CI_LOADTEST=m5:…`). Bài M4 (`live`, `chaos`) giữ nguyên đường cũ.
- Test sẽ chạy: `go test -race -run '^TestVerify' ./internal/queries/flashverify/` (mỗi phép kiểm a–h một ca dữ liệu sai cố ý, ca đúng, ca đợt không có); `go test -run '^TestVerifyFlash' ./cmd/crm/` (lệnh: thoát 0 khi đúng, 1 khi sai, 2 khi sai cách gọi; (g) qua `inventory.Reconcile`); `go test -run '^TestM5' ./tools/loadkit/` (chấm ngưỡng từng bài, đọc số đo `readM5Stats` trên PostgreSQL thật). Sáu bài k6 chạy ở bể GitHub (job `loadtest` của `test-remote.yml`), không chạy trên Mac.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tenancy.ListTenantIDs, tenancy.WithTenant, openCommandDB của `cmd/crm`), `inventory.Reconcile` (đối chiếu held của số dư M5-19), `inventory` câu `FlashUnitsVsOrderLines`/`FlashAvailableInClosedSales` (chỉ trong module inventory, không công bố), `promotion/flashtest`, `invtest.Shop`, `catalogtest`, bộ thử tải M4 (`loadtest/fb_run.sh`, `fb_lib.sh`, `tools/loadkit` `access`, `seed`, `apiClient`, `seedShop`, `seedProduct`, `seedStock`), job `loadtest` của `.github/workflows/test-remote.yml`, `scripts/ci/remote-test.sh` (`CRM_CI_LOADTEST`).
- Dùng lại / viết mới vì: dùng lại `inventory.Reconcile` cho phép kiểm (g) đúng như M5-19 thay vì viết lại câu so held (một nguồn quy luật); dùng lại `fb_lib.sh` (dựng ba vai trò như tiến trình), `loadkit access`/`seed`/`apiClient`/`seedShop`/`seedProduct`/`seedStock` cho dữ liệu thử; dùng lại job `loadtest` và `CRM_CI_LOADTEST` (thêm giá trị `m5-<bài>`), không thêm workflow mới. Viết mới `flashverify` vì hai câu đối chiếu của inventory chỉ kiểm (b), (f) và nằm trong store riêng của inventory (gói khác không gọi được); `flashverify` gom đủ a–h theo một đợt, đọc qua view như mọi gói `internal/queries`. Viết mới `loadkit m5key`/`m5sale`/`m5close` (khóa dịch vụ, đợt flash sale, đóng đợt qua API nhân viên — M4 không có), `m5stats` (`m5_stats_dynsql.go`: chỉ số của M5 — `flash_buy_ms`, `tx_retry` theo SQLSTATE, `cache_hit` — `loadstats.Read` và `readStats` của M4 không đọc) và `m5report` (ngưỡng M5 khác M4) — `m5report` dùng lại `verdict`, `below`, `pickup`, `countZero`, `passIf`, `na`, `k6Of`, `histLine`, `renderVerdicts`, `readJSONFile`, `writeJSON` của bộ M0/M4. Đơn vị hành chính thử: dùng lại `seedAdminUnits` của `m2.go` (`customer.EnsureFixtureAdminUnits` trong `db.RetryTx`). Sau khi gộp Task 88: `m5key`, `m5sale` dùng `addSeedFlags`, `seedFlags.prepare`, `prepareShop` (hook `extra` chèn đơn vị hành chính) thay đoạn mở chủ bảng, tenant, kho viết lại (jscpd báo 6 đoạn lặp ở ci-task lần 1, còn 3 ở lần 2: gom thêm `seedFlags.prepare` cho cả fbseed, m2seed; `openOwnerFrom` cho m2stats, m5stats; `m5key` dùng `seedAdminUnits` của m2).

## Soát thiết kế

- 5.1 Đúng chủ dữ liệu: không thêm dữ liệu; ba view mới mỗi view nằm trong schema của module sở hữu bảng (inventory, promotion, ord).
- 5.2 Không ghi bảng của module khác: `flashverify` chỉ đọc; (g) gọi public API `inventory.Reconcile` (ghi một dòng `inventory.reconcile_runs` của chính inventory, như lần đối chiếu tay).
- 5.3 Đọc qua view: `flashverify` chỉ nhắc `*_v` có trong `docs/views.md` (thêm ba dòng view mới).
- 5.4, 5.5, 5.6: không áp dụng: không có đường ghi nghiệp vụ, không sự kiện, không gọi ra ngoài.
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant` bằng crm_app (DATABASE_URL của vai trò); lệnh tìm tenant của đợt bằng cách thử lần lượt (như recompute-customer-stats). View `security_invoker` nên RLS áp.
- 5.8 Nghiệp vụ không biết HTTP: `flashverify` không nhập net/http.
- 5.9 Đường ghi: không có route mới.
- 5.10 Valkey: bài thử dùng hai Valkey của compose dev; kiểm đúng sai chỉ đọc PostgreSQL.
- 5.11 Thư viện: k6 v2.0.0 đã có ở job loadtest (Task 180, ADR-42); không thêm thư viện Go.
- 5.12 Theo module mẫu: gói đọc theo mẫu `internal/queries/webcatalog` (sqlc riêng, view).
- 6.1–6.3: `Verify` chỉ đọc, trong một transaction `REPEATABLE READ` (`db.TxOptions.IsoLevel`) nên mọi câu a–h (trừ g) thấy cùng một ảnh chụp, không báo lệch giả khi còn lượt mua đang chạy; không khóa dòng nào. (g) chạy `inventory.Reconcile` ở transaction riêng (READ COMMITTED như lịch đêm; câu của nó tự cùng ảnh chụp theo từng lô). Bài thử gọi lệnh sau khi k6 dừng.
- 6.4–6.9: không áp dụng (không ghi, không việc nền, không gọi ngoài); lệnh có `statement_timeout` mặc định của pool.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: chính lệnh này là bộ kiểm quy luật không bán vượt ở quy mô thử tải; test mỗi phép kiểm một ca sai cố ý.
- 6.12 Dữ liệu cá nhân: in ra chỉ mã món, mã khách (UUID), số đếm, `external_ref` (mã đơn website, không phải dữ liệu cá nhân); không SĐT. k6 sinh SĐT giả `09xxxxxxxx` theo số người ảo; không dữ liệu thật trong testdata.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| verify-flash (a)–(h): mỗi phép kiểm một ca sai cố ý, ca đúng, đợt không có, đơn hủy trước/sau khi đóng đợt | `TestVerify*` (`internal/queries/flashverify/flashverify_test.go`) | `go test -count=1 -run '^TestVerify' ./internal/queries/flashverify/` (sau khi gộp main) | `ok  elimaz.com.vn/crm/internal/queries/flashverify 0.986s` |
| Lệnh `crm verify-flash --sale`: thoát 0 đúng, 1 sai, 2 sai cách gọi; (g) qua `inventory.Reconcile` | `TestVerifyFlash` (`cmd/crm/verify_flash_test.go`) | `go test -count=1 -run '^TestVerifyFlash' ./cmd/crm/` | `ok  elimaz.com.vn/crm/cmd/crm 1.141s` |
| Chấm ngưỡng sáu bài (mỗi tiêu chí có ca đỏ), lượt hết lần gửi lại không phải lỗi, đợt một của abandon = tổng − đợt hai, chưa đo ≠ đạt; đọc số đo trên PostgreSQL thật | `TestM5SpikePasses`, `TestM5SpikeFailures`, `TestM5SpikeGaveUpIsNotError`, `TestM5BuyP95UsesDelta`, `TestM5PerCustomer`, `TestM5Sustained`, `TestM5Abandon`, `TestM5CloseWithHolds`, `TestM5WebRead`, `TestM5RenderHasEveryVerdict`, `TestM5ReadStats` | `go test -count=1 -run '^TestM5' ./tools/loadkit/` | `ok  elimaz.com.vn/crm/tools/loadkit 1.893s` |
| FS03, FS16, FS02, FS13, FS05, FS17 — sáu bài k6 | `loadtest/k6/m5/*.js` qua `loadtest/m5_run.sh all` | `CRM_CI_STATIC=false CRM_CI_LOADTEST=m5:all scripts/ci/remote-test.sh <tree> HEAD <rỗng>` → run 46106941415 (GitHub 4 vCPU) | `flash_per_customer`, `flash_sustained`, `flash_close_with_holds`, `web_read`: mọi tiêu chí ĐẠT; `flash_spike`, `flash_abandon` đỏ vì cách đếm của bộ thử (đã sửa, xem `thu-tai-m5.md`) |
| FS03, FS06 chạy lại sau khi sửa cách đếm | `flash_spike.js`, `flash_abandon.js` | `CRM_CI_LOADTEST=m5:flash_spike,flash_abandon …` → run 46109908569 | `REMOTE-TEST XANH`; bảng: "KẾT QUẢ: mọi bài ĐẠT" (500 bought, lỗi khác 0, deadlock +0, verify-flash ĐẠT; abandon 500/250/250, hết oan 0) |
| FS14 ghi phân bố thứ tự đến | `bought_arrival_s`, `arrival_s` | như trên | người mua được bấm trong 5,1 s đầu (p50 3,0 s) / lượt bấm trải 70 s (p50 37,5 s) |
| Kịch bản k6 đúng logic (chạy khói trên máy, vài chục yêu cầu, database riêng trên bộ chung, chỉ api) | per_customer, web_read (5 lượt/s, 5 s), close_with_holds (4 người) | `bash setup2.sh` (scratchpad) | per_customer 1 bought / 4 over_limit / 95 lần 429; close: 4 bought, đóng 1, hoàn tất 2, `VERIFY-FLASH ĐẠT`, m5report thoát 0 |
| Cổng kiểm của task sau khi tách `run`, gom đoạn lặp, gộp main 48356ba9 (gocognit, jscpd, gofumpt, test kiến trúc, test gói đổi và phụ thuộc, `-race` cmd/crm, flashverify, loadkit) | `ci-task.sh` | `scripts/agent/ci-task.sh` → run 46129868965 (GitHub) | `static: success`, `test (1)–(6): success`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 48356ba9…)`. Hai lượt trước (46113105819, 46128981687) đỏ ở gocognit/jscpd rồi gofumpt (đã sửa) và `TestDashboard_ExplainNoSeqScan` (reporting, chập chờn, Task 526; lượt cuối xanh) |

## Test bị xóa hoặc tắt

(không có)



## Reviewer

Kết luận: ĐẠT (agent reviewer, 28/11 chiều, trên bản đã gộp main 48356ba9; `git diff --cached`). Phát hiện phải sửa: không có. Đối chiếu đủ Ảnh hưởng (5 chỗ gọi `seedProduct`, 4 chỗ `seedFlags.prepare`, không còn `check()`, `seedM5AdminUnits`, `validVerifyFlashArgs`), bảng bằng chứng (`TestVerify*`, `TestVerifyFlash`, 11 `TestM5*`, `TestM5ReadStats` kiểm đúng điều tiêu chí nói). Ghi chú không bắt buộc: thêm dòng Ảnh hưởng cho 13 `models.go` sinh lại và `loadtest/README.md` (đã thêm).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 219 không có test e2e; sáu bài thử tải đã chạy ở bể GitHub, xem Bằng chứng)

## Quyết định thay chủ shop

- QĐ-219-1: chạy sáu bài ở bể GitHub (runner 4 nhân, k6 cùng máy với CRM) thay "trên máy này" của Step 2 (chủ shop 26/11, 28/11: Mac không chạy tải; thử tải ở bể GitHub là số đo tạm). Phương án khác: chạy trên Mac (bị cấm), chờ server thử (ngoài máy). Ghi "đo lại trên server thử trước chạy thật" trong `thu-tai-m5.md`.
- QĐ-219-2: `flash_spike` dùng ramping-arrival-rate (lượt bấm/giây) với người ảo = lượt mod 20.000, không mở 20.000 VU thật: trên cùng máy 4 nhân, 20.000 VU làm k6 thành bên chạm trần trước. Tải phía CRM như kế hoạch (1.000 lượt bấm/giây, 20.000 SĐT). Đảo ngược: đổi executor trong `flash_spike.js`.
- QĐ-219-3: bài thử nâng `web_key` bằng `RATE_LIMITS_JSON` (giới hạn mặc định 1.200/phút chặn ngay tải flash sale); giữ mặc định giới hạn theo SĐT, IP và cổng nhận vào (đó là thiết kế đang đo). Số cho production: xem Việc phát hiện thêm.
- QĐ-219-4: `flash_sustained` cho mỗi người mua rồi hoàn tất ngay (kế hoạch đòi "15.000 đơn" và đo nhận việc của sự kiện `order.created`, chỉ sinh khi đơn rời nháp). Nhận việc đo trên mọi việc `event.*` (consumer của sự kiện), không tách riêng `order.created` (nhãn chỉ số là kind của việc).
- QĐ-219-5: `flash_abandon` đợt hai gửi đúng (số người bỏ đi) người mới: mọi người đợt hai phải mua được, một sold_out là "hết oan" — chấm được mà không cần k6 biết số suất còn lúc chạy.
- QĐ-219-6: địa chỉ giao của đơn thử dùng tỉnh, xã thử T01/T01001 do `loadkit m5key` chèn bằng `seedAdminUnits` của `m2.go` (`customer.EnsureFixtureAdminUnits`; database thử không có danh mục hành chính thật; như `seed-web-fixture`). Dùng chung hàm với `m2seed` (bản riêng `seedM5AdminUnits` cùng mã T01 mà khác tên: chạy m2seed rồi m5key trên một database sẽ báo trùng; jscpd).
- QĐ-219-8 (giới hạn `web_key` cho production, chủ shop giao Claude chốt theo số đo): production đặt `RATE_LIMITS_JSON='{"web_key": {"max": 120000, "window_s": 60, "fallback_max": 60000}}'` (2.000 yêu cầu/giây mỗi khóa; đỉnh đo ≈ 1.700/giây gồm lượt bấm, lượt đọc, lượt gửi lại). Không đổi mặc định 1.200/phút trong `cache.DefaultLimits` vì đó là số của spec QĐ23 (và `TestRateLimit_ServiceKeyPerMinute`, threat model dòng D dựa vào); đặt qua biến môi trường đảo ngược được không cần build. Phương án khác: tách giới hạn riêng cho route flash (cần đổi spec, mã ngoài task này); nâng mặc định trong mã (trái QĐ23). Ghi ở `thu-tai-m5.md` mục "Giới hạn web_key cho production"; đo lại trên server thử (Task 221).

- QĐ-219-7: gộp `main` lần thứ hai ngay trước review (lần một lúc bắt đầu chạy thử tải). Lý do: trong lúc chạy thử tải, Task 88 (thử tải M2) gộp vào `main` sửa đúng các tệp của task này (`tools/loadkit/main.go`, `fb.go`, job `loadtest`, `remote-test.sh`, `loadtest/README.md`) và lấy số migration 0542; gộp lại để reviewer duyệt đúng bản đã gộp và điều phối viên không phải giải xung đột. Cách làm: fast-forward lên `main`, áp lại bản vá của task (`git apply --3way`), giải xung đột giữ đủ hai bên (bài m2 và m5 cùng job, lệnh `seed-staging` và `verify-flash`, 56 view). Migration đổi số 0542 → 0543 (số trống trên main và mọi nhánh task-*). Chưa commit nên không có lịch sử cần sửa.

## Việc phát hiện thêm

- QĐ23: giới hạn khóa dịch vụ `web_key` 1.200/phút (20/s) mâu thuẫn với tải flash sale 1.000 lượt bấm/giây qua một khóa của website: website thật sẽ nhận 429 ngay đợt đầu. Bài thử nâng bằng RATE_LIMITS_JSON; số production đã chốt ở QĐ-219-8 (biến môi trường). Còn lại cho lần sửa spec sau: ghi số này vào QĐ23 / RUNBOOK khi dựng server thử (Task 221).

- Thử tải: ở đỉnh `flash_spike` 83/64.986 lượt bấm nhận 503 cả 10 lần gửi lại (từ lần 2 bỏ qua cờ hết suất theo QĐ21 nên vẫn vào cổng nhận vào). Website cần báo "thử lại" cho ca này; xem lại khi chốt số của bộ điều tiết (Điểm dừng D, Task 220).

## Khác kế hoạch

- (a) đếm theo số lượng dòng (không theo số đơn); đơn hủy sau khi đóng đợt được giữ đơn vị sold (05 mục 7.4). (c) với đợt đã đóng chỉ đòi claims ≥ held + sold (claims không trừ lại khi hủy sau đóng). (e) gồm claims và on_hand/held/available của số dư của món.
- Step 2 chạy ở bể GitHub, không trên máy này (QĐ-219-1). Chưa đo: CPU, bộ nhớ theo từng bài (chỉ lớn nhất cả lượt), p95 phía máy chủ của lượt đọc web; ghi ở `thu-tai-m5.md`.
- Thêm `loadkit m5stats`/`m5report` (chấm tự động như bộ M0, M4) thay cho ghi tay; job `loadtest` của `test-remote.yml` nhận `m5:…` (hạn 60 phút như job cũ (trần của test kiến trúc)).

## Tiến độ

- Phiên 28/11 trưa: mọi mã, k6, `m5_run.sh`, job CI, `thu-tai-m5.md`; sáu bài thử tải ĐẠT (run 46106941415 + 46109908569); gộp main lần hai (QĐ-219-7), migration 0543. ci-task lần 2 (run 46113105819) đỏ ở gocognit `run`, jscpd 3 đoạn, `TestDashboard_ExplainNoSeqScan`.
- Phiên 28/11 chiều: tách `run` (`isOneOffCommand`), gom đoạn lặp loadkit (`seedFlags.prepare`, `openOwnerFrom`, `seedAdminUnits` chung), chốt giới hạn `web_key` production (QĐ-219-8). Gộp main lần ba (48356ba9, Task 138; chỉ `docs/views.md` trùng tệp, áp sạch; `make gen` không chênh). `TestDashboard_ExplainNoSeqScan` (reporting): chập chờn, Task 526 đang làm ổn định — không sửa ở đây.
