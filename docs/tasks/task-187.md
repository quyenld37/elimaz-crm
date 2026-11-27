# Task 187 — AdmissionGate: cờ hết suất, số còn, vé mỗi giây, giới hạn đang xử lý

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 187, M5-04; ca FS04, FS07 phần cờ, FS10, HT27; số mặc định QĐ23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM ký hiệu mới trong `internal/platform/cache` (`GateOptions`, `Admission`, `(*Admission).Release`, `AdmissionGate`, `NewAdmissionGate`, `ErrBadState`, hằng lý do `ReasonBusy`, `ReasonInflight`); không ký hiệu nào đã có bị đổi chữ ký hay hành vi → 0 chỗ tham chiếu phải sửa. Chưa ai gọi `NewAdmissionGate` (M5-21/22 nối sau).
- Gói phụ thuộc ngược (go list): gói nhập `internal/platform/cache` (ngoài test): `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/platform/observability`, `internal/wiring/webcache`. Chỉ thêm mã nên các gói này chỉ cần build lại (hook, ci-task).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (chỉ Valkey: khóa `gt:<tenant>:<item>:<unix>`, `gt:<tenant>:all:<unix>` hạn 3 s; hash `fs:<tenant>:<item>` hạn 10 s; ACL `crm_gate` đã cho `~gt:* ~fs:*` cùng `+@hash +@scripting` — docker/valkey/users.acl.tmpl, không đổi).
- Test sẽ chạy: `go test -race -count=1 -run 'TestAdmissionGate' ./internal/platform/cache/` trong lúc viết (Valkey thật qua `testkit.StartValkey`); cả gói `internal/platform/cache` và các gói nhập nó ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tìm "semaphore", "inflight", "cache."), mã gói `cache`: `Conn.Do` (hạn lệnh, cầu dao, chỉ số), `EntityKey` (dựng khóa an toàn), `eval` (EVAL một khóa), `fallbackCounter` (đếm cửa sổ cố định trong tiến trình, có trần khóa), `floorDiv`, `RateLimiter` (cửa sổ cố định một chủ thể). `Ingest.MaxInFlight` của observability là trần riêng của bộ nhận, không xuất ra để dùng.
- Dùng lại / viết mới vì: dùng lại `Conn.Do`, `EntityKey`, `eval` (SetState), `fallbackCounter` (dự phòng theo giây, dùng chế độ xem `count=false` rồi tính, dưới một mutex của cổng để hai bộ đếm món và shop là một bước). Không dùng thẳng `RateLimiter.Allow`: cổng cần kiểm hai bộ đếm (món và cả shop) nguyên tử trong một script và không tính lượt vào bộ đếm shop khi món đã quá ngưỡng; gọi hai lần `Allow` sẽ tiêu lượt của shop cho yêu cầu bị từ chối. Script hai khóa viết mới (`gateScript`); semaphore là bộ đếm nguyên tử nhỏ viết mới (thư viện chuẩn không có semaphore không chặn; `golang.org/x/sync/semaphore` ngoài ADR-44).

## Soát thiết kế

