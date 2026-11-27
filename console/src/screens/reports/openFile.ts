// Mở đường tải tệp xuất (Task 239, 241). Chỉ mở đường dẫn http(s) của kho tệp, không mở chuỗi lạ. `opener` là đối
// tượng để test thay hàm mở (jsdom không điều hướng được).
export const opener = {
  open: (url: string): void => {
    window.location.assign(url);
  },
};

export function openable(url: string): boolean {
  try {
    const p = new URL(url).protocol;
    return p === "https:" || p === "http:";
  } catch {
    return false;
  }
}

export const TEXT_BAD_LINK = "Đường dẫn tải không hợp lệ. Thử lại sau.";

/** Mở url nếu hợp lệ; trả câu lỗi (tiếng Việt) khi không mở được, null khi đã mở. */
export function openFile(url: string): string | null {
  if (!openable(url)) return TEXT_BAD_LINK;
  opener.open(url);
  return null;
}
