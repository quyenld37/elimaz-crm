// Hàng dưới bảng của màn Mã giảm giá: số dòng đang hiện và nút "Tải thêm" khi còn trang (danh sách mã, lượt dùng).

/** Phần của useInfiniteQuery mà hàng "Tải thêm" cần. */
interface Pages {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => Promise<unknown>;
}

export function MoreBar({ text, pages }: { text: string; pages: Pages }) {
  const next = () => {
    void pages.fetchNextPage();
  };
  return (
    <div className="more">
      <span>{text}</span>
      {pages.hasNextPage ? (
        <button type="button" className="btn btn-line" disabled={pages.isFetchingNextPage} onClick={next}>
          Tải thêm
        </button>
      ) : null}
    </div>
  );
}
