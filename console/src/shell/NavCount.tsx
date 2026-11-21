// Số đếm cạnh mục menu (HV02 của mockup M3, docs/mockups/m3/khung.js, m3.css .nav-n): màn khai `badge: <NavCount … />`
// trong route.tsx, tự lấy số (và nghe báo hiệu) rồi truyền vào. 0 → không vẽ gì; có việc quá hạn → nền đỏ. Câu cho
// trình đọc màn hình nằm trong chữ ẩn (thẻ span không nhận aria-label).

export interface NavCountProps {
  /** Số việc đang chờ. */
  n: number;
  /** Số việc quá hạn trong n (tùy chọn). */
  late?: number;
}

export function NavCount({ n, late = 0 }: NavCountProps) {
  if (n <= 0) return null;
  const said = late > 0 ? `${String(n)} việc, ${String(late)} quá hạn` : `${String(n)} việc`;
  return (
    <>
      <span className={late > 0 ? "nav-n is-late" : "nav-n"} aria-hidden="true">
        {n}
      </span>
      <span className="sr-only">{`, ${said}`}</span>
    </>
  );
}
