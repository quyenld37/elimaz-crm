# Task 314 — Thứ tự theo đối tượng không chặn sự kiện vào (M0-15b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `queue.Policy.BlockOnDead` bị **xóa**, thay bằng `queue.Policy.OrderMode` (hằng mới `queue.OrderStrict` = mặc định, `queue.OrderBestEffort`). Chỗ dùng `BlockOnDead` (grep + findReferences): `queue/kind.go`, `queue/pgstore.go`, `queue/enqueue_test.go`, `queue/ordering_test.go`, `events/consumers.go` (đặt `BlockOnDead = true` → bỏ, mặc định đã là strict), `events/emit_test.go` (2 chỗ), chú thích `events/emit.go`, `webhooks/inbound.go` (`InboundKind` đặt `OrderMode: OrderBestEffort`). Sửa hết trong task này.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/platform/{commands,events,schedules,webhooks}` nhập `queue`.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: `queries/platform/jobs.sql` câu `InsertJob` (ghi `order_mode` thay `block_on_dead`) và `ClaimJobs` (luật chặn theo `order_mode`); `make gen` sinh lại `internal/platform/internal/store`. Bảng `platform.jobs`: migration **0012** pha mở rộng — thêm cột `order_mode text NOT NULL DEFAULT 'strict'` (CHECK hai giá trị), điền lại dòng cũ (`block_on_dead = false` có `order_key` → `best_effort`), thêm index duy nhất một phần `(tenant_id, order_key) WHERE status = 'running' AND order_mode = 'best_effort'`. Cột `block_on_dead` ngừng dùng từ bản này (không ghi, không đọc); xóa ở pha thu hẹp sau (ghi ở Việc phát hiện thêm).
- Test sẽ chạy: cả bộ `go test -race ./internal/platform/queue/...` (vùng rủi ro cao), `./internal/platform/{webhooks,commands,events,schedules,selftest}/...`, `./internal/httpapi/...`, `./cmd/crm/...`, `./internal/arch/...` (nếu có), `make ci` phần lint/sqlc vet.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `internal/platform/queue` (lõi duy nhất, spec C1), `testkit.ExplainAsApp` + `testkit.RequireNoSeqScan` (kiểm kế hoạch), `db.RetryTx`/`db.TransientCode`, bộ đếm `jobs_dead` có sẵn của Worker (báo động việc dead).
- Dùng lại / viết mới vì: sửa ngay trong lõi `queue` (không thêm cơ chế thứ hai — spec 04 mục 5.1 "một lõi"); dùng lại `ExplainAsApp`/`RequireNoSeqScan` cho tiêu chí (f), `jobs_dead` làm báo động cho tiêu chí (b), (d); helper test có sẵn (`env`, `fh`, `drain`). Viết mới: hằng `OrderStrict/OrderBestEffort` thay `BlockOnDead` (cách cũ bị xóa, không bọc điều kiện).

## Soát thiết kế

