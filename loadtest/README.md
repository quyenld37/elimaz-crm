# Bộ thử tải M0 (kế hoạch M0 Task 22, ADR-42)

Đo đường webhook → việc nền → báo hiệu và đường đọc API của CRM dưới tải, chấm theo ngưỡng của spec 04 mục 1.
Kết quả mỗi lần chạy ghi vào `docs/do-luong/thu-tai-m0.md` để so giữa các mốc.

## Chạy

```sh
make db-up                      # PostgreSQL, SeaweedFS, Jaeger của dự án compose
loadtest/run.sh normal          # 1×: 10 sự kiện/giây, 30 phút
loadtest/run.sh heavy           # 10×: 100 sự kiện/giây, 15 phút
loadtest/run.sh chaos           # heavy + chaos.sh: giết crm-api, giết crm-worker, ngắt PostgreSQL 60 s, cách nhau 3 phút
CHAOS_LOAD=normal loadtest/run.sh chaos   # chaos.sh cùng tải thường (máy không chịu nổi tải nặng)
loadtest/run.sh soak            # 1× trong 8 giờ
DURATION=1m loadtest/run.sh normal   # chạy khói
```

Nhiều phiên trên một máy: đặt `COMPOSE_PROJECT`, `CRM_PG_PORT`, `CRM_API_PORT`, `CRM_INGEST_PORT`,
`LOADTEST_ACCESS_PORT` riêng (xem đầu `run.sh`). Trên Linux (máy thử) đặt `LOADTEST_ACCESS_LISTEN` là địa chỉ bridge của Docker: container tới máy qua `host-gateway`, không tới được 127.0.0.1. Image riêng `crm:loadtest` (`CRM_IMAGE`), dựng lần đầu hoặc khi
`LOADTEST_BUILD=1`. Kết quả ở `loadtest/out/<bài>-<giờ>/` (bỏ khỏi git; `tokens.json` bị xóa khi bài xong): `report.md` là bảng chấm.
`run.sh` thoát 1 khi có tiêu chí KHÔNG ĐẠT.

## Tải gồm gì

| Phần | Ở đâu | Làm gì |
|---|---|---|
| Webhook | `k6/common.js` `webhook` | `POST /hooks/selftest` có chữ ký HMAC, mỗi lượt một ping, `eventId = lt.<run>.<ms gửi>.<VU>.<lượt>` |
| Đọc API | `k6/common.js` `read` | 50 nhân viên giả, `GET /api/v1/me` và `GET /api/v1/platform/jobs` xen kẽ, chia đều cho 50 thẻ; mỗi nhân viên dưới 5 lượt/giây (giới hạn 300 / 60 s của api) |
| 50 client SSE | `loadkit sse` | Mỗi client một nhân viên, giữ `GET /api/v1/stream`, tự nối lại; đếm pong của lượt chạy, trùng, độ trễ từ lúc gửi |
| Thẻ Access | `loadkit access` | CA, khóa ký mới mỗi lượt; api kiểm bằng bộ kiểm Cloudflare Access thật, trỏ tới máy chủ khóa giả qua `compose.loadtest.yml` |
| Dữ liệu thử | `loadkit seed` | Tenant "Thử tải", `nv01…nv50@loadtest.invalid` vai trò owner, định tuyến selftest `loadtest` |
| Hỏng giữa tải | `k6/chaos.sh` | Gây hỏng, khởi động lại ngay, đo thời gian tới khi ba vai trò healthy và không còn việc pending quá 2 s; thêm 2 lệnh `command.selftest` mỗi giây |
| Lấy mẫu | `run.sh` | `docker stats` mỗi 15 s; `loadkit stats` (việc của lượt chạy, histogram `job_pickup_ms`, dòng chết và kích thước `platform.jobs`) mỗi 5 phút |
| Chấm | `loadkit report` | Bảng tiêu chí → ngưỡng → số đo → ĐẠT / KHÔNG ĐẠT / CHƯA ĐO |

## Ngưỡng và cách đo

