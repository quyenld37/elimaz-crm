# Task 529 — Mã 503 đúng khi PostgreSQL ngừng giữa tải (đường web)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (nền; task sửa phát hiện của Task 220 bước e, không có dòng riêng trong kế hoạch). Spec: 04 mục 13 bảng hỏng ("CRM hoặc PostgreSQL ngừng → website hiện tạm ngưng"), ADR-46, FS11. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Nguyên nhân (đọc mã trước khi viết test): lỗi nối database (`*pgconn.ConnectError`, SQLSTATE 57P01 khi PostgreSQL tắt, kết nối đứt giữa câu, quá hạn) không phải `db.ErrRetryLater` nên `writeHandlerError` (internal/httpapi/errors.go) rơi xuống nhánh cuối → 500 `internal` (kèm ghi `error_events`, log lỗi mỗi yêu cầu). Đường mua flash chạm database đầu tiên ở `idempotency.Acquire` (khóa dịch vụ đã đệm trong `KeyVerifier`), nên gần như mọi lượt trong lúc ngừng nhận 500. `writeAuthError` có nhánh 503 cho ErrRetryLater nhưng thiếu Retry-After.
- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm mới `db.Unavailable(err) bool` và `db.Unreachable(err) bool` (nhóm con "không tới được database", sau review lượt 2; 0 chỗ dùng cũ); thêm mới `testkit.StartSwitchProxy`/`testkit.SwitchProxy` (chuyển từ `switchProxy` của `tests/e2e/stack_test.go`, 6 chỗ dùng trong tests/e2e đổi theo, commit cấu trúc riêng). Hàm nội bộ đổi hành vi: `writeHandlerError` (17 chỗ gọi trong httpapi, mọi route; gồm `HealthDeep`: database ngừng → 503 retry_later thay 500, cảnh báo vẫn do bộ kiểm từ ngoài), `writeAuthError` (2 chỗ), `WriteTxWith` (idempotency.go: trả lại khóa chống trùng cả khi lỗi `db.Unavailable`, sau review lượt 1); thêm `writeRetryLater`, `logHandlerError` (không xuất; tách phần ghi lỗi cũ của `writeHandlerError` để nhánh 503 "database còn sống" cũng ghi). Không đổi chữ ký nào.
- Gói phụ thuộc ngược (go list): `internal/platform/db` 48 gói nhập (chỉ thêm hàm, không đổi hành vi cũ); `internal/httpapi` 6 gói (cmd/crm, tests/e2e…); `internal/testkit` 64 gói (chỉ thêm kiểu).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`; mã `retry_later` đã có trong `x-error-codes`. Hành vi: mọi operation, khi database ngừng/quá hạn, trả 503 `retry_later` + `Retry-After: 1` thay vì 500 `internal`.
- Chỗ dùng trên màn hình: console đã có câu cho `retry_later` (errors.ts); không sửa màn hình.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race -run` các test mới của `internal/platform/db` (`TestUnavailable_*`), `internal/httpapi` (`TestDBUnavailable*`), test cũ bị chạm `TestServiceKeyVerifierErrorNotLogged`; vet/build tests/e2e (proxy chuyển chỗ); ci-task chạy cả gói db, httpapi, testkit và gói phụ thuộc. Bài thử tải `CRM_CI_LOADTEST=m5:chaos` (CHAOS_STEPS=e) trên GitHub.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.TransientCode`, `db.ErrRetryLater`, `cache.ErrUnavailable`); grep `ConnectError|57P01|53300` → `webhooks.unexpectedDBError` (ingest.go, phân loại "database ngừng" để không ghi error_events: ConnectError, SQLSTATE 08/53/57); grep proxy → `switchProxy` trong `tests/e2e/stack_test.go` (gói e2e, không nhập được từ gói khác).
- Dùng lại / viết mới vì: proxy cắt database — dời `switchProxy` sang `internal/testkit` (dùng chung e2e và test gói, không chép). Bộ phân loại — viết mới `db.Unavailable` ở `platform/db` (chủ của lỗi pgx); `TransientCode` là lỗi chạy lại được trong transaction, khác mục đích (không được chạy lại lỗi nối trong RetryTx: database ngừng 30 s). `webhooks.unexpectedDBError` cùng ý nhưng hẹp hơn (không tính kết nối đứt giữa câu, quá hạn); không gộp trong task này để không đổi hành vi webhook (vùng cao khác) — ghi ở Việc phát hiện thêm.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.7: không áp dụng: không có dữ liệu, bảng, transaction mới; không ghi bảng nào.
- 5.8: module không đổi; phân loại lỗi pgx ở `platform/db`, đổi sang HTTP chỉ ở `httpapi/errors.go` (luật "một chỗ").
- 5.9: không có đường ghi mới; 503 không lưu vào `idempotency_keys` (như ErrRetryLater); `WriteTxWith` trả lại khóa ngay (Release chỉ xóa dòng `running` đúng token: COMMIT đã xong thì dòng `done` không bị xóa, gửi lại nhận kết quả đã lưu). Database ngừng hẳn thì Release cũng hỏng → khóa tự hết giữ chỗ 60 s.
- 5.10: không áp dụng: không chạm Valkey.
- 5.11: không thư viện mới (pgx/pgconn đã có).
- 5.12: theo mẫu lỗi của CLAUDE.md: kiểm bằng `errors.As`/`errors.Is`, không so chuỗi.
- 6.1, 6.3, 6.5, 6.10, 6.11: không áp dụng: không đổi câu lệnh, khóa, tiền, quy luật tồn.
- 6.2: 503 không lưu theo khóa; lần gửi lại cùng khóa chạy từ đầu. Lượt đã commit trước khi mất kết nối ở bước commit: client không biết → gửi lại cùng khóa nhận kết quả đã lưu (ADR-41), không đơn trùng.
- 6.4: lỗi nối KHÔNG chạy lại trong RetryTx (database ngừng thì chạy lại chỉ thêm tải); trả 503 + Retry-After: 1 để bên gọi (website) lùi. Lỗi nghiệp vụ, 23xxx, lỗi không phải mạng/SQLSTATE 08/53/57 giữ nguyên mã cũ (test ca âm).
- 6.6, 6.7: không áp dụng: không đổi việc nền, không gọi bên ngoài (worker vẫn coi lỗi là một lần thử).
- 6.8: tiến trình api còn sống, database chết: transaction tự lùi phía PostgreSQL; ca commit không chắc như 6.2.
- 6.9: quá hạn (`context.DeadlineExceeded`, errTimeout của pgconn, 57014) → 503, không treo.
- 6.12: không ghi dữ liệu cá nhân mới. Không tới được database (`db.Unreachable`: mất kết nối, 08, 57P01/02/03, 53300) → 503 không log, không ghi error_events (bảng đó không ghi được; spec 04 dòng "Gom lỗi" giao cho bộ kiểm từ ngoài). Database còn sống mà quá tải/quá hạn (57014, lớp 53 khác, hạn ctx) → 503 VÀ vẫn log + ghi error_events như trước (câu chậm, đầy đĩa phải có cảnh báo).
- 6.13: không áp dụng: không đổi quyền; 503 chỉ trả sau bước xác thực/quyền đã chạy hoặc khi chính bước đó cần database.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) Tái hiện mã thật khi PostgreSQL ngừng giữa tải (proxy cắt kết nối của pool api) | `TestDBUnavailable_WebRouteChain` (ngừng giữa transaction của handler; ngừng trước khóa chống trùng; trở lại → 200) | `go test -race -count=1 -run '^TestDBUnavailable_' ./internal/httpapi/` trước khi sửa | đỏ: `mã 500, mong 503 ({"error":"internal"…})`, log `lỗi xử lý yêu cầu` + `ghi error_events thất bại` (`mở transaction: conn closed`, `ghi nhận khóa chống trùng: … failed to receive message: unexpected EOF`) |
| (2) Phân loại đúng từng lỗi pgx/pgconn thật: connect refused, proxy ngừng lúc nối, đứt giữa câu (broken pipe/EOF), "conn closed", pool + RetryTx qua proxy cắt (không chạy lại, không ErrRetryLater), 57P01 admin shutdown (`pg_terminate_backend`), 57014, context deadline; kể cả khi bọc `%w` | `TestUnavailable_Connect` (refused, proxy ngừng), `TestUnavailable_BrokenMidQuery` (đứt giữa câu, conn closed), `TestUnavailable_PoolRetryTx`, `TestUnavailable_ServerSide` (57P01, 57014, deadline) | `go test -race -count=2 -run '^TestUnavailable_' ./internal/platform/db/` | 6 test PASS ×2, `ok … 2.881s` |
| (2) Không biến lỗi khác thành 503: 28P01 sai mật khẩu, 22P02, 23505, ErrNoRows (thật); 40P01, 23505, context.Canceled, ErrRetryLater, lỗi lạ (tổng hợp); 08006, 53300, 57P03, DeadlineExceeded → có | `TestUnavailable_Connect` (28P01), `TestUnavailable_StatementErrors` (22P02, 23505, ErrNoRows), `TestUnavailable_Synthetic` (14 ca, kiểm cả `Unavailable` và `Unreachable`); mọi ca lỗi thật cũng kiểm `Unreachable` (57014, deadline: false) | như trên | `ok` |
| (2) Lớp HTTP: database ngừng → 503 `retry_later` + `Retry-After: 1`, không log lỗi nội bộ; trở lại → 200. 57014 thật trong transaction của handler → 503, CÓ ghi lỗi, và gửi lại CÙNG khóa → 200 (trước khi sửa `WriteTxWith`: 409 `request_in_progress`, đã thấy đỏ) | `TestDBUnavailable_WebRouteChain` | `go test -race -count=1 -run '^(TestDBUnavailable_.*\|TestServiceKeyVerifierErrorNotLogged)$' ./internal/httpapi/` | `--- PASS` cả 4 test, `ok … 2.758s` (bản sau review lượt 2) |
| (2) Bảng đổi lỗi: 6 loại database ngừng/quá hạn (refused, 57P01, 53300, 57014, 53100, deadline) + ErrRetryLater → 503 có Retry-After; chỉ refused, 57P01, 53300 không ghi lỗi, 57014, 53100, deadline vẫn ghi; hold_expired 422, ErrFlashRetry 503, 23505 và lỗi lạ 500 (có ghi) giữ nguyên | `TestDBUnavailable_HandlerErrorMapping` (11 ca, cột `logged`) | như trên | PASS |
| (2) Bước xác thực (bộ kiểm khóa dịch vụ đọc database): 57P01 và ErrRetryLater → 503 có Retry-After (trước: thiếu Retry-After / 500) | `TestDBUnavailable_AuthStep`; lỗi lạ vẫn 500 `TestServiceKeyVerifierErrorNotLogged` | như trên | PASS |
| (3) Chạy lại bài thử hỏng M5 (5 bước, bước e dừng PostgreSQL 30 s) | `loadtest/m5_run.sh chaos` (k6 đếm lỗi theo mã: `buy_err_500/4xx/5xx_other/net`) | `CRM_CI_LABEL=task-529-lt CRM_CI_STATIC=false CRM_CI_LOADTEST=m5:chaos scripts/ci/remote-test.sh <tree> HEAD <tệp rỗng>` | run 46158491123 `REMOTE-TEST XANH`, "KẾT QUẢ: mọi bài ĐẠT" (chạy trên bản trước review; hai lần sửa sau review chỉ đổi việc trả lại khóa và việc GHI lỗi cho nhóm database còn sống, mã HTTP khi dừng PostgreSQL không đổi); bước e: bought 500, lỗi khác **0 / 126.830** (Lần 2: 15.168 / 28.865), 503 113.586, verify-flash ĐẠT, đơn trùng 0; bước d: 30 lỗi đều "mất kết nối" (api bị giết). `docs/do-luong/thu-tai-m5.md` Lần 3, `docs/RUNBOOK.md` |
| Cổng kiểm của task (cổng tĩnh + test gói đổi và gói phụ thuộc) | ci-task | `scripts/agent/ci-task.sh` | lượt 1 (run 46160164160): mọi phần test xanh, cổng tĩnh đỏ vì lint của hai tệp test mới (contextcheck, gocognit) → đã sửa (tách test, mỗi test tự lấy `t.Context()`). Lượt 2 (run 46161584000): static xanh, 5/6 phần test xanh; phần 2 đỏ ở `TestDashboard_ExplainNoSeqScan`, `TestCarriersExplainNoSeqScan` (gói reporting, test EXPLAIN chập chờn đã biết, không chạm mã đổi; lượt 1 cùng mã chạy đã xanh). Chạy lại gói reporting: `remote-test.sh <tree> HEAD ./internal/modules/reporting/...` run 46162309379 `REMOTE-TEST XANH`. Lượt 3 (sau sửa review: `WriteTxWith` trả lại khóa, test gửi lại cùng khóa): run 46163706511 `CI-TASK XANH` (static + 6/6 phần test xanh, so với điểm tách cbcb88e5). Lượt 4 (sau review lượt 2: `db.Unreachable`, nhóm database còn sống vẫn ghi lỗi): run 46165156167 `CI-TASK XANH` (static + 6/6 phần test) |
| Proxy dời sang testkit không đổi e2e | `TestDatabaseOutageKeepsWebhooks`, test M3 chaos (biên dịch) | `go vet ./internal/testkit/ ./tests/e2e/` | VET-OK (e2e không chạy trong task, điều phối chạy sau gộp) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- k6 `common.js` đếm thêm lỗi khác theo nhóm mã (`buy_err_500`, `buy_err_4xx`, `buy_err_5xx_other`, `buy_err_net`), `loadkit m5report` in các số đó: cần để bước (3) ghi được mã thật (Task 220 chưa ghi mã cụ thể). Không đổi tiêu chí chấm.

## Quyết định thay chủ shop

- QĐ-529-1: lỗi "database ngừng" trả 503 `retry_later` + `Retry-After: 1` cho MỌI route (lớp lỗi chung `writeHandlerError`), không chỉ đường web. Vì: cùng một nguyên nhân, cùng ý nghĩa "thử lại sau"; màn hình đã có câu cho `retry_later`; 500 kèm ghi `error_events` khi database ngừng là sai (bảng đó không ghi được). Phương án khác: chỉ áp cho route `x-auth: service_key` — phức tạp hơn, nhân viên vẫn thấy "lỗi nội bộ" khi database ngừng. Đảo ngược: bỏ hai nhánh `db.Unreachable`/`db.Unavailable` trong `writeHandlerError` và `db.Unreachable` trong `writeAuthError`.
- QĐ-529-2: không chạy lại lỗi nối trong `RetryTx` (database ngừng 30 s thì 3 lần chạy lại chỉ thêm tải, ADR-46 chỉ nói lỗi khóa/tuần tự hóa); bên gọi lùi theo `Retry-After`. Có test khóa điều này (ca "pool and RetryTx through cut proxy").
- QĐ-529-3: lỗi nối mà máy chủ đã trả SQLSTATE ngoài lớp 08/53/57 (28P01 sai mật khẩu, 3D000 không có database) KHÔNG coi là "ngừng" → vẫn 500 và log: đó là lỗi cấu hình, không được che thành "thử lại". `context.Canceled` (khách đóng kết nối) giữ đường cũ.
- QĐ-529-5 (sau review lượt 2): tách hai mức. `db.Unreachable` (không tới được database) → 503 im lặng; `db.Unavailable` mà không Unreachable (57014, 53100/53200/53400, hạn ctx: database còn sống) → vẫn 503 thử lại nhưng log + ghi `error_events` như lỗi ứng dụng. Vì: spec 04 "Gom lỗi" chỉ miễn lỗi "database ngừng"; câu chậm, đầy đĩa phải còn cảnh báo. Phương án khác: trả 500 cho nhóm này như cũ — website lại không hiện "tạm ngưng" khi database quá tải. `WriteTxWith` trả lại khóa cho cả hai nhóm.
- QĐ-529-4: bước (3) chạy đủ 5 bước `m5:chaos` (không riêng bước e: workflow không nhận `CHAOS_STEPS`) để có bằng chứng a–d không đổi.

## Việc phát hiện thêm

- `webhooks.unexpectedDBError` (internal/platform/webhooks/ingest.go) phân loại "database ngừng" hẹp hơn `db.Unavailable` (không tính kết nối đứt giữa câu, quá hạn): có thể dùng `db.Unavailable` để bớt ghi thử `error_events` khi database ngừng. Không đổi trong task này (hành vi webhook, vùng rủi ro cao khác).
- Lượt đã ghi nhận khóa chống trùng rồi database ngừng HẲN: trả khóa (`releaseClaim`) cũng hỏng → khóa giữ chỗ 60 s, lần gửi lại cùng khóa nhận 409 `request_in_progress`. Website nên coi 409 `request_in_progress` là "thử lại" (như 503). Bài thử Lần 3 không gặp (0 lỗi khác), ghi để soát khi làm website (M5-34).
- pgx không đặt hạn nối mặc định: nếu database "biến mất" mà không trả RST (máy chủ khác, mạng), nối có thể treo tới hạn TCP. Bài thử chạy cùng máy nên nối bị từ chối ngay. Nên cân nhắc `connect_timeout` trong `CRM_DATABASE_URL` khi lên server thật.

## Reviewer

Kết luận: ĐẠT.

- Commit cấu trúc (df0e28cf, dời `switchProxy` → `testkit.SwitchProxy`): ĐẠT. Chép nguyên văn, đủ 6 chỗ dùng, import khớp, `go vet` qua. Ghi chú: hai goroutine `cp` trong `pipe` dùng `go` trần nhưng có `done` chờ, giống bản cũ.
- Commit hành vi, lượt 1: CẦN SỬA. `WriteTxWith` chỉ trả lại khóa chống trùng khi gặp `ErrRetryLater`, nên khi gặp lỗi `db.Unavailable` thì lần gửi lại cùng khóa nhận 409 tới 60 s. Đã sửa và thêm ca test gửi lại cùng khóa (thiếu phần sửa thì test đỏ, ra 409).
- Lượt 2 (reviewer mới): CẦN SỬA. Ba loại lỗi 57014, lớp 53 và hạn ctx xảy ra khi database vẫn sống, nhưng đang trả 503 mà không log, không ghi error_events. Điều này trái spec 04 mục "Gom lỗi". Đã sửa: tách `db.Unreachable` ra; nhóm database còn sống vẫn được ghi lỗi (QĐ-529-5); test có thêm cột `logged`.
- Lượt 3: ĐẠT, không còn phát hiện phải sửa. Reviewer xác nhận việc trả lại khóa an toàn cả khi COMMIT đã xong: Release chỉ xóa dòng `running` đúng token. Phân loại 28P01 đúng QĐ-529-3. Bài thử tải Lần 3 vẫn dùng được, vì lần sửa sau đó chỉ đổi việc ghi lỗi, không đổi mã HTTP. Ghi chú không bắt buộc đã xử lý: câu "Đảo ngược" của QĐ-529-1. Ghi chú còn lại đã có trong "Việc phát hiện thêm": database treo không trả RST thì lỗi rơi vào nhóm có ghi, mỗi yêu cầu một dòng log (xem `connect_timeout`); 57014 dưới tải flash cũng mỗi lượt một dòng log.
