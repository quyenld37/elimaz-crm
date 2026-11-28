# Task 139 — Đường trọn M3, bài thử hỏng, thử tải phần mới, hai người bán (M3-48)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md`, mục "Task 139 (M3-48)". Loại việc (quy trình mục 3): A (test đầu cuối, thử tải; sửa lỗi tìm thấy theo loại B)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. Hành vi đổi: `config.Load(RoleAPI, …)` nay đọc `FAKECARRIER_URL`, `FAKECARRIER_WEBHOOK_SECRET` (như ingest, worker; production vẫn cấm) — người dùng: `cmd/crm` (wireM3 → `carrier.FakeOn`). `cash/internal/store.ListOverdue`, `ListDiscrepancies`: cùng tham số, cùng kiểu dòng (sinh lại từ `queries/cash/lists.sql`), người gọi duy nhất `internal/modules/cash/internal/lists` qua `Service.ListOverdue`, `Service.ListDiscrepancies` (httpapi `handlers_cod_*`). `tools/fakecarrier/server.Options` thêm `TrackDelay` (0 = như cũ), trang quản trị `advance` nhận mọi trạng thái `fake.Raw*`; `tools/fakecarrier` đọc `FAKECARRIER_TRACK_DELAY`. `tools/loadkit` thêm lệnh `carrierseed` (`carrier.go`; dữ liệu qua public API module: `customer.EnsureFixtureAdminUnits`, `fulfillment.EnsureCarrierAccount`, `EnsureAccountRoute`, `EnsureSeedLocations`, `fulfillment.Get` ở `carrier_seed.go`), hàm `openLoadShop` dùng chung cho `fbseed` và `carrierseed` (đổi cấu trúc `cmdFBSeed`, không đổi hành vi). Ký hiệu test dùng chung mới: `testkit.SeqScanTables`, `testkit.StoreQuery` (`internal/testkit/explain.go`, `planNode` thêm `Relation Name`; `storeSQLIn` của `fulfillment/lists_stuck_test.go` xóa, dùng `StoreQuery`), `shiptest.Shop.OrderTo` (`Shop.Order` gọi nó với địa chỉ cũ, không đổi hành vi; người dùng `Order` không phải sửa). E2E: `orderRun.race` thay khối hai đơn song song của `TestMotDonDiTron_HaiDonSongSongTon1` (cùng kiểm định), `deliveredHolding` dùng ở `deliveredRun`; `newStack` tách `startAPI`. Bộ thử: `loadtest/fb_lib.sh` (`role_vars` nối hãng giả khi có `LT_FAKECARRIER_URL`), job `loadtest` của `.github/workflows/test-remote.yml` và `scripts/ci/remote-test.sh` nhận bài `carrier`.
- Gói phụ thuộc ngược (go list): `internal/platform/config` ← gần như mọi gói (chỉ đổi nhánh RoleAPI); `cash/internal/store` ← `internal/modules/cash/...`; `tools/fakecarrier/server` ← `tools/fakecarrier`, test của `internal/httpapi`, `internal/adapters/carrier/carriertest`, `internal/modules/fulfillment`, `cmd/crm`. `tests/e2e`, `tools/loadkit` không gói nào nhập.
- Operation OpenAPI bị chạm, oasdiff: không có (getCodDiscrepancies/listCodOverdue giữ hợp đồng).
- Chỗ dùng trên màn hình: không đổi (màn "Lệch đối soát", "Quá hạn" đọc cùng API).
- Câu sqlc, bảng, pha migration: migration mới `0332_cash_open_holdings.sql` (pha mở rộng: bảng `cash.cod_open_holdings` có RLS, trigger AFTER INSERT trên `cash.cod_ledger_entries`, điền số dư cũ, index một phần `cod_statement_lines_discrepancy`); câu `ListOverdue`, `ListDiscrepancies` đọc bảng mới; `make gen` thêm kiểu `CashCodOpenHolding` vào models.go của mọi khối sqlc (mã sinh). ADR-46 nhóm 9 thêm tên bảng.
- Test sẽ chạy: máy (test lẻ): `TestAdvanceEveryRawStatus`, `TestTrackDelay`, `TestLoadConfig` (fakecarrier), `TestM3Contract_PendingEmptyAtClose`, `TestOpenHoldings_FollowLedger`, `TestM3ListPlans_Large`, `TestLoadAPIReadsFakeCarrier`, `TestCarrierSeedAccount` (loadkit), `TestStuck_PlanUsesIndexes` (fulfillment, sau khi đổi sang `testkit.StoreQuery`), các test chốt bảng kê, quá hạn của `cash/internal/statement`; ci-task ở bể cho gói đổi (cash là vùng rủi ro cao: cả bộ). Bài thử tải `carrier` ở bể GitHub. E2E mới viết sẵn, không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục testkit (`ExplainAsApp`, `RequireNoSeqScan`, `OpenStaff`, `MakeDueSoon`, `filestest.MemFileStore`), `shiptest.Shop` (tài khoản fake-1/fake-2 hai người bán, `Order`, `Shipment`, `CODBalances`), e2e `stack`/`orderRun` (ba vai trò, hãng giả tiến trình riêng, `switchProxy`, `waitFor`), `loadtest/fb_run.sh`+`fb_lib.sh` (chạy vai trò như tiến trình), `tools/loadkit` (access, seed), job `loadtest` của `test-remote.yml`.
- Dùng lại / viết mới vì: dữ liệu lớn của test kế hoạch dùng lại `reportseed.Bulk` (320.000 đơn, ~200.000 vận đơn), chỉ thêm sổ COD, bảng kê, việc gọi lại bằng SQL; hàm đọc kế hoạch theo tên bảng viết mới thành `testkit.SeqScanTables` (dùng chung) vì `testkit.ExplainAsApp` chỉ trả loại nút; `seqScans` riêng của `internal/queries/orders_dynsql_test.go` làm cùng việc, gộp ở task sau (Việc phát hiện thêm). Dùng lại `stack`/`orderRun` và thêm tùy chọn `m3` nối đủ hai module M3 như `cmd/crm/wire_m3.go` (fulfillment.Module, cash.Module, kho tệp trong bộ nhớ) — viết mới vì stack M1 chỉ nối lệnh tạo/tra và consumer sổ COD. Bài thử tải viết `loadtest/carrier_run.sh` dùng lại `fb_lib.sh` (start_role, wait_healthy) thay vì sao chép; dựng dữ liệu bằng lệnh `loadkit carrierseed` mới (cùng mẫu `seed`), chấm bằng SQL trên database thử.

