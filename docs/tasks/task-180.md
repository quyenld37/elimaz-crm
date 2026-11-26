# Task 180 — Thử tải M4 (lặp lại HT17 cho ca live)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 180 = M4-40). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu cũ nào đổi tên hay chữ ký. Thêm mới: gói
  `internal/testkit/loadstats` (`Event`, `Stats`, `Events`, `Read`, `HistNames`; chỉ `tools/loadkit` dùng). Đổi hành vi
  `internal/testkit/fakegraph` (cập nhật sau khi chạy khói): đọc `{page-id}` trả thêm `tasks` như `me/accounts` (hàm
  riêng `pageTasks`); người dùng: test của gói đó và `internal/adapters/facebook/*_test.go`, `tests/e2e/channel_silence_test.go`
  (chỉ đọc `access_token`, `name`; thêm trường không đổi kết quả). `package main` của `tools/loadkit`, `tools/fakegraph`:
  không ai nhập. Cấu trúc trong `tools/loadkit`: `runClient`/`streamOnce` nhận interface `frameSink` thay `*collector`
  (chỉ `cmdSSE` và `sse_test.go` gọi), `renderVerdicts` tách khỏi `renderMarkdown` (dùng chung với `renderFB`).
- Gói phụ thuộc ngược (go list): `tools/loadkit`, `tools/fakegraph` là lệnh, không gói nào phụ thuộc. `tools/loadkit`
  nhập thêm public API `catalog.EnsureDefaultSeller`, `inventory.EnsureDefaultWarehouse`, `order.EnsureDefaults`,
  `tenancy.WithTenant` (như `cmd/crm seed-dev`).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Bộ thử chỉ GỌI các operation có sẵn: createProduct,
  createStockDocument, replaceStockDocumentLines, postStockDocument, startFacebookOAuth, exchangeFacebookOAuth,
  connectFacebookPage, setChannelSendMode, startLiveSessionManually, endLiveSession, listConversations,
  getConversation, listMessages, sendMessage, listLiveComments, claimLiveComment, `GET /api/v1/stream`.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không câu sqlc mới, không migration. Câu đọc số đo (chỉ đọc, chủ bảng, trong
  `tenancy.WithTenant`) ở `internal/testkit/loadstats` (hạ tầng thử, như `testkit/conversation.go`; xem Quyết định 4):
  `conversation.messages`, `conversation.live_comment_claims`, `platform.jobs`, `inventory.stock_balances`,
  `inventory.stock_reservations`, `platform.metrics_minute`, `pg_stat_database`.
- Hạ tầng thử: thêm job `loadtest` (input `loadtest`) vào `.github/workflows/test-remote.yml` và biến
  `CRM_CI_LOADTEST` cho `scripts/ci/remote-test.sh` (để chạy bài tải ở bể Blacksmith); test luật của tệp:
  `tests/deploy/remote_ci_test.go`.
- Test sẽ chạy: `go test -race ./tools/loadkit/ ./tools/fakegraph/ ./internal/testkit/fakegraph/
  ./internal/testkit/loadstats/` (gói đổi, có mã viết tay), `go test ./internal/adapters/facebook/` (dùng
  testkit/fakegraph), `go test ./tests/deploy/ -run 'RemoteWorkflow|RemoteTest'`, `go test ./tests/arch/`; kịch bản k6
  kiểm bằng `k6 inspect`; chạy khói 1 phút và bài 15 phút ở bể Blacksmith (không chạy tải trên Mac; Mac chỉ chạy khói
  5 sự kiện/giây, 20 s).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit/fakegraph,
  realtime, metrics.HistBoundsMs, tenancy.WithTenant); bộ thử M0 `loadtest/` và `tools/loadkit` (access, seed, sse,
  stats, report); `tools/fakegraph` (máy Meta giả của Task 146).
- Dùng lại / viết mới vì:
  - Dùng lại `loadkit access` (thẻ Access), `seedStaff` (tenant, 50 nhân viên), `runClient`/`streamOnce` (client SSE;
    tách cách xử lý khung thành tham số để nhận cả khung `conversation.changed`), `percentile`, `metrics.HistBoundsMs`,
    bảng chấm `verdict`/`below`/`renderMarkdown` của report, `parseChaos`.
  - Dùng lại máy giả `internal/testkit/fakegraph` qua `tools/fakegraph` (thêm biến số Page, số PSID, địa chỉ nghe và
    sổ mở luồng live ở lớp lệnh, không sửa testkit).
  - Viết mới `fb_*.js`, `fb_chaos.sh`, `fb_run.sh`: tải M0 là ping selftest; tải M4 là payload Facebook có chữ ký
    `X-Hub-Signature-256`, luồng SSE live và thao tác nhân viên — không có sẵn.
  - Viết mới `fb_run.sh` thay vì thêm mode vào `run.sh`: vai trò chạy như tiến trình trên máy (xem Khác kế hoạch),
    `run.sh` của M0 giữ nguyên.
  - jscpd: hai chỗ trùng đã gộp (`parseFBEventID` đọc phần đuôi bằng `parseEventID` của M0; bảng chấm in bằng
    `renderVerdicts` chung).