| Tiêu chí | Ngưỡng | Đo bằng |
|---|---|---|
| Nhận webhook | p95 < 300 ms | k6 `webhook_ms` (thời gian trả lời của ingest) |
| Webhook tới báo hiệu | p95 < 2 s, p99 < 5 s | `loadkit sse`: lúc nhận pong − lúc gửi trong `eventId`, lần nhận đầu ở mỗi client |
| Việc nền từ lúc ghi tới lúc nhận | p95 < 1 s, p99 < 3 s | histogram `job_pickup_ms` trong `platform.metrics_minute`; chỉ biết cận ngăn (…, 1000, 2000, 5000 ms), chấm "cận ngăn ≤ ngưỡng" |
| API đọc | p95 < 200 ms, p99 < 500 ms | k6 `read_ms` |
| Webhook khác 200 | 0 | k6 `webhook_bad` |
| Mỗi eventId tới mỗi client đúng một lần | thiếu 0, trùng 0 | `loadkit sse` so với số webhook 200 (bài hỏng giữa tải chỉ đòi trùng 0: client mất luồng lúc api chết nhận `resync`) |
| Không mất sự kiện | mọi webhook 200 có việc `inbound.selftest` done | `loadkit stats` sau khi hàng đợi cạn |
| Không lệnh nào gửi hai lần | 0 | việc `command.selftest` của lượt chạy có `attempts > 1` |
| Tự trở lại sau mỗi lần hỏng | ≤ 2 phút | `chaos.sh` (trạng thái healthy của Docker lấy mẫu mỗi 5 s, nên sai số tới 5 s) |
| Ngâm: bộ nhớ | mẫu cuối ≤ mẫu đầu từ giờ thứ nhất + 20% | `docker stats` của `crm-ingest`, `crm-api`, `crm-worker` |
| Ngâm: dòng chết | `n_dead_tup` của `platform.jobs` < 50.000 | `pg_stat_user_tables` |
| Ngâm: kích thước bảng jobs | cuối ≤ lúc giờ thứ hai | `pg_total_relation_size` |

Bài ngâm chạy ngắn hơn hai giờ thì hai tiêu chí cuối là CHƯA ĐO, không phải ĐẠT.

## Giới hạn

- Số đo trên máy dev là để tham khảo; tiêu chí đạt chính thức đo trên máy thử (kế hoạch M0 Task 23 bước 7.9).
- Tải thường là tốc độ đều 10 sự kiện/giây; phát lại một ca live thật khi có Page thử đủ dữ liệu [P].
- Không dùng với database có dữ liệu thật: `chaos.sh` thêm việc thẳng vào `platform.jobs` bằng chủ bảng.

# Bộ thử tải M4 — ca live Facebook (kế hoạch M4 Task 180, HT17 lặp lại, ADR-42)

Đo đường Facebook dưới tải 10× ca live (04 mục 1: đỉnh 100 sự kiện/giây trong 15 phút) và hỏng giữa tải. Kết quả ghi vào
`docs/do-luong/thu-tai-m4.md`.

## Chạy

Chủ shop 26/11: máy Mac không chạy tải nặng. Bài chính chạy ở bể từ xa (job `loadtest` của
`.github/workflows/test-remote.yml`, máy Blacksmith 4 vCPU):

```sh
CRM_CI_RUNNER=blacksmith CRM_CI_STATIC=false CRM_CI_LOADTEST=live  scripts/ci/remote-test.sh <tree|commit> <base> <tệp gói rỗng>
CRM_CI_RUNNER=blacksmith CRM_CI_STATIC=false CRM_CI_LOADTEST=chaos scripts/ci/remote-test.sh …
CRM_CI_LOADTEST=live:1m …                      # chạy khói 1 phút
```

Bảng chấm nằm trong tóm tắt của run (và log bước "Bài thử tải": `gh run view <id> --log`). Hai bài cùng lúc thì đặt
`CRM_CI_LABEL` khác nhau (nhánh `ci/<nhãn>`; cùng nhánh thì lượt sau hủy lượt trước).

Trên một máy có compose dev riêng: `make db-up` rồi `loadtest/fb_run.sh live|chaos` (biến ở đầu tệp). Chạy khói trên bộ
chung `crm-shared`: tạo database riêng (`PG_DB`), `CRM_PG_PORT=5600 CRM_S3_PORT=20600 COMPOSE_PROJECT=crm-shared RATE=5
DURATION=20s`; bài chaos thêm `CHAOS_PG_DOWN=0` (không bao giờ dừng PostgreSQL dùng chung; script từ chối).

## Tải gồm gì

