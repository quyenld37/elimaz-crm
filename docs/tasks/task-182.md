# Task 182 — Đóng mốc M4 (M4-42)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md`, mục "Task 182 (M4-42)". Loại việc (quy trình mục 3): A (đóng mốc), kèm hai sửa loại B từ review độc lập.

## Ảnh hưởng

Ghi chú thứ tự: mục này điền sau khi đã viết ba test đỏ đầu tiên của phần sửa B (review độc lập trả kết quả giữa task);
phần sửa nhỏ, chỉ chạm hàm không xuất.

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **không có**. Đổi hàm không xuất: `send.hideCommand.Execute`
  (thêm `reportAuth`), `send.autoActionCommand.OnTerminal` và interface không xuất `actionCommand` (đổi `OnTerminal` thành
  `settle`; hai chỗ hiện thực: `replyCommand`, `privateCommand` nhúng `sendCommand`), `automation.hideCommand.Execute`
  (thêm `reportAuth`). Gọi `channel.Writer.ReportTokenInvalid` có sẵn (không đổi chữ ký).
- Gói phụ thuộc ngược (go list): `conversation/internal/{send,automation}` được nhập bởi `conversation`,
  `conversation/internal/{automation,inbound,live}`; qua `conversation` tới `internal/httpapi`, `cmd/crm`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không đổi API; tin tự động không rõ đã gửi giờ hiện "đã bỏ" (`discarded`, lý do
  `auto_dropped`) thay vì "không rõ đã gửi chưa" — màn hộp thư đã có trạng thái này cho tin chào (Task 167).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: test mới `TestAutoReply_InDoubtDropped`, `TestAutoHide_RejectionReportsOnlyAuth` (automation),
  `TestHideComment_RejectionReportsOnlyAuth` (send), `TestSignal` (access), `TestSend_HideError` và ca thêm trong
  `TestSend_RejectBeforeCall`, `TestDownload_Rejects`, `TestDownload_SizeLimit` (facebook), test bắt đột biến
  `mutation_m4_test.go` của inbound, send, automation, live; cả gói đổi và gói phụ thuộc qua ci-task; lượt test từ xa
  mọi gói M4 làm bằng chứng nghiệm thu.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `channel.Writer.ReportTokenInvalid` (đường báo token
  hỏng có sẵn của lệnh gửi tin, `merge.go`), `sendCommand.settle(…, auto)` (luật tin tự động có sẵn cho `auto_send`),
  `tenancy.WithTenant` cho transaction ngắn như `sendCommand.note`.
- Dùng lại / viết mới vì: dùng lại cả ba; không thêm hàm chung. Bộ chạy mutation từ xa (`tests/mutationrun`) là công cụ
  tạm, không commit (mục Quyết định).

## Soát thiết kế

- 5.1–5.3: không thêm dữ liệu; `ReportTokenInvalid` là public API của `channel` (không ghi bảng module khác).
- 5.4, 5.5: báo token hỏng trong transaction ngắn riêng sau khi gọi kênh (như `note`), không trong transaction của Runner.
- 5.6: không đổi đường gửi; tin tự động `in_doubt`/`expired` → `discarded`, **không bao giờ gửi lại** (04 mục 5.3).
- 5.7: `tenancy.WithTenant` theo tenant của lệnh. 5.8–5.12: không áp dụng (không HTTP, không thư viện mới).
- 6.1, 6.3: `ReportTokenInvalid` khóa dòng tài khoản kênh (ADR-46 nhóm 3) là khóa duy nhất của transaction đó; hai lệnh
  cùng báo → lần sau thấy đã `degraded`, không cảnh báo thêm (khóa chống trùng của lệnh cảnh báo).
- 6.2: lệnh rejected là trạng thái cuối, Execute không chạy lại; báo hai lần vẫn chỉ một cảnh báo.
- 6.8: lỗi khi báo bị bỏ qua (lượt kiểm sức khỏe mỗi giờ vẫn phát hiện) — không làm hỏng kết quả lệnh.
- 6.4–6.7, 6.9–6.11: không áp dụng (không thêm câu SQL, tiền, quy luật mới). 6.12: không log nội dung. 6.13: không đổi quyền.

