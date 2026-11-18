// Ngâm: tải thường (10 sự kiện mỗi giây, 0,5 lượt đọc mỗi giây mỗi nhân viên) trong 8 giờ. run.sh lấy mẫu bộ nhớ
// mỗi dịch vụ và số đo bảng jobs theo thời gian; loadkit report chấm bộ nhớ, dòng chết, kích thước bảng.
export { webhook, read, handleSummary } from './common.js';
import { options as build } from './common.js';

export const options = build(10, 0.5, '8h');
