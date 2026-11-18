# Task 395 — Compose dev nối hãng giả; kịch bản một đơn chạy trên compose (M1-33b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 395 (M1-33b), ngay trước Task 34).
Loại việc (quy trình mục 3): A. Đổi hạ tầng dev (compose dev, Makefile, công cụ dev); không đổi mã sản phẩm.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không. Mã Go mới chỉ ở gói `main` của công cụ dev mới
  `tools/devaccess` (không xuất ký hiệu, không ai nhập). Không sửa `cmd/crm`, `internal/**`.
- Gói phụ thuộc ngược (go list): `tools/devaccess` là `package main`, không có gói nhập nó. Nó nhập
  `internal/platform/logging` (như `tools/fakecarrier`) và `github.com/golang-jwt/jwt/v5` (đã là phụ thuộc trực tiếp).
  Test của nó nhập `internal/adapters/cfaccess` để kiểm thẻ bằng đúng bộ kiểm thật.
- Operation OpenAPI bị chạm, oasdiff: không đổi.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không. Kịch bản đọc `inventory.warehouses`, `fulfillment.carrier_accounts` bằng chủ bảng
  trong ngữ cảnh tenant (câu SQL sẵn có ở RUNBOOK).
- Hạ tầng: `docker/compose.dev.yml` (biến hãng giả cho `crm-ingest`, `crm-worker`, `fakecarrier`; dịch vụ dev mới
  `devaccess`; `crm-api` trỏ `ACCESS_TEAM_DOMAIN` tới `devaccess`, tin chứng chỉ của nó), `Makefile` (lệnh
  `demo-mot-don`), `scripts/demo/mot-don-compose.sh` (mới), `tools/devaccess/` (mới, có Dockerfile), `docs/RUNBOOK.md`,
  `docs/nghiem-thu-m1.md` (tiêu chí 1). Bổ sung khi chạy: `postgres` dev thêm `init: true`; `fakecarrier` thêm volume `fakecarrier-data` và
  `tools/fakecarrier/Dockerfile` tạo `/data` (Quyết định 3, 4). Dựng lại postgres giữ dữ liệu (volume `pgdata` không đổi). Ai dùng compose dev: các phiên khác (`make db-up` không bật profile `app`,
  `fakecarrier`, `devaccess`: không đổi), `scripts/ops/image-check.sh` (`app-up` không đặt khóa hãng giả → ingest,
  worker chạy như cũ, không nối hãng giả), `tests/deploy` (đọc compose đã gộp).
- Test sẽ chạy: `go test -race ./tools/devaccess/...`, `go test -race -run 'TestDevCompose' ./tests/deploy/`,
  `go test -race ./tools/fakecarrier/...` (không đổi, chạy để chắc Dockerfile/biến), `./tests/arch/...` (lint, ranh
  giới gồm `tools/`); chạy thật trên compose `crm-t395`: `make demo-mot-don` (kịch bản 6 bước); `make ci` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về thẻ Access
  thử hay compose); `internal/testkit/accessjwt.go` (`AccessKeyServer`, `Sign`: máy chủ khóa và ký thẻ thử, nhưng gắn
  `testing.TB`, `httptest` — chỉ dùng được trong test); `internal/adapters/cfaccess` (bộ kiểm thẻ thật);
  `tools/fakecarrier` (mẫu công cụ dev: `loadConfig(getenv)`, `run(ctx, …)`, Dockerfile distroless, profile riêng);
  `scripts/demo/mot-don-di-tron.sh` (kịch bản Task 33); `scripts/ops/image-check.sh` (mẫu script chạy compose).
