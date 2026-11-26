# Hồ sơ App Review của Meta cho CRM ELIMAZ

Kế hoạch M4, Task 179 (M4-39); spec 02 mục 4, 5; 08 hàng M4 ("nộp App Review khi bản đầu chạy được"). Tệp này chỉ là
bản soạn sẵn: **chưa nộp gì lên Meta**. Nộp, xác minh doanh nghiệp, khai URL trong App Dashboard là việc của chủ shop
(tài khoản quản trị app), làm khi bản Inbox đầu chạy được trên staging.

Chỗ ghi `<…>` là giá trị chủ shop điền lúc nộp (không đặt bí mật vào tệp này). Tên nút, màn hình ở mục 3 theo mockup
M4 (Task 142); màn hình M4 (Task 174–178) chưa làm lúc soạn tệp này, quay video sau khi xong và sửa tên nút nếu khác.

## 1. Thông tin chung của app

| Mục | Giá trị |
|---|---|
| Tên app | ELIMAZ CRM |
| Loại app | Business |
| Mô tả ngắn (tiếng Anh, Meta đọc) | Internal CRM of the ELIMAZ fashion shop. Staff reply to customer messages and comments on the shop's own Facebook Pages, create orders from chats and live videos. Only the shop's own Pages are connected; no data is sold or shared. |
| Biểu tượng app | 1024 × 1024 PNG, nền đặc, logo ELIMAZ; tệp `<đường dẫn tệp biểu tượng>` (cùng logo của website) |
| URL chính sách quyền riêng tư | `<URL trang chính sách bảo mật của website elimaz.com.vn>` (trang của website; **cần bổ sung phần "Dữ liệu từ Facebook"** trước khi nộp, xem mục 6) |
| URL điều khoản (tùy chọn) | `<URL trang điều khoản của website>` |
| Callback xóa dữ liệu (Data Deletion Request URL) | `<HOOKS_PUBLIC_BASE_URL>/hooks/facebook/data-deletion` |
| Tên miền app | `<tên miền của console CRM>` |
| Phiên bản Graph API ghim | `v26.0` (`META_GRAPH_VERSION`), hạn `2028-07-29` |

### Callback xóa dữ liệu làm gì (để trả lời người duyệt)

- Meta gửi `POST` form `signed_request`; CRM kiểm chữ ký HMAC-SHA256 bằng app secret (`algorithm = HMAC-SHA256`), sai
  thì trả 400.
- Đúng thì CRM ghi yêu cầu (chỉ lưu băm SHA-256 của mã người dùng app), trả
  `{"url": "<HOOKS_PUBLIC_BASE_URL>/hooks/facebook/data-deletion/status/<mã>", "confirmation_code": "<mã>"}`; mã là 16
  byte ngẫu nhiên (32 ký tự hex).
- Trong vòng 1 phút, worker xóa mọi phiên đăng nhập Facebook (token người dùng, danh sách Page) của người dùng đó ở mọi
  shop, rồi đánh dấu xong. Trang trạng thái trả "Yêu cầu <mã>: đã xử lý lúc …" hoặc "đang xử lý".
- Phạm vi (Q30): callback chỉ mang mã người dùng app (nhân viên đã đăng nhập Facebook vào CRM), không phải khách nhắn
  tin cho Page; yêu cầu xóa dữ liệu khách theo luật Việt Nam xử lý ở module khách hàng.

## 2. Quyền và tính năng xin duyệt

Chỉ xin quyền thật sự dùng (02 mục 4). **Không xin `pages_manage_posts`** (CRM không đăng bài). Lead Ads, Utility
Messages không xin ở lượt này.

Cột "Lời gọi đã chạy" là lời gọi CRM thật sự làm; Meta đòi mỗi quyền có ít nhất một lời gọi thành công trong 30 ngày
trước khi nộp (spec 02 mục 5, [S43]). Trước khi nộp: chạy đủ các bước ở mục 3 trên staging trong vòng 30 ngày, rồi
xem cột "API calls" trong App Review → Permissions and Features đã có số cho từng quyền.

