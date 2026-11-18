// Luật hiện của màn Sự kiện lỗi (kế hoạch M1 Task 330; mockup docs/mockups/m1/su-kien-loi.html). Giờ theo múi Việt
// Nam cố định (shop ở Việt Nam; không phụ thuộc múi của máy đang mở); giờ ngắn "HH:mm dd/MM" ở src/app/time.ts.

import { VN_TIME_ZONE } from "../../app/time";

const CLOCK = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

const COUNT = new Intl.NumberFormat("vi-VN");

/** "14:32:05" (giờ Việt Nam) của mốc ms. */
export function clockTime(ms: number): string {
  return CLOCK.format(ms);
}

/** Số lần có dấu chấm ngăn nghìn: "1.204". */
export function countText(n: number): string {
  return COUNT.format(n);
}

/** Mã nhận diện để tìm trong log: 6 ký tự đầu của dấu vân tay. */
export function shortId(fingerprint: string): string {
  return fingerprint.slice(0, 6);
}