- 5.1 Chủ dữ liệu: `platform.jobs` thuộc platform/queue; chỉ gói queue ghi. Đạt.
- 5.2, 5.3: không ghi/đọc bảng module khác. 5.4, 5.5: không áp dụng: không thêm luồng nghiệp vụ.
- 5.6: không áp dụng: không gọi ra ngoài.
- 5.7: `platform.jobs` là bảng nền tảng không RLS (Global Constraints, migration 0007); cột mới không đổi điều đó.
- 5.8, 5.9: không áp dụng: không có HTTP.
- 5.10, 5.11: không Valkey, không thư viện mới. 5.12: theo mẫu hàng đợi Task 11.
- 6.1 Đồng thời: luật best_effort "chỉ việc đang `running` mới chặn" một mình không đủ cho tiêu chí (c): hai worker đọc ảnh chụp cũ có thể cùng nhận hai việc của một hội thoại (việc đầu vừa tới giờ thử lại đúng lúc việc sau đang được nhận). Cách đúng: (i) câu claim chặn khi có việc cùng khóa đang `running` (bất kể số thứ tự) hoặc việc **đứng trước** đang `pending` đã tới hạn (để việc tới hạn chạy theo thứ tự và không hai việc cùng khóa trong một lần nhận); (ii) index duy nhất một phần trên việc `running` best_effort là chốt chặn cuối ở database: lần nhận thua cuộc đua gặp `23505` trên đúng index đó → Worker coi như không nhận được gì (transaction lùi, việc vẫn `pending`, lượt sau nhận). Strict giữ nguyên câu cũ (mọi việc trước chưa `done`/`expired` chặn, kể cả `dead`).
- 6.2: chống trùng `dedupe_key` không đổi. 6.3 Thứ tự khóa: claim vẫn chỉ khóa dòng `jobs` (nhóm 13 ADR-46, không đổi); chờ trên index duy nhất chỉ giữa hai transaction claim ngắn, nếu deadlock (`40P01`) thì `RetryTx` chạy lại. Không đổi `docs/adr/0046-thu-tu-khoa.md`.
- 6.4, 6.5: READ COMMITTED + câu có điều kiện + index duy nhất; `RetryTx` như cũ.
- 6.6: lịch thử lại, `dead` + `jobs_dead` không đổi; best_effort `dead` vẫn đếm `jobs_dead` (báo động), chỉ không chặn.
- 6.7: không áp dụng. 6.8: giữ chỗ hết hạn về hàng chờ như cũ (sweep chạy trước claim), nên việc `running` mồ côi chặn hội thoại tối đa tới hết giữ chỗ 90 s.
- 6.9: không đổi. 6.10: không áp dụng: không tiền.
- 6.11 Quy luật: thêm test đồng thời (e) 4 worker `-race`; bộ rapid cũ (`property_test.go`) chạy lại cho strict.
- 6.12: không thêm log; `last_error` không đổi. 6.13: không áp dụng: không quyền.

## Bằng chứng

