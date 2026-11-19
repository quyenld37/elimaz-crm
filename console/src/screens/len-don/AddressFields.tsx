// Phần "Giao tới" của màn Lên đơn (mockup docs/mockups/m2/len-don.html trạng thái A; HV06, HV07). Địa chỉ hai cấp
// (tỉnh, xã/phường) theo danh mục listAdminUnits. SĐT người nhận đang là SĐT của hồ sơ khác → cảnh báo "Có thể là cùng
// một khách" kèm liên kết xem hai hồ sơ; không bao giờ tự gộp.
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { Link } from "react-router";
import { getCustomer, listAdminUnits, listCustomers } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import type { CustomerChoice } from "./CustomerPicker";
import { CUSTOMER_TOPIC } from "./CustomerPicker";
import { cleanPhone, displayPhone, type AddressInput } from "./orderRules";
import { useCombo } from "./useCombo";

const ADMIN_TOPIC = "customer.admin-units";
/** SĐT đủ số mới hỏi chủ của số (đuôi số không đủ để nói "cùng một khách"). */
const FULL_PHONE_DIGITS = 10;

export interface AddressFieldsProps {
  value: AddressInput;
  onChange: (a: AddressInput) => void;
  customer: CustomerChoice | null;
  phoneIsCustomers: boolean;
  onPhoneIsCustomers: (v: boolean) => void;
}

export function AddressFields({ value, onChange, customer, phoneIsCustomers, onPhoneIsCustomers }: AddressFieldsProps) {
  const id = useId();
  const set = (patch: Partial<AddressInput>) => {
    onChange({ ...value, ...patch });
  };
  const customerId = customer?.kind === "existing" ? customer.id : "";
  const detail = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "detail", customerId),
    enabled: customerId !== "",
    queryFn: () => unwrap(getCustomer({ path: { id: customerId } })),
  });
  const saved = customerId === "" ? [] : (detail.data?.addresses ?? []);
  const [showSaved, setShowSaved] = useState(false);

  const phone = useDebounced(cleanPhone(value.phone), SEARCH_DELAY_MS);
  const owners = useQuery({
    queryKey: queryKey(CUSTOMER_TOPIC, "search", phone),
    enabled: phone.replace(/\D/g, "").length >= FULL_PHONE_DIGITS,
    queryFn: () => unwrap(listCustomers({ query: { q: phone, limit: 5 } })),
  });
  const other = (owners.data?.items ?? []).find(
    (c) => (c.matchedBy === "phone" || c.matchedBy === undefined) && c.id !== customerId,
  );

  const provinces = useQuery({
    queryKey: queryKey(ADMIN_TOPIC, "provinces"),
    queryFn: () => unwrap(listAdminUnits({})),
    staleTime: Number.POSITIVE_INFINITY,
  });

  return (
    <section className="card" aria-labelledby={`${id}-h`}>
      <div className="row-between">
        <h2 id={`${id}-h`}>Giao tới</h2>
        {saved.length > 0 && (
          <button
            type="button"
            className="btn btn-quiet"
            aria-expanded={showSaved}
            onClick={() => {
              setShowSaved(!showSaved);
            }}
          >
            Chọn địa chỉ đã lưu ({saved.length})
          </button>
        )}
      </div>
      {showSaved && (
        <div className="sugg" aria-label="Địa chỉ đã lưu" style={{ marginBottom: 12 }}>
          {saved.map((s) => (
            <button
              key={s.id}
              type="button"
              className="sugg-i"
              style={{ width: "100%", textAlign: "left", font: "inherit", background: "none", border: 0 }}
              onClick={() => {
                const a = s.address;
                onChange({
                  name: a.name,
                  phone: displayPhone(a.phone),
                  line: a.line,
                  provinceCode: a.provinceCode ?? "",
                  wardCode: a.wardCode ?? "",
                  wardName: a.wardName ?? "",
                });
                setShowSaved(false);
              }}
            >
              <span className="wrap-any">
                <b>{s.address.name}</b> · {displayPhone(s.address.phone)}
              </span>
              <span className="sub wrap-any">
                {[s.address.line, s.address.wardName, s.address.provinceName].filter(Boolean).join(", ")}
              </span>
            </button>
          ))}
        </div>
      )}
      <div className="grid-f">
        <label className="field">
          <span>Người nhận *</span>
          <input
            className="input"
            maxLength={120}
            autoComplete="off"
            value={value.name}
            onChange={(e) => {
              set({ name: e.target.value });
            }}
          />
        </label>
        <label className="field">
          <span>SĐT người nhận *</span>
          <input
            className="input num"
            inputMode="tel"
            maxLength={20}
            autoComplete="off"
            value={value.phone}
            onChange={(e) => {
              set({ phone: e.target.value });
            }}
          />
        </label>
      </div>
      {other !== undefined && (
        <div className="box box-soft" style={{ marginTop: 12 }}>
          <b>Có thể là cùng một khách.</b> Số {displayPhone(phone)} đang là SĐT của hồ sơ khác ({other.code} ·{" "}
          {other.fullName}).{" "}
          {customer?.kind === "existing"
            ? `Số này chỉ ghi ở địa chỉ giao, không gắn vào hồ sơ ${customer.fullName}. Không tự gộp.`
            : "Không tự gộp."}
          {customer?.kind === "existing" && (
            <div className="row" style={{ marginTop: 8 }}>
              <Link className="btn btn-line" to={`/khach/${customer.id}/gop?voi=${other.id}`}>
                Xem hai hồ sơ
              </Link>
            </div>
          )}
        </div>
      )}
      {customer?.kind === "new" && (
        <>
          <label className="check" style={{ marginTop: 8 }}>
            <input
              type="checkbox"
              checked={phoneIsCustomers}
              onChange={(e) => {
                onPhoneIsCustomers(e.target.checked);
              }}
            />{" "}
            Số này là của khách (gắn vào hồ sơ)
          </label>
          <p className="hint">Bật sẵn cho khách mới; tắt khi khách đặt hộ người khác.</p>
        </>
      )}
      <div className="grid-f" style={{ marginTop: 12 }}>
        <label className="field">
          <span>Tỉnh/thành *</span>
          <span className="select-wrap">
            <select
              value={value.provinceCode}
              onChange={(e) => {
                set({ provinceCode: e.target.value, wardCode: "", wardName: "" });
              }}
            >
              <option value="">Chọn tỉnh/thành</option>
              {(provinces.data?.items ?? []).map((p) => (
                <option key={p.code} value={p.code}>
                  {p.name}
                </option>
              ))}
            </select>
          </span>
        </label>
        <WardField value={value} onChange={onChange} />
      </div>
      <label className="field" style={{ marginTop: 12 }}>
        <span>Số nhà, đường *</span>
        <input
          className="input"
          maxLength={500}
          autoComplete="off"
          value={value.line}
          onChange={(e) => {
            set({ line: e.target.value });
          }}
        />
      </label>
      <p className="hint" style={{ marginTop: 4 }}>
        Địa chỉ hai cấp (tỉnh, xã/phường) theo đơn vị hành chính hiện hành. Thiếu tỉnh hoặc xã: chỉ lưu được đơn nháp
        hoặc đơn mới, chưa xác nhận được.
      </p>
    </section>
  );
}

