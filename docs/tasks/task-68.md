# Task 68 — M2-11: Gộp khách, hoàn tác, gợi ý có thể là cùng một khách

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 68 = M2-11). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu có sẵn. Thêm trong `customer`:
  `Merge`, `Candidate`, `MergeEvent`, `Writer.MergeCustomers`, `Writer.UndoMerge`, `GetMerge`, `MergeCandidates`, hằng
  `TopicCustomerMerged`, `TopicCustomerMergeUndone`, `MatchedByPhone`, `MatchedByStaff`. Hàm riêng `Writer.audit` gọi
  qua hàm riêng mới `Writer.auditEntity` (thêm loại đối tượng) — `audit` giữ chữ ký, 0 nơi gọi phải đổi. Sentinel
  `ErrMergeSameCustomer`, `ErrMergeNotLatest`, `ErrMergeWindowExpired`, `*MergeNotLatestError` đã khai ở Task M2-02
  (`errors.go`) và đã đổi sang HTTP ở `internal/httpapi/errors.go` (`customerError`, `detailedError`): chỉ dùng.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/promotion`,
  `internal/testkit/shiptest` nhập `customer`; không gói nào phải sửa vì chỉ thêm.
- Operation OpenAPI bị chạm, oasdiff: `mergeCustomer`, `undoCustomerMerge`, `listCustomerMergeCandidates` — hợp đồng
  có từ Task M2-02 (đang trả 501 qua bản sinh), task này chỉ hiện thực handler trong `handlers_customer_merge.go`;
  `openapi.yaml` không đổi, oasdiff không có gì.
- Chỗ dùng trên màn hình: chưa có màn hình gộp khách trong `console` (grep `merge` trong `console/src`: 0); không đổi.
- Câu sqlc, bảng, pha migration: tệp mới `queries/customer/merge.sql` (khóa nhóm, đổi `root_id`, chèn/đọc/khóa
  `customer_merges`, tìm lần gộp chặn), `queries/customer/candidates.sql` (gợi ý; đọc `ord.orders_v` — ngoại lệ chỉ đọc
  view, Q19). Bảng: `customer.customers` (sửa `root_id`, `version`, `updated_at` — đã GRANT), `customer.customer_merges`
  (chèn; sửa `undone_at`, `undone_by` — đã GRANT), đọc `customer_identities`, `customer_addresses`, `ord.orders_v`.
  Không migration mới. `docs/views.md` thêm hai dòng sự kiện `customer.merged`, `customer.merge_undone`.
- Test sẽ chạy: `go test -race ./internal/modules/customer/...` (không thuộc vùng rủi ro cao), `go test -race
  ./internal/httpapi/ -run 'CustomerMerge'`, rồi cổng kiểm của task (ci-task) đúng một lần (gói đổi + gói phụ thuộc).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.Savepoint`,
  `events.Registry.Emit`, `realtime.Bus`, `identity.RecordAudit`, `tenancy.WithTenant`); trong `customer`: `RootOf`,
  `GroupMembers`, `Writer.signal`, `Writer.audit`, `lockOwner`, `lockPair` (Task 66), `Stats`, `Name`, `logging.Phone`;
  trong httpapi: `refusableWrite`, `isCustomerRefusal`, `statsOut`, `catalogRead`; `identity.GetStaff` (tên người làm).