- Dùng lại / viết mới vì:
  - Dùng lại nguyên `mot-don-di-tron.sh` (không sửa) — script mới `mot-don-compose.sh` chỉ gom biến (mã kho, mã tài
    khoản hãng qua câu SQL của RUNBOOK, thẻ từ `devaccess`, cổng của phiên) rồi gọi nó.
  - Dùng lại `tools/fakecarrier` (image, cổng 127.0.0.1) và cách nối của `cmd/crm` (`FAKECARRIER_URL`,
    `FAKECARRIER_WEBHOOK_SECRET`): chỉ thêm biến trong compose.
  - Viết mới `tools/devaccess`: compose dev cần một máy chủ khóa Access thật (HTTPS) và cách lấy thẻ ngoài test;
    `testkit.AccessKeyServer` không chạy được ngoài `go test`. Không chuyển phần ký thẻ của testkit thành gói dùng
    chung: testkit cần các chế độ hỏng (khóa lạ, kid lạ, 500) mà công cụ dev không cần, hai bên khác lý do thay đổi.
  - Bộ kiểm thẻ trong test của `devaccess` là `cfaccess.New` thật (không viết bộ kiểm riêng).
  - jscpd (`make ci` lần đầu): 2 đoạn trùng mới. (a) dựng JSON bộ khóa giống `testkit/accessjwt.go` `jwksJSON`: giữ
    riêng (testkit chỉ cho test, công cụ là `package main`, không gói nào nhập được cả hai mà không tạo gói dùng chung
    chỉ cho một hàm 6 dòng); viết lại bằng kiểu `jwk` có thẻ JSON. (b) hàm `btoi` giống `tools/routegen`: bỏ hẳn, đếm số
    goroutine còn chờ trực tiếp. Chạy lại: `Found 0 clones`.

## Soát thiết kế

- 5.1–5.3 Chủ dữ liệu, ghi bảng, đọc view: không áp dụng — không mã sản phẩm, không bảng. Kịch bản đọc 2 mã bằng chủ
  bảng trong ngữ cảnh tenant (chỉ dev), như RUNBOOK.
- 5.4, 5.5 Nguyên tử / sau commit: không đổi; kịch bản đi đúng đường thật (lệnh `fulfillment.*`, webhook → ingest →
  worker → consumer `cash`).
- 5.6 Gọi ra ngoài: worker gọi hãng giả qua `FAKECARRIER_URL=http://fakecarrier:4194` (mạng compose), hãng giả gửi
  webhook tới `http://crm-ingest:4191/hooks/fakecarrier`; cả ba dùng một khóa.
- 5.7 Ngữ cảnh shop: `seed-dev` dựng tenant, kho MAIN, tài khoản `fake-1`, tuyến `carrier_account:fake-1`.
- 5.8 Không HTTP trong module: không đổi.
- 5.9 Đường ghi: script Task 33 đã gửi `Idempotency-Key` mới mỗi lệnh.
- 5.10 PostgreSQL là nguồn sự thật: không đổi.
- 5.11 Thư viện: không thêm (`golang-jwt/jwt/v5` đã có, ADR-44 / Task 07). Script dùng `bash`, `curl`, `jq`, `openssl`
  (sinh khóa), `docker compose exec postgres psql` (không cần psql trên máy).
- 5.12 Theo mẫu: `tools/devaccess` theo mẫu `tools/fakecarrier`; dịch vụ dev theo profile riêng như `fakecarrier`.
- 6.1–6.5 Đồng thời, gửi lại, khóa, lỗi tạm, cách ly: không đổi mã chạy.
- 6.6–6.9 Việc nền lỗi, 429, chết giữa chừng, treo: không áp dụng (không mã sản phẩm mới).
- 6.10 Tiền: không đổi.
- 6.11 Quy luật: không có quy luật mới.
- 6.12 Bí mật, dữ liệu cá nhân: khóa webhook hãng giả **sinh lúc chạy** (`openssl rand -hex 32`) trong lệnh make, chỉ
  nằm trong biến môi trường của container, không ghi tệp, không vào repo; compose không chứa khóa cố định nào cho hãng
  giả. Khóa ký thẻ và chứng chỉ của `devaccess` sinh lúc chạy trong volume của dự án compose; volume `devaccess-ca`
  chỉ chứa chứng chỉ công khai (api gắn chỉ đọc). Thẻ không in ra (script Task 33 ghi header vào tệp tạm 600).
- 6.13 Quyền, an toàn: `devaccess` và `fakecarrier` chỉ có trong compose dev (profile riêng), không có trong
  `compose.prod.yml`; cổng phát thẻ và trang `/admin` của hãng giả chỉ mở `127.0.0.1` (không ra ngoài máy); cổng HTTPS
  khóa của `devaccess` không mở ra máy. `devaccess` từ chối chạy khi `CRM_ENV=production`.

## Quyết định thay chủ shop