## Quyết định thay chủ shop

- **QĐ-182-1 Mutation testing chạy trên GitHub Actions, không trên máy Mac.** Máy Mac 16 GB không chạy test nặng (chủ
  shop 27/11). Bộ chạy tạm `tests/mutationrun/<nhóm>` (một test Go gọi `gremlins unleash` với `.gremlins.yaml`, loại
  tệp ngoài nhóm bằng `-E`, luôn đỏ để log hiện) được đẩy bằng `scripts/ci/remote-test.sh <tree>` (cây dựng bằng chỉ mục
  git tạm, không chạm chỉ mục của nhánh), các nhóm song song, mỗi nhóm ≤ 27 phút; kết quả đọc bằng `gh run view
  --log-failed`. Bộ chạy **không commit** (cách chạy ghi ở `docs/mutation.md`). Phương án khác: `make mutation` trên Mac
  (cấm), thêm job gremlins vào workflow (việc công cụ, ngoài phạm vi).
- **QĐ-182-2 Không thêm M4 vào `HIGH_RISK_DIRS`.** Kế hoạch ghi "thêm thư mục vùng rủi ro cao M4" vào `.gremlins.yaml`,
  nhưng danh sách thư mục là `HIGH_RISK_DIRS` của hook (tệp quy trình, chỉ chủ shop đổi; hook chặn) và còn quyết định
  phạm vi test `-race` của ci-task. Chỉ ghi chú vùng M4 và lệnh chạy trong `.gremlins.yaml`; đề xuất chủ shop thêm một
  dòng (Việc phát hiện thêm). Đảo ngược: chủ shop thêm vào `HIGH_RISK_DIRS`, bỏ ghi chú.
- **QĐ-182-3 Sửa hai phát hiện trung bình của review độc lập ngay trong task** (kế hoạch Step 3 "Sửa các phát hiện"):
  (a) tự trả lời / tự nhắn riêng comment không rõ đã gửi hay quá hạn → bỏ (`discarded`, `auto_dropped`) đúng 04 mục 5.3,
  thay Quyết định 4 của Task 166 (giữ "không rõ" cho nhân viên quyết) — spec ghi rõ "Bỏ, ghi nhật ký", tin chào
  (`auto_send`) đã làm vậy, và Meta chỉ cho một tin riêng mỗi comment nên gửi lại tay dễ trùng. Đảo ngược: đổi `true` →
  `false` ở `autoActionCommand.OnTerminal`. (b) lệnh ẩn comment (tay, tự động) gặp lỗi token → `ReportTokenInvalid` ngay
  (02 mục 2), như lệnh gửi tin. Hai phát hiện thấp không sửa ở đây: echo tin riêng (cần payload thật, Task 181), cờ
  "không liên hệ" khi gộp khách (module customer — Việc phát hiện thêm).
- **QĐ-182-4 Ngưỡng mutation tính theo gói**, không theo nhóm tệp (nhóm chỉ để chia thời gian chạy): hiệu quả của gói =
  tổng bị giết / (bị giết + còn sống) của các nhóm.
- **QĐ-182-5 Phần V/D của nghiệm thu, Task 181 (Meta thật), hồ sơ App Review:** ghi "chờ server thử của hệ mới" trong
  `docs/nghiem-thu/m4.md` (giao việc /goal 28/11), không chặn commit. `make ci` toàn bộ (Step 5) không chạy trên máy:
  điều phối viên chạy trên `main` sau khi gộp; task chạy ci-task và lượt test từ xa mọi gói M4.
- **QĐ-182-6 Tệp nghiệm thu ở `docs/nghiem-thu/m4.md`** đúng tên kế hoạch (M1, M2 dùng `docs/nghiem-thu-mN.md`); không
  dời tệp cũ.
