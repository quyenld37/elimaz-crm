// Bộ lọc của danh sách hội thoại (mockup inbox.html trạng thái A, B; HV06 đã sửa): tìm tên hoặc SĐT khách, trạng
// thái, người phụ trách (Của tôi, Chưa ai nhận, tên nhân viên khi người xem quản lý nhân viên), nhãn, loại, "Chỉ chưa
// đọc"; "Bỏ lọc" hiện khi khác mặc định. Chữ tìm gửi sau khi ngừng gõ 300 ms.
import { useEffect, useRef, useState } from "react";
import type { ConversationTag, StaffMember } from "../../api";
import { SEARCH_DELAY_MS } from "../inventory/Balances";
import { DEFAULT_FILTERS, KIND_OPTIONS, STATUS_OPTIONS, isDefault, searchable, type InboxFilters } from "./inboxRules";

export interface FiltersProps {
  value: InboxFilters;
  onChange: (f: InboxFilters) => void;
  tags: readonly ConversationTag[];
  /** Nhân viên khác để chọn (rỗng khi người xem không có quyền xem danh sách nhân viên). */
  staff: readonly StaffMember[];
}

export function Filters({ value, onChange, tags, staff }: FiltersProps) {
  const [text, setText] = useState(value.q);
  // Bỏ lọc hay quay lại trang có q khác: ô tìm theo URL.
  const [seenQ, setSeenQ] = useState(value.q);
  if (seenQ !== value.q) {
    setSeenQ(value.q);
    setText(value.q);
  }
  // Gửi chữ tìm khi ngừng gõ: hẹn giờ trong lúc gõ, gửi với bộ lọc mới nhất (ref), hủy khi rời màn.
  const latest = useRef({ value, onChange });
  useEffect(() => {
    latest.current = { value, onChange };
  });
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(
    () => () => {
      clearTimeout(timer.current);
    },
    [],
  );
  const typed = (t: string) => {
    setText(t);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const l = latest.current;
      if (l.value.q !== t) l.onChange({ ...l.value, q: t });
    }, SEARCH_DELAY_MS);
  };

  const set = (p: Partial<InboxFilters>) => {
    onChange({ ...value, ...p });
  };
  const short = text.trim() !== "" && !searchable(text);
  return (
    <>
      <input
        className="input is-search"
        type="search"
        aria-label="Tìm theo tên hoặc SĐT khách"
        placeholder="Tìm tên hoặc SĐT khách"
        maxLength={100}
        value={text}
        onChange={(e) => {
          typed(e.target.value);
        }}
      />
      {short && <p className="small muted">Nhập từ 2 chữ của tên hoặc từ 4 số cuối SĐT.</p>}
      <div className="row f-row">
        <span className="select-wrap grow">
          <select
            aria-label="Trạng thái"
            value={value.status}
            onChange={(e) => {
              const s = STATUS_OPTIONS.find(([k]) => k === e.target.value);
              if (s !== undefined) set({ status: s[0] });
            }}
          >
            {STATUS_OPTIONS.map(([k, label]) => (
              <option key={k} value={k}>
                {label}
              </option>
            ))}
          </select>
        </span>
        <span className="select-wrap grow">
          <select
            aria-label="Người phụ trách"
            value={value.assignee}
            onChange={(e) => {
              set({ assignee: e.target.value });
            }}
          >
            <option value="">Mọi người</option>
            <option value="me">Của tôi</option>
            <option value="none">Chưa ai nhận</option>
            {staff.map((u) => (
              <option key={u.id} value={u.id}>
                {u.displayName}
              </option>
            ))}
          </select>
        </span>
      </div>
      <div className="row f-row">
        <span className="select-wrap grow">
          <select
            aria-label="Nhãn"
            value={value.tag}
            onChange={(e) => {
              set({ tag: e.target.value });
            }}
          >
            <option value="">Mọi nhãn</option>
            {tags.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </span>
        <span className="select-wrap grow">
          <select
            aria-label="Loại"
            value={value.kind}
            onChange={(e) => {
              const k = KIND_OPTIONS.find(([x]) => x === e.target.value);
              if (k !== undefined) set({ kind: k[0] });
            }}
          >
            {KIND_OPTIONS.map(([k, label]) => (
              <option key={k} value={k}>
                {label}
              </option>
            ))}
          </select>
        </span>
      </div>
      <div className="row f-row">
        <button
          type="button"
          className="toggle"
          aria-pressed={value.unread}
          onClick={() => {
            set({ unread: !value.unread });
          }}
        >
          Chỉ chưa đọc
        </button>
        {!isDefault(value) && (
          <button
            type="button"
            className="btn btn-text"
            onClick={() => {
              onChange(DEFAULT_FILTERS);
            }}
          >
            Bỏ lọc
          </button>
        )}
      </div>
    </>
  );
}
