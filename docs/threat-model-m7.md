# Threat model M7 — đăng nhập riêng, dữ liệu ban đầu, đêm mở

Bản đầu viết ở Task M7-01 (số toàn cục 246, 28/11/2026), trước khi có mã M7, theo mẫu `docs/threat-model-m6.md`. Đây
là bước 2 của "Cách làm việc ở mỗi mốc" (spec 08). Task M7-35 (280) hoàn thiện bản này: điền tên hàm test có thật vào
cột "Test", đổi mỗi dòng **Chưa có** thành tên test hoặc ghi vì sao chấp nhận, rồi soát theo ASVS 5.0.0 (mục cuối).

**Phạm vi:** chỉ phần mới của M7. Ranh giới của M0 (Access, `ingest`, tiến trình → PostgreSQL, `cf-connecting-ip`),
M1 (kho tệp, đường dẫn ký sẵn, `xlsx`), M2–M6 nằm ở `docs/threat-model-m{0,1,2,3,5,6}.md`; M7 không nhắc lại.
Tên hàm, bảng, view thật của mốc trước: `docs/khoi-dong/doi-chieu-gia-dinh-m7.md` (Task 245).

**Nguồn:**

- Kế hoạch `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md`: Q01–Q25, Global Constraints,
  Review Focus, Task 248–280.
- Spec `04-kien-truc.md` mục 11, `05` mục 7.2, `06`; ADR-11, 25, 35, 41, 46, 47.

**Cách đọc:**