- **QĐ-182-7 Đổi khóa token Page:** chưa có việc tự ghi lại token mang khóa cũ; RUNBOOK ghi đường kết nối lại từng Page
  và câu SQL kiểm (M3 cũng chỉ báo `needsRewrap`). Không viết việc ghi lại ở task đóng mốc (Việc phát hiện thêm).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 1: mutation vùng cao M4 ≥ 80% mỗi gói | gremlins lượt 1 (12 nhóm) và lượt 2 (15 nhóm) | `remote-test.sh <tree> HEAD tests/mutationrun/<nhóm>` (QĐ-182-1); run lượt 1 46050800204…46050869441, lượt 2 46056986937…46057067361, chạy lại `manager/lease` 46058736660 | channel 86,05%; facebook 91,46% → 97,56%; access 66,67% → 100%; inbound 72,95% → 94,33%; send 79,26% → 92,01%; automation 79,18% → 92,06%; live 78,85% → 91,61% (bảng `docs/mutation.md`, mục "Lần chạy M4") |
| Đột biến còn sống: test bắt hoặc lý do | `mutation_m4_test.go` ×4, `TestSignal`, `TestReportTokenInvalid_AnyAuthReasonOneAlert`, `TestSend_HideError`… | mỗi test kiểm bằng `go test -overlay` (đột biến → FAIL, mã thật → PASS) | lý do từng nhóm đột biến còn sống: `docs/mutation.md` |
| Review độc lập phát hiện 1 (04 mục 5.3: tin tự động không rõ → bỏ) | `TestAutoReply_InDoubtDropped` | `go test -count=1 -run '^TestAutoReply_InDoubtDropped$' ./internal/modules/conversation/internal/automation/` | đỏ trước sửa (`status:checking`, 0 tin `auto_dropped`), sau sửa `ok`; xanh trong ci-task |
| Review độc lập phát hiện 2 (02 mục 2: lệnh ẩn gặp lỗi token báo ngay; lỗi khác không) | `TestHideComment_RejectionReportsOnlyAuth`, `TestAutoHide_RejectionReportsOnlyAuth` | `go test -count=1 -run '…' ./internal/modules/conversation/internal/{send,automation}/` | đỏ trước sửa ("Page chưa degraded token_invalid"), sau sửa `ok`; đột biến `&&`→`||` của điều kiện mới bị giết (overlay) |
| Step 2: threat model, RUNBOOK, ADR-21 | — | đọc; mọi tên test trong tài liệu grep `func <tên>(` có thật | `docs/threat-model-m4.md` (8 ranh giới), RUNBOOK mục "Hộp chat Facebook và màn Live" (10 khóa cảnh báo, kết nối lại, chỉ xem, không rõ đã gửi, live tay, đổi khóa, xóa dữ liệu), ADR-21 "Kiểm bằng" + "Kiểm ở đâu" |
| Step 3: review độc lập | agent mới, không xem lập luận | — | 0 nặng, 2 trung bình (đã sửa), 4 thấp (1 sửa, 3 ghi lại); `docs/nghiem-thu/m4.md` |
| Step 4: nhân viên trên staging | — | — | chờ server thử của hệ mới (QĐ-182-5) |
| Step 5: cổng kiểm, đo cài đặt agent | ci-task + lượt từ xa gói không đổi | `ci-task.sh` → `CI-TASK XANH` (run 46062061348, lần 2; lần 1 đỏ cổng tĩnh: nolint thừa, gofumpt, ireturn, 13 đoạn jscpd trong test — đã sửa; test cả 6 phần xanh cả hai lần); `remote-test.sh` → `REMOTE-TEST XANH` (run 46063232557) | 17 + 7 gói `ok` (liệt kê ở `docs/nghiem-thu/m4.md`); `docs/do-luong/cai-dat-agent-m4.md` |
| 33 ca M4 có test máy | bảng `docs/nghiem-thu/m4.md` | grep tên test | 33/33 có test có thật; e2e viết sẵn, chưa chạy (mục dưới) |

## E2E viết sẵn, chưa chạy

Không thêm e2e mới. Các e2e M4 sẵn có (`TestFBInboundLatency`, `TestLiveFlow_CommentToConfirmedOrder`,
`TestE2E_ChannelSilence`, `TestE2E_ChannelTokenRevoked`) không chạy trong task (luật 24/11); điều phối viên chạy khi gộp.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Một commit (kế hoạch: commit test thêm ở Step 1, commit đóng mốc ở Step 6): gộp để một lượt reviewer duyệt cả test
  thêm, hai sửa B và tài liệu; không có thay đổi cấu trúc.
