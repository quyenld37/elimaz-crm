# Task 396 — Gửi lại cùng khóa nhận đúng từng byte câu trả lời lần đầu (M0-x3)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `idempotency.Acquire`,
  `idempotency.Claim.Complete`, `idempotency.Stored` giữ nguyên; chỉ đổi cột đọc/ghi bên trong. `httpapi.WriteTx` giữ
  chữ ký, đổi hành vi: `body` kiểu `json.RawMessage` được lưu nguyên từng byte (kiểu khác vẫn `json.Marshal`); 6 chỗ
  gọi (`handlers_jobs.go`, `handlers_identity.go`, `handlers_files.go` ×2, `handlers_order.go`, `refusal.go`) không
  phải sửa. Hàm nội bộ `replay` (1 chỗ gọi) đổi thành hàm ghi kết quả dùng chung cho lần đầu và lần phát lại.
- Gói phụ thuộc ngược (go list): `internal/platform/idempotency` ← chỉ `internal/httpapi`.
  `internal/platform/internal/store` (mã sinh) đổi theo câu sqlc.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Hành vi đổi với mọi route ghi `required`: lần đầu và
  lần phát lại cùng trả đúng byte đã lưu với `Content-Type: application/json; charset=utf-8` (trước: phát lại trả
  `application/json`, thân do jsonb chuẩn hóa lại).
- Chỗ dùng trên màn hình: `console/src/app/idempotency` chỉ đọc mã trả lời và header, không phụ thuộc thứ tự byte;
  không sửa.
- Câu sqlc, bảng, pha migration: bảng `platform.idempotency_keys`; migration mới `0013_idempotency_raw.sql` pha **mở
  rộng** (thêm cột `response_raw bytea` cho phép NULL; số 0013 kiểm bằng `ls migrations/` trên nhánh này, main và các
  nhánh task đang mở). `CompleteIdempotencyKey` ghi cả `response_body` (jsonb, cho bản cũ còn chạy lúc deploy cuốn) và
  `response_raw`; `LockIdempotencyKey` đọc `response_raw`, dòng do bản cũ ghi (raw NULL) thì lấy
  `response_body::text`; `ResetExpiredIdempotencyKey` xóa cả hai. Thu hẹp cột jsonb để task sau (`-- contract:`).
  Chỗ khác chạm bảng (`schedules/cleanup.go` xóa dòng hết hạn; `queue/manage.go`, `db/retry.go` chỉ nhắc tên trong
  chú thích) không đọc cột thân: không đổi.
- Test sẽ chạy: vùng rủi ro cao → cả bộ `./internal/platform/idempotency/...` và `./internal/httpapi/...` dưới
  `-race`; `TestRetryJobFiftyParallel` 20 lần liền (`-count=20`); `./internal/platform/db/...` (migrate),
  `./internal/platform/schedules/...` (dọn khóa), `./internal/platform/tenancy/...` (luật schema), `./tests/arch/...`;
  cuối cùng `make ci` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `internal/platform/idempotency` (Acquire, Claim,
  Stored) và `httpapi.WriteTx`; tìm bộ bọc `http.ResponseWriter` sẵn có trong `internal/httpapi` (`middleware.go` có
  bộ ghi nhớ mã trả lời cho log, metrics — khác việc, không bỏ thân).
- Dùng lại / viết mới vì: dùng lại nguyên `idempotency` và `WriteTx`, chỉ thêm cột. Viết mới một bộ bọc
  `ResponseWriter` nhỏ trong `internal/httpapi/idempotency.go`: việc của nó là bỏ thân do handler (ogen) ghi sau khi
  `WriteTx` đã lưu kết quả cuối, để middleware ghi đúng byte đã lưu. Chỉ middleware chống trùng dùng, nên ở cùng tệp,
  không đưa ra gói chung.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