Bước FAIL (đã có cột order_mode, câu nhận việc chưa đổi): `go test -race -count=1 -run 'BestEffort|ClaimPlan|Strict|Enqueue' ./internal/platform/queue/` →
`TestBestEffortRetryingHeadDoesNotBlock: tin 2, 3 phải xong ngay: {Status:pending Attempts:0 …}`,
`TestBestEffortRunningLaterBlocksDueEarlier: hết 10 s chờ handler bắt đầu`, `TestBestEffortClaimRaceLosesQuietly: câu nhận việc không chờ khóa của index duy nhất`,
`TestBestEffortConcurrentConversation: tin 5: pending (muốn done), gọi 0 lần …`; webhooks `TestSplitFailedEventDoesNotBlockSameKey` FAIL; events `TestDeadEventBlocksLaterEvents` FAIL (strict chưa chặn dead ở bước trung gian).
Kiểm test chốt chặn thật sự bắt lỗi: tắt tạm nhánh `lostOrderRace` → `TestBestEffortClaimRaceLosesQuietly` đỏ với `duplicate key value violates unique constraint "jobs_best_effort_running" (SQLSTATE 23505)`; bật lại → xanh.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (a) tin 1 chờ thử lại 6 giờ, tin 2, 3 xử lý ngay; tin 1 chạy lại đúng lịch | `queue.TestBestEffortRetryingHeadDoesNotBlock`; `webhooks.TestSplitFailedEventDoesNotBlockSameKey` (qua đường tách gói thật: 2 done, 1 pending attempts 1) | `go test -race -count=1 ./internal/platform/queue/...`, `./internal/platform/webhooks/...` | `ok elimaz.com.vn/crm/internal/platform/queue 335.397s` (cùng các gói khác) và chạy riêng sau khi dọn test `ok …/queue 37.766s`; `ok …/webhooks 6.425s` |
| (b) tin 1 dead → tin sau vẫn chạy, có báo động | `queue.TestBestEffortDeadDoesNotBlockButAlerts` (đếm `jobs_dead` = 1, tin 3 đến sau cũng chạy) | như trên | ok |
| (c) tin 1 running → tin 2 chờ; không hai worker cùng một hội thoại | `TestBestEffortRunningBlocksSameConversation`, `TestBestEffortRunningLaterBlocksDueEarlier` (việc sau running chặn việc trước vừa tới lịch), `TestBestEffortClaimRaceLosesQuietly` (cuộc đua ảnh chụp: index duy nhất chặn, Worker không lỗi, việc còn pending attempts 0) | như trên; lặp `-count=5` | ok |
| (d) strict giữ nguyên: dead chặn việc sau + báo động; `order.returned` không chạy trước `order.delivered` | `queue.TestStrictDeadBlocksAndAlerts` (mode "" và "strict"), `TestRetryingHeadBlocks`, `TestOrderingBySeq`, `TestPropertyQueue` (rapid); `events.TestDeadEventBlocksLaterEvents`, các test thứ tự của events | `go test -race -count=1 ./internal/platform/events/...` | `ok elimaz.com.vn/crm/internal/platform/events 131.912s` |
| (e) 50 tin một hội thoại, 10 lỗi ngẫu nhiên, 4 worker song song `-race` → mọi tin không lỗi xong trong một vòng, không xử lý hai lần | `queue.TestBestEffortConcurrentConversation` (đếm Handle mỗi tin = 1, cờ chồng lấn handler = false) | `go test -race -count=5 -run 'BestEffortConcurrent|BestEffortRunning|BestEffortClaimRace' ./internal/platform/queue/` | `ok …/queue 13.601s` |
| (f) EXPLAIN câu claim không Seq Scan trên `jobs` với 100.000 dòng | `queue.TestClaimPlanUsesIndexes` (đọc đúng hằng `claimJobs` sqlc sinh, `testkit.ExplainAsApp` + `RequireNoSeqScan`) | như trên | ok |
| Chính sách: Policy rỗng → strict; OrderMode lạ → ErrInvalidJob; ghi đúng cột | `TestEnqueueDefaultsAndValidation`, `TestEnqueueStoresPolicyAndPayload`; `webhooks.TestInboundKindAndOptions`; `events` test ConsumerKind | như trên | ok |
| Gói phụ thuộc ngược | commands, schedules, selftest, tenancy, db, idempotency, metrics, httpapi, tests/arch, tests/e2e | `go test -race -count=1 ./internal/platform/... ./internal/httpapi/... ./cmd/... ./tests/...` | tất cả `ok` (commands 168.515s, schedules 130.835s, httpapi 120.453s, tests/arch 20.527s, tests/e2e 55.670s) |
| Cổng kiểm | lint, sqlc vet, gen-check, jscpd, catalog | `golangci-lint run …` (0 issues), `make sqlc-vet` (ok), `make gen-check` (không chênh sau khi stage), `jscpd` (0 đoạn trùng mới), `make catalog` (đã sinh lại) | đạt |

## Test bị xóa hoặc tắt

- `queue.TestDeadBlocksWhenBlockOnDead` → đổi tên `TestStrictDeadBlocksAndAlerts` (cùng câu kiểm, thêm ca mode rỗng và kiểm `jobs_dead`), vì `BlockOnDead` bị xóa.
- `queue.TestDeadDoesNotBlockWithoutBlockOnDead` → thay bằng `TestBestEffortDeadDoesNotBlockButAlerts` (cùng câu kiểm dead rồi done, thêm báo động và tin đến sau), vì `BlockOnDead` bị xóa.
- Không nới test nào. `make test-guard` so với `main` báo các tệp test của task khác (main đã đi trước nhánh này: identity, config, tests/arch/viewsdoc, tests/probe); không dòng nào thuộc queue, events, webhooks.

## Khác kế hoạch

- `OrderMode` đặt ở `queue.Policy` (theo loại việc), không ở `NewJob`: kế hoạch ghi "`queue.Job` (hoặc `EnqueueInput`)". Lý do ở mục Quyết định thay chủ shop.
- Luật best_effort chặt hơn chữ của kế hoạch một chút ("chỉ việc đang running mới chặn"): việc **đứng trước** đang pending **đã tới hạn** cũng chặn, để giữ thứ tự giữa các tin đã tới hạn và để mỗi lần nhận chỉ lấy một việc của một hội thoại. Việc chờ thử lại (chưa tới hạn) và dead vẫn không chặn, đúng tiêu chí (a), (b), (e).
- `docs/adr/0046-thu-tu-khoa.md` không đổi: claim vẫn chỉ khóa dòng `platform.jobs`.

## Quyết định thay chủ shop

