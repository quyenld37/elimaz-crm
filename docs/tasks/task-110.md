# Task 110 — Danh sách đơn giao thất bại cần gọi lại

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 110 = M3-19, ca VC07). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu đã có; chỉ thêm. `fulfillment` thêm:
  `CallbackFilter`, `CallbackTask`, `CallbackPage`, `CallbackOutcomeInput`, `(*Service).ListCallbacks`,
  `(*Service).RecordCallbackOutcome`, `(*Service).AssignCallback`, `RedeliverKind`, `RedeliverCommand`,
  `NewRedeliverCommand`, sentinel `ErrCallbackNotFound`, `ErrCallbackClosed`, `ErrCallbackAssignee`.
  `identity` thêm `StaffHas(ctx, tx, userID, p) (bool, error)`; `customer` thêm `Brief(ctx, tx, id) (Customer, error)`
  (hồ sơ gốc không kèm danh tính, địa chỉ); cả hai mới, chỉ dùng ở `internal/httpapi/handlers_callbacks.go`.
  `fulfillment` không gọi `customer` (test kiến trúc `boundaries`), nên tên khách và số nhận/hoàn ghép ở httpapi. Commit cấu trúc: phương thức chưa xuất `(*applyRun).publishCallback`
  (3 chỗ gọi trong `apply_effects.go`) chuyển thành `(*Service).publishCallback(ctx, tx, tenantID, id)`; chỉ gói gốc.
  `(*Module).callbacksHandlers` (gọi từ `module.go` `CommandHandlers`) trả lệnh giao lại khi có adapter hãng.
- Gói phụ thuộc ngược (go list): `fulfillment` ← cmd/crm, internal/adapters/carrier/{,best,carriertest,fake,manual,spx,vtp,vtp/vtptest},
  internal/httpapi, internal/httpapi/web, internal/modules/cash (+ internal/formats, internal/statement),
  fulfillment/internal/carriers/fake, testkit/codtest, testkit/shiptest, wiring/webcache, tools/fakecarrier/server.
  `identity`, `customer` ← nhiều gói (chỉ thêm hàm, không đổi hàm cũ). ci-task chọn theo gói đổi.
