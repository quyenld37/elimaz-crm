# Task 33 — Một đơn đi trọn (e2e), Điểm dừng M1-A

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 33 (M1-33), Điểm dừng M1-A). Loại
việc (quy trình mục 3): A. Task test đầu cuối: chỉ nối dây, không thêm hành vi; thiếu hành vi thì ghi việc loại B.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu mã sản phẩm nào đổi (dự kiến chỉ thêm test, script,
  tài liệu; nếu test lộ chỗ thiếu nối dây thì ghi bổ sung ở đây). Gói test mới `internal/testkit/shiptest` (`Shop`,
  `NewShop`, `Shop.Order`, `Shop.Shipment`, `CODBalances`, hằng `CarrierKey`, `AccountRef`) gom phần dựng "cửa hàng + tài khoản hãng
  giả + khách + đơn + vận đơn" đang lặp ở `internal/modules/fulfillment/env_test.go` và `internal/modules/cash/env_test.go`
  (việc treo của Task 32); hai tệp đó sửa để dùng gói mới.
- Gói phụ thuộc ngược (go list): `shiptest` mới, chỉ gói test nhập (`fulfillment_test`, `cash_test`, `tests/e2e`).
  `shiptest` nhập `invtest`, `customer`, `order`, `fulfillment`, `events`, `queue`, `testkit` — nằm ngoài
  `internal/modules` nên luật ranh giới module (tests/arch) không áp; không gói mã sản phẩm nào nhập nó.
- Operation OpenAPI bị chạm, oasdiff: không đổi. Test và script gọi `createProduct`, `createStockDocument`,
  `replaceStockDocumentLines`, `postStockDocument`, `createOrder`, `getOrder`, `createShipment`, `getShipment`,
  `listProducts` (cột `available` theo `warehouseId`).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không câu, không bảng, không migration mới. Test đọc (chủ bảng) `inventory.stock_balances`,
  `inventory.stock_ledger`, `inventory.stock_reservations`, `cash.cod_ledger_entries`, `cash.cod_balances_v`.
- Test sẽ chạy: `go test -race -run 'TestMotDon' ./tests/e2e/`; vì sửa env test: `./internal/modules/fulfillment/...`,
  `./internal/modules/cash/...`; `./tests/arch/...`; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit không có trong
  danh mục: chỉ gói platform); `internal/testkit` (`StartPostgres`, `AccessKeyServer`, `Do`, `Staff`), `tests/e2e`
  (`newStack`, `buildBinary`, `freePort`, `startRole`, `waitFor`, `missingInTrace`, `openPool`), `invtest.Shop`,
  `catalogtest`; tìm "EnsureCarrierAccount", "CreateManual", "customer.Create" trong test: lặp ở `fulfillment/env_test.go`,
  `cash/env_test.go`, `httpapi/handlers_shipment_test.go`.
- Dùng lại / viết mới vì: dùng lại `newStack` của e2e (thêm tùy chọn nối hãng giả, sổ sự kiện có consumer `cash`, nguồn
  `fakecarrier` ở ingest và worker, hai lệnh vận chuyển — đúng cách `cmd/crm` nối), `missingInTrace` cho dấu vết, chương
  trình hãng giả thật `tools/fakecarrier` (dựng tệp chạy, chạy tiến trình riêng), `invtest.Shop` qua `shiptest`. Viết
  mới `shiptest` (gom đoạn lặp, việc treo của Task 32); để ở `internal/testkit/shiptest` thay vì trong module fulfillment
  vì nó nhập `customer` và `invtest` — module fulfillment không được gọi các module đó (04 mục 3). Không gom
  `httpapi/handlers_shipment_test.go` (gói test nội bộ, chỉ dùng tài khoản hãng, ngoài phạm vi việc treo).
- jscpd (`make ci` lần đầu): 1 đoạn trùng mới — đọc số dư COD qua `cash.cod_balances_v` ở `cash/env_test.go` và
  `tests/e2e/mot_don_di_tron_test.go` → gom thành `shiptest.CODBalances`, cả hai dùng; chạy lại `Found 0 clones`.

## Soát thiết kế

- 5.1–5.3 Chủ dữ liệu, ghi bảng, đọc view: không áp dụng cho mã mới (không bảng, không ghi). Test đọc bảng bằng chủ bảng
  chỉ để kiểm tra, như các test e2e có sẵn.
