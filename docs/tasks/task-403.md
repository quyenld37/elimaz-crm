# Task 403 — Đóng M1: mutation testing vùng cao (M1-55b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`, Task 56 (M1-55), đoạn "Chia nhỏ 19/11"
và Step 2. Vùng rủi ro cao của M1: dòng "Vùng rủi ro cao của M1" ở phần Global Constraints của kế hoạch. Loại việc
(quy trình mục 3): A (chỉ thêm test và tài liệu; không đổi mã chạy).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task chỉ thêm tệp `*_test.go` (gói `_test` hoặc
  gói trong) cho các đột biến còn sống; không đổi mã chạy, không đổi chữ ký nào.
- Gói phụ thuộc ngược (go list): không có, vì không đổi mã không phải test. Test thêm chỉ chạy trong gói của nó.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- `HIGH_RISK_DIRS` (`scripts/agent/lib.sh`, vùng cấm sửa của phiên): **không sửa**; dòng đề xuất ở mục "Dừng để hỏi".
  Các gói vùng cao M1 chưa có trong danh sách được chạy gremlins bằng đúng lệnh mà `make mutation` sẽ chạy cho mục đó
  (thư mục: `gremlins unleash --config .gremlins.yaml ./<thư mục>`; tệp lẻ: cùng lệnh kèm `-E` loại mọi tệp khác của
  gói như `mutation_exclude` của Makefile).
- Tệp đổi: `docs/mutation.md` (lần chạy 19/11 và lý do từng đột biến còn sống), các tệp `*_test.go` mới hoặc thêm ca
  trong gói vùng cao M1 (liệt kê ở bảng bằng chứng), `docs/tasks/task-403.md`. Ba hàm chỉ có trong test thêm vào
  `export_test.go` (không thuộc mã chạy): `order.MergeLines`, `files.LimitedBody`, `files.SetRegenerateTimeout`.
- Gộp `main` (một lần, 20/11, tua nhanh tới Task 89): xung đột duy nhất ở `docs/mutation.md`, giữ cả mục "Lần chạy M2"
  của Task 89 và mục của Task 403.
