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
