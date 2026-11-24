# Task 487 — Bổ sung dữ liệu theo mockup: chiến dịch giảm giá + quà sinh nhật (API + màn)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM, không đổi chữ ký cũ.
  `promotion`: thêm trường `PreviewSample.SKU`, `CampaignTarget.ModelCount` (*int), `BirthdayCustomer.CouponID`,
  `BirthdayCustomerPage.UpcomingWeek`/`Year`/`YearIssued`/`YearUsed`; hàm mới `CampaignTargetsByIDs`,
  `ListBatchStats`, `UsedOrders`. `config.Config` thêm `WebsiteOrigin` + hằng `DefaultWebsiteOrigin`. Các kiểu trên
  được tạo bằng literal có tên trường (grep `promotion.PreviewSample{`, `promotion.CampaignTarget{`, `BirthdayCustomerPage{`:
  chỉ trong gói promotion và `handlers_*`), nên thêm trường không gãy chỗ dùng.
- Gói phụ thuộc ngược (go list): `promotion` ← `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `modules/order`,
  `wiring/webcache`; `platform/config` ← `cmd/crm`, `httpapi`, `adapters/carrier/{,best,spx}`, `platform/{files,metrics,
  observability,telemetry}`, `testkit`. Không đổi gì của `content` (chỉ gọi `content.GetBanners` có sẵn, ở lớp HTTP).
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm (không phá): operation mới `getWebCampaignSettings`
  (`GET /api/v1/web-campaign-settings`, promotion.read); trường mới ở phản hồi `WebCampaign.url` (bắt buộc),
  `WebCampaign.targets`, `WebCampaign.bannerPositions` (chỉ getWebCampaign), `WebCampaignSample.sku` (bắt buộc),
  `CampaignTarget.modelCount`, `BirthdayCustomerPage.upcomingWeek`/`yearGifts`, `BirthdayCustomer.usedOrderCode`,
  `Coupon.boundPhoneMasked`/`usedOrderCode`/`usedOrderId` (id đơn để màn chi tiết đợt mở đơn như mockup);
  `listCouponBatches` nay có `givenCount/usedCount/expiredCount`. Lớp HTTP: `couponOrderCodes` trả `usedOrder{ID, Code}`
  (3 chỗ gọi: `ListCoupons`, `birthdayOrderCodes` → `birthdayPageOut`, `birthdayCSV`).
- Chỗ dùng trên màn hình: `console/src/screens/web-campaigns/` (CampaignList: liên kết `url`; CampaignEditor: tên
  đích từ `targets` (bỏ `resolveTargets` đọc tới 20 trang đích), gợi ý trần, số mẫu danh mục, SKU bảng mẫu;
  CampaignDialogs: banner số mấy khi xóa, câu vượt trần có số; campaignData: `pickedTargets`, `useMaxPercent`,
  `storeCampaign` thay `setQueryData` để bản trả về của lệnh ghi (không có `targets`) không xóa tên đích trong bộ đệm), `console/src/screens/birthday-gifts/` (BirthdayGifts: hộp "Sắp gửi · 7 ngày tới", "Mã đã dùng · năm";
  CustomersTab: "Đã dùng · <mã đơn>"; BatchList: cột Đã tặng/Đã dùng/Hết hạn; BatchDetail: cột "Gắn với số", mã đơn
  là liên kết `/don/<usedOrderId>`). `WEBSITE_ORIGIN` của console vẫn dùng cho bộ phối, quà sinh nhật (ngoài phạm vi).
  `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: không migration. Sửa câu `CoveredVariants` (+ `v.sku`), `TargetCategories` (+ số mẫu),
  `BirthdayGiftsSince` (+ `coupon_id`); câu mới `TargetProductsByIDs`, `TargetCategoriesByIDs`, `BatchStatsMany`,
  `CouponUsedOrders`; câu `BatchStats :one` được thay bằng `BatchStatsMany` (`GetBatchStats` gọi `ListBatchStats`)
  (`queries/promotion/*.sql`, đọc `promotion.coupon_redemptions`, `promotion.coupons` của chính
  promotion và view `catalog.products_v`, `catalog.variants_v`, `catalog.categories_v`).
