// Ô chọn kho của các màn Kho (Tồn kho, Phiếu kho): một lựa chọn mỗi kho API trả, tùy chọn thêm dòng "mọi kho" (giá
// trị rỗng) ở đầu.
import type { Warehouse } from "../../api";

export function WarehouseSelect(p: {
  warehouses: readonly Warehouse[];
  value: string;
  onChange: (id: string) => void;
  /** Nhãn đọc màn hình khi ô không nằm trong <label>. */
  ariaLabel?: string;
  /** Chữ của lựa chọn "mọi kho" (giá trị ""); không có thì không có lựa chọn này. */
  allLabel?: string;
}) {
  return (
    <select
      aria-label={p.ariaLabel}
      value={p.value}
      onChange={(e) => {
        p.onChange(e.target.value);
      }}
    >
      {p.allLabel !== undefined && <option value="">{p.allLabel}</option>}
      {p.warehouses.map((w) => (
        <option key={w.id} value={w.id}>
          {w.name}
        </option>
      ))}
    </select>
  );
}