**Nguyên nhân gốc (đọc mã trước khi viết test):** (1) `response_body` là `jsonb` nên PostgreSQL sắp lại khóa (khóa
ngắn trước, rồi theo bảng chữ cái) và thêm khoảng trắng `"a": 1`; (2) lần đầu thân do ogen ghi (jx, không khoảng
trắng, `Content-Type: application/json; charset=utf-8`), còn bản lưu là `json.Marshal` của cùng giá trị (thoát
`<>&`), phát lại đặt `application/json`; (3) kiểu map của ogen (vd `UploadHeaders`) mã hóa theo thứ tự lặp map ngẫu
nhiên của Go, nên mã hóa hai lần không bao giờ bảo đảm cùng byte. Vì vậy cách bảo đảm "giống hệt từng byte" là: lưu
byte nguyên (`bytea`) **và** câu trả lời lần đầu chính là byte đã lưu (middleware ghi kết quả đã lưu thay cho thân
handler ghi sau khi `WriteTx` đã commit kết quả cuối).

- 5.1 Chủ dữ liệu: bảng `platform.idempotency_keys` của nền tảng; cột mới cùng bảng.
- 5.2, 5.3: không áp dụng: không chạm module khác.
- 5.4 Nguyên tử: `Complete` vẫn ghi status, `response_body`, `response_raw` trong transaction nghiệp vụ, câu cuối
  (ADR-46 nhóm 13).
- 5.5, 5.6: không áp dụng: không có sự kiện, lệnh ra ngoài.
- 5.7 Ngữ cảnh shop: bảng đã có RLS; câu đọc/ghi vẫn trong `tenancy.WithTenant`; cột mới không cần policy mới.
- 5.8 Nghiệp vụ không biết HTTP: `idempotency` vẫn chỉ nhận/trả `[]byte`; Content-Type và bộ bọc nằm ở httpapi.
- 5.9 Đường ghi: không thêm đường ghi; mọi route `required` hưởng hành vi mới; `no-replay` vẫn không lưu thân, lần
  đầu trả nguyên thân handler (không bị thay), lần hai 409 `already_processed`.
- 5.10: không áp dụng: không Valkey.
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: luật migration (mở rộng trước, ghi cả hai, đọc cột mới); test trên PostgreSQL thật.
- 6.1 Hai yêu cầu cùng lúc: không đổi khóa dòng; `TestRetryJobFiftyParallel`, `TestIdempotencyFiftyParallelSameKey`
  so theo byte.
- 6.2 Gửi lại: mục chính của task (ADR-41: phát lại đúng câu trả lời đã lưu).
- 6.3, 6.4, 6.5: không đổi (cùng transaction, cùng thứ tự khóa, cùng `RetryTx`). Handler trả lỗi sau khi `WriteTx` đã
  commit: trước đây lần đầu nhận lỗi còn phát lại nhận kết quả đã lưu; nay lần đầu cũng nhận kết quả đã lưu (khớp dữ
  liệu đã commit).
- 6.6, 6.7: không áp dụng: không việc nền, không gọi ra ngoài.
- 6.8 Tiến trình chết, deploy cuốn: running quá 60 s vẫn được nhận lại. Bản cũ ghi chỉ jsonb → bản mới đọc
  `response_body::text` khi raw NULL; bản mới ghi cả hai → bản cũ đọc jsonb như trước.
