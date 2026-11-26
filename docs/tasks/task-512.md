# Task 512 — Website đọc cài đặt quà sinh nhật từ CRM

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task do điều phối M5 cấp đêm 27/11,
không có mục riêng trong kế hoạch; dòng 512 đã thêm vào `danh-so-task-toan-cuc.md`). Nguồn:
`apps/storefront-crm/docs/tasks/task-213.md` «Việc phát hiện thêm» 1. Loại việc (quy trình mục 3): A

Việc: website (nguồn CRM) hiện thẻ quà sinh nhật trên trang kết quả đơn theo cài đặt của chính website, trong khi quà thật
do CRM phát theo `promotion.birthday_settings` (Task 468) → lệch khi nhân viên đổi cài đặt ở CRM. Thêm operation chỉ đọc
`webBirthdaySettings` `GET /api/v1/web/birthdays/settings` cho khóa dịch vụ.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu cũ. Thêm
  `(*web.Catalog).BirthdaySettings` (gói `internal/httpapi/web`), handler `(*handler).WebBirthdaySettings` (hiện thực
  `gen.Handler`, do ogen sinh thêm phương thức), hằng `cache.SpaceBirthdaySettings` và thêm nó vào `cache.Spaces()`
  (chỗ dùng `Spaces()`: `internal/wiring/webcache/invalidator.go` — resync làm mới mọi vùng, tự gồm vùng mới;
  `invalidator_test.go` kiểm số vùng 14 → sửa thành 15). Dùng lại `promotion.GetBirthdaySettings` (không đổi).
- Gói phụ thuộc ngược (go list): `internal/platform/cache` ← `internal/httpapi`, `internal/httpapi/web`,
  `internal/wiring/webcache`, `cmd/crm`; `internal/httpapi/web` ← `internal/httpapi`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: thêm 1 operation `webBirthdaySettings` (`GET /api/v1/web/birthdays/settings`,
  `x-auth: service_key`, `x-permission: web.catalog.read`, `If-None-Match` → 304, 200 có header `ETag`), thân là schema có
  sẵn `BirthdaySettingsFields` (enabled, amount, minOrder, daysBefore, validDays, minLeadDays — đúng `giftSettingsSchema`
  của website `apps/storefront-crm/src/server/birthday/rules.ts`; xem Khác kế hoạch). oasdiff: chỉ thêm. Không mã lỗi mới.
- Chỗ dùng trên màn hình: không (màn hình nhân viên không gọi `/web/*`; `console/src/api` sinh lại chỉ thêm kiểu).
- Câu sqlc, bảng, pha migration: không câu SQL mới, không migration; đọc `promotion.birthday_settings` qua
  `promotion.GetBirthdaySettings` (câu `GetBirthdaySettings` có sẵn).
- Test sẽ chạy: `TestWebBirthdaySettings` (mới, `internal/httpapi/web`), `TestInvalidator_SystemResync`
  (`internal/wiring/webcache`), tests/arch (routes, webexempt), `internal/httpapi` (errors snapshot không đổi); ci-task
  chạy gói đổi + phụ thuộc.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cache.ReadCache,
  tenancy.WithTenant), mẫu đọc web có ETag `internal/httpapi/web/intake.go`, `locations.go`, `banners.go`; hàm
  `etag`/`matches` của gói web; `promotion.GetBirthdaySettings` (trả mặc định khi chưa lưu).
- Dùng lại / viết mới vì: dùng lại hết các thứ trên; chỉ viết mới hàm `Catalog.BirthdaySettings` (khuôn y như
  `Locations`/`HomeBanners`) và một vùng đệm mới (mỗi loại dữ liệu web một vùng, QĐ14). Không tạo câu SQL mới.

## Soát thiết kế

Mục 5:
- 5.1–5.3: dữ liệu của module promotion, đọc qua public API `promotion.GetBirthdaySettings` (lớp HTTP gọi module, như
  `handlers_birthdays.go` và `web/campaigns.go`); không đọc thẳng bảng.
- 5.4–5.6: không áp dụng: chỉ đọc, không ghi, không gọi ra ngoài.
- 5.7: đọc trong `tenancy.WithTenant(..., db.TxAPI)` với tenant của khóa dịch vụ.
- 5.8: module không đổi; lớp HTTP mỏng (handler gọi `webCatalog`).
- 5.9: GET, không phải đường ghi; khai `x-permission`, `x-auth: service_key`.
- 5.10: Valkey chỉ đệm (60 s), câu trả lời vẫn lấy từ PostgreSQL khi thiếu/hỏng bộ đệm (đường dự phòng của ReadCache);
  website chỉ dùng để hiện thẻ, quà thật CRM phát theo PostgreSQL.
- 5.11: không thư viện mới. 5.12: theo mẫu web đọc có ETag.

Mục 6:
- 6.1–6.3, 6.5: chỉ một câu SELECT không khóa. 6.2: GET không cần khóa chống trùng.
- 6.4: lỗi tạm của database đi qua `WithTenant` (RetryTx) như mọi đường đọc web; bộ đệm bận → 503 như các đường cùng mẫu.
- 6.6–6.8: không áp dụng: không việc nền, không gọi ra ngoài.
- 6.9: timeout của TxAPI như các đường khác.
- 6.10: tiền `amount`, `minOrder` là `int64` đồng, hợp đồng `format: int64` (schema có sẵn `BirthdaySettingsFields`,
  không có `maximum`; giá trị bị CHECK giới hạn ≤ 1.000.000 và ≤ 100.000.000, không cần đổi schema cũ).
- 6.11: không áp dụng: không quy luật tồn/tiền mới.
- 6.12: không dữ liệu cá nhân (cài đặt shop); không có `updatedBy`, `version`, `updatedAt` trong câu trả lời.
- 6.13: khóa dịch vụ thiếu `web.catalog.read` → 403 (guard); có test.

