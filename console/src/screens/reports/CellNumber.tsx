// Ô số bấm được của một bảng báo cáo bán hàng (Task 240, M6-17; hành vi (a)): nút mở danh sách chi tiết khi ô có đơn
// (n > 0), chữ tĩnh khi 0. Ô đang xem được đánh dấu bằng aria-pressed.
import { sameCell, type Cell } from "./cells";
import { vnd } from "./format";

export interface CellNumberProps {
  n: number;
  text: string;
  cell: Cell;
  /** Ô đang mở (theo địa chỉ). */
  open: Cell | null;
  onOpen: (c: Cell) => void;
  money?: boolean;
}

export function CellNumber({ n, text, cell, open, onOpen, money = false }: CellNumberProps) {
  if (n <= 0) return money ? <span className="money">{text}</span> : <>{text}</>;
  return (
    <button
      type="button"
      className={money ? "cell-n money" : "cell-n"}
      aria-pressed={sameCell(cell, open)}
      onClick={() => {
        onOpen(cell);
      }}
    >
      {text}
    </button>
  );
}

/** Ô tiền chỉ để xem của bảng báo cáo: "1.150.000 ₫", hay "—" khi máy chủ trả null (mẫu số 0). */
export function MoneyCell({ v }: { v: number | null }) {
  return (
    <td>
      <span className="money">{v === null ? "—" : vnd(v)}</span>
    </td>
  );
}
