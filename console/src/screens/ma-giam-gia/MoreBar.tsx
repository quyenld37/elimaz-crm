// Hàng dưới bảng của màn Mã giảm giá: số dòng đang hiện và nút "Tải thêm" khi còn trang (danh sách mã, lượt dùng).
// label: chữ của nút khi mockup của màn ghi khác (màn Nhật ký thao tác: "Xem thêm", Task 462).

/** Phần của useInfiniteQuery mà hàng "Tải thêm" cần. */
interface Pages {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => Promise<unknown>;
}

export function MoreBar({ text, pages, label = "Tải thêm" }: { text: string; pages: Pages; label?: string }) {
  const next = () => {
    void pages.fetchNextPage();
  };
  return (
    <div className="more">
      <span>{text}</span>
      {pages.hasNextPage ? (
        <button type="button" className="btn btn-line" disabled={pages.isFetchingNextPage} onClick={next}>
          {label}
        </button>
      ) : null}
    </div>
  );
}