- Test sẽ chạy: gremlins trên vùng cao M1 (`inventory`, `order`, `cash`, `fulfillment/status.go`, `platform/files`,
  `imager`, `catalog/internal/importer/apply.go`, `catalog/article_html.go`, `identity` gồm `staff.go`, `roles.go`);
  `go test -race` của từng gói có test thêm; `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit), các tệp
  `env_test.go`, `export_test.go`, `helpers_test.go` sẵn có của từng gói.
- Dùng lại / viết mới vì: test thêm dùng lại môi trường test sẵn có của từng gói (`env_test.go`, `testkit`), không
  thêm helper dùng chung mới; cấu hình gremlins và đích `make mutation` dùng nguyên (Task 23, 392).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.12: không áp dụng: task không đổi mã chạy, không thêm bảng, truy vấn, đường HTTP, sự kiện, lời gọi ngoài hay
  thư viện. Test thêm nằm trong gói của mã nó kiểm, dùng API công khai hoặc `export_test.go` sẵn có.

Mục 6 (đúng khi chạy đồng thời):
- 6.1–6.13: không áp dụng cho mã chạy (không đổi). Với test thêm: test chạm database chạy trong database riêng do
  testkit tạo, trong `tenancy.WithTenant` (bẫy RLS); không lấy thời gian thật làm tiêu chí đúng/sai (Task 400).
- Tải máy: gremlins giữ `workers: 2` của `.gremlins.yaml` và `GOFLAGS=-p=3` của phiên, không tăng song song.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Vùng cao M1 đạt ≥ 80% (`apply.go`, `status.go`, `cash`, `imager`, `article_html.go`) | lượt gremlins từng mục | `gremlins unleash --config .gremlins.yaml -E '/' [-E '^<tệp khác>\.go$'…] ./<thư mục>` (`docs/mutation.md`) | trước test thêm 80,0% / 100% / 86,0% / 73,6% / 80,4% → sau 100% / 100% / 100% / 99,3% / 92,9% |
| `platform/files` ≥ 80% | gremlins hai phần (trừ `memstore.go`) | như trên, phần 2 giữ 9 tệp | 265/350 = 75,7% → 330/350 = 94,3% (18 đột biến chưa chạy, xem Khác kế hoạch) |
| `inventory` ≥ 80% | gremlins cả gói (trừ `internal/store`) | `gremlins unleash --config .gremlins.yaml --timeout-coefficient 5 -E '/' ./internal/modules/inventory` | 182/198 = 91,9% → 192/198 = 97,0% (45 chưa chạy) |
| `order` (6 tệp M1) ≥ 80% | gremlins 6 tệp | như trên kèm `-E` loại tệp M2 | 54/70 = 77,1% → 69/70 = 98,6%; cả gói (Task 89) 800/879 → 815/879 |
| `identity` (`staff.go`, `roles.go`) ≥ 80% | lần đo 18/11 | không chạy lại (quyết định 3) | 91,53% |
| Mỗi đột biến sống được giết hoặc có lý do vô hại | "Test thêm (Task 403)" và "Đột biến còn sống… (Task 403)" trong `docs/mutation.md` | cài tay từng đột biến vào bản sao mã: `python3 handmut.py <bản sao> <gói> <tệp> <dòng> <cột> <KIỂU> -run <test>` | mọi đột biến ghi "giết" → test đỏ (KILLED); ví dụ `process.go:581:15` → `ODD:w0: tệp ready với kết quả imager lạ`; `returns.go:75:118`, `create.go:104:25`, `transition.go:92:79` → KILLED |
| Test thêm qua trên mã thật | `TestProcessor_*`, `TestRegenerate_*`, `TestFileStore_PresignRejectsBadParams`, `TestLimitedBody_Boundary`, `TestListLost_Pages`, `TestCreateUpload_CreatedBy`, `TestOriginalTTL`, `TestFileProcess_ManifestPixelLimit`, `TestRegisterPurpose_PanicsOnEachChangedField`, `TestReplaceLines_MaxLines`, `TestListDocuments_LimitBounds`, `TestPostDocument_WriterNotWired`, `TestMergeLines_Bounds`, `TestCreateManual_WriterNotWired`, `TestApplyTransition_Signals`, ca thêm vào test cũ | `go test -count=1 -run '<tên>' ./internal/<gói>/` từng gói | từng lệnh `ok` |
| Cổng kiểm của task | toàn bộ | `scripts/agent/ci-task.sh` | lần 1 (03:00): lint đỏ (funlen, gocognit, gofumpt, gosec G115) và jscpd 8 đoạn trùng mới trong test thêm → đã sửa (hàm dùng chung `seedManifest`, `resetFile`, `goodResult`, `assertRecheck`, `processPhoto`, `wantSourceFailedNoCalls`, `draftOrder`). Lần 2 (05:49–07:18): build, vet, lint (`0 issues`), catalog, jscpd (`Found 0 clones`), gitleaks, oasdiff, test-guard **xanh**; test đỏ vì môi trường lúc máy ngủ: đồng hồ lệch (`muốn hẹn lại theo Retry-After 7 s, còn 870.6 s`, `tuổi … -9m51s`) và cache `go-build` bị xóa giữa chừng (`can't import facts … go-build/…` → `[build failed]`). Chạy lại đúng các gói đó: `go test -race ./internal/platform/{files,metrics,observability,realtime,schedules,selftest,tenancy,webhooks,xlsx}/... ./internal/queries/... ./internal/testkit/... ./tests/arch/... ./tools/{fakecarrier,loadkit}/...` → mọi gói `ok` trừ ba test đo thời gian thật (máy còn ngủ); chạy lại riêng lúc 08:26 khi máy thức → `ok` (files 13.351s, metrics 6.985s, observability 2.359s); cả gói `files`: `ok … 85.721s`. Không chạy lại `tests/e2e` (gói không đổi). |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- **Chưa đo hết.** Từ 01:39 ngày 20/11 máy dev ngủ ("Dark Wake Thermal Emergency" trong `pmset -g log`), chỉ thức
  ~1 phút mỗi 15 phút. Còn chưa chạy: 45/248 đột biến của `inventory`, 18/351 của phần 2 `platform/files` (cuối
  `verify.go`); đột biến không phủ cài tay được 83/154 (`files`), 6/66 (`inventory`). Hiệu quả tính trên phần đã chạy;
  mọi mục đã chạy đều ≥ 80% sau test thêm. Lượt CI theo lịch sẽ đo đủ khi các thư mục vào `HIGH_RISK_DIRS`.