| Phần | Ở đâu | Làm gì |
|---|---|---|
| Vai trò CRM | `fb_run.sh`, `fb_lib.sh` | `ingest`, `api`, `worker` dựng từ mã đang thử (`go build ./cmd/crm`), chạy như tiến trình trên máy với cấu hình Meta trỏ tới máy giả; `GOMEMLIMIT` như compose dev |
| Máy Meta giả | `tools/fakegraph` | 2 Page, 2.000 PSID có hồ sơ, Send API, luồng SSE `/{video}/live_comments`, sổ mở luồng `GET /_fake/streams` |
| Dữ liệu thử | `loadkit seed`, `loadkit fbseed` | tenant "Thử tải", 50 nhân viên owner; qua API thật: một mẫu 20 biến thể, phiếu nhập 500 mỗi biến thể (10.000 đơn vị), nối 2 Page bằng mã system user, bật gửi, mỗi Page một phiên live theo dõi tay |
| Tải khách | `k6/fb_live.js` | 70 comment live/giây (một nửa webhook `feed`, một nửa luồng SSE của máy giả, 10% qua cả hai đường cùng mã), 30 tin hộp thư/giây (5% gửi trùng cùng lúc, HT03); webhook có chữ ký `X-Hub-Signature-256` |
| Tải nhân viên | `k6/fb_staff.js` | 20 nhân viên đọc danh sách mỗi 20 s và mở một hội thoại; 10 nhân viên gửi một tin mỗi 10 s; 5 nhân viên chốt một comment mỗi 30 s |
| 50 client SSE | `loadkit sse -fixture` | ghi lúc nhận mỗi khung `conversation.changed`; khi bài xong ghép với hội thoại của từng sự kiện trong database |
| Hỏng giữa tải | `fb_chaos.sh` | giết worker (giữ luồng live), giết api, dừng PostgreSQL 60 s, cách 3 phút; đo trở lại và luồng live mở lại |
| Số đo | `loadkit fbstats`, `loadkit fbreport` | database (trước và sau), máy giả (Send API), bảng chấm |

## Ngưỡng và cách đo

| Tiêu chí | Ngưỡng | Đo bằng |
|---|---|---|
| Nhận webhook | p95 < 300 ms | k6 `webhook_ms` |
| Tin khách tới báo hiệu | p95 < 2 s, p99 < 5 s | mỗi cặp (tin, client): khung `conversation.changed` đầu tiên của hội thoại đó sau lúc gửi (mã `m_<run>_<ms>_…` mang lúc gửi); client không nhận khung nào tính là vô hạn |
| Comment live tới báo hiệu | p95 < 3 s | như trên, mã `lc_<run>_<ms>_…`; lúc gửi là lúc k6 đẩy vào máy giả hay ingest (cùng đồng hồ máy với máy giả, nên "theo đồng hồ của fakegraph") — không lấy `created_time` (giây) và không lấy chỉ số máy chủ `live_comment_to_signal_ms` (đếm cả comment lấy bù muộn, Task 171) |
| Bấm gửi tới Send API | p95 < 1 s | nội dung tin `lt <run> <ms bấm> …` so với lúc máy giả nhận lời gọi |
| API đọc | p95 < 200 ms, p99 < 500 ms | k6 `read_ms` của `fb_staff.js` |
| Mỗi sự kiện tới database đúng một lần | dòng = mã khác nhau = số đã giao | `conversation.messages` theo tiền tố mã của lượt; comment còn đòi thuộc phiên live (LV02) |
| `client_ref` tối đa một lần ở máy giả | trùng 0 | `message.metadata` của các lời gọi `{page}/messages` (HT02) |
| Tổng giữ không vượt tồn | mỗi dòng `held ≤ on_hand`, sổ giữ = tổng `held` | `inventory.stock_balances`, `stock_reservations` |
| `deadlocks` không tăng | cuối = đầu | `pg_stat_database` |
| Không việc `dead` | 0 | `platform.jobs` của tenant từ lúc bắt đầu |
| Tin gửi ra đã gửi (bài live) | không `in_doubt`, `failed`, `expired`, chờ | `conversation.messages` của nhân viên, tự động |
| Tự trở lại (bài chaos) | ≤ 2 phút | ba vai trò sống và healthcheck qua, không còn việc pending quá 2 s; kiểm từ 3 s sau khi gỡ hỏng |
| Luồng live nhận lại (bài chaos) | ≤ 15 s | sổ mở luồng của máy giả; sau ngắt PostgreSQL, luồng chưa từng đóng tính là nhận lại ngay |