| Quyền / tính năng | Dùng trong CRM để | Lời gọi đã chạy (chỗ trong mã) | Bước quay màn hình |
|---|---|---|---|
| `pages_show_list` | Liệt kê Page nhân viên quản trị lúc kết nối Page | `GET /me/accounts?fields=id,name,tasks` (`facebook.ListPages`), `GET /me?fields=id` (`facebook.AppUserID`) | 3.1 |
| `pages_manage_metadata` | Đăng ký, hủy đăng ký webhook của Page (11 trường) | `POST /{page-id}/subscribed_apps`, `DELETE /{page-id}/subscribed_apps` (`facebook.Subscribe`, `Unsubscribe`) | 3.1, 3.7 |
| `pages_messaging` | Nhận tin khách qua webhook, gửi trả lời, trả lời riêng comment (Private Reply), đọc hội thoại | `POST /{page-id}/messages` (`facebook.Send`), `GET /{page-id}/conversations` (`facebook.FetchHistory`, `Backfill`) | 3.2, 3.3, 3.5 |
| `pages_read_engagement` | Đọc bài của Page, tên người tương tác, comment trong ca live | `GET /{post-id}?fields=…` (`facebook.FetchPost`); comment live (đường đọc live của M4) | 3.4, 3.5 |
| `pages_read_user_content` | Đọc comment của khách dưới bài và video live | webhook `feed`; `GET /{comment-id}/comments` (`facebook.Lookup`) | 3.4, 3.5 |
| `pages_manage_engagement` | Trả lời công khai, ẩn comment của khách | `POST /{comment-id}/comments`, `POST /{comment-id}` với `is_hidden` (`facebook.Send`) | 3.4 |
| `business_management` | Đi kèm `pages_messaging` khi Page thuộc Business Manager; token system user | `GET /{page-id}?fields=access_token,name,tasks` bằng token system user (`facebook.PageToken`) | 3.1 (cách system user) |
| Human Agent (tính năng) | Nhân viên trả lời khách sau 24 giờ và trong 7 ngày kể từ tin cuối của khách; chỉ tin do người gõ, không tự động | `POST /{page-id}/messages` với thẻ `HUMAN_AGENT` (`facebook.Send`) | 3.6 |
| Business Asset User Profile Access (tính năng) | Hiện tên, ảnh đại diện khách trong hộp chat và thẻ khách | `GET /{psid}?fields=first_name,last_name,profile_pic` (`facebook.FetchProfile`) | 3.2 |

### Mô tả cách dùng (chép vào ô "How will your app use…", tiếng Anh)

- **pages_show_list:** After a shop admin logs in with Facebook for Business, the CRM lists the Pages the admin manages so they can choose which of the shop's own Pages to connect.
- **pages_manage_metadata:** When a Page is connected, the CRM subscribes the app to the Page's webhooks (messages, feed, comments) and unsubscribes when the Page is disconnected.
- **pages_messaging:** Staff read customers' messages to the shop's Page in the CRM inbox and reply from the CRM; they can send a private reply to a customer's comment.
- **pages_read_engagement / pages_read_user_content:** The CRM shows comments customers leave on the shop's posts and live videos so staff can answer questions and record orders.
- **pages_manage_engagement:** Staff reply publicly to a comment or hide a comment (e.g. one containing a phone number) from the CRM.
- **business_management:** Required with pages_messaging for Pages owned by the shop's Business Manager; the shop may connect a Page with a system user token.
- **Human Agent:** A staff member (never an automation) replies to a customer within 7 days of the customer's last message when the customer's question needs more time (e.g. stock check, shipping issue).
- **Business Asset User Profile Access:** The inbox shows the customer's name and profile picture so staff know who they are talking to.

## 3. Kịch bản quay màn hình (từng bước trên staging)

Quay toàn màn hình 1920 × 1080, có chú thích tiếng Anh từng bước, không cắt ghép giữa bước. Mỗi video ≤ 5 phút. Dùng
Page thử và tài khoản thử ở mục 4, không dùng Page thật hay khách thật.

