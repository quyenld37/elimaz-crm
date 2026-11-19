// Ô tìm và chọn khách của màn Lên đơn (mockup docs/mockups/m2/len-don.html trạng thái A; HV03, HV05). Tìm bằng
// listCustomers (mã KH, SĐT đủ hay đuôi 4 số trở lên, tên không dấu; luật ở customer.Search). Gợi ý che giữa SĐT tới khi
// chọn. Dòng cuối "+ Khách mới" để lên đơn cho khách chưa có hồ sơ (tên, SĐT lấy từ người nhận).
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useId, useState, type RefObject } from "react";
import { getCustomer, listCustomers, type CustomerListItem } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { formatMoney } from "../products/productRules";
import { customerQuery, displayPhone, maskPhone, shortDate } from "./orderRules";
import { useCombo } from "./useCombo";

/** Khách đang chọn trên form: hồ sơ có sẵn, hay khách mới (chưa có hồ sơ). */
export type CustomerChoice = { kind: "existing"; id: string; code: string; fullName: string } | { kind: "new" };

/** Số gợi ý tối đa; gõ thêm để thu hẹp. */
export const CUSTOMER_LIMIT = 8;

export const CUSTOMER_TOPIC = "customer.changed";

export interface CustomerPickerProps {
  value: CustomerChoice | null;
  /** Chọn khách; text là chữ đang gõ (khi chọn "Khách mới", để điền sẵn người nhận). */
  onChange: (v: CustomerChoice | null, text: string) => void;
  inputRef: RefObject<HTMLInputElement | null>;
}

function statsText(c: CustomerListItem): string {
  const s = c.stats;
  const parts = [`${String(s.orderCount)} đơn`];
  if (s.deliveredCount === 0) parts.push("chưa nhận đơn nào");
  else {
    if (s.returnedCount > 0) parts.push(`hoàn ${String(s.returnedCount)}`);
    if (s.lastOrderAt !== undefined) parts.push(`mua gần nhất ${shortDate(s.lastOrderAt)}`);
  }
  return parts.join(" · ");
}

function MaskedPhone({ c }: { c: CustomerListItem }) {
  const phone = c.phones[0];
  if (phone === undefined) return <>chưa có SĐT</>;
  const m = maskPhone(phone);
  const tail = m.slice(-4);
  return c.matchedBy === "phone" || c.matchedBy === "phone_tail" ? (
    <>
      {m.slice(0, -4)}
      <mark>{tail}</mark>
    </>
  ) : (
    <>{m}</>
  );
}