Bài chaos không chấm độ trễ và "tin gửi ra đã gửi" (mất luồng SSE khi api chết, tin `in_doubt` khi giết worker giữa lúc
gửi là đúng thiết kế, CH18); mọi tiêu chí "đúng một lần", "không thiếu" vẫn chấm (HT01: database ngừng, ingest vẫn trả 200).

## Giới hạn

- Ghép khung theo hội thoại có thể ghép nhầm với khung của một thay đổi khác cùng hội thoại trong khoảng trễ (mỗi PSID
  nhận một sự kiện mỗi ~60–130 s): số đo có thể sớm hơn thật một chút ở vài cặp.
- Mọi nhân viên là owner phạm vi toàn shop (mỗi client SSE nhận báo hiệu của cả hai Page): nặng hơn kế hoạch "mỗi người
  một phạm vi Page".
- Máy Meta giả không phải Meta: độ trễ, giới hạn tốc độ, cách cắt luồng thật đo ở Task 181 (E4).

# Bộ thử tải M2 — tìm khách trên 1 triệu khách, lên đơn 10× (kế hoạch M2 Task 88, M2-31)

Đo tìm khách (DH02) trên dữ liệu cỡ năm thứ hai và đường lên đơn dưới tải 10× của 04 mục 1. Kết quả ghi vào
`docs/do-luong/m2.md`. Ngoài `make ci`.

## Chạy

Chủ shop 26/11: máy Mac không chạy tải nặng. Bài chạy ở bể từ xa (job `loadtest` của `.github/workflows/test-remote.yml`,
runner `ubuntu-24.04` 4 vCPU, PostgreSQL 18 của compose dev cấu hình mặc định như production):

```sh
CRM_CI_RUNNER=github CRM_CI_STATIC=false CRM_CI_LOADTEST=m2   CRM_CI_LABEL=<nhãn> scripts/ci/remote-test.sh <tree|commit> <base> <tệp gói rỗng>
CRM_CI_LOADTEST=m2:1m …     # chạy khói: tải 1 phút, cỡ nhỏ (20.000 khách, 50.000 đơn)
```

Trên một máy có compose dev riêng (không phải bộ chung `crm-shared`: script từ chối): `make db-up` rồi `make perf-m2`
(biến ở đầu `loadtest/m2_run.sh`).

## Tải gồm gì

| Phần | Ở đâu | Làm gì |
|---|---|---|
| Vai trò CRM | `m2_run.sh`, `fb_lib.sh` | `api`, `worker` dựng từ mã đang thử, chạy như tiến trình trên máy |
| Dữ liệu qua API | `loadkit m2seed` | tenant "Thử tải", 50 nhân viên owner (`loadkit seed`); 20 biến thể × 100.000 tồn, 5 biến thể × 10 tồn (để tranh), 10 mã giảm 10.000đ (2 mã tối đa 100 lượt); một tỉnh, một xã thử (`T01`, `T01001`) |
| Dữ liệu lớn | `tools/seedperf` | COPY bằng người quản trị: 1.000.000 khách, 1.300.000 SĐT giả `+8490xxxxxxx` không trùng, 5.000.000 đơn đã kết thúc (giao 85%, hoàn 7%, hủy 8%) với 8.000.000 dòng rải trong 24 tháng, chỉ số khách tính từ đơn; tenant "Thử tải B" 100.000 khách cùng các SĐT đó (RLS thật); rồi `VACUUM (ANALYZE)` |
| Tìm khách | `k6/m2-search.js` | 20 lượt/giây trong 5 phút `GET /api/v1/customers?q=`: đuôi SĐT 4–6 số 60%, SĐT đủ 20%, tên 15% (nửa họ tên đủ, nửa hai chữ cuối), mã khách 5% |
| Lên đơn | `k6/m2-orders.js` | 2 đơn/giây trong 5 phút + đợt 20 đơn/giây trong 30 giây (phút thứ 2), 30% dùng mã, 10% tranh 5 biến thể ít hàng, 50% khách có sẵn; song song danh sách đơn trang đầu 10 lượt/giây |
| Câu chậm | auto_explain của `crm_app` trong database đo | câu từ 50 ms ghi kế hoạch (`log_analyze`, `log_buffers`, `log_timing off`, không ghi giá trị tham số) vào log PostgreSQL; `loadkit m2slow` lấy ba câu chậm nhất |
| Số đo, chấm | `loadkit m2stats`, `loadkit m2report` | tồn, sổ giữ, mã, deadlock, `tx_retry_exhausted` trước và sau bài lên đơn; dung lượng database, bốn bảng lớn |