function WardField({ value, onChange }: { value: AddressInput; onChange: (a: AddressInput) => void }) {
  const inputId = useId();
  const listId = useId();
  const [typing, setTyping] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const text = typing ?? value.wardName ?? "";
  const term = useDebounced(typing?.trim() ?? "", SEARCH_DELAY_MS);
  const wards = useQuery({
    queryKey: queryKey(ADMIN_TOPIC, "wards", value.provinceCode, term),
    enabled: open && value.provinceCode !== "",
    queryFn: () =>
      unwrap(listAdminUnits({ query: { parent: value.provinceCode, ...(term === "" ? {} : { q: term }) } })),
    placeholderData: keepPreviousData,
  });
  const items = open && value.provinceCode !== "" ? (wards.data?.items ?? []).slice(0, 8) : [];
  const pick = (i: number) => {
    const w = items[i];
    if (w === undefined) return;
    setTyping(null);
    setOpen(false);
    onChange({ ...value, wardCode: w.code, wardName: w.name });
  };
  const nav = useCombo(setOpen, items.length, pick);
  return (
    <div className="field">
      <label htmlFor={inputId}>Xã/phường *</label>
      <div className="combo" onBlur={nav.onBlur}>
        <input
          id={inputId}
          className="input"
          role="combobox"
          aria-expanded={items.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          autoComplete="off"
          maxLength={100}
          disabled={value.provinceCode === ""}
          placeholder={value.provinceCode === "" ? "Chọn tỉnh trước" : "Gõ để lọc xã/phường"}
          value={text}
          {...nav.input((s) => {
            setTyping(s);
            if (value.wardCode !== "") onChange({ ...value, wardCode: "", wardName: "" });
          })}
        />
        {items.length > 0 && (
          <div className="sugg" role="listbox" id={listId} aria-label="Xã/phường" onMouseDown={nav.keepFocus}>
            {items.map((w, i) => (
              <div
                key={w.code}
                className="sugg-i"
                role="option"
                aria-selected={nav.active === i}
                onClick={() => {
                  pick(i);
                }}
              >
                <span>{w.name}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