- Dọn kèm (cấu trúc, không đổi hành vi): `ListBirthdayCustomers` tách `matchesText`, `setPageItems` (gocognit);
  test `TestCouponAPI_Redemptions` dùng helper mới `couponEnv.redeemOnNewOrder` chung với test mới (jscpd).
- Test sẽ chạy: test mới/sửa bằng `-run` trong `internal/modules/promotion` (webcampaign_test, birthday, batch,
  manage), `internal/httpapi` (handlers_web_campaigns_test, handlers_birthdays_test, handlers_promotion_test),
  `internal/platform/config` (config_test); `tests/arch`; console vitest của hai thư mục màn + `src/app/errors.test.ts`;
  cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (che SĐT: không có hàm Go
  che số kiểu "0905 *** 781"; chỉ `maskPhone` của console), `promotion.MaxPercent` (trần), `promotion.GetBatchStats`
  (số của một đợt), `order.Codes` (mã đơn theo id), `content.GetBanners` (danh sách banner), `catalog.DefaultSeller`,
  `config` reader `httpURL`.
- Dùng lại / viết mới vì: dùng lại `MaxPercent`, `order.Codes`, `content.GetBanners`, `CampaignPath`. Viết
  `ListBatchStats` (nhiều đợt một câu; `GetBatchStats` gọi lại nó cho một đợt để không có hai câu đếm), `UsedOrders`
  (chưa có hàm đọc đơn đã dùng của nhiều mã một lần; `ListRedemptions` chỉ một mã, phân trang), hàm che SĐT
  `maskPhone` trong promotion (một chỗ dùng; dạng giống `maskPhone` của console). Không thêm hàm vào content (Task 488
  đang sửa content): lọc banner theo chiến dịch ở lớp HTTP trên `GetBanners`.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 chủ dữ liệu: số đếm, đơn đã dùng, đích, trần thuộc promotion (đọc trong schema promotion); tên miền website là
  cấu hình tiến trình (`config`). 5.2 không ghi bảng module khác (task chỉ thêm đường đọc). 5.3 đọc catalog qua
  `products_v`/`variants_v`/`categories_v`; mã đơn qua `order.Codes` (public API, ở lớp HTTP như `ListCouponRedemptions`);
  banner qua `content.GetBanners` ở lớp HTTP (promotion không nhập content: test kiến trúc). 5.4–5.6 không áp dụng:
  không có ghi mới. 5.7 mọi câu chạy trong tx tenant của `catalogRead`/`campaignRead`. 5.8 module không nhập HTTP;
  ghép `url` ở lớp HTTP. 5.9 không có đường ghi mới; operation mới là GET có `x-permission: promotion.read`. 5.10 không
  dùng Valkey. 5.11 không thêm thư viện. 5.12 theo mẫu handler mỏng + hàm đọc của module.
- 6.1–6.13: chỉ đọc, không khóa, không đổi thứ tự khóa ADR-46; số đếm đọc trong một tx đọc (ảnh chụp nhất quán của
  câu). `sum/count` dùng `count(*)` (không NULL); tập rỗng có ca test (đợt không mã → 0, năm không quà → 0/0).
  Không lỗi tạm mới, không việc nền, không gọi ngoài: 6.4–6.13 không áp dụng.
- Dữ liệu cá nhân: SĐT gắn mã chỉ trả dạng che (`0905 *** 781`), không vào log/audit. Quyền: các trường mới nằm trong
  operation `promotion.read` sẵn có (banner số mấy, mã đơn là dữ liệu `promotion.read` đã thấy ở `listCouponRedemptions`).

## Quyết định thay chủ shop