### 3.1 Kết nối Page (`pages_show_list`, `pages_manage_metadata`, `business_management`)

1. Mở console staging, đăng nhập (mục 4), vào **Cài đặt → Kênh**.
2. Bấm **Kết nối Facebook** → hộp thoại Facebook for Business mở cùng tab → đăng nhập tài khoản thử, chọn Page thử,
   cấp quyền.
3. Quay về CRM: danh sách Page hiện (từ `me/accounts`) → chọn Page thử → **Kết nối**.
4. Page hiện trạng thái "Đang hoạt động", chế độ "Chỉ xem"; mở Graph API Explorer hoặc chú thích cho thấy app đã
   đăng ký webhook của Page.
5. (system user) Bấm **Kết nối bằng mã system user**, dán token system user của Business Manager, chọn Page.

### 3.2 Nhận tin và xem hồ sơ khách (`pages_messaging`, Business Asset User Profile Access)

1. Trên điện thoại, tài khoản "khách thử" nhắn cho Page thử: "Áo này còn size M không?".
2. Trong vòng 2 giây, tin hiện ở hộp chat CRM, kèm tên và ảnh đại diện của khách thử.

### 3.3 Trả lời tin (`pages_messaging`)

1. Chuyển Page sang **Gửi thật** (quản trị bật).
2. Nhân viên gõ trả lời trong CRM → bấm **Gửi** → tin tới Messenger của khách thử (quay cả điện thoại).

### 3.4 Comment dưới bài (`pages_read_user_content`, `pages_manage_engagement`, `pages_read_engagement`)

1. Khách thử comment dưới một bài của Page thử: "Giá bao nhiêu ạ?".
2. Comment hiện trong CRM kèm bài gốc → nhân viên **Trả lời công khai** → comment trả lời hiện trên Facebook.
3. Khách thử comment một số điện thoại → nhân viên **Ẩn** → comment bị ẩn trên Facebook.

### 3.5 Trả lời riêng comment và comment trong ca live (`pages_messaging`, `pages_read_engagement`)

1. Trên comment ở 3.4, nhân viên bấm **Nhắn riêng** → khách thử nhận tin trong Messenger.
2. Page thử phát một video live ngắn; khách thử comment "chốt áo A1"; comment hiện trên màn **Live** của CRM.

### 3.6 Human Agent

1. Chuẩn bị: khách thử nhắn tin cuối hơn 24 giờ trước (dưới 7 ngày).
2. Trong CRM, nút **Trả lời (nhân viên)** hiện; nhân viên gõ và gửi → tin tới khách. Chú thích: chỉ tin do người gõ,
   tự động hóa không dùng được thẻ này.

### 3.7 Ngắt kết nối và xóa dữ liệu

1. **Cài đặt → Kênh → Ngắt kết nối** Page thử → app hủy đăng ký webhook, token Page bị xóa.
2. (Chú thích) Gỡ app khỏi Facebook của tài khoản thử → Meta gọi callback xóa dữ liệu → mở URL trạng thái → "đã xử lý
   lúc …".

## 4. Hướng dẫn cho người duyệt (ô "Instructions for reviewers")

- Địa chỉ staging: `<URL console staging>`. Staging đặt sau Cloudflare Access; người duyệt **không** nhận mã một lần
  qua email: chủ shop tạo một **chính sách Access riêng cho đợt duyệt** (ADR-35 hệ quả, spec 07) cho phép
  `<email tài khoản người duyệt>` vào thẳng, gỡ chính sách khi Meta trả kết quả.
- Tài khoản CRM thử: `<email>` / `<mật khẩu>` (vai trò quản trị shop thử, chỉ thấy Page thử).
- Page thử: `<tên Page thử>` (`<page id>`), người duyệt được thêm vai trò Tester trên app để nhắn và comment.
- Tài khoản Facebook thử đóng vai khách: dùng công cụ Test Users của app hoặc `<tài khoản thử>`.
- Bước thử ngắn (tiếng Anh, chép vào ô):
  1. Log in to `<staging URL>` with the test CRM account.
  2. Go to Settings → Channels, the test Page is already connected (or connect it with "Connect Facebook").
  3. From the test Facebook account, send a message to the test Page; it appears in the CRM inbox within seconds. Reply from the CRM.
  4. Comment on the pinned post of the test Page; reply publicly or hide the comment from the CRM.
  5. Human Agent: open the conversation labelled "older than 24h" and use "Reply (staff)".

