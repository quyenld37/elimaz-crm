// Khung chung của ba danh sách COD (bảng kê, lệch đối soát, quá hạn): đọc theo trang bằng con trỏ, trạng thái đang tải,
// lỗi API (không vẽ như rỗng), rỗng, và nút Tải thêm. Còn nextCursor thì chưa coi là hết.
import { useInfiniteQuery, type QueryKey } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { errorMessage } from "../../app/errors";
import { PAGE } from "./codRules";

interface Page<T> {
  items: T[];
  nextCursor?: string;
}

/** Danh sách theo trang của key; fetch nhận limit và con trỏ (trang đầu: không có). */
export function usePaged<T>(key: QueryKey, fetch: (q: { limit: number; cursor?: string }) => Promise<Page<T>>) {
  const list = useInfiniteQuery({
    queryKey: key,
    initialPageParam: "",
    getNextPageParam: (last: Page<T>) => last.nextCursor,
    queryFn: ({ pageParam }) => fetch(pageParam === "" ? { limit: PAGE } : { limit: PAGE, cursor: pageParam }),
  });
  return { list, rows: list.data?.pages.flatMap((p) => p.items) ?? [] };
}

export interface PagedProps<T> {
  paged: ReturnType<typeof usePaged<T>>;
  /** Tiêu đề và câu của trạng thái rỗng. */
  emptyTitle: string;
  emptyText: string;
  /** Chữ cạnh nút Tải thêm, theo số dòng đã tải. */
  countText: (n: number) => string;
  children: ReactNode;
}

export function Paged<T>({ paged, emptyTitle, emptyText, countText, children }: PagedProps<T>) {
  const { list, rows } = paged;
  if (list.isPending) return <p className="muted">Đang tải…</p>;
  if (list.isError && list.data === undefined) {
    return (
      <div className="box box-err" role="alert">
        {errorMessage(list.error)}
      </div>
    );
  }
  if (rows.length === 0) {
    return (
      <div className="empty">
        <h2>{emptyTitle}</h2>
        <p className="small muted">{emptyText}</p>
      </div>
    );
  }
  return (
    <>
      {children}
      {list.hasNextPage && (
        <div className="more">
          <span>{countText(rows.length)}</span>
          <button
            type="button"
            className="btn btn-line"
            disabled={list.isFetchingNextPage}
            onClick={() => {
              void list.fetchNextPage();
            }}
          >
            Tải thêm
          </button>
        </div>
      )}
    </>
  );
}
