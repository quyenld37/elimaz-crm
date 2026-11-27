// Thân của một báo cáo tồn đã tải xong (Task 241, M6-18): dòng "meta", bảng cuộn ảo hay câu trống, lỗi và nút "Tải thêm"
// của trang sau, chú thích công thức. Dùng chung cho Tồn và Xuất nhập tồn.
import type { ReactNode } from "react";
import { VirtualTable, type VColumn } from "./VirtualTable";
import { count } from "./format";
import type { MorePages } from "./useMorePages";

export interface PagedReportProps<T> {
  /** Câu bên trái dòng meta (giờ cập nhật hay kỳ). */
  meta: string;
  label: string;
  more: MorePages<T>;
  columns: readonly VColumn<T>[];
  rowKey: (row: T) => string;
  empty: ReactNode;
  footnote: string;
}

export function PagedReport<T>({ meta, label, more, columns, rowKey, empty, footnote }: PagedReportProps<T>) {
  return (
    <>
      <div className="meta">
        <span>{meta}</span>
        <span>{`${count(more.rows.length)} mã hàng${more.hasMore ? ", còn nữa" : ""}`}</span>
      </div>
      {more.rows.length === 0 ? (
        empty
      ) : (
        <VirtualTable
          label={label}
          rows={more.rows}
          columns={columns}
          rowKey={rowKey}
          canLoadMore={more.hasMore && !more.loading && more.error === null}
          onNearEnd={more.loadMore}
        />
      )}
      {more.error !== null && <p className="field-err">{more.error}</p>}
      {more.hasMore && (
        <div className="row">
          <button type="button" className="btn btn-line" disabled={more.loading} onClick={more.loadMore}>
            Tải thêm
          </button>
        </div>
      )}
      <p className="xs muted">{footnote}</p>
    </>
  );
}