## Quyết định thay chủ shop

1. **Quyền `web.catalog.read`** (có sẵn), không thêm quyền mới. Phương án khác: `web.birthdays.write` (quyền ghi, buộc
   khóa chỉ đọc phải mang quyền ghi) hay quyền mới `web.birthdays.read` (thêm quyền, phải đổi `docs/quyen.md`, khóa
   website phải cấp lại). Cài đặt quà là thông tin công khai trên website, cùng loại với chiến dịch giảm giá
   (`webCampaigns` dùng `web.catalog.read`). Đảo ngược dễ: đổi một dòng `x-permission`.
2. **Chưa lưu cài đặt → trả mặc định của CRM** (`promotion.DefaultBirthdaySettings`: `enabled=false`, 50.000 đ, 7 ngày,
   14 ngày, 30 ngày, đơn tối thiểu 0), không 404. Lý do: đúng Task 468 (CRM chưa bật thì CRM không phát quà; website
   không nên hứa quà mà CRM sẽ không phát); website không phải xử lý ca riêng.
3. **Bộ đệm vùng mới `birthday_settings`, 60 s, không sự kiện làm mới.** Lưu cài đặt (Task 468) không phát sự kiện; thêm
   sự kiện là việc ngoài phạm vi. Cài đặt đổi hiếm, lệch tối đa 60 s chỉ ảnh hưởng chữ trên thẻ. Phương án khác: không
   đệm (mỗi trang kết quả đơn một câu SELECT) — giữ khuôn chung của các đường web đọc có ETag.
4. **Không có Cache-Control riêng**: dự án đặt `Cache-Control: no-store` chung cho mọi câu trả lời API (middleware);
   các đường web đọc khác chỉ có ETag/304. Làm y như vậy.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chưa lưu → mặc định CRM (enabled false, 50000, 0, 7, 14, 30), đúng 6 trường; ETag; If-None-Match khớp → 304 rỗng; hạn đệm 60 s | `TestWebBirthdaySettings` | `go test -race -count=1 -run '^TestWebBirthdaySettings$' ./internal/httpapi/web/` | `ok elimaz.com.vn/crm/internal/httpapi/web 2.435s` |
| Nhân viên lưu cài đặt → website thấy số mới, ETag mới; gửi ETag cũ → 200 | `TestWebBirthdaySettings` | như trên | như trên |
| Không khóa → 401; khóa thiếu `web.catalog.read` (khóa `web.orders.read`) → 403 | `TestWebBirthdaySettings` | như trên | như trên |
| Resync làm mới mọi vùng, gồm vùng mới (15 vùng) | `TestInvalidator_SystemResync` | `go test -race -count=1 -run '^TestInvalidator_SystemResync$' ./internal/wiring/webcache/` | `ok elimaz.com.vn/crm/internal/wiring/webcache 1.703s` |
| Luật hợp đồng (x-auth, x-permission), kiến trúc | tests/arch | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 47.292s` |
| Lint, jscpd, mã lỗi console | cổng tĩnh | `make jscpd`; `golangci-lint run ./internal/httpapi/... ./internal/platform/cache/... ./internal/wiring/webcache/...`; `cd console && npx tsc --noEmit && npx vitest run src/app/errors.test.ts` | `Found 0 clones`; `0 issues.`; tsc không lỗi; `Tests 17 passed (17)` |
| Cổng kiểm của task (mã sinh khớp, gói đổi + phụ thuộc, -race cho httpapi, httpapi/web, cache, webcache) | ci-task | `ci-task.sh` (bản của dự án) | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh c3dd185f…)`, run 45893065914: static success, test (1) success, test (2) success |

## E2E viết sẵn, chưa chạy

(không có: task không có e2e trong kế hoạch)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không thêm schema `WebBirthdaySettings`: câu trả lời dùng lại `BirthdaySettingsFields` có sẵn (đúng 6 trường, cùng
  nguồn với màn hình nhân viên) để không lặp hợp đồng; ogen chỉ sinh thêm `BirthdaySettingsFieldsHeaders` và
  `WebBirthdaySettingsNotModified`.
- Gộp `main` trước review: `Already up to date` (main c3dd185f đã nằm trong nhánh).

## Việc phát hiện thêm

1. (Phía website) Khi website chuyển sang đọc `webBirthdaySettings`: mặc định của CRM là `enabled=false` (website
   mặc định `true`), nên thẻ quà tắt tới khi nhân viên bật ở CRM — chủ shop cần bật cài đặt ở CRM trước khi website
   chuyển.
2. (Phía website) Website kiểm bằng `z.strictObject`: nếu sau này `BirthdaySettingsFields` có thêm trường cho màn hình
   nhân viên, đường web cũng trả trường đó và website sẽ từ chối → khi ấy tách schema riêng cho web, hoặc website bỏ
   `strict` khi đọc từ CRM.

## Reviewer

Kết luận: ĐẠT (reviewer, lượt 1, trên phần đã stage). Không có phát hiện phải sửa. Ảnh hưởng khớp cả hai chiều; bảng
bằng chứng đúng (`TestWebBirthdaySettings` so đúng 6 khóa, 304 rỗng, TTL 60 s, 401/403; `TestInvalidator_SystemResync`
15 vùng); dùng lại `etag`, `matches`, ReadCache, `promotion.GetBirthdaySettings`; không test bị nới; không phạm luật dự
án; bốn quyết định thay chủ shop hợp lý. Ghi chú không bắt buộc đã xử lý: sửa câu 6.10 (schema cũ không có `maximum`),
ghi hai việc phía website vào «Việc phát hiện thêm».
