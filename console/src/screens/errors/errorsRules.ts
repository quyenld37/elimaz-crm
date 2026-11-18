// Luật hiện của màn Sự kiện lỗi (kế hoạch M1 Task 330; mockup docs/mockups/m1/su-kien-loi.html). Giờ theo múi Việt
// Nam cố định (shop ở Việt Nam; không phụ thuộc múi của máy đang mở).

const TZ = "Asia/Ho_Chi_Minh";

const SHORT = new Intl.DateTimeFormat("en-GB", {
  timeZone: TZ,
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  hourCycle: "h23",
});

const CLOCK = new Intl.DateTimeFormat("en-GB", {
  timeZone: TZ,
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

const COUNT = new Intl.NumberFormat("vi-VN");

/** "06:12 18/11" (giờ:phút ngày/tháng, giờ Việt Nam). Chuỗi không phải giờ → "". */
export function shortTime(iso: string): string {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return "";
  const p = Object.fromEntries(SHORT.formatToParts(ms).map((x) => [x.type, x.value]));
  return `${p.hour ?? ""}:${p.minute ?? ""} ${p.day ?? ""}/${p.month ?? ""}`;
}

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