## Việc phát hiện thêm

- `MemStore` (kho giả của test) không mô phỏng lifecycle xóa `crm-tmp`, nên test dựng lại tệp mất trước đây vẫn qua
  khi bỏ nhánh "đọc từ ảnh gốc". Đã sửa riêng `TestVerifier_MarksMissingLost` (xóa tay tệp tạm); có thể cho `MemStore`
  một hàm hết hạn `crm-tmp` để test khác khỏi phải nhớ.
- `inventory`: cả gói test > 10 phút khi máy tải nặng; `make mutation` trên gói này nên có lịch CI riêng.
- Phần chưa đo ở "Khác kế hoạch".

## Quyết định thay chủ shop

1. **Không sửa `HIGH_RISK_DIRS`** (`scripts/agent/lib.sh` là vùng cấm sửa của phiên; danh sách này còn quyết định
   phạm vi test của hook và `ci-task.sh`). Chạy gremlins thẳng trên các mục vùng cao M1 bằng đúng cách `make mutation`
   sẽ chạy khi mục được thêm; dòng đề xuất ở "Dừng để hỏi". Phương án khác: tự sửa `lib.sh` — trái luật phiên.
2. **Gói `order` chỉ đột biến các tệp của M1** (`code.go`, `create.go`, `events.go`, `order.go`,
   `shipment_progress.go`, `transition.go`; xác định bằng commit tạo tệp). Lý do: gói nay chứa cả mã M2 (911 đột biến
   chạy được, test cả gói ~320 s mỗi lượt khi máy bận → ước ~40 giờ); phần M2 (sửa, hủy, nháp, mã giảm giá, trả hàng…)
   đo ở task đóng M2. Phương án khác: chạy cả gói — không xong trong phiên.
3. **`identity` (`staff.go`, `roles.go`) không chạy lại, dùng lần đo 18/11** (91,53%, 108/118). Từ lần đo đó hai tệp
   chỉ đổi ở commit `5d3e3a3` (thêm chuỗi quyền M2 vào danh sách vai trò mẫu và nhãn) — không thêm toán tử nào nên không
   sinh đột biến mới. Phương án khác: chạy lại 126 đột biến (~3 giờ trên máy đang tải 16–28) — không đổi kết luận.
4. **Không đột biến mã sinh sqlc trong thư mục con `internal/store`** (cờ `-E '/'` như cách Makefile làm với tệp lẻ):
   mã sinh không sửa tay, đột biến ở đó đo test của sqlc chứ không phải luật nghiệp vụ. Ghi trong `docs/mutation.md`;
   khi điều phối viên thêm thư mục vào `HIGH_RISK_DIRS`, `make mutation` sẽ đột biến cả thư mục con (số có thể khác).
5. **`platform/files/memstore.go` không tính vào hiệu quả.** `MemStore` là kho tệp giả trong bộ nhớ cho test (chỉ
   `internal/testkit` và test dùng; `cmd/crm` nối `S3Store`). Đột biến sống ở đó (kiểm tham số của kho giả) không đo
   luật của CRM. Phương án khác: thêm test cho kho giả — tốn công, không bảo vệ mã chạy thật.