## Soát thiết kế

Task chỉ thêm test, công cụ thử; mã sản phẩm không đổi (trừ lỗi tìm thấy, ghi ở "Khác kế hoạch").
- 5.1–5.3, 5.7: test đọc bằng chủ bảng trong `tenancy.WithTenant`; câu EXPLAIN chạy bằng `crm_app` trong ngữ cảnh tenant.
- 5.4–5.6: kiểm lại trên đường trọn (một transaction cho đơn + giữ hàng; tín hiệu hãng → lệnh tra → áp; gọi hãng qua lệnh `in_doubt`).
- 5.8–5.12: không áp dụng (không thêm module, route, thư viện; k6 đã có ở ADR-42).
- 6.1, 6.2: `m3_chaos` (d) hai kế toán chốt cùng lúc; (b) 50 webhook khi database ngừng → mỗi vận đơn áp đúng một lần.
- 6.3: thử tải đòi `pg_stat_database.deadlocks` không tăng.
- 6.4–6.6, 6.9: không đổi; thử tải đòi `tx_retry_exhausted` = 0, không việc `dead`.
- 6.8: `m3_chaos` (a) worker chết sau khi hãng nhận lệnh tạo → `in_doubt`, tra → `done`, đúng một kiện ở hãng; (c) worker chết giữa lúc ghi dòng bảng kê → chạy lại đủ dòng, không trùng.
- 6.10: đường trọn kiểm tổng sổ COD = 0 sau mỗi chặng.
- 6.11: không thêm quy luật mới (rapid của vùng đã có).
- 6.12: dữ liệu thử là tên, SĐT giả; không in SĐT, thẻ ra log test.
- 6.1, 6.3 cho bảng mới `cash.cod_open_holdings`: dòng của một vận đơn chỉ bị khóa (INSERT … ON CONFLICT DO UPDATE trong trigger, nên hai transaction cùng chèn dòng đầu không lỗi unique) ngay sau khi chèn dòng sổ của cùng vận đơn; mọi đường ghi sổ COD giữ khóa tư vấn `cash.cod:<shipment>` trước (ADR-46 mục cash), chốt bảng kê lấy khóa các vận đơn của lô theo mã tăng dần, nên không thêm thứ tự khóa mới. Hai lần ghi cùng vận đơn nối tiếp trên dòng đó; số dư về 0 thì xóa dòng ngay trong trigger.
- 6.10 cho bảng mới: sổ vẫn là nguồn sự thật; bảng chỉ do trigger ghi trong cùng transaction (crm_app chỉ SELECT; hàm SECURITY DEFINER, RLS theo app.tenant_id); `TestOpenHoldings_FollowLedger` so với `cash.shipment_cod_v` qua dự kiến, thu, trả một phần, đảo, trả đủ; bộ rapid của chốt bảng kê (`TestProperty_MatchConfirm`) chạy trên câu mới.
- 6.13: `m3_two_sellers` — tài khoản hãng của người bán B không dùng được cho đơn của A (409 `carrier_account_unavailable`), API (người bán mặc định A) không đọc được vận đơn, việc gọi lại của B.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `m3_pending.txt` rỗng khi đóng mốc | `TestM3Contract_PendingEmptyAtClose`, `TestM3Contract_PendingList` (tests/arch) | `go test -count=1 -run '^(TestM3Contract_PendingEmptyAtClose\|TestM3Contract_PendingList)$' ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 0.668s`; không còn route M3 trả 501: `TestM3Routes_PendingList` ở ci-task |
| Câu danh sách M3 không Seq Scan trên bảng lớn (320.000 đơn, ~200.000 vận đơn, >1.000.000 dòng sổ COD, 100 bảng kê × 1.000 dòng, 100.000 việc gọi lại) | `TestM3ListPlans_Large` (11 ca: lệch, quá hạn, bảng kê, dòng bảng kê, nghĩa vụ hoàn, gọi lại) | `go test -count=1 -v -timeout 15m -run '^(TestM3ListPlans_Large\|TestOpenHoldings_FollowLedger)$' ./internal/modules/cash/` | trước khi sửa: `listDiscrepancies: Seq Scan trên bảng lớn [cod_statement_lines orders shipments cod_transfers]`, `listOverdue: … [shipments]`; sau khi sửa `--- PASS: TestM3ListPlans_Large (150.03s)`, thời gian câu: listDiscrepancies 793,9 ms (lần đầu, đệm lạnh) / 189,8 ms, listOverdue 3,3 / 0,4 ms, còn lại ≤ 1,3 ms |
| Số dư hãng đang giữ theo vận đơn luôn bằng sổ, crm_app không ghi tay được | `TestOpenHoldings_FollowLedger` | (cùng lệnh trên) | `--- PASS: TestOpenHoldings_FollowLedger (1.15s)`; trước migration: `relation "cash.cod_open_holdings" does not exist` |
| Danh sách lệch, quá hạn giữ đúng nghĩa sau khi viết lại câu | `TestListOverdue`, `TestMatch_Classifies`, `TestConfirm_TI02`, `TestConfirm_TI05Shortfall`, `TestConfirm_Duplicate`, `TestConfirm_ManyBatches`, `TestConfirm_ParallelStatementsNoDeadlock`, `TestProperty_MatchConfirm` (rapid) | `go test -count=1 -run '^(TestMain\|TestProperty_MatchConfirm\|TestConfirm_TI02\|…)$' ./internal/modules/cash/internal/statement/` | `ok … statement 22.541s`; `-race` của 4 test: `ok … 4.609s` |
| api đọc hãng giả (Gửi hãng qua API với hãng giả) | `TestLoadAPIReadsFakeCarrier` | `go test -count=1 ./internal/platform/config/` | trước khi sửa FAIL; sau `ok … config 0.476s` |
| Trang quản trị hãng giả đổi mọi trạng thái; tra chậm theo cấu hình | `TestAdvanceEveryRawStatus`, `TestTrackDelay`, `TestAdvanceAndForge`, `TestLoadConfig` | `go test -count=1 -run '^(TestAdvanceEveryRawStatus\|TestTrackDelay\|TestAdvanceAndForge)$' ./tools/fakecarrier/server/`; `-run '^TestLoadConfig$' ./tools/fakecarrier/` | `ok … server 0.672s`, `ok … fakecarrier 0.434s` (trước: `unknown field TrackDelay`, `c.trackDelay undefined`) |
| Dữ liệu bài thử tải (tài khoản, tuyến, mã địa giới; chờ vận đơn) | `TestCarrierSeedAccount` | `go test -count=1 -run '^TestCarrierSeedAccount$' ./tools/loadkit/` | `ok … loadkit 2.196s`; `TestStuck_PlanUsesIndexes` sau khi đổi sang `testkit.StoreQuery`: `ok … fulfillment 9.516s` |
| Thử tải webhook hãng 10× (20 tin/giây, 15 phút): nhận p95 < 300 ms, webhook → đổi trạng thái p95 < 5 s, không webhook khác 200, deadlock không tăng, `tx_retry_exhausted` = 0 | `loadtest/carrier_run.sh` + `loadtest/k6/carrier_webhooks.js` | `CRM_CI_STATIC=false CRM_CI_LOADTEST=carrier CRM_CI_LABEL=t139-lt scripts/ci/remote-test.sh <tree> 83c88921 <tệp rỗng>` | `REMOTE-TEST XANH` run 46107770766: nhận p95 1,7 ms (18.000 tin 200), webhook → đổi trạng thái p95 212,2 ms (p99 216,5 ms), gửi 18.000 áp 18.000, deadlocks 0 → 0, tx_retry_exhausted 0, việc dead 0 → 0 (số đo tạm, bể GitHub 4 lõi; `docs/do-luong/thu-tai-m3.md`); chạy khói lại với mã cuối (`CRM_CI_LOADTEST=carrier:1m`): run 46112220852 `REMOTE-TEST XANH`, mọi tiêu chí ĐẠT |
| Cổng kiểm của task (cổng tĩnh, test gói đổi và phụ thuộc; đổi testkit nên chạy ALL) | ci-task | ci-task (bể GitHub) | lượt cuối run 46112178984: `static: success`, test 1–4, 6 `success`; phần 5 chỉ đỏ `TestDashboard_ExplainNoSeqScan` (reporting, không chạm phần nào của task, kế hoạch "chi phí sát nhau" đã ghi ở Task 229; xanh ở run 46110239956; chạy riêng trên máy `ok … reporting 36.580s`) |
| Đường trọn, hai người bán, bốn ca hỏng trên ba vai trò thật | `TestM3FullFlow*`, `TestM3TwoSellers`, `TestM3Chaos_*` (tests/e2e) | `go vet ./tests/e2e/` | biên dịch, vet qua; **chưa chạy** (mục "E2E viết sẵn, chưa chạy") |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `m3_explain` không đặt ở `tests/e2e` mà ở gói `internal/modules/cash` (`TestM3ListPlans_Large`, `m3_plans_test.go`): chạy được trong ci-task (e2e không chạy trong lúc làm task), nên tìm và sửa được lỗi ngay. Các câu danh sách M3 khác đã có test kế hoạch ở gói của chúng: đơn có bộ lọc vận đơn (`TestOrderListPlans`, 200.000 đơn), đơn kẹt và cần xem (`TestStuck_PlanUsesIndexes`), giữ hàng quá hạn (`TestAgedReservations_Plan`).
- Lỗi tìm thấy và đã sửa trong task (theo loại B, test đỏ trước): (1) "Lệch đối soát", "Quá hạn chưa có tiền" quét cả `shipments`, `cod_transfers`, `cod_statement_lines`, `orders` trên dữ liệu lớn → bảng số dư `cash.cod_open_holdings` (migration 0332) và viết lại hai câu; (2) vai trò api không đọc `FAKECARRIER_URL` nên "Gửi hãng" qua API với hãng giả luôn 409 `carrier_account_unavailable` (thấy khi chạy khói bài thử tải) → api đọc như ingest, worker; (3) trang quản trị hãng giả chỉ đổi được created/picked_up/delivered nên e2e `TestM3ReverseDelivered_*` (gọi `returning`) không thể qua → nhận mọi trạng thái `fake.Raw*`; (4) e2e cũ tạo đơn không có mã tỉnh, phường trong khi hãng giả khai `Locations` (address_unmapped) → stack e2e nạp sẵn mã đã khớp, `orderReq` và `scripts/demo/mot-don-di-tron.sh` gửi mã tỉnh, phường.
- Bài thử tải cần thêm: `loadtest/carrier_run.sh`, lệnh `loadkit carrierseed`, bài `carrier` của job `loadtest` (`test-remote.yml`, `remote-test.sh`), `FAKECARRIER_TRACK_DELAY` của hãng giả (kế hoạch chỉ nêu `carrier_webhooks.js`).
- Không chạy lại `heavy.js` của M0 (xem Quyết định thay chủ shop QĐ-139-4).

