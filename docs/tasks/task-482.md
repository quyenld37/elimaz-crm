# Task 482 — Giới hạn số lần đoán mã giảm giá

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `cache.RateLimiter` thêm phương thức `Peek` (một hiện thực: `rateLimiter`; không bản giả nào hiện thực interface — grep `Allow(ctx context.Context, l cache.Limit` chỉ gặp `ratelimit.go`); `cache.Limits` thêm hai trường `CouponGuessCustomer`, `CouponGuessIP` (dùng ở `cmd/crm/servicekey.go`, `limits_test.go`; `ParseLimits` nhận thêm hai tên); `httpapi` thêm `CouponGuard`, `NewCouponGuard`, `Deps.CouponGuard`. `tooFastError` (không xuất) thêm trường `code`.
- Gói phụ thuộc ngược (go list): `internal/platform/cache` ← `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/platform/observability`, `internal/wiring/webcache`; `internal/httpapi` ← `cmd/crm`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: `webQuote`, `webCreateOrder` — thêm trường tùy chọn `clientIp` vào `WebQuoteRequest`, `WebOrderRequest` (không phá vỡ); thêm mã `coupon_attempts_exceeded` vào `x-error-codes`. Mã sinh `gen/**`, `console/src/api/**` qua `make gen`.
- Chỗ dùng trên màn hình: chỉ câu tiếng Việt của mã lỗi mới trong `console/src/app/errors.ts` (màn không gọi route website).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `internal/platform/cache` (Peek: Valkey, dự phòng; DefaultLimits, ParseLimits), `internal/httpapi` (`TestCouponGuard_*`, errors snapshot, errors_web), `internal/httpapi/web` (`TestWebCoupon_GuessRateLimited*` qua báo giá và tạo đơn), `cmd/crm` build; ci-task theo gói đổi.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục cache (`RateLimiter`, `SubjectKey`, `Limits`), `httpapi.NewWebOrderLimiter` (mẫu nối giới hạn theo SĐT), `tooFastError`/`writeTooFast` (429 kèm Retry-After), `customer.NormalizePhone`, `retryAfterSeconds`.
- Dùng lại / viết mới vì: dùng lại `cache.RateLimiter` (Valkey gate + dự phòng trong tiến trình), `SubjectKey` (băm HMAC SĐT, IP), `tooFastError` (thêm mã riêng thay vì kiểu lỗi mới). Viết mới `RateLimiter.Peek`: `Allow` luôn tính một lượt, mà yêu cầu là chỉ đếm lần nhập mã SAI và chặn cả lần nhập mã đúng khi đã quá ngưỡng — cần xem bộ đếm mà không tính lượt. Viết mới `httpapi.CouponGuard` (không gộp với `webOrderLimiter`: khác việc — đếm lần đoán sai theo hai chủ thể, có bước xem trước và bước ghi sau).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: bộ đếm tần suất là hạ tầng `platform/cache` (Valkey gate), không phải dữ liệu nghiệp vụ; module `order`, `promotion` không đổi.
- 5.2 Không ghi bảng module khác: không ghi bảng nào.
- 5.3 Đọc module khác qua view: không đọc thêm; lý do mã sai lấy từ kết quả `order.QuoteWeb` (`Coupon.Reason`) và `order.CouponInvalidError`.
- 5.4 Nguyên tử: lời gọi Valkey nằm ngoài transaction (trước `WithTenant`/`WriteTx` để xem, sau khi transaction xong để ghi lần sai); test kiến trúc giữ module không nhập cache.
- 5.5 Sau commit: ghi lần sai của tạo đơn chạy sau `WriteTx` (đã commit hoặc đã lưu từ chối), không trong hàm của `RetryTx`.
- 5.6 Gọi ra ngoài: chỉ Valkey gate, hạn 50 ms, cầu dao; lỗi → bộ đếm trong tiến trình (FallbackMax).
- 5.7 Ngữ cảnh shop: khóa Valkey theo chủ thể đã băm (SĐT, IP của khách); không tenant trong khóa vì SubjectKey dùng secret của máy chủ — một khách đoán mã ở mọi shop cùng máy chủ bị đếm chung (chấp nhận: hiện một shop).
- 5.8 Không HTTP trong module: đúng; mọi phần ở `internal/httpapi`.
- 5.9 Mọi đường ghi: không thêm đường ghi; `webQuote` vẫn `exempt` (không ghi PostgreSQL — Valkey không phải dữ liệu); 429 của tạo đơn trả trước `WriteTx` nên không lưu theo khóa chống trùng.
- 5.10 PostgreSQL là nguồn: không quyết định nghiệp vụ nào đọc Valkey; giới hạn chỉ là lớp điều tiết (mã đúng/sai vẫn kiểm bằng `promotion.Check`).
- 5.11 Không thư viện mới.
- 5.12 Theo mẫu: nối như `NewWebOrderLimiter` (Deps nil → bộ đếm trong tiến trình, secret ngẫu nhiên); lỗi đổi ở `errors_web.go`.