## Soát thiết kế

Task chỉ thêm công cụ đo và kịch bản; không đổi mã ứng dụng.

- 5.1–5.12: không áp dụng: không thêm dữ liệu, module, đường ghi hay thư viện. Công cụ đo gọi public API của module
  (seed như `cmd/crm seed-dev`), đọc số đo bằng chủ bảng trong `tenancy.WithTenant` (5.7), SQL dựng lúc chạy chỉ ở
  `*_dynsql.go` có test PostgreSQL thật (luật CSDL). Không thư viện mới (k6 đã dùng ở M0).
- 6.1 (đồng thời): kịch bản cố ý tạo đồng thời (cùng comment qua hai đường, cùng webhook hai lần, chốt cùng lúc);
  bộ đếm đo kết quả: mỗi sự kiện một dòng, `client_ref` tối đa một lần ở Meta giả, tổng giữ ≤ tồn.
- 6.2 (gửi lại): webhook trùng (HT03) và `Idempotency-Key` mới cho mỗi lần bấm gửi, chốt.
- 6.3 (deadlock): đọc `pg_stat_database.deadlocks` đầu và cuối bài, đòi không tăng.
- 6.6, 6.8 (việc nền lỗi, tiến trình chết): đếm việc `dead` trong tenant thử; `fb_chaos.sh` giết worker, api, ngắt
  PostgreSQL; đo trở lại ≤ 2 phút, luồng live nối lại ≤ 15 s, thiếu 0, gửi hai lần 0.
- 6.12 (dữ liệu cá nhân): PSID, tên, nội dung là giả; log công cụ không in nội dung tin; thẻ Access xóa khi bài xong.
- 6.4, 6.5, 6.7, 6.9, 6.10, 6.11, 6.13: không áp dụng: không đổi mã chạy trong transaction, tiền, quyền.

## Bằng chứng