## Quyết định thay chủ shop

- QĐ-139-1 (sửa Seq Scan của hai danh sách tiền): chọn bảng số dư "hãng đang giữ" theo vận đơn do trigger của dòng sổ ghi trong cùng transaction (sổ vẫn là nguồn sự thật, ADR-24), thay vì chỉ thêm index (vẫn phải cộng sổ của mọi vận đơn đã giao mỗi lần mở trang: chậm dần theo số đơn) hay ghi số dư bằng mã Go ở `PostWithID` (mọi đường ghi phải nhớ gọi). Dễ đảo ngược: bỏ trigger, câu đọc lại view cũ. Phương án khác: bảng tóm tắt làm mới theo lịch (sai lệch tới lần làm mới — không hợp cho tiền).
- QĐ-139-2 (hai người bán trên đường trọn): API chạy với người bán mặc định, nên đường của người bán B đi bằng public API module (`order.Writer.CreateManual`, `shiptest.Shop.Shipment`) cùng hãng giả, webhook, worker thật; API của A đòi không thấy gì của B. Phương án khác: thêm chọn người bán vào API — ngoài phạm vi M3.
- QĐ-139-3 (thử hỏng (a) "giết worker sau khi hãng nhận lệnh tạo"): dùng chế độ `timeout_after_create` của hãng giả (hãng tạo kiện rồi treo tới quá hạn 25 s của lệnh → in_doubt), rồi tra lại bằng `commands.ResolveInDoubt`; (c) "giết worker giữa lúc ghi lô dòng" bằng proxy cắt database của api, worker sau khi có lô đã commit. Cùng hiệu quả với lệnh đang chạy mà không cần tiến trình worker riêng; phần "worker chết thật" đã có `TestWorkerCrashDoesNotSendTwice` (e2e M0) và `SetAfterBatch` của gói statement.
- QĐ-139-4 (`heavy.js` của M0): không chạy lại trong task này — bài đó dựng image Docker và compose riêng, không có ở bể từ xa; đường webhook selftest không đổi ở M3 (task chỉ thêm trigger trên sổ COD, sửa hai câu đọc). Ghi "chờ server thử của hệ mới" ở `docs/do-luong/thu-tai-m3.md`.
- QĐ-139-5 (số migration): 0332 — số kế tiếp của dải M3 (0331 là số lớn nhất của M3), trống ở main và mọi nhánh task-*; goose chạy không theo thứ tự (`WithAllowOutofOrder`).
- QĐ-139-6 (số đo thử tải): số đo ở bể GitHub (runner 4 nhân) là số đo tạm, ghi rõ "đo lại trên server thử trước chạy thật".