6. **Lượt `platform/files` chia hai phần và đọc số từ log** (phần 1 bị dừng giữa chừng; phần 2 giữ 9 tệp còn lại).
   Đột biến sống của cả hai phần được cài tay lại trên bản sao mã có test mới (như các gói khác).
7. **Dừng đo khi máy ngủ** (01:55 ngày 20/11) thay vì chờ tới sáng: phần còn lại cần nhiều giờ máy thức; dành máy cho
   cổng kiểm và reviewer. Ghi rõ phần chưa chạy (Khác kế hoạch). Phương án khác: chờ chủ shop mở máy — task không xong
   trước sáng. Đã thử `caffeinate -i -w <PID>` cho các lượt của mình (chỉ chặn ngủ khi rảnh, không thắng gập máy).
8. **Đột biến không phủ của `inventory` cài tay chỉ chạy test của tệp tương ứng** (bỏ `*Deadlock*`); ca "sống" chạy
   lại với test rộng hơn (3 ca ở `reserve.go` → bị giết bởi `ByItems|Movement`). Lý do: cả gói > 10 phút một lượt.
9. **Giữ `TestCreateManual_WriterNotWired` dù đột biến `order.go:147` tương đương**: test giữ hành vi "Writer thiếu phụ
   thuộc thì lỗi" ở mức đơn hàng; không đổi mã.

## Dừng để hỏi

- **Thêm vùng cao M1 vào `HIGH_RISK_DIRS`** (điều phối viên sửa `scripts/agent/lib.sh`). Dòng đề xuất (giữ thứ tự cũ,
  thêm phần M1 theo dòng "Vùng rủi ro cao của M1" của kế hoạch; `identity`, `inventory` đã có):

  ```
  HIGH_RISK_DIRS="internal/platform/tenancy internal/platform/queue internal/platform/commands internal/platform/idempotency internal/platform/db internal/platform/webhooks internal/modules/identity internal/modules/inventory internal/httpapi/guard.go internal/modules/order internal/modules/cash internal/modules/fulfillment/status.go internal/platform/files internal/imager internal/modules/catalog/internal/importer/apply.go internal/modules/catalog/article_html.go"
  ```

  Lưu ý cho người sửa: (a) Makefile đặt tên tệp JSON theo `basename` tệp lẻ (`guard`, `status`, `apply`,
  `article_html`) — không trùng; (b) thêm `internal/modules/order` làm mọi task chạm `order` chạy cả bộ `order`
  (~5 phút) ở `ci-task.sh`; (c) `make mutation` trên cả gói `order` hiện ~900 đột biến, nên cần lịch CI riêng hoặc
  giới hạn tệp như quyết định 2; (d) `TestMakeMutation_CoversHighRiskDirs` trong `tests/deploy/ci_test.go` đọc thẳng
  danh sách nên không cần sửa test.

## Reviewer

Kết luận: ĐẠT (lượt 2, 20/11).

- Lượt 1: CẦN SỬA — `lost_test.go:37` so `lost_at` (đồng hồ database) với `time.Since` (đồng hồ máy test), đỏ oan khi
  hai đồng hồ lệch (đã thấy lệch ~10 phút sau khi máy ngủ). Đã sửa: so hai mốc database với nhau. Ghi chú không bắt
  buộc đã làm: sửa câu "thêm ca `ODD:w0`" trong `docs/mutation.md` (test là mới). Không nới hạn 200 ms của
  `TestClient_RetryAfterBounds`: nới lên 2 s cho client kịp chờ Retry-After tối thiểu 1 s, đổi điều test kiểm.
- Lượt 2: ĐẠT, không phát hiện phải sửa. Ghi chú: `HIGH_RISK_DIRS` chưa thêm (Dừng để hỏi) và phần đo còn thiếu (Khác
  kế hoạch) cần chủ shop/điều phối viên quyết trước khi coi Step 2 là xong.