1. **Thêm máy chủ thẻ Access giả cho dev (`tools/devaccess`)** — kế hoạch chỉ nói nối hãng giả, nhưng compose dev
   hiện để `ACCESS_TEAM_DOMAIN=dev.cloudflareaccess.invalid`: không thẻ nào qua được, kịch bản đỏ ngay bước 1 trên
   compose (Task 33 chỉ chạy được trong test nhờ `testkit.AccessKeyServer`). Chọn công cụ dev riêng phục vụ
   `/cdn-cgi/access/certs` qua HTTPS (chứng chỉ tự ký sinh lúc chạy) và phát thẻ qua cổng chỉ mở 127.0.0.1; api tin
   chứng chỉ qua `SSL_CERT_DIR`. Không đổi mã sản phẩm, bộ kiểm thẻ vẫn đi đúng đường thật (HTTPS, kid, aud, iss,
   MFA). Phương án khác: thêm biến bỏ qua xác thực ở dev (đổi mã xác thực — vùng rủi ro cao, có thể lọt sang
   production); hoặc chỉ nối hãng giả và để kịch bản chạy bằng test (không đạt Step 2). Đảo ngược: xóa dịch vụ và thư
   mục công cụ.
2. **Khóa hãng giả sinh mỗi lần chạy `make demo-mot-don`, không lưu tệp**: compose đặt `FAKECARRIER_URL` cho
   ingest, worker chỉ khi có `CRM_FAKECARRIER_SECRET` (`${…:+…}`), nên `make app-up`, `image-check` chạy như cũ (không
   nối hãng giả). Lệnh make dựng lại ingest, worker, fakecarrier cùng lúc với cùng khóa. Phương án khác: lưu khóa vào
   tệp `.env.*` theo dự án (thêm tệp bí mật trên đĩa); dùng chuỗi dev cố định trong compose (trái Step 2).
3. **Thêm `init: true` cho PostgreSQL của compose dev** (phát hiện khi chạy `make ci`): healthcheck `pg_isready` và
   `docker compose exec postgres psql` (script của task này) chạy bằng exec; dưới tải nặng `pg_isready` thoát mã 2, tiến
   trình mồ côi rơi về postmaster (PID 1) và postmaster coi là sập, khởi động lại cả cụm (log: `untracked child process
   (PID 11021) exited with exit code 2` → `terminating any other active server processes`), làm đỏ hàng loạt test của
   `httpapi`, `inventory`, `files`, `queue`. Production đã có `init: true` (Task 21); dev làm giống. Phương án khác: script
   chạy psql trong container riêng (vẫn để healthcheck làm sập cụm). Đảo ngược: xóa một dòng.
4. **Hãng giả giữ kiện trong volume `fakecarrier-data`** (`FAKECARRIER_DATA=/data/fakecarrier.json`, Dockerfile tạo
   `/data` cho người dùng 65532): mỗi lần chạy có khóa mới nên container hãng giả được dựng lại; trước đây trạng thái nằm
   trong `/tmp` của container, bộ đếm quay về `FC00000001`, trùng mã vận đơn đã có → lệnh `refresh_status` `in_doubt`
   (`shipments_tracking_key`). Phương án khác: giữ khóa cố định theo dự án trong tệp (bí mật trên đĩa).
5. **Cổng của phiên lấy từ biến** (`CRM_API_PORT`, `CRM_INGEST_PORT`, `CRM_FAKECARRIER_PORT`, `CRM_DEVACCESS_PORT`)
   để nhiều phiên chạy song song; phiên này dùng 18390, 18391, 18394, 18395.

## Bằng chứng

