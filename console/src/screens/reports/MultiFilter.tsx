// Bộ lọc nhiều lựa chọn của Báo cáo đơn (Task 240, M6-17; mockup don.html trạng thái A, D: .chips, .ms). Mỗi bộ lọc là
// một chip ("Page: tất cả" / "Page: 2 đã chọn"); bấm mở khung tích chọn, "Áp dụng" mới gửi (một lần tải cho cả lần
// chọn). Không có ô gõ chữ: danh sách lấy từ getSalesReportFilters, đã giới hạn theo phạm vi người xem ở máy chủ.
import { useState } from "react";
import type { SalesReportFilters } from "../../api";
import {
  FILTER_FIELDS,
  MAX_PICKED,
  carrierLabel,
  hasFilters,
  noFilters,
  type FilterField,
  type OrderFilters,
} from "./salesTables";

interface Option {
  id: string;
  name: string;
}

const CHANNELS: readonly Option[] = [
  { id: "facebook", name: "Facebook" },
  { id: "web", name: "Website" },
  { id: "manual", name: "Nhập tay" },
];

/** Lựa chọn của một bộ lọc; mã đã chọn mà danh sách chưa có (chưa tải xong, đã bị xóa) vẫn hiện, bằng chính mã. */
function optionsOf(field: FilterField, lists: SalesReportFilters | undefined, picked: readonly string[]): Option[] {
  const base = baseOptions(field, lists);
  const known = new Set(base.map((o) => o.id));
  return [...base, ...picked.filter((p) => !known.has(p)).map((p) => ({ id: p, name: p }))];
}

function baseOptions(field: FilterField, lists: SalesReportFilters | undefined): Option[] {
  switch (field) {
    case "channel":
      return [...CHANNELS];
    case "sourceId":
      return lists?.sources ?? [];
    case "careBy":
    case "createdBy":
      return lists?.staff ?? [];
    case "pageId":
      return lists?.pages ?? [];
    case "carrierKey":
      return [
        ...(lists?.carrierKeys ?? []).map((k) => ({ id: k, name: carrierLabel(k) })),
        { id: "unknown", name: "Chưa gửi hãng" },
      ];
  }
}

export interface MultiFilterProps {
  value: OrderFilters;
  lists: SalesReportFilters | undefined;
  onChange: (next: OrderFilters) => void;
  disabled?: boolean;
}

export function MultiFilter({ value, lists, onChange, disabled = false }: MultiFilterProps) {
  const [open, setOpen] = useState<FilterField | null>(null);
  const [draft, setDraft] = useState<string[]>([]);
  const current = FILTER_FIELDS.find((f) => f.field === open);

  const toggleChip = (field: FilterField) => {
    if (open === field) {
      setOpen(null);
      return;
    }
    setDraft(value[field]);
    setOpen(field);
  };
  const apply = () => {
    if (current === undefined) return;
    onChange({ ...value, [current.field]: draft });
    setOpen(null);
  };

  return (
    <div className="stack-sm">
      <div className="chips">
        <span className="small label">Lọc:</span>
        {FILTER_FIELDS.map((f) => {
          const n = value[f.field].length;
          return (
            <button
              key={f.field}
              type="button"
              className={open === f.field ? "btn btn-dark" : "btn btn-quiet"}
              aria-expanded={open === f.field}
              disabled={disabled}
              onClick={() => {
                toggleChip(f.field);
              }}
            >
              {`${f.label}: ${n === 0 ? "tất cả" : `${String(n)} đã chọn`}`}
            </button>
          );
        })}
        {hasFilters(value) && (
          <button
            type="button"
            className="btn btn-text"
            disabled={disabled}
            onClick={() => {
              setOpen(null);
              onChange(noFilters());
            }}
          >
            Bỏ lọc
          </button>
        )}
      </div>
      {current !== undefined && (
        <div className="ms" role="group" aria-label={`Lọc theo ${current.label}`}>
          {optionsOf(current.field, lists, draft).map((o) => (
            <label key={o.id} className="check">
              <input
                type="checkbox"
                checked={draft.includes(o.id)}
                disabled={!draft.includes(o.id) && draft.length >= MAX_PICKED}
                onChange={(e) => {
                  setDraft(e.target.checked ? [...draft, o.id] : draft.filter((d) => d !== o.id));
                }}
              />{" "}
              {o.name}
            </label>
          ))}
          <div className="row ms-actions">
            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                setDraft([]);
              }}
            >
              Bỏ chọn
            </button>
            <button type="button" className="btn btn-dark" onClick={apply}>
              Áp dụng
            </button>
          </div>
        </div>
      )}
      <p className="xs muted">
        Danh sách Page, nguồn, nhân viên, hãng lấy trong phạm vi người xem. Bộ lọc không có ô gõ chữ.
      </p>
    </div>
  );
}