- 5.4 Nguyên tử: kiểm qua API — tạo đơn và giữ hàng cùng một transaction (2 đơn song song, tồn 1 → một 201, một 409).
- 5.5 Không nguyên tử: kiểm đường sau commit — webhook → lệnh `refresh_status` → sự kiện `shipment.status_changed` →
  consumer `cash`; test chờ có hạn 5 s mỗi bước.
- 5.6 Gọi ra ngoài: hãng giả qua lệnh `fulfillment.create_shipment`/`refresh_status` (có `in_doubt`), không gọi thẳng.
- 5.7 Ngữ cảnh shop: dùng tenant, tuyến `carrier_account:fake-1` như `seed-dev`.
- 5.8 Không HTTP trong module: không đổi mã module.
- 5.9 Đường ghi: mỗi lệnh ghi trong test và script mang `Idempotency-Key` mới.
- 5.10 PostgreSQL là nguồn sự thật: test so số dư với tổng sổ trên database.
- 5.11 Không thêm thư viện. Script dùng `bash`, `curl`, `jq` (công cụ máy, không phải thư viện Go).
- 5.12 Theo mẫu: test e2e theo `tests/e2e` có sẵn.
- 6.1 Đồng thời: ca 2 đơn song song tồn 1 (tiêu chí "Xong" số 4).
- 6.2 Gửi lại: khóa chống trùng mỗi lệnh; không thêm ca gửi lại (đã có test riêng ở từng task).
- 6.3–6.5 Khóa, lỗi tạm, cách ly: không đổi; test chạy trên mã thật.
- 6.6, 6.8, 6.9 Việc nền lỗi, chết giữa chừng, treo: không áp dụng (đường thành công); test đòi 0 việc `dead`.
- 6.7 429: không áp dụng (hãng giả không trả 429).
- 6.10 Tiền: kiểm tổng sổ COD = 0, `grand_total 530000` đồng `int64`.
- 6.11 Quy luật: kiểm "tổng sổ kho từng ngăn = số dư" và "tổng sổ COD = 0" ở cuối đường.
- 6.12 Dữ liệu cá nhân: script không in SĐT, địa chỉ (chỉ in mã, trạng thái, số); không ghi bí mật Access vào tệp.
- 6.13 Quyền: test dùng chủ shop (`identity.SeedOwner`, mọi quyền), script dùng thẻ của người chạy.

## Bằng chứng