- `.gremlins.yaml` chỉ thêm ghi chú (QĐ-182-2); `make ci` toàn bộ thay bằng ci-task + lượt từ xa (QĐ-182-5).
- Test bắt đột biến của inbound, send, automation, live do bốn agent con viết theo danh sách đột biến còn sống (mỗi
  agent một tệp `mutation_m4_test.go`, kiểm bằng overlay); tôi đọc lại, bỏ một test dựa vào khe giờ thật 0,8 s
  (`ManagerLocalExpiryBeforeDatabaseLease`, trái luật Task 400) và sửa một test chập chờn trên CI
  (`TestMutationM4_ForcedSSENeverFallsBackToPoll`).

## Việc phát hiện thêm

- Thêm vùng cao M4 vào `HIGH_RISK_DIRS` (tệp `lib.sh` của hook, chủ shop sửa): `internal/modules/channel
  internal/adapters/facebook/signature.go internal/adapters/facebook/policy.go internal/adapters/facebook/send.go
  internal/adapters/facebook/download.go internal/modules/conversation/internal/inbound
  internal/modules/conversation/internal/send internal/modules/conversation/internal/automation
  internal/modules/conversation/internal/live internal/modules/conversation/internal/access` (khi đó `make mutation` và
  ci-task tự phủ M4).
- Việc ghi lại token Page mang `key_id` cũ sau khi đổi `FIELD_ENC_KEYS` (như `NeedsRewrap` của M3), để không phải kết
  nối lại từng Page.
- Gộp khách (M2 `customer.Merge`): cờ "không liên hệ" của hồ sơ bị gộp không chuyển sang hồ sơ gốc, nên tin tự động có
  thể lại gửi cho khách đã yêu cầu dừng (review độc lập M4, phát hiện 4; `task-152.md` đã ghi chờ chủ shop).
- Echo của tin nhắn riêng (`recipient.comment_id`) chưa được bộ tách đọc: kiểm bằng payload thật ở Task 181 (E3/E8).
- Gremlins chỉ chạy test của chính gói bị đột biến: nhiều đột biến "còn sống" của `inbound`, `send` được test của gói
  khác (`automation`, `conversation`, `httpapi`) bắt. Đo bằng `--integration` (vài giờ máy mỗi gói) khi có bể rảnh.
- Bài học (đưa vào "Bẫy đã gặp" nếu lặp lại): chính sách chung giữa hai task (Task 157 ↔ 166 cho tin tự động không rõ;
  Task 156 ↔ 158 cho lỗi token của lệnh không phải gửi tin) dễ bị bỏ sót — task sau phải đọc lại bảng 04 mục 5.3.

## Reviewer

Kết luận: ĐẠT (vòng 1, trên bản đã gộp `main` 5e14ec76, phần stage 22 tệp).

- Không có phát hiện phải sửa. Reviewer kiểm: `autoActionCommand.OnTerminal` → `settle(…, true)` khớp 04 mục 5.3; đường
  `dead` giữ nguyên (`failed`, `ReportTokenInvalid` khi auth); `replyCommand`/`privateCommand` không ghi đè `OnTerminal`.
  `reportAuth` chạy trong transaction tenant ngắn riêng, chỉ khóa dòng tài khoản kênh (không vấn đề ADR-46); bỏ qua lỗi
  hợp lý (giống `note`); chạy lại vô hại. Mọi test mới `t.Parallel()`, không `t.Setenv`, live chờ theo sự kiện; tên test
  trong tài liệu đều có thật.
- Ghi chú không bắt buộc: số test inbound trong `docs/mutation.md` ghi 14, thật là 15 — đã sửa. ADR-21 còn nhắc
  `TestSourceSignatureAndSplit` của M1 (không còn trong mã) — dòng có từ trước, ngoài phạm vi. `TestMutationM4_QuietStreamStaysOnSSE`
  có thể chập chờn nếu luật "im lặng" đổi sau này — ghi lại.
