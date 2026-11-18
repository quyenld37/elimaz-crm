// Tải thường (1×): 10 sự kiện mỗi giây trong 30 phút, 50 nhân viên mỗi người 0,5 lượt đọc mỗi giây.
// [P] Thay bằng phát lại một ca live thật khi có Page thử đủ dữ liệu (kế hoạch M0 Task 22).
export { webhook, read, handleSummary } from './common.js';
import { options as build } from './common.js';

export const options = build(10, 0.5, '30m');