Lệnh chung (bộ chung crm-shared, database thử riêng mỗi test): `CRM_PG_PORT=5600
CRM_TEST_DATABASE_URL=postgres://crm_app:…@127.0.0.1:5600/crm go test -race -count=1 -v -run TestMotDon ./tests/e2e/`.
Lần chạy đầu (trước khi có script): `TestMotDonDiTron_Script` FAIL `exit status 127 … mot-don-di-tron.sh: No such file
or directory`; hai ca còn lại đỏ vì test đếm `stock_reservations`, `ord.orders` bằng chủ bảng ngoài ngữ cảnh tenant (RLS →
0 dòng) — lỗi của test, sửa bằng `orderRun.count` trong `tenancy.WithTenant`. Đường đi của đơn qua ngay từ lần đầu: nối dây
của Task 31, 32 đã đủ, không phải sửa mã sản phẩm.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mẫu 1 biến thể 250000 → phiếu nhập 5 → ghi sổ (`posted`, on_hand 5) → đơn 2 cái ship 30000: `confirmed`, `grandTotal 530000`, `codAmount 530000`, held 2, available 3 (cả cột `available` của `listProducts`) | `TestMotDonDiTron` | lệnh chung | PASS |
| Tạo vận đơn (202) → `awaiting_pickup`, có mã hãng, trong 5 s | như trên | như trên | PASS, 214 ms |
| Hãng giả `advance picked_up` (webhook có chữ ký về ingest, 200) → trong 5 s: vận đơn `shipping`, tồn (5, held 0, in_transit 2, available 3), COD `to_collect 530000` | như trên | như trên | PASS, 257 ms |
| `advance delivered` → trong 5 s: đơn `delivered`, tồn (on_hand 3, in_transit 0, available 3), COD `carrier_holding 530000`, `to_collect 0`; giữ hàng 1 dòng `consumed`, 0 dòng khác; sổ COD có dòng, tổng 0; tổng sổ kho từng ngăn = số dư; 0 việc `dead` | như trên | như trên | PASS, 221 ms |
| Span nhận webhook `/hooks/fakecarrier` (ingest), `job fulfillment.refresh_status`, `job event.cash` cùng TraceID | như trên (`traceLinksWebhookToCash`) | như trên | PASS (`dấu vết 403d57d3… nối webhook → refresh_status → cash`) |
| Tồn 1, hai đơn song song khóa khác nhau → một 201, một 409 `out_of_stock`; 1 đơn; tồn (1, held 1) | `TestMotDonDiTron_HaiDonSongSongTon1` | như trên | PASS |
| `scripts/demo/mot-don-di-tron.sh <api>` in đủ 6 bước, `grandTotal 530000`, `available 3`, `awaiting_pickup`, `shipping`, `đơn delivered`, "Xong"; không in thẻ, SĐT | `TestMotDonDiTron_Script` | như trên | PASS (kết quả script chép ở log test) |
| Gom dựng dữ liệu thử (việc treo Task 32): test fulfillment, cash dùng `shiptest`, vẫn qua | toàn bộ test hai gói | `go test -race ./internal/modules/cash/... ./internal/modules/fulfillment/...` | `ok cash 85.8s`, `ok fulfillment 48.5s` |
| Stack e2e dùng chung đổi (sổ sự kiện, hãng giả tùy chọn, seed cửa hàng): các test e2e khác vẫn qua | cả gói `tests/e2e`, `tests/arch` (gồm lint mã thật) | `go test -race -count=1 -timeout 40m ./internal/modules/cash/... ./internal/modules/fulfillment/... ./tests/e2e/ ./tests/arch/ ./internal/testkit/...` (sau khi sửa theo reviewer) | `ok cash 89.8s`, `ok fulfillment 74.0s`, `ok tests/e2e 148.6s`, `ok tests/arch 42.5s` |
| Cổng kiểm | `make ci` | `CRM_PG_PORT=5600 make ci` sau `git merge main` (Task 36, fast-forward) | build, vet, lint (`0 issues`), gen-check, sqlc-vet qua; bước `test`: mọi gói task chạm qua (`cash`, `fulfillment`, `tests/e2e 319.8s`, `tests/arch`), nhưng `httpapi`, `inventory`, `platform/queue` quá hạn 10 phút và `platform/files` `TestPublicVariants`, `imager` `TestImager_HangKilledWithinDeadline` đỏ — máy tải nặng (load average ~27, nhiều phiên dùng chung), task không chạm các gói này; chạy lại riêng `-timeout 30m`: xem dòng dưới. Các cổng sau `test` chạy riêng `make vuln oasdiff gen-check catalog-check jscpd test-guard gitleaks console-check` → exit 0 (jscpd lần đầu báo 1 đoạn trùng, đã gom — mục Tái dùng) |
| Gói không chạm, đỏ do quá tải trong `make ci` | chạy lại riêng | `go test -race -count=1 -timeout 30m ./internal/httpapi/ ./internal/modules/inventory/ ./internal/platform/files/ ./internal/platform/queue/ ./internal/imager/` | `ok httpapi 471.3s`, `ok inventory 509.8s`, `ok files 433.7s`, `ok queue 491.2s`, `ok imager 14.0s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bước 4 của kế hoạch ("chạy `mot-don-di-tron.sh http://localhost:4190` với compose dev"): không chạy compose (phiên
  dùng bộ chung crm-shared, cấm `docker compose up`; compose dev cũng chưa nối hãng giả cho ingest, worker). Thay bằng
  `TestMotDonDiTron_Script`: chạy đúng script đó bằng `bash` với api thật của stack e2e và hãng giả thật.
- Script nhận thêm `CRM_ACCESS_JWT` (thẻ Access, máy dev và test) ngoài service token của staging; cần `WAREHOUSE_ID`,
  `CARRIER_ACCOUNT_ID` (chưa có API danh sách kho, tài khoản hãng). Sổ COD chưa có API (M3) nên script in `available`,
  trạng thái; số dư COD do test kiểm, RUNBOOK có câu SQL.