export function CustomerPicker({ value, onChange, inputRef }: CustomerPickerProps) {
  const listId = useId();
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const typed = customerQuery(text);
  const term = useDebounced(typed.kind === "query" ? typed.q : "", SEARCH_DELAY_MS);
  const found = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "search", term),
    enabled: open && term !== "",
    queryFn: () => unwrap(listCustomers({ query: { q: term, limit: CUSTOMER_LIMIT } })),
    placeholderData: keepPreviousData,
  });
  const hits = typed.kind === "query" && term !== "" ? (found.data?.items ?? []) : [];
  const canNew = typed.kind === "query" && term === typed.q && found.isSuccess && !found.isPlaceholderData;
  const count = open ? hits.length + (canNew ? 1 : 0) : 0;

  const pick = (i: number) => {
    const c = hits[i];
    setOpen(false);
    setText("");
    if (c === undefined) onChange({ kind: "new" }, text.trim());
    else onChange({ kind: "existing", id: c.id, code: c.code, fullName: c.fullName }, "");
  };
  const nav = useCombo(setOpen, count, pick);

  return (
    <section className="card" aria-labelledby={`${listId}-h`}>
      <div className="row-between">
        <h2 id={`${listId}-h`}>Khách</h2>
        <span className="small muted">
          <span className="kbd">F2</span> tìm khách
        </span>
      </div>
      <div className="combo" onBlur={nav.onBlur}>
        <input
          ref={inputRef}
          className="input is-search"
          type="search"
          role="combobox"
          aria-label="Tìm khách theo đuôi SĐT, tên hoặc mã KH"
          aria-expanded={count > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          autoComplete="off"
          maxLength={100}
          placeholder="Đuôi SĐT (4 số trở lên), tên không dấu hoặc mã KH"
          value={text}
          {...nav.input((s) => {
            setText(s);
          })}
        />
        {count > 0 && (
          <div className="sugg" role="listbox" id={listId} aria-label="Kết quả tìm khách" onMouseDown={nav.keepFocus}>
            {hits.map((c, i) => (
              <div
                key={c.id}
                className="sugg-i"
                role="option"
                aria-selected={nav.active === i}
                onClick={() => {
                  pick(i);
                }}
              >
                <b className="ellip">{c.fullName}</b>
                <span className="small muted num">{c.code}</span>
                <span className="sub">
                  <MaskedPhone c={c} /> · {statsText(c)}
                </span>
              </div>
            ))}
            {canNew && (
              <div
                className="sugg-new"
                role="option"
                aria-selected={nav.active === hits.length}
                onClick={() => {
                  pick(hits.length);
                }}
              >
                <span className="wrap-any">{`+ Khách mới "${text.trim()}"`}</span>
              </div>
            )}
          </div>
        )}
        {typed.kind === "short" && open ? (
          <p className="hint" style={{ marginTop: 6 }}>
            {typed.text}
          </p>
        ) : (
          count > 0 && (
            <p className="hint" style={{ marginTop: 6 }}>
              ↑ ↓ để chọn, Enter để lấy khách. Số đủ 10 chữ số thì tìm đúng số; 4–9 số là đuôi số. Danh sách che giữa số
              khi chưa chọn.
            </p>
          )
        )}
        {found.isError && open && <p className="field-err">Chưa tìm được khách. Thử lại sau ít giây.</p>}
      </div>
      {value !== null && (
        <Picked
          value={value}
          onClear={() => {
            onChange(null, "");
            inputRef.current?.focus();
          }}
        />
      )}
    </section>
  );
}

function Picked({ value, onClear }: { value: CustomerChoice; onClear: () => void }) {
  const id = value.kind === "existing" ? value.id : "";
  const detail = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "detail", id),
    enabled: id !== "",
    queryFn: () => unwrap(getCustomer({ path: { id } })),
  });
  if (value.kind === "new") {
    return (
      <div className="picked" style={{ marginTop: 16 }}>
        <span className="avatar" aria-hidden="true">
          +
        </span>
        <div className="grow stack-sm" style={{ gap: 4 }}>
          <b>Khách mới</b>
          <p className="small muted">Tên và SĐT lấy từ người nhận ở phần Giao tới; hồ sơ tạo khi lưu đơn.</p>
        </div>
        <button type="button" className="btn btn-quiet" onClick={onClear}>
          Đổi khách
        </button>
      </div>
    );
  }
  const c = detail.data;
  const phone = c?.identities.find((x) => x.kind === "phone")?.value;
  const s = c?.stats;
  return (
    <div className="picked" style={{ marginTop: 16 }}>
      <span className="avatar" aria-hidden="true">
        {Array.from(value.fullName.trim().split(/\s+/).at(-1) ?? "?")[0]}
      </span>
      <div className="grow stack-sm" style={{ gap: 4, minWidth: 0 }}>
        <div className="row">
          <b className="wrap-any">{value.fullName}</b>
          <span className="chip">{value.code}</span>
          {c?.tags.map((t) => (
            <span key={t.tag.id} className="chip chip-line">
              {t.tag.name}
            </span>
          ))}
        </div>
        {s !== undefined && (
          <p className="small muted">
            {phone === undefined ? "Chưa có SĐT" : displayPhone(phone)} · {s.orderCount} đơn · {s.deliveredCount} thành
            công · {s.returnedCount} hoàn (tỷ lệ hoàn {Math.round(s.returnRateBp / 100)}%) · đã chi{" "}
            <span className="money">{formatMoney(s.totalSpent)}</span>
          </p>
        )}
      </div>
      <button type="button" className="btn btn-quiet" onClick={onClear}>
        Đổi khách
      </button>
    </div>
  );
}