1. **Số đầu trang Quà sinh nhật theo mockup**: thay hai ô "Đến hạn, chờ tạo mã" và "Đã dùng" (Task 209 tạm vẽ vì
   thiếu số) bằng "Sắp gửi · 7 ngày tới" và "Mã đã dùng · năm N" (dùng / đã tạo) như mockup đã duyệt. Số "Đến hạn"
   vẫn xem được ở bộ lọc trạng thái của tab Tất cả khách. Phương án khác: giữ sáu ô (lệch mockup).
2. **Thêm `usedOrderId` vào `Coupon`** (ngoài danh sách đã ghi lúc đầu): mockup chi tiết đợt có mã đơn là liên kết mở
   đơn, màn đơn mở theo id. Chỉ thêm trường tùy chọn (oasdiff không phá). Phương án khác: chỉ hiện mã đơn dạng chữ.
3. **`WEBSITE_ORIGIN` không thêm vào `docker/compose.prod.*`**: biến tùy chọn, mặc định `https://elimaz.com.vn` (tên
   miền của spec); sửa hạ tầng docker cần chủ shop. Khi website đổi tên miền thì thêm biến vào compose prod.
4. **Kênh đã gửi ("Đã tặng 08/11 · qua Zalo") chưa làm**: CRM không lưu kênh website đã gửi mã quà; ghi ở Việc phát
   hiện thêm. Màn hiện "Đã tặng 08/11".
5. **Trần chỉ đọc**: thêm `GET /api/v1/web-campaign-settings` cho câu "trần của shop 50%"; màn sửa trần không làm
   (Task 452 ghi là task cài đặt riêng). Đọc trần lỗi → câu chung không số, form vẫn dùng được.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đích chiến dịch theo mã: tên, mã, số mẫu danh mục (cả 0), giữ thứ tự, bỏ mã không còn | `TestCampaignTargetsByIDs` | `go test -race -count=1 -run 'Birthday\|TestCampaignTargetsByIDs\|TestPreviewSamples_SKU\|TestListBatchStats_UsedOrders\|TestCoupon_MaskedBoundPhone' ./internal/modules/promotion/` | `ok elimaz.com.vn/crm/internal/modules/promotion 2.874s` |
