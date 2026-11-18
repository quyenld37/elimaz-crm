# Task 408 — Lệnh tạo vận đơn kết thúc hỏng: vận đơn creating → cancelled, gỡ vận đơn hiện hành (M1-r3)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù M1-r3 của review độc lập, phát hiện D-03 trong `docs/review-doc-lap-m1.md`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào (store sinh thêm `MarkCreateInDoubt`, `CancelCreatingShipment`, `ClearCurrentShipment`, trường `FulfillmentShipment.CreateInDoubtAt` — gói internal). Thêm mới: `order.DetachShipment` (0 chỗ dùng trước task, 1 chỗ gọi mới trong `fulfillment`), phương thức `(*fulfillment.CreateCommand).OnTerminal` (hiện thực `queue.TerminalHook`; `commands.Runner.TerminalHook` và `commands.ResolveInDoubt` tự nhận qua ép kiểu interface, không có chỗ gọi trực tiếp), hằng `fake.ModeReject` (hãng giả chỉ dùng trong test).
- Gói phụ thuộc ngược (go list): `fulfillment` ← cmd/crm, internal/adapters/fakecarrier, internal/httpapi, internal/modules/cash, internal/testkit/shiptest, fulfillment/internal/carriers/fake; `order` ← cmd/crm, internal/httpapi, internal/modules/cash, internal/modules/fulfillment, internal/testkit/shiptest.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (console không đọc trạng thái lệnh tạo vận đơn).
- Câu sqlc, bảng, pha migration: thêm `CancelCreatingShipment` (queries/fulfillment/shipments.sql: `creating → cancelled` chỉ khi chưa có dấu in_doubt), `MarkCreateInDoubt` (ghi `create_in_doubt_at` cho vận đơn còn creating) và `ClearCurrentShipment` (queries/ord/orders.sql: gỡ `current_shipment_id` khi đúng vận đơn). Migration mới `0130_fulfillment_create_in_doubt.sql` (pha mở rộng: thêm cột cho phép rỗng `fulfillment.shipments.create_in_doubt_at`, `GRANT UPDATE (create_in_doubt_at)` cho crm_app; số kế tiếp chưa dùng của dải M1 trên main và các worktree đang mở). CHECK đã cho `cancelled`; unique một phần `shipments_active_order` đã bỏ qua `cancelled`. `make gen` sinh lại `models.go` của mọi gói store (sqlc dùng chung thư mục migrations) — chỉ thêm trường `CreateInDoubtAt` vào `FulfillmentShipment`.
- Tài liệu: `docs/views.md` dòng `shipment.status_changed` thêm trạng thái `cancelled` (lệnh tạo kết thúc hỏng).
- Test có sẵn đổi cấu trúc (không đổi điều kiểm): `TestShipment_TimeoutAfterCreate` dùng helper `createTimedOut` (jscpd).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/modules/order/... ./internal/modules/cash/...`, rồi cổng kiểm của task `ci-task.sh` (gói đổi + phụ thuộc ngược).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm gỡ hay hủy vận đơn); mẫu `catalog.ImportApplier.OnTerminal` (`internal/modules/catalog/imports_apply.go`) cho hook kết thúc; `order.Lock`, `commandBase.emitStatus`, `parsePayload`, `store.LockShipment`, `fulfillment.Get` có sẵn.
- Dùng lại / viết mới vì: dùng lại `order.Lock` (khóa đơn trước, ADR-46 nhóm 7), `LockShipment`, `emitStatus`, `parsePayload`, `Get`; viết mới `order.DetachShipment` vì order chỉ có `AttachShipment` (gắn) — gỡ là việc ngược, thuộc order (fulfillment không ghi ord.orders); viết mới câu `CancelCreatingShipment` vì `UpdateShipmentProgress` không có điều kiện trạng thái và Advance giữ quy tắc "chỉ tiến" (cancelled không có hạng).

## Soát thiết kế

- 5.1 Chủ dữ liệu: vận đơn thuộc fulfillment, `current_shipment_id` thuộc order — mỗi bên đổi bảng của mình.
- 5.2 Không ghi bảng module khác: fulfillment gọi `order.DetachShipment` (public API mới), không UPDATE ord.orders.
- 5.3 Đọc qua view/API: `order.Lock` là API của order; không đọc thẳng bảng khác.
- 5.4 Nguyên tử: hủy vận đơn + gỡ vận đơn hiện hành + phát sự kiện nằm trong transaction đổi lệnh sang dead/expired (hook chạy trước câu đổi jobs; lỗi → cả transaction lùi, lệnh giữ trạng thái cũ, chạy lại được).
- 5.5 Không nguyên tử đi sau: `shipment.status_changed` qua `events.Emit` (outbox) trong cùng tx.
- 5.6 Gọi ra ngoài: chỉ hủy khi chắc chắn hãng chưa có kiện — lệnh `dead` (từ chối, hết lượt not_sent, tra lại xác nhận chưa gửi) hoặc `expired` (not_sent quá hạn) **và** lệnh chưa từng `in_doubt`. Lệnh vào `in_doubt` → hook ghi dấu `create_in_doubt_at` trên vận đơn; sau đó dù người chạy lại và lệnh thành dead/expired, vận đơn có dấu không bị hủy (kiện của lần gửi trước có thể còn ở hãng — phát hiện của reviewer lượt 1). Hook không gọi hãng.
- 5.7 Ngữ cảnh shop: hook chạy trong tx có tenant của lệnh (Runner, Worker, ResolveInDoubt); câu mới chạy dưới RLS bằng crm_app.
- 5.8 Không HTTP: không chạm.
- 5.9 Đường ghi HTTP: không áp dụng: không có đường mới (việc nền).
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: hook theo mẫu catalog; lỗi bọc một lần; test gói `_test` trên PostgreSQL thật.
- 6.1 Đồng thời: `CancelCreatingShipment` có điều kiện `status = 'creating'` trên dòng đã khóa; `ClearCurrentShipment` có điều kiện `current_shipment_id = @shipment_id`. Lệnh của một vận đơn chạy lần lượt theo `OrderKey`; nếu vận đơn đã sang awaiting_pickup thì hook không đổi gì.
- 6.2 Gửi lại: hook chạy lại không đổi gì lần hai (điều kiện creating). Người chạy lại lệnh dead sau khi đã hủy: Execute thấy vận đơn không còn creating → rejected `shipment_not_creating`, không gọi hãng.
- 6.3 Thứ tự khóa: ord.orders (nhóm 7, `order.Lock`) → fulfillment.shipments (nhóm 8) → domain_events → platform.jobs (nhóm 13, câu cuối của Runner) — đúng ADR-46, cùng chiều với RefreshCommand.OnDone.
- 6.4 Lỗi tạm: hook trả lỗi → transaction lùi, lệnh giữ trạng thái, chạy lại theo lịch có sẵn.
- 6.5 Cách ly: READ COMMITTED + khóa dòng + câu có điều kiện.
- 6.6 Việc nền lỗi: dùng lịch có sẵn của queue/commands.
- 6.7 429: không áp dụng (không gọi ngoài).
- 6.8 Chết giữa chừng: transaction tự lùi; in_doubt chỉ ghi dấu, không hủy; dấu chặn hủy cả ở lần kết thúc sau đó (chuỗi in_doubt → chạy lại → dead có test).
- 6.9 Treo: hook chỉ vài câu khóa dòng, trong giới hạn sẵn có.
- 6.10 Tiền: không ghi sổ; consumer cash bỏ qua trạng thái `cancelled` (chỉ ghi shipping/delivered).
- 6.11 Quy luật: "một lệnh không gửi hai lần" giữ nguyên — hook không gửi; test chạy lại lệnh dead sau hủy không tạo kiện.
- 6.12 Dữ liệu cá nhân: payload sự kiện chỉ mã, trạng thái, COD; không log.
- 6.13 Quyền: không áp dụng (việc nền của hệ thống, không có đường người dùng mới).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Hãng từ chối (rejected → dead): vận đơn creating → cancelled (version +1), đơn gỡ vận đơn hiện hành (version +1, vẫn confirmed), tồn giữ nguyên, 1 sự kiện `cancelled` seq = version; tạo lại vận đơn cho cùng đơn được và lên awaiting_pickup; chạy lại lệnh dead cũ không tạo kiện, không đổi vận đơn đã hủy. Mỗi người bán (KH14) | `TestShipment_CreateRejectedCancels` (+ `expectCancelled`) | `go test -race ./internal/modules/fulfillment/...` (trong ci-task) | `ok elimaz.com.vn/crm/internal/modules/fulfillment 25.701s` |
| Lệnh chắc chắn chưa gửi quá hạn (expired) → vận đơn cancelled, đơn gỡ vận đơn, hãng 0 kiện | `TestShipment_CreateExpiredCancels` | như trên | ok |
| Lệnh in_doubt (hãng đã tạo kiện, trả lời quá giờ) → dấu `create_in_doubt_at`, vận đơn còn creating và gắn; người chạy lại, hãng từ chối → dead nhưng KHÔNG hủy, không tạo được vận đơn thứ hai, hãng vẫn 1 kiện | `TestShipment_InDoubtThenDeadKeeps` | như trên | ok (lượt sửa sau reviewer) |
| Hook gọi thẳng: in_doubt chỉ ghi dấu rồi expired không hủy; vận đơn không có, payload hỏng, vận đơn đã qua creating → không đổi (không ghi dấu); vận đơn creating chưa in_doubt gọi dead hai lần → hủy một lần, một sự kiện, đơn gỡ | `TestShipment_CreateTerminalHookDirect` | như trên | ok |
| `order.DetachShipment`: chỉ gỡ đúng vận đơn (version +1), vận đơn khác / lần hai → không đổi; người bán khác, đơn không có → ErrNotFound; gắn lại sau gỡ được. Mỗi người bán | `TestDetachShipment` | `go test -race ./internal/modules/order/...` (trong ci-task) | `ok elimaz.com.vn/crm/internal/modules/order 94.331s` |
| Consumer sổ COD không vỡ khi có sự kiện cancelled; các gói phụ thuộc | toàn bộ test gói cash, order, fulfillment, httpapi, cmd/crm, tests/e2e, tests/arch | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh d265bd0…)`; `ok …/modules/cash 63.431s` |
| Cổng tĩnh: build, vet, golangci-lint, gen-check, sqlc vet, jscpd, gitleaks, oasdiff, test-guard | — | `ci-task.sh` | tất cả không ĐỎ (lần 1 đỏ: nolint thừa, jscpd đoạn khóa đơn trùng AttachShipment, gen-check do chưa stage — đã sửa) |
| Sau sửa của reviewer lượt 1 (migration 0130 → chạy toàn bộ test) | toàn bộ | `ci-task.sh` lần 3 | mọi cổng xanh trừ jscpd: phần dựng ca timeout_after_create trùng giữa `TestShipment_TimeoutAfterCreate` và test mới; `ok …/modules/fulfillment 5.610s`, `ok …/modules/order 28.549s`, `ok …/modules/cash 39.101s` |
| Sửa jscpd (chỉ tệp test: gom helper `createTimedOut`, dùng ở cả hai test) | `TestShipment_*` | `jscpd …` → `Found 0 clones`; `golangci-lint run ./internal/modules/fulfillment/...` → `0 issues`; `go test -race -count=1 ./internal/modules/fulfillment/` | `ok elimaz.com.vn/crm/internal/modules/fulfillment 6.876s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Dấu `create_in_doubt_at` không bị xóa khi `ResolveInDoubt` tra hãng thấy "chưa gửi" (lệnh về pending, không qua hook). Hậu quả: vận đơn đó nếu sau này bị từ chối vẫn kẹt creating như trước Task 408 (an toàn, cần người xử lý). Muốn gỡ dấu cần hook cho đường requeue của commands (vùng rủi ro cao, ngoài phạm vi). Ghi cho M3 (đường hủy vận đơn).
- Người bấm "bỏ" (`queue.AbandonJob`) một lệnh tạo vận đơn đang `in_doubt` → `expired` mà không qua OnTerminal (thiết kế của Task 90: "bỏ" không gọi hook). Vận đơn khi đó vẫn `creating` và còn gắn vào đơn. Không tự hủy là đúng vì in_doubt có thể hãng đã tạo kiện; đường hủy vận đơn của M3 (hay runbook tra hãng rồi hủy) cần xử lý ca này. Ghi cho M3.

## Quyết định thay chủ shop

- Reviewer lượt 1 (CẦN SỬA): lệnh từng in_doubt rồi người chạy lại, lần sau dead → hook cũ hủy vận đơn dù kiện lần đầu có thể còn ở hãng (hai kiện COD). Chọn: lưu dấu trên vận đơn (cột mới `create_in_doubt_at`, migration chỉ thêm 0130) và không hủy vận đơn có dấu. Vì: platform.jobs không giữ lịch sử in_doubt (RetryJob xóa attempts, finished_at), fulfillment không đọc bảng platform; dấu trên dữ liệu của chính module là chỗ đúng chủ. Phương án khác: chỉ ghi "Việc phát hiện thêm" và để hook hủy (không an toàn: có thể sinh hai kiện); hay thêm cờ vào `queue.Job`/platform.jobs (đổi vùng rủi ro cao queue/commands, lớn hơn phạm vi). Đảo ngược: bỏ đọc cột, cột rỗng vô hại.

- Phát `shipment.status_changed` với `status = cancelled` khi hủy vận đơn creating (seq = version mới). Vì: spec 04 mục 3 khai fulfillment phát `shipment.status_changed` cho mọi đổi trạng thái, version vận đơn tăng thì seq phải có sự kiện tương ứng; consumer cash bỏ qua trạng thái khác shipping/delivered nên không ghi sổ. Phương án khác: không phát (version nhảy mà không có sự kiện, màn hình realtime không biết). Dễ đảo: bỏ một dòng gọi `emitStatus`. Đã sửa dòng trong `docs/views.md`.
- `order.DetachShipment` không xét trạng thái đơn (M2 có `cancel_requested` khi đang có vận đơn hiện hành): vận đơn chưa từng tồn tại ở hãng thì gỡ luôn đúng. Hàng giữ không nhả (đơn vẫn chờ giao, tạo lại vận đơn được) — đúng đề xuất D-03, không đổi hành vi khác.

## Reviewer

Kết luận: ĐẠT — hai lượt `reviewer` trên `git diff --cached`.

- Lượt 1: CẦN SỬA — chuỗi lệnh in_doubt → người chạy lại (`queue.RetryJob`) → dead làm hook hủy vận đơn dù kiện lần đầu có thể còn ở hãng (hai kiện COD). Đã sửa: migration 0130 cột `create_in_doubt_at`, hook ghi dấu khi in_doubt, `CancelCreatingShipment` chỉ hủy khi chưa có dấu; test `TestShipment_InDoubtThenDeadKeeps` (xem "Quyết định thay chủ shop").
- Lượt 2: ĐẠT, không phát hiện phải sửa. Reviewer kiểm mọi đường vào in_doubt (Runner `inDoubt`, ghi done lỗi, Worker hết giữ chỗ) đều gọi hook ghi dấu. Ghi chú không bắt buộc: chú thích `OnTerminal` và mục 5.6 xếp "tra lại xác nhận chưa gửi" vào nhóm được hủy, thực tế `AutoRetryNotSent = true` nên ResolveInDoubt đưa lệnh về hàng chờ, và vận đơn đó đã có dấu nên không bị hủy — hành vi an toàn hơn lời mô tả; không sửa mã để giữ dấu duyệt, ghi ở đây. Hai ca ở "Việc phát hiện thêm" để M3, an toàn (vận đơn kẹt creating, không hủy nhầm).