- Thêm `buildPackage` (tách từ `buildBinary`) để dựng `tools/fakecarrier` trong test.
- `docs/nghiem-thu-m1.md`: điền cột Kết quả dòng 1 (cục bộ đạt, chưa chạy staging) và dòng 4.

## Quyết định thay chủ shop

1. **Gói gom dựng dữ liệu đặt ở `internal/testkit/shiptest`**, không phải `fulfillment/fulfilltest` như Task 32 gợi ý:
   gói trong `internal/modules/fulfillment` không được nhập `customer`, `inventory` (04 mục 3, `tests/arch`). Phương án
   khác: nới luật ranh giới cho gói test (trái spec), hoặc để lặp như cũ.
2. **Test e2e chạy ba vai trò trong tiến trình test, hãng giả là tiến trình riêng** (dựng `tools/fakecarrier`): giữ
   exporter dấu vết trong bộ nhớ (kế hoạch) mà hãng giả vẫn là chương trình thật gửi webhook có chữ ký qua HTTP.
   Phương án khác: chạy cả ba vai trò bằng tệp chạy (không thấy span).
3. **Không sửa `docker/compose.dev.yml`** để nối hãng giả cho `crm-ingest`, `crm-worker` (đổi hạ tầng dùng chung; ghi ở
   "Dừng để hỏi"). Script được kiểm qua test thay cho compose.

## Dừng để hỏi

- (Không chặn task) Compose dev chưa nối hãng giả: `fakecarrier` thiếu `FAKECARRIER_WEBHOOK_URL` (tới
  `crm-ingest:4191/hooks/fakecarrier`) và `FAKECARRIER_WEBHOOK_SECRET`; `crm-ingest`, `crm-worker` thiếu `FAKECARRIER_URL`,
  `FAKECARRIER_WEBHOOK_SECRET`. Đổi `docker/` là đổi hạ tầng nên chờ chủ shop OK; sau đó `make app-up` +
  `mot-don-di-tron.sh http://localhost:4190` chạy được trên compose.

## Việc còn lại

- Điểm dừng M1-A trên staging (giữ lại, cần chủ shop OK): deploy image M1 (cả `fakecarrier`), `migrate`, `seed-dev`
  trên database staging, chạy `mot-don-di-tron.sh` với service token chủ shop cấp (`CF_ACCESS_CLIENT_ID`,
  `CF_ACCESS_CLIENT_SECRET`), ghi kết quả vào `docs/RUNBOOK.md` và `docs/nghiem-thu-m1.md`. Trang `/admin` của hãng giả chỉ
  nghe máy nội bộ: trên staging chạy script từ chính máy chủ hoặc qua đường hầm SSH.

## Việc phát hiện thêm

- Chưa có API đọc kho, tài khoản hãng, sổ COD: script phải nhận mã kho, mã tài khoản qua biến môi trường (M2/M3).
- `httpapi/handlers_shipment_test.go` còn tự dựng tài khoản hãng (gói test nội bộ của httpapi); có thể dùng `shiptest`
  ở task sau.

## Reviewer

Kết luận: CHƯA REVIEW LẦN 2

- Lần 1: CẦN SỬA. (1) Câu SQL lấy `warehouse_id`, `carrier_account_id` ở RUNBOOK chạy bằng chủ bảng ngoài ngữ cảnh tenant
  ra 0 dòng (RLS ép cả chủ bảng) → sửa: chạy trong transaction có `set_config('app.tenant_id', …, true)`, câu sổ COD
  cũng vậy. (2) Dòng bằng chứng "các test e2e khác vẫn qua" còn trống → điền. Ghi chú không bắt buộc đã làm: secret
  Access không còn trên dòng lệnh `curl` (header ghi tệp tạm quyền 600, `-H @tệp`, xóa khi thoát). Ghi chú không sửa:
  `$(available)` trong dòng in lỗi thì in rỗng (chỉ hiển thị); tiêu chí 1 của `nghiem-thu-m1.md` nói "đơn 1 chiếc, còn
  4" còn kế hoạch Task 33 làm 2 chiếc, còn 3 (cùng ý nghĩa; báo chủ shop); test script cần `jq` trên máy chạy.
- Sau lần 1 còn sửa thêm do jscpd: gom `shiptest.CODBalances`.