## 5. Danh sách kiểm trước khi nộp

### Business Verification (02 mục 5; làm sớm, mắt xích dài nhất)

- [ ] Security Center của Business Manager: tên pháp lý đúng giấy đăng ký kinh doanh (hộ kinh doanh: **chưa xác minh**
      Meta có nhận không, chuẩn bị cả giấy phép kinh doanh và hóa đơn điện, nước đứng tên).
- [ ] Địa chỉ, số điện thoại doanh nghiệp khớp giấy tờ; website HTTPS `https://elimaz.com.vn` có cùng tên, địa chỉ ở
      chân trang.
- [ ] Email tên miền (`…@elimaz.com.vn`) hoặc xác minh qua điện thoại / tên miền (DNS TXT).
- [ ] Nộp, chờ tối đa 14 ngày làm việc; giữ ảnh chụp kết quả.

### Hồ sơ App Review

- [ ] Chính sách quyền riêng tư đã có phần "Dữ liệu từ Facebook" (mục 6), mở được không cần đăng nhập.
- [ ] Data Deletion Request URL đã khai trong App Dashboard → Settings → Basic; thử một lần bằng nút kiểm của Meta,
      trang trạng thái trả "đã xử lý".
- [ ] Biểu tượng 1024 × 1024, tên app, danh mục, email liên hệ.
- [ ] Mỗi quyền ở mục 2 có ≥ 1 lời gọi thành công trong 30 ngày qua (App Review → Permissions and Features).
- [ ] Một video cho mỗi nhóm bước ở mục 3, có chú thích tiếng Anh.
- [ ] Hướng dẫn người duyệt (mục 4), chính sách Access riêng đã bật, tài khoản thử đăng nhập được.
- [ ] Không có `pages_manage_posts` trong danh sách xin.
- [ ] App chuyển sang chế độ Live sau khi được duyệt; staging, production dùng đúng `META_APP_ID`.

### Sau khi được duyệt (02 mục 5, mục 7)

- [ ] Ghi hạn Data Use Checkup hằng năm vào biến `META_DATA_USE_CHECKUP_DUE=YYYY-MM-DD` của worker: còn ≤ 30 ngày, CRM
      gửi cảnh báo thường `data-use-checkup:<hạn>` qua Telegram, 7 ngày một lần tới khi đổi hạn mới.
- [ ] App luôn có lưu lượng thật (app không hoạt động 28 ngày có thể mất quyền).
- [ ] Mỗi lần thêm quyền mới: nộp lại App Review cho quyền đó.

## 6. Phần "Dữ liệu từ Facebook" cần thêm vào chính sách quyền riêng tư (bản nháp)

> **Dữ liệu từ Facebook và Messenger.** Khi bạn nhắn tin hoặc bình luận trên Trang Facebook của ELIMAZ, chúng tôi nhận
> tên, ảnh đại diện công khai, mã người dùng theo Trang và nội dung tin, bình luận để trả lời và xử lý đơn hàng của bạn.
> Dữ liệu chỉ dùng cho việc chăm sóc khách hàng của ELIMAZ, không bán, không chia sẻ cho bên thứ ba ngoài đơn vị vận
> chuyển giao hàng cho bạn. Hội thoại được giữ tối đa 24 tháng. Bạn có thể yêu cầu xóa dữ liệu bằng cách nhắn
> "xóa dữ liệu" cho Trang hoặc gửi email tới `<email liên hệ>`; nhân viên Facebook của ELIMAZ (người đăng nhập app) có
> thể gỡ app trong cài đặt Facebook để xóa dữ liệu đăng nhập của mình.