Mục 6:
- 6.1 Hai yêu cầu cùng lúc: bộ đếm là INCR nguyên tử (Valkey) / mutex (dự phòng). Nhiều yêu cầu đoán song song cùng vượt bước xem trước có thể lọt thêm tối đa số yêu cầu đang chạy — chấp nhận (lớp điều tiết; sau đó bị chặn hết cửa sổ). Có test song song.
- 6.2 Gửi lại: báo giá không khóa; tạo đơn gửi lại cùng khóa được middleware trả bản đã lưu, không qua handler nên không đếm lại.
- 6.3 Thứ tự khóa: không khóa database mới.
- 6.4 Lỗi tạm: không áp dụng (không transaction mới).
- 6.5 Mức cách ly: không áp dụng.
- 6.6–6.8 Việc nền: không áp dụng.
- 6.9 Treo lâu: mỗi lệnh Valkey hạn 50 ms, cầu dao 5 s; tối đa 2 lệnh xem + 2 lệnh ghi mỗi yêu cầu có mã.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: test rapid sẵn có của RateLimiter; thêm test Peek không tính lượt.
- 6.12 Dữ liệu cá nhân: SĐT, IP chỉ vào khóa Valkey qua `SubjectKey` (HMAC); không log; mã giảm giá không vào log. Thời gian phản hồi: mã đúng và mã sai đều làm đúng một lệnh Valkey mỗi chủ thể sau khi kiểm (Peek với mã đúng, Allow với mã sai), để không lộ đúng/sai qua thời gian rõ rệt.
- 6.13 Quyền: không đổi (route vẫn `web.orders.create`).

## Bằng chứng

