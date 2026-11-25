// Package fake là adapter kênh nhắn tin giả (kế hoạch M4, Task 145): chứng minh "thêm kênh không sửa lõi" và làm kênh
// cho test của mọi task hội thoại trước khi có adapter Facebook. Chỉ bật khi CRM_ENV khác production. Chỉ nhập
// internal/platform/** và phần công khai của internal/modules/channel (luật (n)). Khung của Task 143.
package fake