- 6.9 Treo, kích thước: không đổi giới hạn thời gian. Thân lưu hai bản trong 72 giờ — chấp nhận tới khi thu hẹp cột jsonb.
- 6.10: không áp dụng: không tiền. 6.11: quy luật rapid của idempotency (`property_test.go`) chạy lại nguyên.
- 6.12 Dữ liệu cá nhân: không log thân; không thêm log.
- 6.13 Quyền: không đổi (middleware vẫn sau auth, guard).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đỏ trước khi sửa (gói idempotency) | `TestCompleteStoresExactBytes`, `TestLegacyRowWithoutRawReplaysJSONB` và 6 test đọc cột mới | `go test -race ./internal/platform/idempotency/...` (chưa có migration, mã) | `FAIL` 8 test (cột `response_raw` chưa có) |
| Đỏ trước khi sửa (httpapi) | `TestIdempotencyReplayExactBytes/{ok,raw,after}`, `TestIdempotencyReplayAfterOneHour` | cùng test, `idempotency.go` của HEAD + chỉ thêm hằng `jsonContentType` | `FAIL`: "thân khác byte" (ok, raw); "after" lần đầu 500 còn phát lại 200; Content-Type `application/json` |
| Thân lưu, đọc lại đúng từng byte (khóa lạ, khoảng trắng, `<>&`, `1.50`); jsonb vẫn ghi | `TestCompleteStoresExactBytes` | `go test -race ./internal/platform/idempotency/...` | `ok … 118.714s` |
| Dòng do bản cũ ghi (raw NULL) vẫn phát lại từ jsonb | `TestLegacyRowWithoutRawReplaysJSONB` | như trên | `ok` |
| Lần hai cùng khóa: thân giống hệt từng byte, cùng `Content-Type` (route thử JSON thứ tự khóa lạ, khoảng trắng riêng) | `TestIdempotencyReplayExactBytes/raw` (lần đầu = đúng `rawProbe`), `/ok`, `/after` | `go test -race ./internal/httpapi/...` | `ok … 108.790s` |
| `TestRetryJobFiftyParallel` so theo byte + Content-Type, qua 20 lần liền dưới `-race` | `TestRetryJobFiftyParallel` | `go test -race -count=20 -run '^TestRetryJobFiftyParallel$' ./internal/httpapi/` | `ok … 33.661s` |
| `no-replay` vẫn không lưu thân | `TestIdempotencyNoReplay` (response_body NULL, lần hai 409) + `TestCompleteNilBodyStoresNull` (raw NULL) | hai lệnh trên | `ok` |
| Cổng kiểm toàn bộ trên bản đã gộp main | — | `make ci` (sau khi stage, gộp main `04614f9`) | build, vet, lint, gen-check, sqlc-vet, govulncheck ("No vulnerabilities found") qua; bước `test`: 32 gói `ok` (có `idempotency` 265 s, `platform/db`, `schedules`, `tenancy`, `tests/arch`), 5 gói đỏ vì máy quá tải (load average 26, 33 container của phiên song song): 4 gói hết giờ 10 phút giữa test đang chạy (`httpapi`, `identity`, `inventory`, `files`) và 2 test canh giờ (`TestImager_HangKilledWithinDeadline`, `TestPublicVariants`). Chạy lại riêng 5 gói: `go test -race -count=1 -p 1 -timeout 40m …` → cả 5 `ok` (`httpapi` 114.9 s, `imager` 8.5 s, `identity` 119.4 s, `inventory` 523.5 s, `files` 285.8 s) |
| Các cổng sau khi gộp main lần hai (`fa4a934`, Task 38: gói xlsx; không chạm httpapi, idempotency) | — | `make build vet lint gen-check sqlc-vet oasdiff catalog-check jscpd test-guard gitleaks console-check` | `EXIT 0`, golangci-lint "0 issues", gitleaks "no leaks found" |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hai test cũ được siết, không nới: `TestAcquireNewKeyCompleteThenReplay` trước kiểm thân chứa `"count": 1` (dạng jsonb
  chuẩn hóa), nay đòi đúng byte `{"count":1}`; `TestIdempotencyReplayAfterOneHour` trước đòi phát lại
  `application/json` và so theo nội dung JSON, nay đòi cùng `application/json; charset=utf-8` và cùng từng byte.
- Ngoài cột mới (kế hoạch), phải đổi cả câu trả lời **lần đầu**: xem "Quyết định thay chủ shop" 1.

## Quyết định thay chủ shop

