// Quy tắc hiển thị của màn Tệp bị mất (kế hoạch M1 Task 331): tên mục đích tệp và giờ phát hiện.

/** Tên tiếng Việt của mục đích tệp M1 (internal/platform/files/purpose.go); khóa lạ hiện nguyên. */
const PURPOSE_LABELS: Readonly<Record<string, string>> = {
  product_image: "Ảnh sản phẩm",
  catalog_import: "Tệp nhập Excel",
};

export function purposeLabel(purpose: string): string {
  return PURPOSE_LABELS[purpose] ?? purpose;
}

const LOST_AT = new Intl.DateTimeFormat("en-GB", {
  timeZone: "Asia/Ho_Chi_Minh",
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  hourCycle: "h23",
});

/** "03:00 18/11" theo giờ Việt Nam như mockup; chuỗi không phải thời gian → "—". */
export function lostAtText(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const part = (type: Intl.DateTimeFormatPartTypes) => LOST_AT.formatToParts(d).find((p) => p.type === type)?.value;
  return `${part("hour") ?? ""}:${part("minute") ?? ""} ${part("day") ?? ""}/${part("month") ?? ""}`;
}