- Dùng lại / viết mới vì: dùng lại `RootOf`, `GroupMembers`, `Writer` (Events/Queue/Realtime), `signal`, audit (tách
  `auditEntity` để loại đối tượng là `customer_merge`), `db.Savepoint` (lùi về savepoint nhả khóa dòng khi gốc đổi giữa
  chừng), `refusableWrite`, `statsOut`, `identity.GetStaff`. Không dùng `lockPair`: nó khóa hai dòng và giữ khóa cũ khi
  khóa lại; gộp cần khóa cả nhóm theo mã tăng dần và nhả khóa khi gốc đổi (Soát thiết kế 6.3). Viết mới: câu SQL khóa
  nhóm, đổi `root_id` hàng loạt, tìm lần gộp chặn, gợi ý theo SĐT (chưa có câu nào làm việc này). Hằng
  `MatchedByPhone` (`search.go`, cùng giá trị "phone") dùng lại cho `matched_by`, chỉ thêm `MatchedByStaff`. Tách
  `statsOf` (dựng `Stats` từ cột chỉ số) để `getOne` và `MergeCandidates` dùng chung. Test: số lượt rapid đặt bằng
  `testkit.SetRapidChecks(200)` trong `main_test.go` (gói chưa có TestMain); jscpd báo 3 đoạn trùng ở lần chạy đầu
  (`propertyChecks` chép từ catalog, đọc `root_id` hai lần, tạo ba khách) — đã gộp: dùng `testkit.SetRapidChecks`,
  `rootsWhere`, `people`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1 Chủ dữ liệu: `customer_merges`, `root_id` thuộc `customer` (04 mục 3); bảng có sẵn từ migration 0201.
- 5.2 Không ghi bảng module khác: chỉ ghi `customer.*`; audit qua `identity.RecordAudit` (public API).
- 5.3 Đọc module khác qua view: gợi ý đọc `ord.orders_v` (`customer_id`, `ship_phone`, `created_at`) — ngoại lệ chỉ đọc
  view đã ghi ở kế hoạch (G9, Task M2-02) và `docs/views.md` (Q19).
- 5.4 Nguyên tử: đổi `root_id` + dòng gộp + audit + sự kiện trong một transaction của bên gọi.
- 5.5 Không nguyên tử: tính lại chỉ số của hai gốc đi sau commit qua sự kiện `customer.merged`/`customer.merge_undone`
  (consumer `customer_stats` của task sau, kế hoạch M2 dòng 1120). Báo hiệu `customer.changed` cho cả hai gốc.