- Operation OpenAPI bị chạm, oasdiff: `listCallbackTasks`, `recordCallbackOutcome`, `assignCallbackTask` (thân hàm thay
  501; hợp đồng đã có từ Task 95 — không sửa `openapi.yaml`, oasdiff không đổi). Bỏ 3 dòng ở
  `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn "Cần gọi lại" là Task 135; console chỉ có client sinh sẵn).
- Câu sqlc, bảng, pha migration: không migration. Câu mới trong `queries/fulfillment/callbacks.sql`:
  `ListCallbackTasks`, `CountOpenCallbacks` (đếm có trần 1.000), `CallbackTaskShipment`, `LockCallbackTask`,
  `SetCallbackOutcome`, `InsertCallbackLog`, `AssignCallbackTask`, `CallbackLogTarget` (lệnh giao lại). Bảng
  `fulfillment.callback_tasks` (khóa dòng, sửa), `fulfillment.callback_task_logs` (chỉ chèn), đọc `fulfillment.shipments`,
  `ord.orders_v` (cột `customer_id`). Chỉ mục `callback_tasks_status_due` (0300) đã có. Test: `lists_stuck_test.go`
  tách helper dùng chung `ownerBulkExec`, `requireNoSeqScan`, `storeSQLIn` (jscpd).
- Test sẽ chạy: `internal/modules/fulfillment` (`TestCallbacks_*` mới, `TestStuck_PlanUsesIndexes`), `internal/httpapi`
  (`TestCallbacksAPI_*` mới, `TestDomainErrorSnapshot*`), `internal/modules/identity` (`TestStaffHas`),
  `internal/modules/customer` (`TestBrief`), `cmd/crm` (`TestWireM3_KeepsM1Parts` thêm lệnh), `tests/arch`
  (`TestM3Contract_PendingList`, ranh giới module). Cả gói chạy ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (lists.Cursor,
  db.NullUUID, testkit.ExplainAsApp/RequireNoSeqScan, commands.CommandKind, queue.Enqueue DedupeKey, logging.ScrubText);
  trong fulfillment: `lockForCancel` (khóa đơn → vận đơn), `parseListCursor`/`pageLimit` (danh sách vận hành),
  `logging.ScrubText` (che SĐT trong ghi chú, như `addressNote`), `classifyCreateError`, `targetFailed`, `commandAccount`, `(*Service).adapter`
  (lệnh gọi hãng), `RequestAddressChange` (Task 108); httpapi: `refusableWrite`, `staffNames`, `deliveryStatsOut`,
  `orderActor`, `shipAuthorize`, `int32n`; identity: `Can`, câu `ListUserGrants`, `GetStaffUser`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên. Viết mới: lệnh giao lại (`redeliver_command.go`, theo mẫu
  `UpdateAddressCommand` nhưng không có hook đổi dữ liệu: kết quả hãng không đổi bảng nào của CRM); `identity.StaffHas`
  (chưa có hàm "người dùng X có quyền P" chạy trong tx; `freshGrants` chưa xuất và gắn với actor đang thao tác);
  `customer.Brief` (`customer.Get` đọc cả danh tính có SĐT, địa chỉ, nhãn — thừa và lộ dữ liệu cá nhân cho một tên).
  jscpd: `publishCallback` giống `cash.publishStatement` (báo hiệu của hai module khác nhau, cùng mẫu 6 dòng) — giữ
  riêng, mỗi module báo hiệu của mình; viết lại thân để không lặp chuỗi token. Test: gom phần nạp dữ liệu lớn và EXPLAIN
  thành helper dùng chung với `lists_stuck_test.go`.

## Soát thiết kế

5.1 Chủ dữ liệu: `callback_tasks`, `callback_task_logs` thuộc fulfillment (0300). 5.2 Không ghi bảng module khác: đổi
địa chỉ đi qua `RequestAddressChange` (fulfillment), đơn chỉ khóa qua `order.LockSnapshot`. 5.3 Đọc module khác qua
view: `ord.orders_v` (fulfillment được gọi order); tên khách (`customer.Brief`) và số nhận/hoàn
(`customer.DeliveryStatsFor`) đọc ở httpapi vì fulfillment không được gọi customer. 5.4 Nguyên tử: ghi kết quả + nhật ký + yêu cầu đổi địa chỉ + lệnh giao
lại trong một transaction (`WriteTx` qua `refusableWrite`). 5.5 Sau commit: gọi hãng giao lại là lệnh `commands`
ghi trong tx, worker chạy sau. 5.6 Gọi ra ngoài: `AutoRetryNotSent = false`, `Lookup` → `unknown` (in_doubt không gửi
lại). 5.7 Mọi câu trong `tenancy.WithTenant`; không bảng mới. 5.8 Module không nhập net/http; handler mỏng. 5.9 Hai
đường ghi đã có `IdempotencyKey` và `x-permission` trong hợp đồng. 5.10 Không dùng Valkey. 5.11 Không thư viện mới.
5.12 Theo mẫu: lệnh theo `address_command.go`, danh sách theo `area_lists.go`, test `shipEnv`/`trackEnv`.

6.1 Đồng thời: khóa việc `FOR UPDATE` sau đơn và vận đơn, so `version` (bắt buộc ở ghi kết quả, tùy chọn ở giao
việc) → `ErrVersionConflict`. 6.2 Gửi lại: khóa chống trùng của middleware (test 50 yêu cầu cùng khóa → 1 dòng nhật
ký); lệnh giao lại có khóa `redeliver:<mã nhật ký>`. 6.3 Thứ tự khóa ADR-46 nhóm 7 → 8: `order.Lock` → `shipments` →
`callback_tasks` → `address_change_requests` (RequestAddressChange khóa lại đơn, vận đơn đã giữ trong cùng tx: không
đổi thứ tự). Áp kết quả tra cũng khóa đơn → vận đơn → callback_tasks: cùng chiều. 6.4 Lỗi tạm: `refusableWrite` dùng
`db.RetryTx`; thân hàm gán lại biến kết quả. 6.5 READ COMMITTED + khóa dòng. 6.6 Lệnh giao lại: lỗi chắc → dead (cảnh
báo của commands), không rõ → in_doubt. 6.7 429/bận: `classifyCreateError` như lệnh tạo. 6.8 Tiến trình chết: lệnh
thành in_doubt. 6.9 Lời gọi hãng tối đa 20 s. 6.10 Không chạm tiền. 6.11 Không quy luật mới cho rapid (không vùng tồn/tiền);
quy luật trạng thái kiểm bằng test bảng. 6.12 Dữ liệu cá nhân: danh sách không có SĐT, địa chỉ; ghi chú qua
`logging.ScrubText` (che SĐT) trước khi lưu; payload lệnh chỉ có mã nhật ký (không ghi chú); audit chỉ có trạng thái,
kết quả, mã. 6.13 Quyền: `fulfillment.callbacks.work` (toàn shop, guard kiểm); `redeliver_new_address` thêm kiểm
`fulfillment.ship` ở kho của vận đơn (như requestAddressChange); người được giao phải có `fulfillment.callbacks.work`
(đọc lại trong tx).

## Quyết định thay chủ shop

1. **SĐT cho nút "Hiện số" (Task 135):** không thêm SĐT hay operation mới vào danh sách. `CallbackTask` đã có `orderId`;
   màn đọc SĐT qua `getOrder` sẵn có (quyền `order.read`, vai trò sale có) khi người dùng bấm "Hiện số". Phương án
   khác: operation đọc SĐT riêng cho việc gọi lại (mở rộng hợp đồng) — chỉ cần nếu chủ shop muốn người gọi lại không
   có `order.read`.
2. **`redeliver_new_address` cần thêm `fulfillment.ship` ở kho của vận đơn** (thiếu → 403), như `requestAddressChange`:
   đổi địa chỉ giao là thao tác nhạy cảm, không để quyền gọi lại mở rộng nó. Vai trò sale có cả hai quyền nên không
   đổi gì cho người dùng hiện có. Phương án khác: chỉ cần `fulfillment.callbacks.work`.
3. **Ghi kết quả / giao việc khi việc đã `done` hay `closed` → 409 `invalid_transition`** (sentinel `ErrCallbackClosed`).
   Kế hoạch chỉ nêu `closed`; việc `done` đã có kết quả cuối (đã yêu cầu giao lại, khách hủy), ghi đè dễ làm mất dấu.
   Phương án khác: cho ghi tiếp trên `done`.
4. **Mã lỗi chỉ dùng mã có sẵn** trong hợp đồng (như Task 100, 117): việc không có → 404 `not_found`; đã đóng → 409
   `invalid_transition`; thiếu `nextCallAt` khi `reschedule`, thiếu `newAddress` khi `redeliver_new_address`, ghi chú
   quá dài → 400 `invalid_input` (`ErrShipInvalid`); người được giao không có quyền / không hoạt động → 422
   `invalid_input` (`ErrCallbackAssignee`). Không thêm mã vào `x-error-codes`.
5. **Lệnh giao lại chỉ ghi khi gọi được qua API** (vận đơn `delivery_failed`, tài khoản `api`, adapter khai `Redeliver`,
   có mã hãng); không thì nhật ký ghi kèm câu "Nhân viên báo hãng giao lại (hãng không nhận yêu cầu qua API)" trước
   ghi chú. Kết quả hãng không đổi bảng nào của CRM (trạng thái giao lại về qua tra trạng thái); lỗi chắc → dead (cảnh
   báo chung của commands), không rõ → in_doubt. Phương án khác: thêm loại "Cần xem" khi giao lại thất bại (cần
   migration CHECK kind) — để khi chủ shop muốn.
6. **`no_answer` không có `nextCallAt`** vẫn hợp lệ (việc sang `in_progress`, giữ hẹn cũ); `reschedule` bắt buộc
   `nextCallAt`. Phương án khác: bắt buộc cả hai.
7. **Danh sách mặc định** (không có `status`) là việc còn mở (`open` + `in_progress`); `openCount` đếm việc mở toàn shop,
   trần 1.000.

## Bằng chứng

Lệnh chung: `go test -race -count=1 -run '<tên>' ./<gói>/` (từng test khi viết), và cổng kiểm `ci-task.sh` của task
(CI-TASK XANH so với f9bd3f3: build, vet, lint, gen-check, sqlc-vet, catalog, jscpd 0 clone, gitleaks, oasdiff,
test-guard, test kiến trúc; `-race` cmd/crm 7,8 s, httpapi 42,5 s, customer 39,2 s, fulfillment 52,4 s, identity 45,4 s;
các gói chỉ dùng tới chạy thường đều ok).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: báo hiệu `callback_task.changed` vẫn phát khi mở/đóng việc qua áp kết quả tra | `TestApply_FailedTwiceThenDelivered`, `TestApply_ReturningClosesCallbacks` (fulfillment) | `go test -race -count=1 -run '^(TestApply_FailedTwiceThenDelivered\|TestApply_ReturningClosesCallbacks)$' ./internal/modules/fulfillment/` | `ok … 2.052s` |
| VC07: thất bại → việc open, hạn = thất bại + 24 giờ; sau 23 giờ việc quá hạn đứng đầu, `overdue`; `no_answer` + hẹn → in_progress; `redeliver` → done, hãng giả nhận đúng 1 lệnh giao lại (job done), nhật ký đã che SĐT; thất bại lần 2 sau giao lại → việc `attempt_no 2`; giao được → lần 2 closed delivered, lần 1 giữ done; ghi kết quả lên cả hai → `ErrCallbackClosed`; phiếu xuất của V1 đúng 1 dòng | `TestCallbacks_VC07` | `go test -race -count=1 -run '^TestCallbacks_VC07$' ./internal/modules/fulfillment/` | `ok … 2.339s` |
| Hãng chuyển hoàn khi việc open → closed `carrier_returning`; ghi kết quả → `ErrCallbackClosed`, không nhật ký; danh sách mở rỗng | `TestCallbacks_ReturningClosesTask` | `go test -race -count=1 -run '^TestCallbacks_' ./internal/modules/fulfillment/` | `ok … 4.472s` (cả nhóm) |
| Từ chối không đổi gì: kết quả lạ, `reschedule` thiếu giờ, địa chỉ mới thiếu địa chỉ, ghi chú > 1.000 → `ErrShipInvalid`; lệch version → `ErrVersionConflict`; việc lạ → `ErrCallbackNotFound`; `redeliver` không API → done, nhật ký "Nhân viên báo hãng giao lại …: <ghi chú>", không lệnh | `TestCallbacks_OutcomeRules` | như trên | ok |
| Khách hủy → done, ghi chú cho kho ở nhật ký; báo hiệu `callback_task.changed` (Requires `fulfillment.callbacks.work`) | `TestCallbacks_CustomerCancel` | như trên | ok |
| Hai lần ghi cùng version song song → 1 qua, 1 `ErrVersionConflict`, 1 dòng nhật ký | `TestCallbacks_ConcurrentOutcome` | như trên | ok |
| Giao lại địa chỉ mới: kho ngoài phạm vi → lỗi Authorize, việc vẫn open; được → done + 1 yêu cầu đổi địa chỉ (manual, pending) cùng transaction | `TestCallbacks_RedeliverNewAddress` | như trên | ok |
| Giao việc: lệch version → `ErrVersionConflict`; giao → assignee, version + 1; lọc theo người được giao; việc đã xong → `ErrCallbackClosed`; danh sách done không overdue | `TestCallbacks_Assign` | như trên | ok |
| Phân trang (due_at, id), `dueBefore`, con trỏ hỏng → `ErrInvalidPage`, `status=closed` → `ErrShipInvalid` | `TestCallbacks_ListPages` | `go test -race -count=1 -run '^TestCallbacks_(PlanUsesIndexes\|ListPages)$' ./internal/modules/fulfillment/` | `ok … 3.991s` |
| 2.000 việc mở + 25.000 việc xong: `listCallbackTasks`, `countOpenCallbacks` không Seq Scan (`ExplainAsApp`); `openCount` trần 1.000, trang 50 | `TestCallbacks_PlanUsesIndexes` | như trên | ok |
| HTTP danh sách: overdue, tên khách, `deliveryStats`, `openCount`; JSON không có SĐT/địa chỉ; `assignee=me`, `status=done`; chỉ `fulfillment.read` → 403 | `TestCallbacksAPI_List` | `go test -race -count=1 -run '^TestCallbacksAPI_' ./internal/httpapi/` | `ok … 2.866s` |
| HTTP ghi kết quả: không quyền → 403; 50 yêu cầu cùng khóa → 1 dòng nhật ký, 1 audit không chép ghi chú; địa chỉ mới thiếu `fulfillment.ship` → 403, không đổi gì; lệch version → 409 `version_conflict`; việc đã xong → 409 `invalid_transition`; việc lạ → 404 | `TestCallbacksAPI_RecordOutcome` | như trên | ok |
| HTTP giao việc: người không có `fulfillment.callbacks.work` → 422 `invalid_input`; lệch version → 409; giao → 200 kèm tên, version 2, audit | `TestCallbacksAPI_Assign` | như trên | ok |
| Mã lỗi mới khớp bảng (3 sentinel) | `TestDomainErrorSnapshot`, `TestDomainErrorSnapshot_TableMembership` | `go test -count=1 -run '^TestDomainErrorSnapshot' ./internal/httpapi/` | `ok … 0.441s` |
| `identity.StaffHas`: sale/chủ có, kho không, bị khóa / không có → false, vai trò lưu trữ không tính | `TestStaffHas` | `go test -race -count=1 -run '^TestStaffHas$' ./internal/modules/identity/` | `ok … 1.859s` |
| `customer.Brief`: hồ sơ con → gốc (tên, mã); mã lạ → `ErrNotFound` | `TestBrief` | `go test -race -count=1 -run '^TestBrief$' ./internal/modules/customer/` | `ok … 1.847s` |
| Ba operation hết 501, bỏ khỏi `m3_pending.txt`; fulfillment không gọi customer | `TestM3Contract_PendingList`, test ranh giới (`tests/arch`) | `go test -count=1 ./tests/arch/` | `ok … 19.273s` |
| Lệnh `command.shipment.redeliver` nối khi có hãng | `TestWireM3_KeepsM1Parts` | `go test -count=1 -run '^TestWireM3_KeepsM1Parts$' ./cmd/crm/` | `ok … 0.685s` |

## Test bị xóa hoặc tắt

(không có; `storeSQL` của `lists_stuck_test.go` thay bằng helper `storeSQLIn`/`requireNoSeqScan`, đủ 5 câu cũ vẫn kiểm)

## Khác kế hoạch

- Không sửa `cmd/crm/wire_m3.go`: lệnh giao lại không cần hàm audit, nên nối qua `(*Module).callbacksHandlers` (như lệnh
  hủy), tự có trong `CommandHandlers()` khi có adapter hãng; `cmd/crm/wire_m3_test.go` thêm tên lệnh.
- Tên khách không đọc qua `customer.customers_v` trong SQL của fulfillment (test kiến trúc chặn: fulfillment không được
  gọi customer) mà qua `customer.Brief` ở httpapi — đúng phương án "ghép ở httpapi" của kế hoạch.
- Thứ tự viết: mã và test viết trong cùng phiên, phần lớn test mới chạy lần đầu sau khi mã đã có (không ghi lại lần đỏ
  cho từng test). Các lần đỏ thật: `TestCallbacks_PlanUsesIndexes` (Seq Scan → câu danh sách chọn trang trên riêng
  `callback_tasks` rồi mới nối), `tests/arch` (fulfillment đọc `customer.customers_v` → chuyển sang httpapi),
  `TestCallbacksAPI_RecordOutcome` (500 do Service thử thiếu Events).

## Việc phát hiện thêm

- Cổng `gen-check` của ci-task so cây làm việc với index (`git diff --exit-code -- internal …`): phần sửa chưa stage làm
  cổng đỏ dù mã sinh không lệch; phải stage trước khi chạy ci-task (lần chạy đầu của task này đỏ vì vậy). Không sửa
  công cụ.
- `git merge main` lúc gộp: Already up to date (main chưa đổi từ điểm tách nhánh f9bd3f3).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 110 không có test e2e)

## Reviewer

Commit 1/2 (cấu trúc, `apply_effects.go`): Kết luận: ĐẠT. Không có phát hiện phải sửa. Ghi chú không bắt buộc: hai test
bằng chứng chạy qua `publishCallback` với Bus thật nhưng không khẳng định nội dung báo hiệu (commit 2 thêm kiểm
`callback_task.changed` ở `TestCallbacks_CustomerCancel` cho đường của Service); "3 chỗ gọi" thực là 2 chỗ gọi + 1 định
nghĩa.