## Ngưỡng và cách đo

| Tiêu chí | Ngưỡng | Đo bằng |
|---|---|---|
| Tìm khách | p95 < 300 ms (DH02) | k6 `search_ms` (mọi loại); từng loại `search_{tail,phone,name,code}_ms` ở "Số đo khác" |
| Tìm khách lỗi | < 0,1% | k6 `search_bad` (mã khác 200) / `search_req` |
| Tìm SĐT đủ, mã: luôn thấy | không thấy = 0 | k6 `search_miss` (mọi SĐT, mã tìm đều đã nạp) |
| createOrder | p95 < 400 ms | k6 `create_ms` |
| listOrders | p95 < 200 ms | k6 `list_ms` |
| createOrder: có đơn tạo được, lỗi ngoài nghiệp vụ | < 0,1% (thêm so với kế hoạch: chạy khói 27/11 cho thấy mọi lượt lỗi mà các tiêu chí khác vẫn đạt) | k6 `create_bad` (không phải 201, 409 `out_of_stock`/`insufficient_stock`, 422 `coupon_*`) |
| listOrders lỗi | < 0,1% (thêm như trên) | k6 `list_bad` |
| Không bán vượt | mỗi dòng held ≤ on_hand; sổ giữ = tổng held | `inventory.stock_balances`, `stock_reservations` sau bài |
| Mã giảm giá | `used_count` = số lượt `active`, ≤ `max_uses` | `promotion.coupons`, `coupon_redemptions` |
| deadlocks | không tăng | `pg_stat_database` |
| `tx_retry_exhausted` | < 1% số lượt createOrder | `platform.metrics_minute` từ lúc bắt đầu bài lên đơn |
| Không việc dead | 0 | `platform.jobs` |

## Giới hạn

- Số đo trên bể GitHub là **số đo tạm**: đo lại trên server thử trước chạy thật.
- Nạp bằng COPY với `session_replication_role = replica` (trigger kiểm tổng, khóa ngoại tắt trong phiên nạp); dữ liệu tự
  đúng do seedperf dựng và test `internal/testkit/perfseed` kiểm lại bằng SQL. Không có vận đơn, lịch sử trạng thái, nhóm khách gộp.
- auto_explain bật trong lúc tải (đếm dòng, buffers ở mọi câu của crm_app): số đo có thể chậm hơn bản không đo một chút.

# Bộ thử tải M5 — flash sale và lượt đọc của website (kế hoạch M5 Task 219 / M5-36, ADR-42)

Đo lượt mua flash sale và lượt đọc của website dưới tải; kiểm không bán vượt bằng `crm verify-flash`. Kết quả ghi vào
`docs/do-luong/thu-tai-m5.md`.

## Chạy

Máy Mac không chạy tải. Bài chính chạy ở bể từ xa (job `loadtest` của `.github/workflows/test-remote.yml`):

```sh
CRM_CI_STATIC=false CRM_CI_LOADTEST=m5:all scripts/ci/remote-test.sh <tree|commit> <base> <tệp gói rỗng>
CRM_CI_LOADTEST=m5:flash_spike,web_read …      # một số bài
CRM_CI_LOADTEST=m5:flash_spike@20s …           # chạy khói: rút phần giữ tải (spike, sustained, web_read)
```

Trên một máy có compose dev riêng: `make dev` (cần profile `valkey`) rồi `loadtest/m5_run.sh all|<bài>[,<bài>…]` (biến ở
đầu tệp). Không chạy trên bộ chung `crm-shared` (bài đổi giới hạn, đẩy tải vào PostgreSQL dùng chung).

## Tải gồm gì

