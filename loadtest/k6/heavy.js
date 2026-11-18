// Tải nặng (10×): 100 sự kiện mỗi giây trong 15 phút, 50 nhân viên mỗi người 2 lượt đọc mỗi giây (4× bài thường:
// 10× sẽ vượt giới hạn 300 yêu cầu / 60 s mỗi nhân viên của api). chaos.sh chạy cùng bài này.
export { webhook, read, handleSummary } from './common.js';
import { options as build } from './common.js';

export const options = build(100, 2, '15m');