1. **OrderMode ở Policy của loại việc** (chọn) thay vì trường của từng việc. Vì: chế độ do nhóm việc quyết định (spec 04 mục 5.1: `inbound.*` khác `event.*`), thay đúng chỗ của `BlockOnDead` cũ (cùng ý nghĩa, theo luật "chuyển cách mới thì xóa cách cũ"), không để hai việc cùng hội thoại mang hai chế độ khác nhau. Phương án khác: `NewJob.OrderMode` — linh hoạt hơn nhưng bên gọi có thể đặt sai. Đảo ngược dễ: thêm trường vào NewJob, cột đã theo từng dòng.
2. **Chốt chặn bằng index duy nhất một phần** `jobs_best_effort_running` + Worker coi `23505` trên đúng index đó là "không nhận được gì". Vì: câu NOT EXISTS dưới READ COMMITTED không tự chặn được cuộc đua khi việc trước vừa tới lịch đúng lúc việc sau đang được nhận; khóa toàn cục cho câu nhận việc làm chậm mọi worker. Phương án khác: `pg_advisory_xact_lock` theo khóa thứ tự (khó trong một câu nhận nhiều việc).
3. **Điền lại dòng cũ** trong migration: `block_on_dead = false` có `order_key` → `best_effort` (đúng nghĩa cũ: chỉ sự kiện vào dùng false). Chưa chạy production nên chỉ là an toàn.
4. **Giữ cột `block_on_dead`** (pha mở rộng), không ghi, không đọc; xóa ở pha thu hẹp sau.

## Việc phát hiện thêm

- Pha thu hẹp sau: migration xóa cột `platform.jobs.block_on_dead` (kèm dòng `-- contract:`), khi bản chạy thật đã dùng `order_mode`.
- Strict (sự kiện nội bộ) vẫn có thể cho hai việc cùng đối tượng chạy song song nếu một việc có `order_seq` nhỏ hơn được ghi **sau** khi việc lớn hơn đã running. Với `events.Emit` không xảy ra (Seq tăng dưới khóa dòng của đối tượng), nên không sửa trong task này.
- Lần nhận thua cuộc đua (23505 trên `jobs_best_effort_running`) lùi cả lô; các việc của hội thoại khác trong lô đó chờ tới Poll kế tiếp (tối đa 2 s). Có thể gọi `w.signal()` trong nhánh đó nếu cần nhận lại ngay.
- Nếu sau này triển khai cuốn chiếu (bản cũ và bản mới cùng chạy claim), pha mở rộng cần ghi cả `block_on_dead` lẫn `order_mode`. Hiện chưa chạy production nên không cần.
- Thứ tự số migration: 0012 có trước 0010, 0011 (dành cho Task 19, 20). Khi các task đó gộp sau, goose cần cho phép migration thiếu số hoặc đổi số; điều phối viên kiểm khi gộp.

## Reviewer

Kết luận: ĐẠT (agent reviewer, một lượt, trên `git diff --cached`). Không có phát hiện ảnh hưởng tính đúng. Reviewer đã kiểm: strict tương đương câu cũ với `block_on_dead = true` (đúng điều kiện events luôn đặt) và không loại việc nào đang dùng `BlockOnDead = false` bị đổi lặng lẽ; best_effort không thể có hai việc cùng (tenant, order_key) running (trong một lần nhận nhờ luật "việc trước tới hạn chặn", giữa hai lần nhận nhờ index duy nhất; chỉ `ClaimJobs` đặt running); `lostOrderRace` chỉ khớp 23505 trên đúng index; không có vòng chờ gây kẹt; migration đúng luật; test (a)–(f) kiểm đúng tiêu chí.
Ghi chú không bắt buộc (không sửa, ghi ở Việc phát hiện thêm): (1) lần nhận thua cuộc đua lùi cả lô, các việc khác trong lô chờ tới Poll kế tiếp (tối đa 2 s); (2) triển khai cuốn chiếu hai bản cùng chạy cần ghi cả hai cột; (3) một order_key trộn hai chế độ có thể kẹt trên lý thuyết, thực tế không xảy ra vì tiền tố khóa khác nhau; (4) số migration 0012 trước 0010, 0011.