| Phần | Ở đâu | Làm gì |
|---|---|---|
| Vai trò CRM | `m5_run.sh`, `fb_lib.sh` | `ingest`, `api`, `worker` dựng từ mã đang thử, chạy như tiến trình; api nối hai Valkey của compose dev (`VALKEY_CACHE_URL`, `VALKEY_GATE_URL`); `RATE_LIMITS_JSON` nâng `web_key` (xem "Giới hạn"); cổng nhận vào flash, giới hạn theo SĐT, IP giữ mặc định |
| Dữ liệu thử | `loadkit seed`, `m5key`, `m5sale` | tenant "Thử tải", 2 nhân viên owner; khóa dịch vụ (`web.catalog.read`, `web.flash.buy`) qua API nhân viên; tỉnh, xã thử T01/T01001; mỗi bài một đợt flash sale mới (một món, tồn suất + 100), mở sau 20 s; `web_read` thêm 30 mẫu × 3 biến thể |
| Tải khách | `k6/m5/*.js` | gọi thẳng API web bằng khóa dịch vụ như website; mỗi người ảo một SĐT giả `09xxxxxxxx` và IP `10.x.y.z`; 503 → gửi lại cùng `Idempotency-Key` theo `Retry-After`, `X-Attempt` tăng (tối đa 10) |
| Số đo | `loadkit m5stats` (trước, sau), `crm verify-flash`, `loadkit m5report` | `pg_stat_database` (deadlock, kết nối), `platform.metrics_minute` (`flash_buy_ms`, `job_pickup_ms` của việc `event.*`, `tx_retry` theo SQLSTATE, `tx_retry_exhausted`, `flash_result`, `cache_hit`/`cache_miss`), bảng chấm |

| Bài | Kịch bản | Đạt khi |
|---|---|---|
| `flash_spike` (FS03) | 500 suất, giới hạn 1; lượt bấm lên 1.000/giây trong 10 s, giữ 60 s; người ảo = lượt mod 20.000 (ramping-arrival-rate thay 20.000 VU thật) | đúng 500 bought; `flash_buy_ms` p95 ≤ 300 ms (cận ngăn histogram); ngoài 503, 429 không mã lỗi nào; verify-flash đạt; deadlock +0; `tx_retry_exhausted` < 1% lượt mua |
| `flash_per_customer` (FS05) | một SĐT, 100 yêu cầu song song (mỗi yêu cầu một mã đơn), 429 không gửi lại | đúng 1 bought; verify-flash đạt |
| `flash_sustained` (FS16) | 15.000 suất; 50 người mới mỗi giây trong 5 phút, mỗi người mua rồi hoàn tất đơn | 15.000 bought, 15.000 hoàn tất; p95 ≤ 300 ms; nhận việc `event.*` p95 ≤ 1 s; không lỗi; verify-flash đạt; deadlock +0 |
| `flash_abandon` (FS06) | 500 suất, hạn giữ 5 phút; đợt một 500 người, số chẵn hoàn tất; phút 6 đợt hai đúng số người bỏ đi mua lại | 500 bought; đợt hai mua được = số suất mở lại; đợt hai không nhận sold_out ("hết oan"); verify-flash đạt |
| `flash_close_with_holds` (FS17) | 200 người giữ suất; giây 60 nhân viên đóng đợt; giây 70 số chẵn hoàn tất; chờ số lẻ hết hạn giữ | 200 bought; đóng được; 100 hoàn tất; verify-flash đạt (gồm (g) đối chiếu held của số dư) |
| `web_read` (FS02, FS13) | 500 yêu cầu/giây trong 10 phút, chia đều `catalog/products?ids` (10 mẫu), `availability` (10 biến thể), `flash-sales/current` | k6 `read_ms` p95 < 200 ms, p99 < 500 ms; không lỗi; trúng bộ đệm > 95% |

FS14 (ghi, không chấm): phân bố giây từ lúc mở tới lượt bấm đầu của người mua được (`bought_arrival_s`) so với mọi lượt
bấm (`arrival_s`).

## Giới hạn

- k6, CRM, PostgreSQL, Valkey cùng một máy: số đo tạm; đo lại trên server thử với k6 từ máy khác (Task 221 Step 4).
- `web_key` mặc định 1.200/phút (QĐ23) chặn ngay tải flash sale qua một khóa; bài nâng lên để đo CRM. Số cho production
  ghi ở "Việc phát hiện thêm" của Task 219.
- p95 phía máy chủ đọc từ histogram theo phút (cận ngăn 50, 100, 200, 300, 400, 500, 1000 ms…): "≤ 300 ms" là chấm chặt
  nhất có được.