Bộ Docker riêng `crm-t395` (PostgreSQL 5895), cổng phiên: api 18390, ingest 18391, fakecarrier 18394, devaccess 18395
(`export CRM_API_PORT=18390 CRM_INGEST_PORT=18391 CRM_FAKECARRIER_PORT=18394 CRM_DEVACCESS_PORT=18395`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 1 (đỏ): kịch bản trên compose dev trước khi sửa | chạy tay | `make db-up image app-up`, `seed-dev`, `fakecarrier` lên, `mot-don-di-tron.sh http://127.0.0.1:18390` | `LỖI: POST /api/v1/catalog/products → 401 (mong 201): unauthenticated` — đỏ từ bước 1 (không có máy chủ khóa Access), chưa tới bước hãng giả |
| Đối chứng sau khi có `devaccess` nhưng không đặt khóa hãng giả (`make app-up`) | chạy tay | như trên, thẻ lấy từ `devaccess` | bước 1–3 qua (`grandTotal 530000`, `available 3`), bước 4 `LỖI: quá 10 s chờ vận đơn awaiting_pickup (đang là creating)` — đỏ đúng ở bước gửi hãng giả |
| Test compose đỏ trước khi sửa | `TestDevCompose_FakeCarrierWiredWithRuntimeSecret`, `TestDevCompose_DevAccessForAPI` | `go test -count=1 -run 'TestDevCompose\|TestProdCompose_NoDevTools' ./tests/deploy/` | FAIL: `crm-ingest: FAKECARRIER_URL "", muốn "http://fakecarrier:4194"` …, `compose dev thiếu devaccess` |
| Có `CRM_FAKECARRIER_SECRET`: ingest, worker có `FAKECARRIER_URL=http://fakecarrier:4194` và cùng khóa; fakecarrier gửi webhook tới `crm-ingest:4191/hooks/fakecarrier` cùng khóa; cổng chỉ 127.0.0.1; api không có biến hãng giả | `TestDevCompose_FakeCarrierWiredWithRuntimeSecret` | `go test -count=1 ./tests/deploy/` | `ok tests/deploy` |
| Không đặt khóa: không biến hãng giả nào có giá trị; tệp compose không ghi khóa cố định | `TestDevCompose_NoSecretNoFakeCarrier` | như trên | `ok` |
| `devaccess`: chỉ mở cổng phát thẻ ở 127.0.0.1, read_only, cap_drop ALL, không chạy khi chỉ `db-up`; api `ACCESS_TEAM_DOMAIN=devaccess:8443`, `SSL_CERT_DIR`, volume CA chỉ đọc; production không có `fakecarrier`, `devaccess` | `TestDevCompose_DevAccessForAPI`, `TestProdCompose_NoDevTools` | như trên | `ok` |
| Thẻ của `devaccess` qua bộ kiểm thật `cfaccess` (HTTPS, chỉ tin chứng chỉ trong thư mục CA, kid, aud, iss, MFA); sai AUD bị từ chối; khởi động lại giữ khóa và chứng chỉ; thư mục CA chỉ có chứng chỉ, khóa riêng quyền 600; `/token` đòi email; `CRM_ENV=production` từ chối | `TestToken_AcceptedByRealVerifier`, `TestToken_WrongAUDRejected`, `TestRestart_KeepsKeysAndCert`, `TestFiles_PrivateKeysStayPrivate`, `TestTokenEndpoint_RequiresEmail`, `TestLoadConfig` | `go test -race -count=1 ./tools/devaccess/` | trước khi có mã: không biên dịch (`undefined: run`, `loadKeys`); sau: `ok tools/devaccess 2.4s` |
| Step 2: kịch bản chạy đủ 6 bước trên compose, in kết quả | chạy thật | `make demo-mot-don COMPOSE_PROJECT=crm-t395` | 1. mẫu (123 ms) → 2. `NK-000001 posted, available 5` → 3. `DH0000001 confirmed, grandTotal 530000, available 3` → 4. `awaiting_pickup, mã hãng FC00000001, COD 530000` (372 ms) → 5. `shipping, available 3` (500 ms) → 6. `đơn delivered, vận đơn delivered, available 3` (532 ms); sổ COD `carrier_holding 530000`, `sales_clearing -530000`, `to_collect 0`; tổng sổ COD 0; tồn `3 0 0` |
| Step 3: `nghiem-thu-m1.md` tiêu chí 1 khớp kế hoạch (đơn 2 chiếc, còn 3) | đọc lại | — | đã sửa, thêm kết quả compose |
| Chạy lần hai trên cùng database, hãng giả dựng lại với khóa mới (lần đầu lộ lỗi trùng mã hãng, Quyết định 4) | `TestDevCompose_FakeCarrierKeepsParcels` (đỏ trước khi sửa: `FAIL tests/deploy`) + chạy thật | `docker compose -p crm-t395 … down -v`, `make db-up`, `make demo-mot-don` hai lần | lần 1: `FC00000001`, đủ 6 bước, sổ COD `carrier_holding 530000`, `sales_clearing -530000`, `to_collect 0`, tổng 0, tồn `3 0 0`; lần 2: `FC00000002`, đủ 6 bước (322/350/361 ms ở bước 4–6), cùng số dư |
| PostgreSQL dev có `init` (Quyết định 3) | `TestDevCompose_PostgresInit` | `go test ./tests/deploy/`; `docker inspect -f '{{.HostConfig.Init}}' crm-t395-postgres-1` | `ok`; `true`. Test viết cùng lúc với sửa (không có lần đỏ riêng) |
| Lint mã mới | golangci-lint | `golangci-lint run ./tools/devaccess/... ./tests/deploy/...` | `0 issues` |
| Cổng kiểm | `make ci` | sau `git merge main` (fast-forward Task 50) | build, vet, lint `0 issues` qua; bước `test`: mọi gói task chạm qua (`tests/deploy`, `tools/devaccess`, `tools/fakecarrier`, `tests/arch`, `tests/e2e 285.9s`, `adapters/fakecarrier`); `httpapi`, `inventory`, `queue` quá 10 phút, `files` `TestPublicVariants`, `imager` `TestImager_HangKilledWithinDeadline` đỏ (load average ~27). Chạy lại riêng: hàng loạt đỏ do PostgreSQL tự khởi động lại (Quyết định 3); jscpd báo 2 đoạn trùng mới (mục Tái dùng) |
| Chạy lại sau khi sửa (postgres có `init`, jscpd) | các gói đỏ và gói task chạm | `go test -race -count=1 -timeout 30m ./internal/httpapi/ ./internal/modules/inventory/ ./internal/platform/files/ ./internal/platform/queue/ ./internal/imager/ ./tools/... ./tests/deploy/ ./tests/arch/` | `ok httpapi 157.9s`, `ok inventory 221.9s`, `ok files 152.1s`, `ok queue 177.3s`, `ok imager 15.1s`, `ok tools/devaccess`, `ok tools/fakecarrier`, `ok tests/deploy`, `ok tests/arch`; log postgres: 0 lần "terminating any other active server processes" |
| Cổng sau khi gộp `main` lần hai (Task 320, chỉ catalog/httpapi) | các cổng không phải test + test gói `main` vừa đổi | `make build vet lint gen-check catalog-check oasdiff jscpd test-guard gitleaks console-check sqlc-vet`; `make vuln` ở lượt trước; `go test -race ./internal/modules/catalog/... ./tests/deploy/ ./tools/devaccess/` | exit 0: `0 issues`, `Found 0 clones`, `no leaks found`, `No vulnerabilities found`, sqlc-vet `ok`; `ok catalog 130.1s`, `ok tests/deploy`, `ok tools/devaccess` (lượt `test-guard` trước đỏ vì nhánh chưa có tệp test mới của `main` → gộp lại) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm công cụ dev `tools/devaccess` và dịch vụ compose `devaccess` (xem Quyết định 1): kế hoạch không nêu, nhưng thiếu
  nó thì kịch bản không qua nổi bước 1 trên compose.
- Thêm `scripts/demo/mot-don-compose.sh` và `make demo-mot-don` để "chạy trên compose" là một lệnh lặp lại được;
  `mot-don-di-tron.sh` (Task 33) giữ nguyên.
- `tests/deploy/compose_test.go`: `config` tách thành `configEnv` (đặt biến cho `docker compose config`); `port` thêm
  `host_ip`, `volume` thêm `read_only` để kiểm.

## Việc phát hiện thêm

- `make image` ghi đè tag dùng chung `crm:dev` trên máy (mọi dự án compose dùng cùng tag). Phiên này đã dựng lại tag đó
  từ mã của nhánh (tương đương `main` lúc đó). Nên có tag theo dự án (`crm:$(COMPOSE_PROJECT)`) ở một task hạ tầng sau.
- `docs/tasks/task-33.md` mục "Dừng để hỏi" về compose dev đã được giải ở task này.

## Reviewer

Kết luận: ĐẠT (lần 2)

- Lần 1: ĐẠT, không phát hiện bắt buộc (reviewer chỉ chạy được `go vet`; hook chặn `go test` với vai reviewer, bằng
  chứng test lấy từ bảng trên). Ghi chú đã sửa: RUNBOOK nói `make db-reset` xóa volume của hãng giả, `devaccess` —
  lệnh đó không kèm profile nên có thể bỏ sót và làm trùng mã hãng → RUNBOOK ghi lệnh `down -v` kèm ba profile (lệnh đã
  dùng thật ở bằng chứng); chú thích đầu compose nói `make dev` bật cả `devaccess` → sửa (do `make demo-mot-don` bật).
  Ghi chú không sửa (chỉ hiển thị, có `die` chặn ở bước lấy mã): `|| true` trong `sql()` có thể nuốt lỗi psql ở dòng in
  sổ COD cuối; `DEMO_EMAIL` có dấu `+` chưa URL-encode (email mặc định không có).
- Lần 2: ĐẠT, không phát hiện bắt buộc (chỉ đổi tài liệu, mã không đổi so với lần 1). Ghi chú không sửa: lệnh `down -v`
  trong RUNBOOK chưa kèm profile `imager`, `filesync` (không ảnh hưởng mã hãng).