| Bảng giá mẫu có SKU biến thể | `TestPreviewSamples_SKU` | như trên | ok |
| Số đã tặng/dùng/hết hạn của nhiều đợt một câu (đợt rỗng → 0); đơn đã dùng của mã | `TestListBatchStats_UsedOrders` | như trên | ok |
| "Sắp gửi · 7 ngày tới", quà của năm (năm không quà → 0/0) | `TestListBirthdayCustomers_WeekAndYear` | như trên | ok |
| SĐT gắn mã chỉ trả dạng che `0905 *** 781` | `TestCoupon_MaskedBoundPhone` | như trên | ok |
| API chiến dịch: `url` theo WEBSITE_ORIGIN, `targets`, `bannerPositions`, `sku`, `getWebCampaignSettings` | `TestWebCampaignAPI_MockupData` | `go test -race -count=1 -run '^(TestWebCampaignAPI_MockupData\|TestBirthdayAPI_MockupData\|TestCouponAPI_Redemptions)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.636s` |
| API quà sinh nhật: upcomingWeek, yearGifts, usedOrderCode, CSV "Đơn đã dùng", listCoupons boundPhoneMasked + usedOrderCode + usedOrderId, số đợt trong listCouponBatches | `TestBirthdayAPI_MockupData` (+ `checkBirthdayBatch`) | như trên | ok |
| Lượt dùng mã vẫn đúng sau khi dùng helper chung | `TestCouponAPI_Redemptions` | như trên | ok |
| WEBSITE_ORIGIN: mặc định, bỏ "/" cuối, từ chối đường dẫn/không http | `TestLoadWebsiteOrigin` | `go test -count=1 ./internal/platform/config/` | `ok … 0.404s` |
| Màn chiến dịch: liên kết = `url`; số mẫu, trần (có số / lỗi → câu chung), SKU, tên đích từ `targets` không đọc trang 100, banner số mấy / không banner nào, sửa khi đang chạy vẫn có tên đích | `campaignRules.test.ts` (đích, trần, cảnh báo banner, liên kết), `CampaignEditor.test.tsx` ("danh sách chọn danh mục hiện mã kèm số mẫu…", "đọc trần lỗi…", "mở chiến dịch tạm dừng…", "Xem trước giá khi form đã sửa…"), `CampaignList.test.tsx` ("chép liên kết…", it.each "422 percent_too_high…", "Sửa khi đang chạy…", "xóa: cảnh báo banner số mấy…", "xóa khi không banner nào…") | `cd console && npx vitest run src/screens/web-campaigns src/screens/birthday-gifts src/app/errors.test.ts` | `Tests 112 passed (112)` |
| Màn quà sinh nhật: hai ô số như mockup (cả 0/0), "Đã dùng · DH…", cột Đã tặng/Đã dùng/Hết hạn (đợt cũ → 0), "Gắn với số" che, mã đơn liên kết `/don/<id>`, không gắn số → — | `BirthdayGifts.test.tsx` ("bốn số tổng như mockup", "năm chưa có quà…", "quà đang tắt…"), `CustomersTab.test.tsx` ("mỗi trạng thái một dòng…"), `BatchList.test.tsx` ("danh sách đợt: cột…", "chi tiết đợt: SĐT gắn mã đã che…") | như trên | như trên |
| Cổng tĩnh + test gói đổi và phụ thuộc | toàn bộ | `ci-task.sh` (scripts/agent) | `CI-TASK XANH (so với điểm tách nhánh 104bd032…)`; httpapi 58.8s, promotion 57.0s, config 2.7s (-race); tests/arch ok |

E2E viết sẵn, chưa chạy: không có (kế hoạch không ghi e2e cho task này).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Kênh website đã gửi mã quà (Zalo/email) chưa có trong CRM → chưa hiện "qua Zalo" ở chi tiết đợt (mockup D).
- Mockup C: câu "Xem trước lại rồi mới kích hoạt (giờ là 26 mẫu · 101 biến thể)" cần lỗi `preview_required` trả số
  mới; hiện màn chỉ nêu số sau khi bấm Xem trước lại.
- Màn sửa trần phần trăm chiến dịch (đã ghi ở Task 452) vẫn chưa có.
- (Reviewer, không bắt buộc) `storeCampaign` gọi `removeQueries` khi đích đổi: form đang mở có thể dựng lại từ bản mới
  (mất câu "Đã lưu"/bảng xem trước vừa hiện); cân nhắc `invalidateQueries`.
- (Reviewer, không bắt buộc) `CouponUsedOrders` lấy lượt có id lớn nhất; lượt `released` rồi bật lại giữ id cũ nên mã
  dùng nhiều lần có thể không ra đơn mới nhất (mã quà một lần không ảnh hưởng).
- `WEBSITE_ORIGIN` nên vào `docker/compose.prod.env.example` khi chủ shop duyệt đổi hạ tầng.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau ci-task XANH).

- Phát hiện phải sửa: không có. 7 mục (đủ yêu cầu, bằng chứng, ảnh hưởng, tái dùng, cách cũ, test bị tắt, luật dự án):
  đạt; tên/chữ ký cũ (`resolveTargets`, `TARGET_PAGES_MAX`, `BatchStatsParams`, `campaignOut(`) không còn chỗ dùng.
- Ghi chú không bắt buộc (chép vào Việc phát hiện thêm): `storeCampaign` dùng `removeQueries`; `CouponUsedOrders`
  theo id lớn nhất; ghi rõ `BatchStats` → `BatchStatsMany` ở Ảnh hưởng (đã ghi).
