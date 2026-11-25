// Package facebook là adapter kênh Facebook Messenger và comment Page (spec 02, 04 mục 4; kế hoạch M4): Graph client có
// appsecret_proof, kiểm chữ ký webhook, tách gói thành sự kiện chuẩn hóa của channel, gửi, SendPolicy, tải tệp an
// toàn, luồng comment live. Mọi kiểu dữ liệu của Meta ở lại trong gói này; chỉ nhập internal/platform/** và phần công
// khai của internal/modules/channel (luật (n)). Khung của Task 143; hiện thực ở Task 146–150, 170, 171.
package facebook