- 5.1–5.9: không áp dụng: không bảng, không module nghiệp vụ, không đường HTTP; mã nằm ở `internal/platform/cache` (gói hạ tầng, module nghiệp vụ không nhập — luật kiến trúc (l)).
- 5.10 PostgreSQL là nguồn sự thật: cổng chỉ chắn tải. `SoldOut` trả `known = false` khi thiếu khóa, hết hạn (đối chiếu mốc `until` trong hash với đồng hồ của cổng) hay Valkey lỗi; tài liệu của interface ghi bên gọi phải xuống database, không bao giờ coi "không biết" là hết. `Remaining` chỉ để hiện số, bỏ món không có trạng thái. Valkey lỗi ở `Admit` → bộ đếm trong tiến trình với ngưỡng dự phòng (50%), `Degraded`, không bao giờ trả lỗi.
- 5.11: không thêm thư viện (valkey-go, rapid, uuid đã có).
- 5.12: theo mẫu `ratelimit.go` (interface + struct riêng, `Now` giả cho test, chỉ số qua `metrics.Registry`, test trên Valkey thật bằng `testkit.StartValkey`, quy luật bằng rapid).
- 6.1 đồng thời: bộ đếm Valkey tăng trong một script (nguyên tử trên máy chủ); semaphore là vòng CAS trên `atomic.Int64` không bao giờ vượt `MaxInflight`; `Release` dùng `atomic.Bool.CompareAndSwap` nên gọi nhiều lần (kể cả song song) chỉ trả một chỗ; đường dự phòng giữ một mutex qua cả hai bộ đếm. Test 500 goroutine → đúng 50 vé; test `-race`.
- 6.2, 6.3, 6.4, 6.5, 6.6, 6.8, 6.10: không áp dụng: không ghi database, không hàng đợi, không tiền.
- 6.7 quá tải: chính cổng là chỗ trả "bận" với `RetryAfter` (phần còn lại của giây + ngẫu nhiên 0–500 ms để các khách không cùng quay lại một lúc; `inflight` 1 s).
- 6.9 treo: mọi lệnh Valkey đi qua `Conn.Do` (hạn 50 ms, cầu dao); `Admit` không chờ chỗ semaphore (đầy → từ chối ngay).
- 6.11 quy luật: rapid 100 lượt: vé `Admitted` mỗi giây ≤ `PerSecond` (và ≤ `GlobalPerSecond` cả shop), vé đang giữ ≤ `MaxInflight`, khớp mô hình chính xác.
- 6.12 dữ liệu cá nhân: khóa chỉ có mã tenant, mã món (uuid), giây unix; nhãn chỉ số là hằng (`busy`, `inflight`).
- 6.13: không áp dụng: quyền kiểm ở lớp HTTP (M5-05), cổng không quyết định quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| PerSecond 3: 3 lần đầu được, lần 4 busy (RetryAfter 700–1200 ms), giây sau được lại; chỉ số gate_admitted, gate_busy{busy} | `TestAdmissionGate_PerSecond` | `go test -race -count=1 -run 'TestAdmissionGate' ./internal/platform/cache/` | `ok elimaz.com.vn/crm/internal/platform/cache 4.588s` |
| GlobalPerSecond 4, hai món PerSecond 3 → tổng 4; lượt bị món từ chối không tiêu lượt của shop; tenant khác riêng | `TestAdmissionGate_GlobalPerSecond` | như trên | ok |
| MaxInflight 2: lần 3 inflight (RetryAfter 1 s); Release một vé → lần 4 được; Release 10 lần song song và lặp lại không làm âm; gauge gate_inflight | `TestAdmissionGate_MaxInflight` | như trên | ok |
| 500 goroutine cùng giây, PerSecond 50, MaxInflight 1000 → đúng 50 vé | `TestAdmissionGate_Concurrent` | như trên (-race) | ok |
| Khóa bộ đếm hạn ≤ 3 s, hash trạng thái hạn ≤ 10 s | `TestAdmissionGate_KeyTTL` | như trên | ok |
| SetState(0, true) → (true, true); sau 11 s → known = false; chưa có trạng thái, tenant khác → known = false; remaining âm → ErrBadState | `TestAdmissionGate_SoldOutState` | như trên | ok |
| Remaining 3 món, một món chưa có trạng thái → map 2 phần tử; trạng thái quá hạn bị bỏ; danh sách rỗng → map rỗng khác nil | `TestAdmissionGate_Remaining` | như trên | ok |
| Valkey dừng: FallbackPerSecond 1 → lần 2 busy, Degraded; ngưỡng shop dự phòng; SoldOut known = false; SetState ErrUnavailable; Remaining rỗng; gate_degraded 5 | `TestAdmissionGate_ValkeyDown` | như trên | ok |
| Conn nil: mặc định QĐ23 (dự phòng 50% của 100 = 50 vé; MaxInflight 64) | `TestAdmissionGate_NilConnDefaults` | như trên | ok |
| Quy luật rapid 100 lượt: vé mỗi (món, giây) ≤ PerSecond, mỗi giây shop ≤ GlobalPerSecond, đang giữ ≤ MaxInflight, khớp mô hình (cả khi dự phòng), không rò chỗ | `TestAdmissionGate_PropertyLimits` | như trên | ok |
| Test kiến trúc | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 19.328s` |
| Lint, mã lặp | golangci-lint, jscpd | `golangci-lint run ./internal/platform/cache/...`; `make jscpd` | `0 issues.`; `Found 0 clones.` |
| Danh mục dùng chung có ký hiệu mới | catalog-check | `make catalog` (lượt ci-task đầu đỏ ở catalog-check) | `docs/danh-muc-dung-chung.md` +21 dòng |
| Cổng của task trên bản đã gộp main b256382b (race gói cache, plain các gói phụ thuộc) | ci-task | ci-task (lượt 2) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/45931119907`; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh b256382b…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hash `fs:<tenant>:<item>` có thêm trường `until` (mốc hết hạn, ms, theo đồng hồ của cổng) ngoài `remaining`, `soldout`: `SoldOut`/`Remaining` coi trạng thái quá 10 s là "không biết" theo đúng đồng hồ của cổng (test bằng đồng hồ giả, không chờ 11 s thật). PEXPIRE 10 s vẫn đặt như kế hoạch.
- `NewAdmissionGate` nhận `c == nil` (luôn dự phòng, SoldOut luôn không biết) cho cấu hình chưa có Valkey gate.

## Quyết định thay chủ shop

- Số mặc định lấy đúng QĐ23 ("chưa có số đo thì 100 và 300", 64 đang xử lý, dự phòng 50%). Đọc `FLASH_ADMIT_*` từ cấu hình là việc của bước nối (M5-21/22), không làm ở đây. Dễ đảo: chỉ là tham số `GateOptions`.
- Lượt bị ngưỡng món từ chối không tính vào bộ đếm shop; lượt bị shop từ chối trả lại lượt của món (script DECR): món nóng không ăn hết lượt của cả shop. Phương án khác: tăng cả hai rồi so (đơn giản hơn nhưng lượt bị từ chối vẫn tiêu lượt của shop).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (vòng 1, không có phát hiện phải sửa). Đã soát semaphore CAS, Release lặp/song song, script Lua
(INCR rồi PTTL<0 → PEXPIRE; món quá ngưỡng không chạm shop; shop quá ngưỡng DECR món), ACL `crm_gate`, đường dự
phòng dưới `fbMu`, hạn sống 3 s/10 s và `until`, known=false không bao giờ thành "hết", RetryAfter > 0.
Ghi chú không bắt buộc (không sửa, không ảnh hưởng tính đúng): tài liệu `SetState` ghi "bọc ErrUnavailable" nhưng lỗi
lệnh (vd NOPERM) do `Conn.Do` trả nguyên — bên gọi chỉ bỏ qua lỗi; gauge `gate_inflight` có thể tạm hiện số cũ khi
song song; EVAL quá hạn sau khi máy chủ đã chạy thì lượt đó có thể bị đếm hai lần (Valkey + dự phòng) — chấp nhận vì
cổng chỉ chắn tải.