- Cột "Task" ghi số toàn cục kèm số nội bộ M7-NN. Cột "Test" ghi tên tệp test kế hoạch giao kèm ca; Task 280 điền
  tên hàm. Dòng ghi **(thêm)** là ca kế hoạch chưa có, Task 246 đề xuất thêm (xem `docs/tasks/task-246.md`, "Việc
  phát hiện thêm").
- Task in nghiêng *(để sau)* thuộc nhóm chưa làm ở đợt 28/11 (248–259, 266–268, 270–272, 274–280). Đợt 28/11 làm
  245, 246, 247, 260–265, 269, 273. Dòng của task để sau vẫn giữ: mối đe dọa có, chỉ test chưa viết.
- STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu

```
                         ┌───────────────── đường chính (M0, không đổi) ─────────────────┐
 Nhân viên ──(Cloudflare Access, hai bước)──▶ crm.<miền> ──▶ crm-api /api/v1/**  (thẻ Access → AuthUser)
                                                               │  đăng ký mật khẩu, TOTP, mã khôi phục (B2)
                                                               │  quản trị: danh sách đăng ký, phiên, đặt lại bước hai
                                                               │
 (B1) Người có quyền trên nền tảng triển khai ── đặt DIRECT_LOGIN_UNTIL (≤ 24 giờ) ── deploy lại crm-api
                                                               │  Recorder → platform.direct_login_windows → cảnh báo gấp
                                                               ▼
 Nhân viên ──(Cloudflare, KHÔNG Access, không đệm,          crm-direct.<miền> ──▶ crm-api  DirectHost (theo Host,
              giới hạn tần suất /direct/api/login)                                   404 mọi đường khi đóng)
                                                               │  /direct/api/login, /second-factor, /enroll…  (B3)
                                                               │     argon2id (≤ 4 cùng lúc) ─ khóa tạm ─ loginlimit IP
                                                               │  cookie __Host-crm_session → sessionAuth → guard (B4)
                                                               │  GET/HEAD khác → reverse proxy crm-console
                                                               ▼
                                                      PostgreSQL: identity.user_credentials, user_second_factors
                                                      (secret_enc qua fieldcrypt), user_sessions (sha256 mã phiên),
                                                      login_attempts (chỉ ghi thêm), idempotency_keys (no-replay:
                                                      không thân trả lời)

 (B5) Quản trị ──▶ resetUserSecondFactor ──▶ mã đặt lại một lần (30 phút) ══ đi NGOÀI hệ thống (gọi điện, gặp) ══▶ nhân viên
      Máy chủ ──▶ crm identity reset-factor (crm_app, actor system/cli) → stdout → chủ shop; cảnh báo gấp sau commit

 (B6) Nhân viên kho ──▶ createUpload (mục đích inventory_count_import | order_fallback_import, ≤ 5 MiB, .xlsx)
         ──▶ kho tệp riêng tư (bucket private) ──▶ worker FileStore.Open + xlsx.Read (giới hạn M1)
         ──▶ áp số đếm (stocktake_lines) | order.Create mỗi dòng một transaction
      Tệp đơn dự phòng (tên, SĐT, địa chỉ người nhận) nằm trên máy nhân viên khi CRM ngừng, rồi trên kho tệp riêng tư

 (B7) Dữ liệu ban đầu và đêm mở (nhân viên, console → crm-api, x-permission):
      catalog publish/unpublish │ phiên mở sổ, đếm cộng dồn │ hàng hoàn đơn cũ │ go-live Page │ sổ đêm mở,
      mốc A/B, người quyết, openSales │ theo dõi 14 ngày │ báo cáo sẵn sàng
      worker ──(lệnh ra ngoài, chỉ đọc)──▶ Meta Graph API: đọc lại tin/comment một khung giờ (đối chiếu T2)
      hãng vận chuyển ──(webhook M3)──▶ ingest → inbound.Handler: vận đơn không có trong CRM → unmatched_carrier_events
      website THỬ ──(khóa dịch vụ có web.catalog.preview)──▶ crm-api /web/catalog: thấy cả sản phẩm draft (preview: true)
```

**Dữ liệu nhạy cảm mới:**

- Mật khẩu (chỉ dạng băm argon2id), bí mật TOTP (mã hóa bằng `FIELD_ENC_KEYS`), mã khôi phục, mã đặt lại, mã phiên
  (chỉ `sha256` trong database). Bí mật một lần đi qua câu trả lời HTTP đúng một lần.
- Tệp đơn dự phòng: tên, SĐT, địa chỉ người nhận (dữ liệu cá nhân).
- Sản phẩm chưa mở bán (bí mật kinh doanh tới đêm mở).
- Nhật ký đăng nhập: IP, giờ (dữ liệu cá nhân của nhân viên).

## (B1) Cửa sổ đường riêng: `DIRECT_LOGIN_UNTIL`, tên miền riêng

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| T | Người có quyền trên nền tảng (hoặc kẻ chiếm tài khoản nền tảng) đặt `DIRECT_LOGIN_UNTIL` rất xa để mở cửa sau lâu dài | Hạn tính từ lúc tiến trình khởi động; quá 24 giờ → coi như **tắt** (đóng, không mở) và cảnh báo gấp `direct-login-config:too_long` | *248 (M7-03)*, *252 (M7-07)* | `internal/platform/config/directlogin_test.go` (25 giờ → `too_long`), `directhost_test.go` (25 giờ → mọi yêu cầu 404), `rules_directlogin_test.go` |
| R | Bật đường riêng mà không ai biết | `Recorder` ghi `platform.direct_login_windows` lúc khởi động và mỗi 60 s; luật `CheckRules` báo `urgent` `direct-login-open:<until>` nhắc lại mỗi 30 phút tới khi hết hạn; người được đặt biến do chủ shop nêu tên | *252*, *259 (M7-14)* | `recorder_test.go` (hai tiến trình → 1 dòng), `rules_directlogin_test.go`, `tests/e2e/directlogin_test.go` (khởi động với `UNTIL = now + 2h` → cảnh báo gấp) |
| S, I | Đường riêng chưa bật vẫn lộ ra ngoài (có trang đăng nhập để dò, có `/health`) — DN06 | Khi đóng: **mọi** method, mọi đường dẫn trên tên miền riêng → 404 giống hệt đường dẫn lạ của tên miền chính (cùng thân trừ `request_id`, cùng tập header); khi mở vẫn không phục vụ `/health/*`; đường dẫn `/direct/` trên tên miền chính → 404 | *252*, *259*, *277 (M7-32)* | `directhost_test.go` (6 đường dẫn mẫu so thân và header), `tests/e2e/directlogin_test.go`, Task 277 Step 2 (từ máy ngoài) |
| S | Giả `Host` qua `X-Forwarded-Host` để chạm đường riêng từ tên miền chính (hoặc ngược lại) | `DirectHost` chỉ đọc `r.Host` (bỏ cổng, chữ thường), không đọc `X-Forwarded-Host`; đường chính bỏ qua cookie, đường riêng bỏ qua thẻ Access | *252*, *255 (M7-10)* | `directhost_test.go` (`X-Forwarded-Host` → không khớp), `session_auth_test.go` (cookie ở đường chính bị bỏ qua) |
| I | Cloudflare đệm trang hay câu trả lời có cookie của tên miền riêng | `Cache-Control: no-store` trên mọi câu trả lời của tên miền riêng; tên miền riêng tắt đệm trên Cloudflare | *252*, *277* | `directhost_test.go` (header `no-store`), Task 277 (cấu hình Cloudflare, ghi ở `production-dung-may.md`) |
| S | Vào thẳng máy gốc, giả `cf-connecting-ip` để né `loginlimit` | Máy chỉ nhận kết nối qua Cloudflare (M0); `loginlimit` dùng cùng cách lấy IP của M0 | M0, *277* | `TestProdCompose_OnlyCloudflareIngress`, `TestClientIP` (M0), Task 277 Step 2 |

## (B2) Đăng ký mật khẩu, TOTP, mã khôi phục qua đường chính

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| S | Mật khẩu đoán được | 12–128 ký tự sau NFC, chặn 10.000 mật khẩu phổ biến (nhúng, không gọi dịch vụ ngoài) và mật khẩu chứa phần trước `@` của email | *250 (M7-05)*, *253 (M7-08)* | `password_test.go` (11/12/129 ký tự, `Password1234`, chứa email), `handlers_me_directlogin_test.go` (422 `password_policy` kèm `reason`) |
| I | Mật khẩu lộ khi database bị đọc | argon2id `m = 19 MiB, t = 2, p = 1`, muối 16 byte, chuỗi PHC; so bằng `subtle.ConstantTimeCompare`; thông số cũ → băm lại lần đăng nhập đúng kế tiếp | *250* | `password_test.go`, `password_property_test.go` (rapid) |
| I | Bí mật TOTP, mã khôi phục, mã đặt lại nằm trong `idempotency_keys` (route trả bí mật) | Bốn route khai `x-idempotency: no-replay` (ADR-47): khóa vẫn chống chạy hai lần, `response_body` NULL; gửi lại → 409 `already_processed` | *248*, *253*, *256 (M7-11)* | `routes_test.go` (đúng bốn route `no-replay`), `handlers_me_directlogin_test.go` (`response_body` NULL, không chứa `secret`/mã), `handlers_reset_test.go` |
| I | Bí mật TOTP lộ khi database hay bản sao lưu bị đọc | `secret_enc` qua `fieldcrypt.Keyring.Seal` với AAD gắn tenant, bảng, cột, dòng; mã khôi phục chỉ `sha256` | *249 (M7-04)*, *251 (M7-06)* | `totp_test.go` (cột `secret_enc` không chứa bí mật dạng byte hay chữ), `recovery_test.go` (database chỉ có `sha256`), `schema_m7_test.go` (CHECK `secret_enc`/`secret_kid`/`secret_hash`) |
| T | Bấm hai lần tạo hai bộ mã khôi phục (bộ đầu người dùng đã chép thành vô hiệu mà không biết) | Khóa chống trùng `no-replay`: 50 yêu cầu cùng khóa → 1 lần chạy | *253* | `handlers_me_directlogin_test.go` (50 `regenerateMyRecoveryCodes` song song → 1 bộ) |
| S | Người lấy được phiên đường riêng đổi mật khẩu để chiếm tài khoản | Đổi trên đường riêng phải có `currentPassword` đúng (sai tính vào khóa tạm); đổi mật khẩu thu hồi mọi phiên đường riêng khác (`password_changed`) | *253* | `handlers_me_directlogin_test.go` (`current_password_required`, `failed_count` tăng, 2 phiên `revoked`) |
| R | Chối đã đặt mật khẩu, đã bật TOTP | Audit `identity.password_set` (`changed_fields = {password}`, không giá trị), `identity.totp_activated` | *253* | `handlers_me_directlogin_test.go` (JSON audit `identity.password_set` không chứa mật khẩu); **(thêm)** `handlers_me_directlogin_test.go`: `confirmMyTotp` đúng → 1 dòng audit `identity.totp_activated`, không chứa bí mật hay mã khôi phục |
| I | Nhắc đăng ký lộ tên, email nhân viên qua kênh cảnh báo | Văn bản chỉ có số đếm ("N nhân viên chưa đăng ký…") | *253* | `enroll_reminder_test.go` (văn bản không chứa email, tên của dữ liệu thử) |

## (B3) Đăng nhập trên đường riêng: mật khẩu → bước hai

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| S | Dò mật khẩu một tài khoản (DN02) | 5 lần sai trong 15 phút → khóa tạm 15 phút; đếm bằng một câu `UPDATE … RETURNING` có điều kiện (không đọc ra Go rồi ghi lại); lần đúng xóa đếm bằng `UPDATE` có điều kiện `locked_until` | *254 (M7-09)* | `login_test.go` (5 sai → lần 6 đúng vẫn 401, +16 phút → 200; **20 goroutine sai cùng lúc → `failed_count = 20`, 1 cảnh báo**), `login_property_test.go` (rapid: không phiên mới trong khoảng khóa) |
| S | Dò nhiều tài khoản từ một máy (rải mật khẩu) | `loginlimit`: 20 lần thất bại mỗi IP trong 10 phút → 429 `too_many_attempts` 10 phút, kiểm **trước** khi đọc database; Cloudflare giới hạn tần suất `/direct/api/login`; ≥ 3 người bị khóa hoặc ≥ 50 lần sai trong 10 phút → cảnh báo gấp `direct-login-attack` | *254*, *277* | `handlers_directlogin_test.go` (21 lần từ `203.0.113.7` → 429; IP khác → 401), `login_test.go` (3 người bị khóa → 1 cảnh báo `urgent`) |
| S | Dùng lại mã TOTP vừa nhìn thấy (vai, quay màn hình, phát lại) | Mỗi bước thời gian dùng được một lần: `UPDATE … WHERE last_used_step < $s`, 0 dòng → `ErrCodeReused`; lệch ±1 chu kỳ | *251* | `totp_test.go` (**20 goroutine cùng mã đúng → đúng 1 `nil`**; mã bước `t−1` sau khi dùng `t` → `ErrCodeReused`), `totp_property_test.go` (rapid) |
| S | Dò mã 6 số hay mã khôi phục ở bước hai | Sai bước hai tính vào cùng bộ đếm khóa tạm; mã khôi phục 50 bit, dùng một lần (`UPDATE … status = 'active'` → `used`) | *251*, *254* | `login_test.go` (TOTP sai 5 lần → khóa), `recovery_test.go` (20 goroutine cùng mã → đúng 1 thành công) |
| S | Phiên chờ bước hai dùng như phiên thật (vào chỉ với một bước) — Review Focus 2 | Phiên `pending_second_factor` hạn 5 phút; `LoadSession` chỉ nhận `stage = 'active'`; qua bước hai thì thu hồi phiên chờ (`rotated`) và cấp **mã mới** (chống cố định phiên) | *254*, *255* | `login_test.go` (**cookie phiên chờ gọi `GET /api/v1/me` → 401 `session_expired`**; cookie mới khác mã chờ; +6 phút → `pending_expired`) |
| I | Liệt kê email có tài khoản (câu báo lỗi khác nhau) | Một câu `login_failed` cho mọi trường hợp (sai, email lạ, chưa đăng ký, đang khóa, bị khóa bởi quản trị); email lạ chạy `DummyVerify` | *254* | `handlers_directlogin_test.go` (email lạ cùng mã, cùng thân; `DummyVerify` gọi 1 lần) |
| I | Liệt kê email bằng **thời gian** trả lời: nhánh "đang khóa", "chưa đăng ký", "bị khóa bởi quản trị" trả ngay không băm, nhanh hơn email lạ (có `DummyVerify`) và mật khẩu sai | Mọi đường thất bại tốn đúng một lần băm: ba nhánh không băm gọi `DummyVerify` như email lạ (vẫn trong giới hạn 4 chỗ băm và `loginlimit`) | *254* | **(thêm)** `login_test.go`: email lạ, người `locked`, chưa có mật khẩu, đang khóa tạm, mật khẩu sai → mỗi ca đúng 1 lần băm (đếm bằng móc của `Hasher`) |
| D | Dồn đăng nhập làm hết bộ nhớ, CPU vì argon2id (19 MiB mỗi lần) | Tối đa 4 lần băm cùng lúc (≤ 76 MiB), chờ chỗ tối đa 2 s rồi 503 `retry_later` có `Retry-After: 1`; băm **ngoài** transaction (không giữ khóa, kết nối); `loginlimit` và giới hạn Cloudflare chặn trước | *250*, *254*, *259*, *279 (M7-34)* | `password_test.go` (10 goroutine, giới hạn 2 → đo được 2; `ErrBusy`), `handlers_directlogin_test.go` (Hasher bận → 503), `loadtest/k6/direct_login.js` (50 người/10 s, RSS tăng ≤ 100 MB, 503 ≤ 5%) |
| D | Khóa tạm bị lợi dụng để khóa người thật (gõ sai 5 lần email của người khác) | Chấp nhận (DN02 đòi khóa): khóa chỉ 15 phút, chỉ ở đường riêng (đường chính Access không bị ảnh hưởng), có cảnh báo; quản trị thấy được qua cảnh báo `direct-login-locked` | *254* | `login_test.go` (khóa 15 phút rồi mở) — rủi ro chấp nhận |
| R | Chối đã đăng nhập qua đường riêng | Mỗi lần: dòng `login_attempts` (chỉ ghi thêm, trigger chặn `UPDATE`/`DELETE`, `crm_app` không có quyền sửa), audit `identity.direct_login`, cảnh báo thường `direct-login:S-…`; dùng mã khôi phục → `direct-login-recovery:U-…` | *249*, *254* | `schema_m7_test.go` (`UPDATE` → `append-only`/lỗi quyền), `login_test.go` |
| I | Mật khẩu, email, mã TOTP, mã phiên lọt vào log, dấu vết, cảnh báo | Người dùng nhắc bằng `U-<8>`, phiên bằng `S-<8>`; payload việc `identity.login_alert` chỉ có khóa, mức, mã; cảnh báo gửi **sau** transaction (C21) | *254* | `login_test.go` (log JSON của cả bộ test không chứa mật khẩu, email, mã TOTP, mã phiên) |

## (B4) Phiên cookie trên đường riêng

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| I | Mã phiên bị đọc bởi mã chạy trên trang (XSS) — DN07 | Cookie `__Host-crm_session`, `HttpOnly; Secure; SameSite=Strict; Path=/`, không `Domain`; thân trả lời không bao giờ có mã phiên; màn đăng nhập không ghi gì vào `localStorage`/`sessionStorage`/IndexedDB | *254*, *257 (M7-12)*, *259* | `handlers_directlogin_test.go` (cờ cookie, thân không chứa mã), `LoginPage.test.tsx` (theo dõi `Storage.prototype.setItem` → 0 lần), `console/e2e/directlogin.spec.ts` (`document.cookie` không có `crm_session`) |
| I | Mã phiên lộ khi database bị đọc | Database giữ `sha256(mã)`; mã 32 byte `crypto/rand` | *249*, *254* | `login_test.go` (`token_hash = sha256(mã)`) |
| T | Gửi lệnh thay từ trang khác (CSRF) | `SameSite=Strict`; method ghi trên đường riêng chỉ nhận `Origin = DIRECT_LOGIN_ORIGIN` (thiếu `Origin` → 403); luật `content-type: application/json` của M0 | *255* | `origin_m7_test.go` (`evil.example`, `Origin` tên miền chính, thiếu `Origin`, `text/plain` → 403; đúng → 200) |
| S | Phiên để quên trên máy dùng chung — DN05 | Ngồi không 30 phút, tối đa 12 giờ, kiểm mỗi yêu cầu; tối đa 5 phiên mỗi người (vượt → thu hồi phiên cũ nhất, `limit`) | *254*, *255* | `session_auth_test.go` (+31 phút → 401; chạm mỗi 20 phút → 200 tới 12 giờ, 12:01 → 401), `login_test.go` (6 lần đăng nhập → phiên đầu `limit`), `sessions_property_test.go` (rapid: hợp lệ **khi và chỉ khi**…) |
| E | Người bị khóa hay bị gỡ quyền vẫn dùng phiên cũ (DN04, PQ05 trên đường riêng) | `LoadSession` nạp người dùng mỗi yêu cầu (`LoadUserByID`); `SetLocked(true)` thu hồi mọi phiên trong **cùng** transaction (`user_locked`); SSE kiểm lại mỗi 10 s và đóng | *255* | `session_auth_test.go` (khóa → mọi phiên `revoked`, yêu cầu kế tiếp 401; ca SSE đóng trong 1 s là ca thu hồi phiên, cùng cơ chế kiểm lại), `sessions_property_test.go` |
| S | Cookie sai dạng hay mã của shop khác dùng để dò phiên, gây lỗi 500 | Tách `v1.<tenant>.<mã>`; sai dạng, tenant khác → 401 `session_expired` (tìm dưới RLS của tenant trong cookie), không 500 | *255* | `session_auth_test.go` (`v2.x.y`, `v1.<không uuid>.y`, đúng mã khác tenant → 401) |
| E | Thu hồi phiên của người khác qua route "phiên của tôi" | `revokeMySession` chỉ phiên của chính mình (người khác → 404); thu hồi phiên người khác cần `identity.sessions.manage` | *255* | `session_auth_test.go` (`revokeMySession` id người khác → 404) |
| R | Chối đã vào qua đường Access (không có phiên CRM) | `login_attempts(path = 'access')` một dòng mỗi `(user, iat)` lần đầu thấy, `ON CONFLICT DO NOTHING`, bộ đệm LRU | *255* | `auth_m7_test.go` (100 yêu cầu cùng thẻ → 1 dòng; `iat` mới → dòng thứ hai) |
| D | Bảng phiên, nhật ký đăng nhập phình mãi | Lịch `identity.sessions_cleanup`: xóa phiên thu hồi/hết hạn quá 30 ngày, mỗi lượt ≤ 5.000 dòng. `login_attempts` chỉ ghi thêm, không dọn trong M7 (chấp nhận, dung lượng nhỏ) | *255* | `sessions_test.go` (31 ngày xóa, 29 ngày còn) |

## (B5) Mất điện thoại: quản trị đặt lại, mã đặt lại ngoài hệ thống, lệnh khẩn cấp

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| E | Tự đặt lại bước hai cho mình (người lấy được mật khẩu và phiên Access của quản trị bỏ bước hai của chính họ) | `ResetSecondFactor` từ chối `actor == target` (403 `cannot_reset_self`); quyền `identity.factors.reset` chỉ vai trò `owner` (`manager` bị loại trừ); màn quản trị không có nút cho chính mình | *248*, *256*, *258 (M7-13)* | `handlers_reset_test.go` (A → A: 403; không quyền: 403), `m7_identity_contract_test.go` (chỉ `owner` có quyền), `ResetFactorDialog.test.tsx` |
| S | Mã đặt lại bị đoán, bị dùng lại, dùng quá hạn | 20 ký tự base32 (100 bit), chỉ `sha256` trong database, hạn 30 phút, tiêu một lần bằng `UPDATE` có điều kiện; sai tính vào khóa tạm; vẫn phải có mật khẩu đúng trước | *256* | `handlers_reset_test.go` (mã sai → 401 và `failed_count` tăng; dùng lại → 401; +31 phút → 401) |
| S | Kẻ mạo danh nhân viên gọi điện xin quản trị đặt lại (lừa đảo qua điện thoại) | Ngoài phạm vi phần mềm: quản trị xác minh người gọi theo `dang-nhap-du-phong.md` (**(thêm)** nội dung bắt buộc của Task 276, F13); mỗi lần đặt lại có cảnh báo `direct-login-reset:U-…` và audit; kẻ mạo danh vẫn cần mật khẩu của nhân viên | *256*, *276 (M7-31)* | `handlers_reset_test.go` (cảnh báo, audit); quy trình ở `docs/khoi-dong/dang-nhap-du-phong.md` — rủi ro chấp nhận |
| R | Chối đã đặt lại bước hai cho người khác | Audit `identity.factor_reset` (`after: {reason}`, không có mã), cảnh báo thường; lệnh khẩn cấp: audit `actor_type = 'system'`, `actor_id = 'cli'` và cảnh báo **gấp** `identity-breakglass:U-…` sau commit | *256* | `handlers_reset_test.go`, `cmd/crm/identity_breakglass_test.go` |
| I | Mã đặt lại nằm lại trong bảng chống trùng, log | Route `no-replay` (`response_body` NULL); lệnh khẩn cấp chỉ in mã ra stdout một lần, không ghi log | *256* | `handlers_reset_test.go` (gửi lại → 409, `response_body` NULL), `identity_breakglass_test.go` (email lạ → không in mã) |
| E | Ai vào được máy chủ đều đặt lại được bước hai của bất kỳ ai | Chấp nhận (Q07: quyền vào máy chủ là quyền đặt lại); cảnh báo gấp mỗi lần; ai được vào máy chủ là quyết định hạ tầng của chủ shop (ADR-43, Task 277) | *256* | `identity_breakglass_test.go` (cảnh báo gấp) — rủi ro chấp nhận |
| S | Phiên cũ của người vừa bị đặt lại còn dùng được | Đặt lại thu hồi mọi yếu tố và mọi phiên (`factor_reset`) trong cùng transaction | *256* | `handlers_reset_test.go` (2 phiên của B `revoked`) |

## (B6) Tệp nhập: số đếm kiểm kho, bảng tính đơn dự phòng

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| D | Tệp rất lớn, bom zip, rất nhiều dòng làm hết bộ nhớ worker | Mục đích tệp giới hạn `.xlsx` ≤ 5 MiB lúc tải lên (M1); `FileStore.Open(maxBytes = 5 MiB)`; `xlsx.Read` của M1 giới hạn giải nén 50 MiB, 20.000 dòng, 2.000 ký tự mỗi ô | 263 (M7-18), *268 (M7-23)*, M1 | `count_import_test.go` (`.xlsx` 6 MiB → `file_too_large`, `.csv` bị từ chối; 3.000 dòng < 30 s), M1: `TestReadZipBomb`, `TestReadZipBombLyingHeader`, `TestReadSizeLimits`, `TestReadTooManyRows`; **(thêm)** `fallback_import_test.go`: `.xlsx` 6 MiB mục đích `order_fallback_import` → `file_too_large` |
| T | Gắn tệp sai: tệp của mục đích khác, chưa tải xong, của tenant khác, hay đã xóa, vào lệnh nhập | Kiểm `files.Status` trước khi ghi dòng nhập: không có/tenant khác/đã xóa → 404, sai mục đích hoặc chưa tải xong (`status ≠ uploaded`) → 422 (mẫu `catalog.CreateImport` của M1, `ErrFileNotReady`) | 263, *268* | **(thêm)** `count_import_test.go`: `fileId` mục đích `catalog_import` → 422, tệp chưa gọi `completeUpload` (chưa `uploaded`) → 422, tệp tenant khác → 404; **(thêm)** `fallback_import_test.go` cùng ba ca |
| T | Ô công thức trong tệp nhập được tính, hay tên sản phẩm là công thức chạy khi mở mẫu dự phòng | Đọc: ô có công thức → lỗi dòng `formula`, không tính (M1 `CodeFormulaNotAllowed`). Ghi mẫu: mọi ô qua `xlsx.Sanitize` | 263, *268* | `count_import_test.go` (ô `=1+1` → `formula`), `fallback_template_test.go` (`=HYPERLINK("x")` → `'=HYPERLINK…`) |
| T | Nhập lại cùng tệp làm số đếm, đơn bị nhân đôi | Số đếm: `UNIQUE (stocktake, sha256, mode)` — `replace` trả dòng cũ, `add` lần hai → 409 `import_already_applied`. Đơn dự phòng: `UNIQUE (tenant, file_sha256)` → 409 `import_exists`; khóa chống trùng mỗi đơn = `uuidv5(tenant ‖ Mã dòng)` qua `order.Create` | 263, *268* | `count_import_test.go`, `fallback_import_test.go` (nhập lại → 409; chép đơn sang tệp khác → `duplicate 2`; việc chết giữa chừng chạy lại → không trùng) |
| I | Tệp đơn dự phòng (tên, SĐT, địa chỉ) lộ: trên kho tệp, trong payload việc, log, dòng kết quả | Bucket riêng tư, không đường dẫn công khai; payload việc chỉ có mã; `error_codes` chỉ mã lỗi và số dòng; `error_rows` của số đếm không chứa nội dung ô | 263, *268* | `fallback_import_test.go` (payload, log, `fallback_import_rows` không chứa SĐT, địa chỉ, tên của dữ liệu thử), `count_import_test.go` (`error_rows` và log không chứa nội dung ô) |
| I | Tệp đơn dự phòng **nằm lại trên kho mãi** sau khi nhập xong | Kế hoạch chưa có hạn: đề xuất `files.Register`/mục đích `order_fallback_import` có hạn (ví dụ 30 ngày sau `done`) và lượt dọn `MarkDeleted` như tệp xuất của M6 | *268* | **(thêm)** `fallback_import_test.go`: tệp của lô `done` có `expires_at`, sau hạn lượt dọn đánh dấu xóa |
| I | Bảng tính dự phòng đã điền nằm trên máy nhân viên khi CRM ngừng | Ngoài phần mềm: `du-phong.md` dặn lưu ở máy shop, xóa sau khi nhập lại (**(thêm)** nội dung bắt buộc của Task 276, F13); mẫu tải trước chỉ có mã dòng và danh mục hàng (không dữ liệu khách) | *268*, *276* | **(thêm)** `fallback_template_test.go`: trang "Đơn" của mẫu chỉ có Mã dòng, mọi ô khác trống; không trang nào chứa tên, SĐT, địa chỉ khách của dữ liệu thử — phần còn lại là rủi ro chấp nhận |
| E | Người không có quyền nhập đơn hàng loạt, nhập số đếm vào kho không được giao | `order.fallback.import` (`ScopeNone`, chỉ `owner`, `manager`); nhập số đếm cần `inventory.stocktake` trên kho của phiên | 260 (M7-15), 263, *268* | `m7_launch_contract_test.go` (vai trò mẫu), `count_import_test.go` **(thêm)**: người có `inventory.stocktake` ở kho khác → 403 |
| D | Lô nhập đơn khóa lâu, deadlock với nhân viên tạo đơn tay | Mỗi đơn một transaction theo thứ tự khóa của `order.Create`; dòng kết quả chỉ chèn, không khóa `ord.fallback_imports` (C17) | *268* | `fallback_import_test.go` (100 đơn + 20 nhân viên, `deadlocks` không tăng; `FOR UPDATE NOWAIT` không bị chặn) |

## (B7) Dữ liệu ban đầu, đêm mở, theo dõi; worker → Meta; website thử

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| E | Khóa dịch vụ của website thử (có `web.catalog.preview`) dùng để **đặt hàng** sản phẩm nháp | `order.Create` từ chối biến thể không `sellable` (422 `variant_not_for_sale`) bất kể quyền xem trước | 261 (M7-16) | `preview_test.go` (đơn web với A `draft` bằng khóa có `web.orders.create` → 422) |
| I | Website thật thấy sản phẩm chưa mở bán (lộ hàng mới, giá) qua bộ đệm 60 s dùng chung | Khóa thường không bao giờ thấy `draft`; khóa đệm tách theo cờ xem trước (`p=1`) | 261 | `preview_test.go` (khóa thường ngay sau khóa xem trước trong 60 s → vẫn không có A) |
| E | Gán `web.catalog.preview` cho vai trò nhân viên, hay cấp khóa xem trước cho website thật | Quyền chỉ nằm trong `ServiceKeyPermissions`, không thuộc `identity.Permissions`; gán cho vai trò → bị từ chối; tạo khóa cần OK chủ shop (G3) | 260, 261 | `m7_launch_contract_test.go` (không vai trò nào có), `preview_test.go` (gán cho vai trò → từ chối) |
| I | Khóa xem trước của website thử bị lộ → người ngoài thấy hàng chưa mở bán | Chấp nhận: chỉ đọc, không đặt hàng được; thu hồi khóa bằng thao tác khóa dịch vụ của M5; sau đêm mở thu hồi khóa (**(thêm)** nội dung bắt buộc của sổ tay đêm mở, Task 276, F13) | 261, *276* | — rủi ro chấp nhận |
| T, R | Hiện/ẩn hàng loạt nhầm, hoặc chối đã hiện | `catalog.launch.publish` (`ScopeNone`); đánh giá lại điều kiện **dưới khóa**; mỗi sản phẩm một sự kiện và một dòng audit; ẩn lại được | 261 | `launch_test.go` (50 song song cùng khóa → 1 lần; hai khóa khác → A hiện một lần; đánh giá lại dưới khóa), `launch_property_test.go`; **(thêm)** `launch_test.go`: mỗi sản phẩm hiện/ẩn có đúng 1 dòng audit |
| T | Tồn mở sổ khác số đếm, đăng hai lần, có hàng nhập chen giữa (Review Focus 4, MO02) | Chỉ trên kho trống (kiểm lúc tạo và lại lúc đăng, dưới khóa); phải đếm đủ; khóa phiên `FOR UPDATE` rồi `ApplyStocktake` của M1; index một phiên mở sổ mỗi kho | 262 (M7-17) | `opening_test.go` (`on_hand = counted`; **50 `post` cùng khóa + 2 khóa khác → một lần đăng**; phiếu nhập chen giữa → `warehouse_not_empty`), `opening_property_test.go` (rapid) |
| T | Đếm cộng dồn mất lần đếm, ra số âm, hay đổi dòng sau khi phiên đã đăng | Một câu upsert cộng có điều kiện `counted + by >= 0`; khóa chia sẻ phiên `FOR SHARE` chặn đăng chen giữa | 263 | `opening_count_test.go` (**3 người × 100 lần `+1` → 300**; `by = -3` → `negative_count`; `+1` song song với đăng → không dòng nào đổi sau `applied`) |
| T, R | Nhân viên kho "nhập hàng hoàn đơn cũ" giả để tăng tồn (gian lận), hay nhập một kiện hai lần (MO08) | Tham chiếu chuẩn hóa **duy nhất** (409 `legacy_return_exists`); quyền theo kho; audit `inventory.legacy_return` (tham chiếu, kho, dòng); theo dõi 14 ngày đếm số phiếu mỗi ngày | 264 (M7-19), *271 (M7-26)* | `legacy_return_test.go` (`"vtp 1234 5678"` rồi `"VTP12345678"` → 409; hai khóa khác cùng lúc → 1 thành công; 403 kho khác; audit không chứa ghi chú), `watch_test.go` |
| S, D | Webhook hãng của đơn Nhanh cũ (hay webhook giả có chữ ký hợp lệ) gây cảnh báo rác, việc `dead`, tra hãng | Không có vận đơn trong CRM → `unmatched_carrier_events` (đếm theo `(hãng, mã vận đơn)`), việc `done`, không tra hãng, không cảnh báo; chữ ký webhook vẫn do M3 kiểm | 265 (M7-20), M3 | `inbound_m7_test.go` (1.000 sự kiện → 0 cảnh báo, máy chủ hãng giả 0 lần gọi; VC10 vẫn xanh) |
| I | CRM vô tình gọi hay đọc Nhanh (ADR-11) | Test kiến trúc quét chuỗi `nhanh.vn`, `nhanh.group`, `open.nhanh`, `NHANH_` trong mã | 265 | `tests/arch/no_nhanh_test.go` (mã thật → 0; `testdata` → nêu `sample.go`) |
| I | Danh sách sự kiện ngoài hệ thống lộ dữ liệu khách | Chỉ mã vận đơn, hãng, số lần, lần cuối; quyền `fulfillment.unmatched.read` | 265 | `unmatched_test.go` |
| E | Mở gửi Page không được giao; bật tự động khi Page còn chỉ xem | `channel.golive` theo Page; bật quy tắc trên Page `read_only` → 409 `page_read_only`; `setChannelSendMode` sang `enabled` → 409 `use_go_live` | *267 (M7-22)* | `golive_test.go` (P1 gọi cho P2 → 403; bật R1 khi `read_only` → 409; `use_go_live`) |
| T | Mở gửi bắn tin tự động ngược cho khách đã nhắn trong T2, hay hai tin cho một comment (Review Focus 5, MO06) | Sự kiện có `OccurredAt < live_since` → `skipped_before_live`; `UNIQUE (rule, external_ref)` của `automation_runs` | *267* | `golive_test.go` (comment 1 phút trước `live_since` → 0 tự động; cùng webhook hai lần → 1), `golive_property_test.go` (rapid) |
| R | Chối đã tắt tự động ở công cụ cũ khi mở gửi | Ô xác nhận bắt buộc, ghi vào audit `channel.go_live` (`oldToolAutomationsOff: true`) — tự khai, không kiểm được công cụ cũ (chấp nhận) | *267* | `golive_test.go` (`false` → 422; `true` → 1 dòng audit) |
| I, D | Đọc Meta (đối chiếu T2) lộ token, giữ transaction khi gọi mạng, hay đọc quá nhiều | Token giải mã trong transaction ngắn chỉ đọc (`channel.CredentialsFor`), gọi Meta **ngoài** transaction ở worker; tối đa 20.000 mục mỗi khung (`truncated`); lỗi đọc → `not_sent` thử lại, quá 6 giờ → `expired`; cảnh báo không chứa nội dung tin, PSID, tên khách | *266 (M7-21)* | `delivery_audit_test.go` (Meta 500 → `pending` rồi `expired`; 25.000 tin → `truncated`; văn bản cảnh báo) |
| I | Xem báo cáo T2 của Page không được giao | `conversation.t2.read` theo Page | *266* | **(thêm)** `t2report_test.go` hoặc test handler: người có quyền ở P1 đọc báo cáo P2 → 403 |
| E | Người không phải người quyết bấm "đi/dừng", "mở bán", "đóng theo dõi" | Chỉ `decider_user_id` của lượt (403 `not_decider` dù có `reporting.launch.decide`); người quyết phải `active` và có quyền lúc tạo lượt | *270 (M7-25)*, *271* | `launch_test.go` (người khác → 403 `not_decider`), `watch_test.go` (`closeWatch` người khác → 403) |
| T | "Mở bán" khi mốc A/B chưa đạt, đã cũ, hay bấm hai lần | `openSales` đòi lần đánh giá đạt mới nhất của A và B trong 30 phút, bước 1–6 `done`; index một lượt `running`, một lượt `live` `completed` mỗi tenant | *270* | `launch_test.go` (`checkpoint_not_passed`, `checkpoint_stale`; 50 cùng khóa → 1; hai khóa → 1 thành công 1 409), `launch_property_test.go` (rapid: không `point_of_no_return_at` khi mốc chưa đạt) |
| R | Chối đã quyết "đi", "dừng", "mở bán" | Kế hoạch chỉ có audit cho `decision go`; **(thêm, đổi nhỏ thiết kế)** audit cả `decision stop` và `openSales` (người quyết, lượt, quyết định, lý do) — F9 | *270* | **(thêm)** `launch_test.go`: `decision go`, `decision stop`, `openSales` mỗi cái đúng 1 dòng audit có `actor_id = decider` |
| I | Kết quả mốc, báo cáo sẵn sàng, tin `watch:<ngày>` lộ dữ liệu khách hay tên nhân viên | `result` chỉ mã và số đếm; báo cáo sẵn sàng không trả tên (sản phẩm chưa đạt chỉ là số lượng); văn bản theo dõi chỉ số | 269 (M7-24), *270*, *271* | `readiness_test.go` (JSON không chứa email, tên của dữ liệu thử), `launch_test.go` (`result` chỉ mã và số), `watch_test.go` |
| T | Báo cáo sẵn sàng hay mốc "xanh" giả khi nguồn số liệu chưa có | Mục có nguồn thuộc task để sau trả `warn` "chưa có nguồn", không `ok` (Q15: thiếu số liệu không tính là xanh; quyết định điều phối 28/11 cho Task 269) | 269, *271* | **(thêm)** `readiness_test.go`: mỗi mục có nguồn thuộc task để sau (`identity.enrollment_complete`, `identity.no_grant_users`, `conversation.t2_pass`, phần của Task 267) → `warn`, `Value` "chưa có nguồn", không bao giờ `ok` (F12); `watch_test.go` (thiếu đối chiếu COD → `green = false`) |
| T | Chạy `crm seed-staging` nhầm trên production (dữ liệu giả lẫn dữ liệu thật) | Từ chối khi `CRM_ENV=production` (thoát 2, không ghi gì); SĐT giả trong dải `09999xxxxx` | *275 (M7-30)* | `cmd/crm/seed_staging_test.go` |

## Rủi ro chấp nhận

- Khóa tạm 15 phút có thể bị lợi dụng để chặn một nhân viên khỏi **đường riêng** (B3); đường chính không bị ảnh hưởng.
- Người có quyền vào máy chủ đặt lại được bước hai của bất kỳ ai (Q07); mỗi lần là cảnh báo gấp (B5).
- Người có quyền trên nền tảng triển khai bật được đường riêng tối đa 24 giờ mỗi lần deploy; mỗi lần bật là cảnh báo
  gấp nhắc lại 30 phút (B1).
- Lừa đảo qua điện thoại để xin mã đặt lại: chống bằng quy trình xác minh người gọi và việc vẫn cần mật khẩu (B5).
- Ô "đã tắt ở công cụ cũ" khi mở gửi là tự khai (B7).
- Khóa xem trước của website thử bị lộ: chỉ thấy hàng chưa mở bán, không đặt được (B7).
- `login_attempts` chỉ ghi thêm, không dọn trong M7 (B4).

## Chương ASVS 5.0.0 cần soát ở Task M7-35 (280)

Chỉ tên chương, theo trí nhớ của người viết, **chưa xác minh** với bản gốc OWASP ASVS 5.0.0; Task 280 đọc bản gốc
(ghi đường dẫn, ngày đọc), sửa tên chương nếu khác, rồi soát từng yêu cầu mức 2. Thứ tự: kỹ nhất ở năm chương đầu.

| Chương (tên theo bản 5.0.0, chưa xác minh) | Vì sao M7 chạm | Ranh giới |
|---|---|---|
| V6 Authentication | Mật khẩu, argon2id, TOTP, mã khôi phục, khóa tạm, đặt lại bước hai | B2, B3, B5 |
| V7 Session Management | Cookie `__Host-`, hết hạn ngồi không và tuyệt đối, thu hồi, xoay mã sau bước hai | B3, B4 |
| V8 Authorization | Quyền mới của M7, `cannot_reset_self`, người quyết, phạm vi kho/Page, khóa xem trước | B4, B5, B7 |
| V14 Data Protection | Bí mật một lần không lưu (`no-replay`), tệp dự phòng có SĐT, hạn giữ tệp, `Cache-Control: no-store` | B1, B2, B6 |
| V16 Security Logging and Error Handling | `login_attempts`, audit, cảnh báo không mang dữ liệu cá nhân, câu lỗi đăng nhập chung | B2–B7 |
| V3 Web Frontend Security | `SameSite`, kiểm `Origin`, không lưu gì vào storage của trình duyệt | B4 |
| V4 API and Web Service | Route công khai `/direct/api/*`, `content-type`, giới hạn tần suất | B3, B4 |
| V5 File Handling | Tệp nhập `.xlsx`: kích thước, bom zip, mục đích tệp, kho riêng tư | B6 |
| V1 Encoding and Sanitization | Công thức trong ô Excel (đọc và ghi) | B6 |
| V2 Validation and Business Logic | Mở sổ một lần, đếm cộng dồn, mốc đi/dừng, mở gửi một lần | B7 |
| V11 Cryptography | argon2id, `crypto/rand`, `fieldcrypt` (AES-GCM có AAD), `sha256` mã phiên | B2–B4 |
| V12 Secure Communication | Tên miền riêng qua Cloudflare, máy chỉ nhận kết nối qua Cloudflare | B1 |
| V13 Configuration | `DIRECT_LOGIN_*`, `FIELD_ENC_KEYS`, `CRM_ENV`, đường riêng không phục vụ `/health` | B1 |
| V15 Secure Coding and Architecture | Không gọi mạng trong transaction, thứ tự khóa ADR-46, không gọi Nhanh | B3, B7 |

Không áp dụng (Task 280 xác nhận lại): V9 Self-contained Tokens (thẻ Access thuộc M0), V10 OAuth and OIDC (CRM không
làm nhà cung cấp danh tính), V17 WebRTC.