Máy đo bài chính: Blacksmith `blacksmith-4vcpu-ubuntu-2404` (4 vCPU), mỗi bài một máy riêng; số đo đầy đủ ở
`docs/do-luong/thu-tai-m4.md`. Mã đo: nhánh task-180 trên gốc m4 sau Task 172 (trước khi gộp Task 173).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| HT17 + CH01: tin khách tới báo hiệu p95 < 2 s, p99 < 5 s dưới tải 10× | bài `live` (fb_run.sh), tiêu chí "Tin khách tới báo hiệu" | `CRM_CI_RUNNER=blacksmith CRM_CI_STATIC=false CRM_CI_LOADTEST=live scripts/ci/remote-test.sh <tree> HEAD <rỗng>` (run 45826397968) | p95 296 ms, p99 305 ms, 27.001 tin × 50 client, thiếu khung 0 — ĐẠT |
| HT17: nhận webhook p95 < 300 ms; comment live tới báo hiệu p95 < 3 s; bấm gửi → Send API p95 < 1 s; API đọc p95 < 200 ms, p99 < 500 ms | bài `live` | như trên | 1,3 ms; 292 ms; 6 ms; 4,2 ms / 200,7 ms — ĐẠT |
| LV02: đủ comment dưới tải, mỗi sự kiện một lần (HT03: webhook trùng cùng lúc) | bài `live`, "Mỗi comment live…", "Mỗi tin khách…" | như trên | 63.001/63.001 comment (6.252 qua cả hai đường), 27.001/27.001 tin (1.335 lượt gửi trùng) — ĐẠT |
| client_ref tối đa một lần; tổng giữ ≤ tồn; deadlocks không tăng; không việc dead | bài `live` | như trên | 900 Send API trùng 0; giữ 149 ≤ 10.000; 0 → 0; dead 0 — ĐẠT |
| LV10, HT01, HT02: hỏng giữa tải, trở lại ≤ 2 phút, luồng live ≤ 15 s, thiếu 0, gửi hai lần 0 | bài `chaos` (fb_chaos.sh) | `… CRM_CI_LOADTEST=chaos …` (run 45826397630) | trở lại ≤ 3,1 s ×3; luồng live 10,0 s (giết worker), 4,5 s (PostgreSQL); 63.000/63.000 comment, 27.001/27.001 tin; 840 Send API trùng 0; webhook khác 200: 0 — ĐẠT |
| Kịch bản chạy được (Step 1: chạy thử 1 phút) | bài `live:1m` | `… CRM_CI_LOADTEST=live:1m …` (run 45825756924; lại trên bản đã gộp m4 sau Task 173 và đã dời câu đo vào loadstats: run 45847037249) | mọi tiêu chí ĐẠT cả hai lượt |
| Cổng kiểm của task | toàn bộ gói (đổi testkit) + cổng tĩnh | `ci-task.sh` (run 45849732524, blacksmith) | `CI-TASK XANH`; lượt trước đỏ ở gitleaks (dòng `ClickToAPI` bị coi là khóa), đã sửa |
| Bảng chấm: mỗi ngưỡng có ca KHÔNG ĐẠT, bài chaos không chấm độ trễ | `TestFBEvaluate_AllPass`, `TestFBEvaluate_EachThresholdFails` (18 ca), `TestFBEvaluate_Chaos`, `TestParseLiveRegain`, `TestRenderFB` | `go test -race -count=1 ./tools/loadkit/` | ok |
| Cách đếm: mã sự kiện, ghép khung với hội thoại (cặp thiếu tính vô hạn), client_ref trùng, độ trễ bấm gửi | `TestFBEventID`, `TestFBSink_Result`, `TestRankWithMissing`, `TestSendStats` | như trên | ok |
| Câu đọc số đo đúng trên PostgreSQL thật (lượt, chiều vào, RLS tenant, trạng thái gửi, tồn, dead, histogram) | `TestReadFBStats` | `go test -race -count=1 ./internal/testkit/loadstats/` | ok |
| Máy Meta giả: 2 Page, PSID, địa chỉ nghe, sổ mở luồng; đọc Page có tasks | `TestLoadConfig_LoadTest`, `TestServer_PagesAndProfiles`, `TestServer_StreamLog`, `TestServer_PageTokenWithTasks` | `go test -race -count=1 ./tools/fakegraph/ ./internal/testkit/fakegraph/` | ok |
| Job loadtest chạy ở bể từ xa, k6 ghim có sha256, không vps; remote-test.sh gửi input loadtest | `TestRemoteWorkflow_LoadtestJob`, `TestRemoteTest_LoadtestInput` | `go test -count=1 -run 'RemoteWorkflow|RemoteTest' ./tests/deploy/` | ok |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 180 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

1. Kế hoạch ghi "`ingest`, `api`, `worker` từ image (M0 Task 21)". Bài chạy ba vai trò từ cùng mã (`go build ./cmd/crm`)
   như tiến trình trên máy thử: test-remote.yml cấm dựng image (luật Task 493, `TestRemoteWorkflow_DispatchOnlyWithInputs`)
   và máy Mac không chạy tải nặng. Khác image: không có giới hạn bộ nhớ của container (giữ `GOMEMLIMIT` như compose dev),
   CPU, bộ nhớ đo bằng `ps` thay `docker stats`.
2. Thêm tệp ngoài danh sách Files: `loadtest/fb_run.sh`, `loadtest/fb_lib.sh` (bộ chạy, `run.sh` của M0 dùng container),
   `tools/loadkit/fb*.go`, `internal/testkit/loadstats`, `tools/fakegraph/streams.go`, job `loadtest` của
   `.github/workflows/test-remote.yml` và biến `CRM_CI_LOADTEST` của `scripts/ci/remote-test.sh` (đường đo ở bể).
3. Step 2 "chạy 15 phút trên máy này": chạy ở bể Blacksmith (chủ shop 26/11), không chạy trên Mac.
4. Kết nối Page trong dữ liệu thử dùng đường "mã system user" của API thay đăng nhập Facebook (Việc phát hiện thêm 1).

## Quyết định thay chủ shop

1. **Chạy vai trò như tiến trình trên máy thử, không dựng image** (Khác kế hoạch 1). Phương án khác: thêm job dựng image
   vào test-remote.yml (trái luật Task 493 chủ shop đã chốt), hoặc dừng hỏi. Dễ đảo ngược: chỉ là bộ chạy.
2. **Mọi nhân viên giả là owner phạm vi toàn shop** (kế hoạch: "mỗi người một phạm vi Page"): mỗi client SSE nhận báo
   hiệu của cả hai Page, tức gấp đôi số khung so với kế hoạch — bài nặng hơn, không nhẹ hơn. Phương án khác: cấp quyền
   theo Page cho từng nhân viên (thêm phần seed). Ghi ở `loadtest/README.md`, Giới hạn.