1. **Lần đầu trả chính byte đã lưu.** Chỉ đổi cột sang `bytea` là chưa đủ: lần đầu thân do ogen mã hóa lại (map như
   `UploadHeaders` lặp ngẫu nhiên, jx không thoát `<>&` còn `json.Marshal` thoát), nên khác byte bản lưu. Chọn: sau khi
   `WriteTx` commit kết quả cuối, middleware bỏ thân handler ghi và ghi đúng status, byte đã lưu. Phương án khác: bắt
   mọi handler tự ghi byte do `WriteTx` trả (phải sửa 6 chỗ và cách sinh mã ogen, vẫn dễ lệch); hay chỉ sắp khóa map
   (không bảo đảm cho kiểu sau này). Dễ đảo ngược: bỏ `storedWriter` là về cách cũ.
2. **Handler trả lỗi sau khi kết quả đã commit** → lần đầu nhận kết quả đã lưu (trước: lỗi, rồi phát lại lại 200).
   Chọn vì khớp dữ liệu đã commit và khớp lần phát lại. Log lỗi của handler vẫn ghi như cũ.
3. **`Content-Type` của kết quả lưu** là hằng `application/json; charset=utf-8` (đúng giá trị ogen đặt) cho cả lần
   đầu và phát lại, không thêm cột lưu Content-Type: mọi thân lưu là JSON (`Complete` đòi JSON vì cột jsonb còn ghi).
   Phương án khác: cột `response_content_type` — để khi có route ghi trả thân không phải JSON.
4. **`json.RawMessage` lưu nguyên byte** (kiểm `json.Valid`, sai → 500 lỗi lập trình); kiểu khác vẫn `json.Marshal`.
5. **Deploy cuốn:** câu đọc lấy `COALESCE(response_raw, response_body::text)` để khóa do bản cũ ghi trong 72 giờ vẫn
   phát lại được; bản mới ghi cả hai cột cho bản cũ đọc. Thu hẹp cột jsonb: task sau (ghi ở "Việc phát hiện thêm").

## Việc phát hiện thêm

- Pha thu hẹp: khi không còn bản nào đọc `response_body`, migration sau xóa cột đó (`-- contract:` ghi bản đã ngừng
  dùng), bỏ `COALESCE` và việc ghi jsonb trong `queries/platform/idempotency.sql` (sau ít nhất 72 giờ từ lần deploy
  bản này, để dòng cũ hết hạn).
- (reviewer) Nếu lùi về bản trước 0013 lâu hơn 72 giờ rồi tiến lại, `ResetExpiredIdempotencyKey`/`Complete` của bản
  cũ không xóa `response_raw`, nên một khóa dùng lại đúng lúc đó có thể phát lại thân cũ qua `COALESCE`. Gần như không
  xảy ra (khóa ngẫu nhiên, việc dọn xóa dòng hết hạn); task thu hẹp nên xử lý cùng.
- (reviewer) `json.Valid` không kiểm UTF-8: `json.RawMessage` có UTF-8 sai sẽ lỗi ở `convert_from` (500, khóa giữ
  running 60 s). Chưa chỗ gọi nào truyền `RawMessage`; là lỗi lập trình.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một lượt, trên `git diff --cached` sau khi gộp main `fa4a934`).

- Phát hiện phải sửa: không có. Reviewer đã kiểm: lần đầu và phát lại cùng đi qua `writeStored` (cùng status, byte,
  Content-Type); 6 chỗ gọi `WriteTx` không phải sửa; no-replay đi thẳng; `fn` chạy lại gán lại `result`, `st.result`
  chỉ gán sau commit; `writeStored` xử lý header đúng; SQL với NULL đúng; deploy cuốn tương thích hai chiều; mã sinh
  khớp nguồn; migration chỉ mở rộng.
- Ghi chú không bắt buộc: hai mục ghi ở "Việc phát hiện thêm" (lùi bản > 72 giờ; UTF-8 của `json.RawMessage`) — không
  sửa trong task này. Đường dẫn console đúng là `console/src/app/idempotency.ts` (không đổi).