## E2E viết sẵn, chưa chạy

- `tests/e2e/m3_full_flow_test.go`: `TestM3FullFlow`, `TestM3FullFlow_CancelAwaitingPickup`, `TestM3FullFlow_CarrierReturn`, `TestM3FullFlow_CustomerReturnRefund`.
- `tests/e2e/m3_two_sellers_test.go`: `TestM3TwoSellers`.
- `tests/e2e/m3_chaos_test.go`: `TestM3Chaos_CreateInDoubt`, `TestM3Chaos_WebhooksDuringDatabaseOutage`, `TestM3Chaos_StatementBatchRestart`, `TestM3Chaos_TwoAccountantsConfirm`.
- Stack đủ M3: `tests/e2e/m3_stack_test.go` (`stackOptions.m3`, `appURL`). Đã sửa sẵn cho e2e cũ: mã địa giới của hãng giả (`TestMotDonDiTron*`, `TestM3ReverseDelivered_*`).
- Biên dịch, `go vet` qua; chạy ở lượt gộp mốc (`CRM_CI_E2E=1 scripts/ci/merge-test.sh`).

## Việc phát hiện thêm

- Các ca đủ của M3 ở test cấp gói (đối chiếu "Bảng ca M3 → task" của kế hoạch, grep mã ca trong `*_test.go`): DH03, DH15, KH09, VC01–VC18, TI01–TI10, TD02–TD10 đều có test ở gói của task gốc; không ca nào thiếu.
- E2E của M3 viết trước (Task 118) chưa từng chạy được (trang quản trị hãng giả, mã địa giới): lượt gộp mốc M3 nên chạy `CRM_CI_E2E=1` để xác nhận cả bộ e2e.
- `scripts/demo/mot-don-di-tron.sh` nay gửi mã tỉnh, phường 79/26734; `scripts/demo/mot-don-compose.sh` (`make demo-mot-don`) và `seed-dev` chưa nạp hai mã đó vào danh mục hành chính và `carrier_locations` của hãng giả, nên trên compose dev kịch bản dừng ở bước lên đơn (trước đó đã dừng ở bước gửi hãng vì address_unmapped). Cần cho `seed-dev` hay `mot-don-compose.sh` nạp sẵn hai mã (dùng `customer.EnsureFixtureAdminUnits`, `fulfillment.EnsureSeedLocations` như `loadkit carrierseed`).
- `seqScans` của `internal/queries/orders_dynsql_test.go` trùng việc với `testkit.SeqScanTables` mới: gộp ở task sau.
- Bảng `cash.cod_open_holdings` dùng khóa tự nhiên `(tenant_id, shipment_id)` (bảng số dư theo vận đơn, như `inventory` số dư theo khóa), không có cột `id`.
- "Lệch đối soát" (`ListDiscrepancies`) trên dữ liệu lớn không còn quét cả bảng nhưng vẫn 190–800 ms: nhánh "phần còn mở" duyệt mọi vận đơn còn số dư hãng giữ (khoảng 10% vận đơn đã giao ở dữ liệu thử) để tìm vận đơn đã có lần trả; ngưỡng API đọc p95 < 200 ms (04 mục 1) sát. Có thể thêm cờ "đã có lần trả chưa đảo" vào `cash.cod_open_holdings` (trigger ghi) ở task sau nếu đo trên server thử thấy chậm.
- `TestDashboard_ExplainNoSeqScan` (reporting, không chạm phần nào của task) đỏ một lần ở ci-task (run 46111124412: kế hoạch có Seq Scan) và xanh ở lượt trước cùng mã (run 46110239956): kế hoạch phụ thuộc thống kê lấy mẫu của ANALYZE, test chập chờn; nên xem ở task của reporting.
- `test-remote.yml` job `loadtest` nhánh M4 (`fb_run.sh`) dùng `set -o pipefail` dưới `bash -e`: bài đỏ thì bước dừng trước khi ghi bảng chấm vào tóm tắt (nhánh `carrier` đã tránh bằng `set +e`); nên sửa cùng cách khi chạm tới.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 28/11/2026, trên phần đã stage sau khi gộp main f8b007e1).

- Phát hiện phải sửa: không có. Đã kiểm: migration 0332 (số dư luôn khớp sổ vì sổ chỉ ghi thêm, một dòng carrier_holding mỗi bút toán; SECURITY DEFINER `search_path = ''`, REVOKE PUBLIC; RLS ép theo `app.tenant_id`; điền số dư cũ không mất, không trùng nhờ khóa của CREATE TRIGGER; mọi đường ghi sổ lấy `LockShipmentCOD` trước, không đường deadlock mới); `ListDiscrepancies` tương đương câu cũ, phân trang từng nhánh rồi hợp vẫn đúng top-N; `ListOverdue` cùng điều kiện, thứ tự, con trỏ; `config.Load` cho api đúng, production vẫn chặn.
- Ghi chú không bắt buộc (đã xử lý trong tệp task): kịch bản demo compose chưa nạp mã 79/26734 (Việc phát hiện thêm); mục Tái dùng sửa cho khớp `testkit.SeqScanTables`; khóa chính tự nhiên của bảng mới (Việc phát hiện thêm); mục Ảnh hưởng thêm `TestCarrierSeedAccount`, `TestStuck_PlanUsesIndexes`.