Lệnh chung: `ci-task.sh` (cổng kiểm của task) → `CI-TASK XANH (so với điểm tách nhánh 3074cbf…)`; mọi cổng tĩnh xanh (build, vet, lint 0 issues, gen-check, catalog-check, jscpd 0 clones, gitleaks, oasdiff, test-guard, console-check, test kiến trúc); `-race`: `ok cmd/crm`, `ok internal/httpapi 23.9s`, `ok internal/httpapi/web 16.1s`, `ok internal/platform/cache 13.5s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Vượt ngưỡng nhập mã sai → 429 `coupon_attempts_exceeded` kèm Retry-After (báo giá) | `TestWebCoupon_GuessRateLimited` | `go test -race -count=1 -run '^(TestWebCoupon_GuessRateLimited\|TestWebCoupon_GuessRateLimitedOnCreate)$' ./internal/httpapi/web/` | `--- PASS: TestWebCoupon_GuessRateLimited (0.51s)` |
| Mã đúng sau khi bị chặn vẫn bị chặn tới hết cửa sổ; hết cửa sổ → áp được | `TestWebCoupon_GuessRateLimited` (bước "mã đúng sau khi bị chặn", "hết cửa sổ"), `TestCouponGuard_BlocksAfterFailures` (RetryAfter 10 phút) | như trên; `go test -race -count=1 -run '^TestCouponGuard_.*$' ./internal/httpapi/` | PASS; `ok internal/httpapi` |
| Khách khác (SĐT khác, IP khác) không bị ảnh hưởng; cùng IP thì IP đủ ngưỡng chặn cả SĐT mới; IPv6 theo /64; không IP → theo khóa dịch vụ | `TestWebCoupon_GuessRateLimited`, `TestWebCoupon_GuessRateLimitedOnCreate`, `TestCouponGuard_BlocksAfterFailures` | như trên | PASS |
| Tạo đơn: mã sai (422 coupon_invalid not_found) được đếm; bị chặn → 429, không đơn, không lưu theo khóa; không mã vẫn 201 | `TestWebCoupon_GuessRateLimitedOnCreate`, `TestWebCoupon_GuessRateLimited` | như trên | `--- PASS: TestWebCoupon_GuessRateLimitedOnCreate (0.52s)` |
| Valkey ngừng → bộ đếm trong tiến trình (FallbackMax), vẫn chặn | `TestCouponGuard_ValkeyDownFallback`, `TestRateLimiter_PeekDoesNotCount` (phần dự phòng) | như trên; `go test -race -count=1 -run '^TestRateLimiter_PeekDoesNotCount$' ./internal/platform/cache/` | `ok internal/platform/cache` |
| Peek không tính lượt; Peek từ chối sau Max lượt, RetryAfter tới hết cửa sổ | `TestRateLimiter_PeekDoesNotCount` | như trên | PASS |
| Lần sai song song đếm nguyên tử | `TestCouponGuard_ParallelFailures` | như trên | PASS |
| Giới hạn mặc định và RATE_LIMITS_JSON nhận hai tên mới | `TestDefaultLimits`, `TestParseLimits` | `go test -race -count=1 -run '^(TestDefaultLimits\|TestParseLimits)$' ./internal/platform/cache/` | `ok` |
| Mã lỗi mới: errors_web.go + x-error-codes + snapshot + câu tiếng Việt | `TestDomainErrorSnapshot` (dòng `tooFastError coupon`), `console/src/app/errors.test.ts` | `go test -count=1 -run '^TestDomainErrorSnapshot' ./internal/httpapi/`; `cd console && npx vitest run src/app/errors.test.ts` | `ok`; `Tests 17 passed (17)` |
| Không lộ đúng/sai qua thời gian: mã đúng và mã sai cùng số lệnh Valkey | `CouponGuard.Record` (Peek khi đúng, Allow khi sai — mỗi chủ thể một lệnh) | đọc mã | — |
| Báo giá vẫn không ghi PostgreSQL (exempt) | `tests/arch` webexempt | `go test -count=1 ./tests/arch/` | `ok 29.1s` |

## E2E viết sẵn, chưa chạy

(không có — kế hoạch không có e2e cho việc này; đủ ca ở test gói `internal/httpapi/web`.)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

Task không có mục riêng trong kế hoạch; nguồn là threat model M5 dòng "Mã quà sinh nhật bị dò" (`TestWebCoupon_GuessRateLimited`) và ghi chú "Việc phát hiện thêm" của Task 197. Dòng threat model đã cập nhật tên test của Task 482.

## Quyết định thay chủ shop

1. **Chỉ đếm lần nhập mã SAI** (lý do `not_found`, `bound_other` — hai lý do lộ mã có tồn tại không), không đếm mọi lần dùng mã: website báo giá lại mỗi khi giỏ đổi, đếm mọi lần thì khách thật dùng mã đúng cũng bị chặn. Cần xem bộ đếm mà không tính lượt → thêm `RateLimiter.Peek`. Phương án khác: đếm mọi lần dùng mã với ngưỡng rộng (không cần Peek nhưng chặn nhầm khách thật; ngưỡng rộng thì dò được nhiều hơn).
2. **Ngưỡng mặc định**: 10 lần sai / 15 phút mỗi SĐT, 30 lần sai / 15 phút mỗi IP; Valkey ngừng → một nửa (theo cách của QĐ23). Đổi được bằng `RATE_LIMITS_JSON` (`coupon_guess_customer`, `coupon_guess_ip`) không cần sửa mã.
3. **IP khách**: hợp đồng chưa có; header `Cf-Connecting-Ip` là IP máy website (dùng cho CIDR khóa dịch vụ), không phải IP khách. Thêm trường thân tùy chọn `clientIp` (`format: ip`, ogen tự kiểm → 400 khi sai) vào `WebQuoteRequest`, `WebOrderRequest` — cùng tên với `clientIp` của lượt mua flash sale trong kế hoạch M5. IPv6 đếm theo /64; IPv4 dạng `::ffff:` coi như IPv4.
4. **Website không gửi `clientIp`** → lần sai đếm theo khóa dịch vụ với ngưỡng IP (đóng, an toàn: SĐT do người dùng tự nhập nên giới hạn chỉ theo SĐT bị lách bằng cách đổi SĐT). Hệ quả: khi website chưa chuyển IP, 30 lần sai trong 15 phút của mọi khách cộng lại sẽ chặn dùng mã của mọi khách tới hết cửa sổ (đặt hàng không mã vẫn được). Đảo ngược: tăng `coupon_guess_ip` qua `RATE_LIMITS_JSON`, hoặc website gửi `clientIp` (Task 465).
5. **Mã lỗi riêng `coupon_attempts_exceeded`** (429 + Retry-After) thay vì dùng lại `too_fast`: website cần nói đúng với khách ("nhập sai mã quá nhiều lần, thử lại sau hoặc đặt không mã"), khác "đặt quá nhiều đơn". Dùng lại kiểu `tooFastError` (thêm trường `code`), không thêm kiểu lỗi mới.
6. Chặn chỉ áp cho yêu cầu **có mã**; báo giá, tạo đơn không mã không bị ảnh hưởng.

## Việc phát hiện thêm

- **Cho Task 465 (website):** gửi `clientIp` (lấy từ `cf-connecting-ip` của khách) trong thân `POST /api/v1/web/quote` và `POST /api/v1/web/orders`; xử lý 429 `coupon_attempts_exceeded` (đọc `Retry-After`, câu cho khách: nhập sai mã quá nhiều lần, thử lại sau hoặc bỏ mã để đặt). Chưa gửi `clientIp` thì mọi lần sai đếm chung theo khóa dịch vụ (Quyết định 4).
- Đơn `submittedVia = queued` với mã sai không báo `coupon_invalid` (CRM bỏ mã, tạo đơn theo số CRM, `priceReview`), nên không được đếm là lần sai; người gửi có thể suy ra mã đúng/sai từ `grandTotal`. Dò kiểu này phải tạo đơn thật và vướng giới hạn 20 đơn/phút mỗi SĐT; chưa xử lý ở đây (cần quyết định: đếm cả trường hợp này hay giấu kết quả mã ở đơn queued). Bước chặn trước (đã quá ngưỡng → 429) vẫn áp cho đơn queued có mã.
- Khóa đếm không chứa tenant (SubjectKey dùng secret máy chủ): khi có nhiều shop, lần sai của một khách ở shop này tính cả ở shop khác. Hiện một shop.

## Reviewer

Kết luận: ĐẠT (lần 1, agent reviewer trên phần đã stage). Phát hiện phải sửa: không có. Reviewer đã kiểm: `Peek` cùng khóa cửa sổ với `Allow`, Valkey nil → 0, lỗi → dự phòng chỉ đọc; không lệnh Valkey nào trong `RetryTx` (xem trước trước `WriteTx`, ghi lần sai sau, `refused` gán lại mỗi lần chạy); 429 không lưu theo khóa; khóa Valkey chỉ HMAC; mã lỗi mới đủ bốn chỗ; không test bị xóa/nới; mã sinh khớp hợp đồng. Ghi chú không bắt buộc (đã có trong tệp task): website chưa gửi `clientIp` thì mọi lần sai dồn vào bộ đếm của khóa dịch vụ (Quyết định 4, Task 465); đơn `queued` mã sai không được đếm (Việc phát hiện thêm); `TestCouponGuard_ParallelFailures` chỉ kiểm "vẫn chặn", không kiểm số đếm chính xác.