3. **Độ trễ comment live đo phía client theo lúc k6 đẩy vào máy giả/ingest** (cùng đồng hồ máy), không dùng chỉ số máy
   chủ `live_comment_to_signal_ms` (đếm cả comment lấy bù muộn, ghi chú Task 171; và `created_time` của webhook tính
   bằng giây). Chỉ số máy chủ vẫn in để tham khảo.
4. **Câu đọc số đo qua nhiều schema đặt ở `internal/testkit/loadstats`** thay `tools/loadkit/*_dynsql.go`: luật
   `TestSQLUsageProductCode` không cho gói ngoài module đọc bảng `conversation`, `inventory`; testkit là chỗ của SQL hạ
   tầng thử (không quét, như `testkit/conversation.go`), `tools/fakegraph` đã nhập testkit lúc chạy. Không nới luật nào.
   Phương án khác: công bố view mới (migration, ngoài phạm vi), hoặc thêm ngoại lệ vào test kiến trúc (nới test).
5. **Sửa máy Meta giả `internal/testkit/fakegraph`**: đọc `{page-id}` trả `tasks` như Meta và như `me/accounts` của
   chính máy giả; thiếu thì CRM coi mọi Page thiếu quyền, không nối được Page nào qua mã system user. Có test.
6. Bài chaos không chấm độ trễ và "mọi tin gửi ra đã gửi" (như bộ M0: mất luồng SSE khi giết api, `in_doubt` khi giết
   worker giữa lúc gửi là đúng thiết kế); mọi tiêu chí đúng một lần, không thiếu, trở lại, luồng live vẫn chấm.

## Việc phát hiện thêm

1. Máy Meta giả (`internal/testkit/fakegraph`) chưa đổi được token dài hạn: `oauth/access_token` với
   `grant_type=fb_exchange_token` trả lỗi mã 100, nên `exchangeFacebookOAuth` (bước 2 của `ExchangeCode`) luôn trả
   `oauth_state_invalid` khi chạy với máy giả — đường đăng nhập Facebook của console dev với `tools/fakegraph` không đi
   hết được. Không sửa trong task này (bộ thử dùng đường mã system user).
2. Hai bài 15 phút chạy trên mã trước Task 173 (gộp m4 sau khi đo); trên bản đã gộp chỉ chạy lại bài khói 1 phút
   (ĐẠT). Lần đo sau (Task 181, trước ngày mở) chạy lại đủ trên mã mới.
3. `api` ghi cảnh báo `webcache: … cache: unavailable` mỗi 2 s khi không có Valkey (máy thử không bật Valkey): log ồn,
   không ảnh hưởng số đo.
4. (Reviewer) Dữ liệu thử không đặt mẫu tin xin địa chỉ cho Page, nên 149 lần chốt `askAddress: true` không sinh tin
   riêng: tiêu chí "client_ref tối đa một lần" mới phủ đường gửi của nhân viên (900 / 840 tin), chưa phủ tin tự động
   xin địa chỉ. Bổ sung khi chạy lại (Task 181 hay lần đo trước ngày mở): seed mẫu xin địa chỉ.
5. (Reviewer) `fb_chaos.sh` không kiểm PostgreSQL đã thật sự ngừng sau `docker compose stop` (sai `COMPOSE_PROJECT` thì
   bước ngắt không ngắt gì mà vẫn chấm). Lượt đã ghi có dấu hiệu ngừng thật (130 lượt đọc lỗi, luồng live phải nối lại).
   Nên thêm kiểm `SELECT 1` phải lỗi sau `stop`.

## Reviewer

Commit 1 (cấu trúc: `frameSink` trong `sse.go`, `renderVerdicts` trong `report.go`): **ĐẠT**, không phát hiện phải
sửa. Bằng chứng giữ hành vi: `TestSSEClientReadsAndReconnects`, `TestSSEClientRetriesOnError`, `TestRenderMarkdown`
(`go test -race -count=1 ./tools/loadkit/`).

Commit 2 (hành vi: bài thử tải M4): **ĐẠT**, không phát hiện phải sửa. Reviewer đối chiếu: 18 ca ngưỡng của
`TestFBEvaluate_EachThresholdFails` khớp kế hoạch, không nới; cặp thiếu khung tính vô hạn; đúng một lần = dòng = mã
khác nhau = số đã giao, cùng chặn webhook khác 200 và đẩy máy giả lỗi; kịch bản chỉ giết PID của mình, từ chối dừng
`crm-shared`, xóa thẻ thử. Ghi chú không bắt buộc đã chép vào Việc phát hiện thêm 4, 5.