- 5.6 Gọi ra ngoài: không áp dụng: không có.
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant` của bên gọi; RLS lọc tenant; khách của tenant khác → 404.
- 5.8 Không biết HTTP: module không nhập `net/http`; handler mỏng `handlers_customer_merge.go`.
- 5.9 Đường ghi: hai POST có `IdempotencyKey`, `x-permission: customer.merge` (sẵn trong `openapi.yaml`).
- 5.10 Valkey: không áp dụng: không dùng.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: Writer của catalog/customer (sự kiện, báo hiệu, audit trong tx bên gọi); sentinel lỗi; test `_test`
  trên PostgreSQL thật, rapid cho quy luật.

Mục 6 (đúng khi chạy đồng thời; kế hoạch yêu cầu soát kỹ câu 1, 3, 11):
- 6.1 Hai yêu cầu cùng lúc: gộp khóa mọi dòng của {gốc giữ lại} ∪ {nhóm bị gộp} (`FOR UPDATE`, `ORDER BY id`), đọc lại
  gốc và thành viên dưới khóa; lệch → lùi savepoint (nhả khóa) và làm lại, tối đa 3 lần. B→A và A→B song song: bên sau
  thấy gốc đã đổi, làm lại, ra `merge_same_customer`. Hoàn tác khóa `moved_ids` rồi khóa dòng gộp và kiểm lại
  `undone_at IS NULL`.
- 6.2 Gửi lại: `Idempotency-Key` ở hai đường ghi (middleware chống trùng có sẵn).
- 6.3 Thứ tự khóa (ADR-46 nhóm 5: `customers` → … → `customer_merges`; cùng bảng theo khóa chính tăng dần): gộp khóa
  **cả tập** dòng khách cần chạm trong một câu theo `id` tăng dần (không khóa gốc trước rồi mới khóa thành viên — cách
  đó ngược thứ tự với `lockPair` của tìm-hoặc-tạo khi thành viên có mã nhỏ hơn gốc); gốc đổi giữa lúc đọc và lúc khóa
  thì lùi savepoint để nhả khóa rồi mới khóa lại (không giữ khóa cũ khi khóa tập mới — chỗ Task 66 cảnh báo). Hoàn tác
  khóa `moved_ids` theo `id` tăng dần rồi mới khóa dòng `customer_merges`. Test song song không deadlock: B→A/A→B 100
  vòng, và gộp song song với `FindOrCreateForOrder`, đếm `pg_stat_database.deadlocks`.
- 6.4 Lỗi tạm thời: `WriteTx` của lớp HTTP chạy lại cả transaction (có sẵn); hàm gộp gán lại mọi biến trong thân.
- 6.5 Cách ly: READ COMMITTED + khóa dòng + đọc lại sau khóa.
- 6.6–6.8 Việc nền, 429, chết giữa chừng: không áp dụng: không có việc nền hay gọi ra ngoài; transaction tự lùi.
- 6.9 Treo: thời hạn câu lệnh/chờ khóa mặc định của pool (ADR-26); nhóm khách nhỏ.
- 6.10 Tiền: không áp dụng: chỉ số tính lại ở task consumer.
- 6.11 Quy luật (rapid, 200 lượt): chuỗi gộp/hoàn tác ngẫu nhiên trên 2–8 hồ sơ — mọi `root_id` trỏ tới hồ sơ gốc;
  hoàn tác lần gần nhất trả đúng ánh xạ trước lần đó; số dòng `customers` không đổi.
- 6.12 Dữ liệu cá nhân: audit, sự kiện, lỗi chỉ mang mã; `MatchedPhones` là `logging.Phone` (tự che khi log).
- 6.13 Quyền: `customer.merge` (owner, manager) kiểm ở guard theo `x-permission`; test vai trò sale → 403.

## Quyết định thay chủ shop

- **QĐ1 — hoàn tác một lần gộp đã hoàn tác:** kế hoạch chỉ nói "lần gộp chưa hoàn tác", không nói mã lỗi khi đã hoàn
  tác. Chọn `ErrVersionConflict` (409 `version_conflict`, mã có sẵn trong hợp đồng: "trạng thái đã đổi, tải lại"), kể cả
  khi hai yêu cầu hoàn tác chạy song song (bên sau thấy `undone_at` dưới khóa). Phương án khác: 404 (sai nghĩa: lần gộp
  vẫn có), hoặc 200 không làm gì (giấu việc người khác vừa hoàn tác). Dễ đảo: đổi một dòng trong `merge.go`.
- **QĐ2 — chọn lần gộp chặn khi có nhiều lần:** trả lần đã đưa gốc giữ lại vào gốc **hiện tại** của nó (lần phải hoàn
  tác trước tiên); không có thì lần mới nhất. Kế hoạch chỉ ghi "`blockingMergeId` = lần A→D" cho ca một lần chặn.
- **QĐ3 — tên người làm trong câu trả lời (`actor.name`):** đọc `identity.GetStaff` (tên hiển thị); nhân viên không còn
  → tên rỗng (hợp đồng: hệ thống có id rỗng). Lần gộp do hệ thống (`Actor.ID` rỗng) ghi `actor_id` = uuid 0 (cột NOT
  NULL), `undone_by` để trống.

## Khác kế hoạch

- `MergeCustomers`, `UndoMerge` là phương thức của `customer.Writer` (không phải hàm tự do như Interface ghi): theo mẫu
  đường ghi của module (Q-T57-1) — cần `Events`, `Queue`, `Realtime` để phát sự kiện và báo hiệu. Chữ ký còn lại giữ
  đúng (`ctx, tx, survivorID, mergedID, matchedBy, a, now` → `(Merge, error)`; `ctx, tx, mergeID, a, now` → `error`).
  Thêm `GetMerge` (handler trả lần gộp sau khi hoàn tác) và `MergeEvent` (payload).
- Bước khóa của gộp: kế hoạch ghi khóa `{s, m}` rồi mới khóa thành viên của `m`. Làm: khóa **một lần** cả tập
  `{s} ∪ thành viên(m)` theo `id` tăng dần rồi đọc lại gốc/nhóm dưới khóa; lệch thì lùi savepoint (nhả khóa) và làm lại
  (tối đa 3). Lý do: khóa gốc trước rồi thành viên sau ngược thứ tự với `lockPair` (tìm-hoặc-tạo khóa thành viên rồi
  gốc theo mã tăng dần) khi thành viên có mã nhỏ hơn gốc → có thể deadlock; giữ khóa cũ khi khóa lại cũng vậy. Kết quả
  nghiệp vụ không đổi.
- Điều kiện chặn hoàn tác: kế hoạch ghi "lần gộp chưa hoàn tác khác tạo sau lần này mà `moved_ids` chứa `survivor_id`
  hoặc một phần tử của `moved_ids` này". Làm: "lần chưa hoàn tác khác mà `moved_ids` chứa `survivor_id`" — tương đương
  và không dựa vào thứ tự giờ (`created_at` lấy từ tham số `now`, có thể trùng): (a) lần sau dời một phần tử của
  `moved_ids` thì phải dời cả nhóm của gốc giữ lại, nên chứa `survivor_id`; (b) lần trước chứa `survivor_id` thì đã được
  hoàn tác (lúc gộp `survivor_id` là gốc). Ngược lại, so theo "chứa một phần tử của `moved_ids`" mà không có thứ tự sẽ
  chặn nhầm ở ca chuỗi (C→B rồi B→A: lần C→B có C trong `moved_ids`). Quy luật rapid kiểm điều này.
- Test đồng thời thêm `TestMergeVsForOrderNoDeadlock` (gộp song song với tìm-hoặc-tạo, theo ghi chú của Task 66).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| DH14: gộp B vào A → `Get(A)` có cả hai hồ sơ, địa chỉ của B; O2, ghi chú giữ `customer_id = B`; `moved_ids = [B]`; sự kiện `customer.merged` Seq 1, audit, báo hiệu hai gốc; hoàn tác → `root_id(B) = B`, `customer.merge_undone` Seq 2, `undone_at/by`; hoàn tác lần hai → `ErrVersionConflict` | `TestMergeDH14` (+ `checkUndoDH14`) | `go test -race ./internal/modules/customer/ -run 'Merge\|Candidates' -count=1` | `ok elimaz.com.vn/crm/internal/modules/customer 40.391s` |
| Chuỗi: C vào B, rồi B vào A (qua mã C) → `moved_ids = [B, C]` theo mã; hoàn tác → B, C về gốc B | `TestMergeChain` | như trên | ok |
| Gộp đè: hoàn tác B→A khi còn A→D → `*MergeNotLatestError{BlockingMergeID = A→D}`, không đổi gì; hoàn tác A→D rồi B→A được; lần độc lập C→A không chặn | `TestUndoMergeNotLatest` | như trên | ok |
| `created_at` + 30 ngày 1 giây → `ErrMergeWindowExpired`; đúng 30 ngày → được | `TestUndoMergeWindow` | như trên | ok |
| A vào A, B (đã thuộc A) vào A, A vào B → `ErrMergeSameCustomer`; matchedBy lạ → `ErrInvalidInput`; không có / tenant khác → `ErrNotFound` | `TestMergeErrors` | như trên | ok |
| Đồng thời B→A và A→B, 100 vòng: đúng một bên thành công, bên kia `merge_same_customer`; không vòng; `pg_stat_database.deadlocks` không tăng | `TestMergeConcurrentOpposite` | như trên | ok |
| Gộp song song với `FindOrCreateForOrder` theo mã thành viên (nhỏ hơn gốc), 20 vòng: không deadlock (ADR-46, ghi chú Task 66) | `TestMergeVsForOrderNoDeadlock` | như trên | ok |
| Quy luật rapid 200 lượt (TestMain `SetRapidChecks(200)`): mọi `root_id` trỏ gốc; hoàn tác lần gần nhất trả đúng ánh xạ trước; số dòng `customers` không đổi; gộp cùng gốc/lỗi không đổi gì | `TestMergeUndoProperty` | như trên | ok |
| Gợi ý: địa chỉ của A có SĐT là danh tính của B → có B (mã, tên, chỉ số, `MatchedPhones = [+84987654321]`); tenant khác lỗi; sau gộp không còn | `TestMergeCandidatesByAddressPhone` | như trên | ok |
| Gợi ý theo SĐT người nhận 50 đơn gần nhất của cả nhóm (đơn thứ 51 không tính), gom theo gốc, `MatchedPhones` đủ và theo thứ tự | `TestMergeCandidatesByRecentOrders` | như trên | ok |
| HTTP: 200 kèm `movedIds`, `actor.name`; 422 `merge_same_customer`; sale (customer.read, customer.write) → 403 cho gộp và hoàn tác; 409 `merge_not_latest` kèm `blockingMergeId`; 409 `version_conflict` hoàn tác lần hai; 422 `merge_window_expired`; tenant khác 404; gợi ý qua GET; log không có SĐT | `TestCustomerMergeAPI` | `go test -race ./internal/httpapi/ -run 'TestCustomerMergeAPI' -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 2.456s` |
| Không vỡ phần có sẵn của customer, httpapi và gói phụ thuộc; cổng tĩnh (build, vet, lint, gen-check, sqlc vet, jscpd, oasdiff, test-guard, gitleaks) | cả gói | cổng kiểm của task (ci-task), lần 2 | `ok …/internal/modules/customer 133.495s`, `ok …/internal/httpapi 158.503s`; cổng tĩnh xanh; `queue` đỏ do lock timeout dữ liệu thử, chạy lại xanh (Việc phát hiện thêm) |

## Test bị xóa hoặc tắt

(không có)

## Việc phát hiện thêm

- `internal/modules/inventory` `TestReserve_NoDeadlockOnCrossOrder` đỏ một lần trong cổng kiểm của task (lượt 56: `55P03`
  lock timeout) khi chạy cùng nhiều gói trên PostgreSQL chung; chạy lại riêng `-count=2` xanh. Task này không chạm
  inventory; ghi lại để điều phối viên theo dõi test chập chờn dưới tải.
- Cổng kiểm lần 2 của task: mọi cổng tĩnh xanh, `customer` (133.5 s) và `httpapi` (158.5 s) xanh; chỉ
  `internal/platform/queue` `TestClaimLongQueueSameOrderKey/strict/head_running=false` đỏ ở bước chèn 50 000 dòng thử
  (`55P03` lock timeout trên PostgreSQL chung). Chạy lại riêng `-count=2`: `ok … queue 19.116s`. Task không chạm queue.
- (Ghi chú của reviewer) `lockPair` của Task 66 khi khóa lại lần hai vẫn giữ khóa gốc cũ rồi khóa gốc mới, có thể không
  theo id tăng dần → về lý thuyết có vòng chờ với một lần gộp chạy cùng lúc. Nên sửa ở task riêng theo cách của gộp (lùi
  savepoint để nhả khóa trước khi khóa lại).
- Mô tả `description: Chưa làm ở Task M2-02 (trả 501…)` của ba operation gộp trong `openapi.yaml` vẫn còn (Task 66, 67
  cũng để nguyên cho operation của mình); dọn chung một lần ở task khác cho đồng bộ, không đổi trong task này.

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau khi gộp main).

- Phát hiện phải sửa: không có.
- Thứ tự khóa: gộp khóa cả tập theo id tăng dần trong một câu, lùi savepoint nhả khóa khi gốc đổi; hoàn tác khóa
  `moved_ids` tăng dần rồi mới khóa `customer_merges` — đúng ADR-46 nhóm 5; không vòng chờ với `lockPair`; có test
  hai cặp song song đếm deadlock.
- Điều kiện chặn hoàn tác: lập luận tương đương kế hoạch đứng vững; nhánh `errRootChurn` (500) khi hồ sơ đã dời không
  còn trỏ gốc giữ lại mà không có lần chặn là nhánh không thể xảy ra.
- QĐ1–QĐ3 có trong mã và có test.
- Ghi chú không bắt buộc (không sửa): `lockPair` của Task 66 (đã ghi ở Việc phát hiện thêm); `candidates_test.go` ca tenant
  khác chỉ kiểm `err != nil` (HTTP test đã kiểm 404).
