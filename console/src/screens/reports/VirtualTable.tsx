// Bảng số liệu cuộn ảo theo cửa sổ (Task 241, M6-18; mockup ton-kho.html: .rt-frame, cột đầu dính trái): chỉ dựng các
// dòng đang thấy (cộng 8 dòng mỗi phía), hai dòng đệm giữ chiều cao, như bảng của màn Sản phẩm. Cuộn tới gần cuối và
// còn trang sau thì gọi onNearEnd.
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useState, type ReactNode } from "react";

/** Mọi dòng một hàng chữ (tên cắt bằng dấu …), cao cố định nên không cần đo lại từng dòng. */
const ROW_HEIGHT = 41;

export interface VColumn<T> {
  header: string;
  /** Căn trái (chữ); mặc định căn phải (số). */
  left?: boolean;
  /** Chữ đậm (cột kết quả). */
  bold?: boolean;
  cell: (row: T) => ReactNode;
}

export interface VirtualTableProps<T> {
  label: string;
  rows: readonly T[];
  columns: readonly VColumn<T>[];
  rowKey: (row: T) => string;
  canLoadMore: boolean;
  onNearEnd: () => void;
}

export function VirtualTable<T>({ label, rows, columns, rowKey, canLoadMore, onNearEnd }: VirtualTableProps<T>) {
  // Khoảng từ đầu trang tới thân bảng (cho cuộn theo cửa sổ), đo một lần khi thân bảng gắn vào.
  const [margin, setMargin] = useState(0);
  const bodyRef = useCallback((el: HTMLTableSectionElement | null) => {
    if (el !== null) setMargin(el.getBoundingClientRect().top + window.scrollY);
  }, []);
  const v = useWindowVirtualizer({
    count: rows.length,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    scrollMargin: margin,
  });
  const items = v.getVirtualItems();
  const first = items.at(0);
  const last = items.at(-1);
  const lastIndex = last?.index ?? -1;
  useEffect(() => {
    if (canLoadMore && lastIndex >= rows.length - 1) onNearEnd();
  }, [canLoadMore, lastIndex, rows.length, onNearEnd]);

  const padTop = first === undefined ? 0 : first.start - margin;
  const padBottom = last === undefined ? 0 : v.getTotalSize() - (last.end - margin);
  return (
    <div className="rt-frame scroll-x">
      <table className="rt" aria-label={label}>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.header} className={c.left === true ? "l" : undefined}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody ref={bodyRef}>
          {padTop > 0 && (
            <tr className="v-pad" aria-hidden="true">
              <td colSpan={columns.length} style={{ height: padTop }} />
            </tr>
          )}
          {items.map((it) => {
            const row = rows.at(it.index);
            if (row === undefined) return null;
            return (
              <tr key={rowKey(row)} style={{ height: ROW_HEIGHT }}>
                {columns.map((c) => (
                  <td
                    key={c.header}
                    className={
                      [c.left === true ? "l" : "", c.bold === true ? "bold" : ""].join(" ").trim() || undefined
                    }
                  >
                    {c.cell(row)}
                  </td>
                ))}
              </tr>
            );
          })}
          {padBottom > 0 && (
            <tr className="v-pad" aria-hidden="true">
              <td colSpan={columns.length} style={{ height: padBottom }} />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
