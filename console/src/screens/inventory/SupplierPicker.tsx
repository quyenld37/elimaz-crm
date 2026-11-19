// Ô chọn nhà cung cấp có thêm nhanh (kế hoạch M1 Task 327; mockup docs/mockups/m1/kho-phieu.html trạng thái B, lớp
// .combo). Gõ để tìm trong nhà cung cấp đang dùng (listSuppliers, không gồm đã lưu trữ); tên gõ chưa có thì dòng cuối là
// "+ Thêm nhà cung cấp "…"": một createSupplier rồi ô chọn nhận nhà cung cấp mới. Dùng ở phiếu nhập (Task M1-51c).
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FocusEvent } from "react";
import { createSupplier, listSuppliers, type Supplier } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { SEARCH_DELAY_MS, useDebounced } from "./Balances";
import { NAME_MAX, SUPPLIERS_KEY, TEXT_NAME_TAKEN, canQuickAdd } from "./supplierRules";

/** Số dòng tối đa trong danh sách gợi ý; gõ thêm để thu hẹp. */
export const PICKER_LIMIT = 8;

export interface SupplierPickerProps {
  /** Nhà cung cấp đang chọn; null khi chưa chọn. */
  value: Supplier | null;
  /** Gọi khi chọn, thêm nhanh, hoặc khi người dùng gõ lại (null: bỏ lựa chọn cũ). */
  onChange: (s: Supplier | null) => void;
  /** Có inventory.suppliers.manage: hiện dòng thêm nhanh (máy chủ vẫn kiểm quyền). */
  canCreate: boolean;
  label?: string;
}

export function SupplierPicker({ value, onChange, canCreate, label = "Nhà cung cấp" }: SupplierPickerProps) {
  const qc = useQueryClient();
  const inputId = useId();
  const listId = useId();
  const errId = useId();
  const [text, setText] = useState(value?.name ?? "");
  // Bên gọi đổi nhà cung cấp (ví dụ phiếu nháp tải xong sau khi ô đã hiện): ô gõ hiện tên mới.
  const [shownId, setShownId] = useState(value?.id ?? null);
  if ((value?.id ?? null) !== shownId) {
    setShownId(value?.id ?? null);
    if (value !== null) setText(value.name);
  }
  const [open, setOpen] = useState(false);
  const term = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const create = useWriteAction((key, body: { name: string }) =>
    unwrap(createSupplier({ body, headers: { "Idempotency-Key": key } })),
  );

  const found = useQuery({
    queryKey: [...SUPPLIERS_KEY, "list", { q: term, includeArchived: false }],
    enabled: open,
    queryFn: () => unwrap(listSuppliers({ query: term === "" ? {} : { q: term } })),
    placeholderData: keepPreviousData,
  });
  const options = (found.data ?? []).slice(0, PICKER_LIMIT);
  // Kết quả của lần gõ trước (đang tải lần mới, hay còn đang chờ gõ: term chưa bằng tên gõ) không đủ để biết tên gõ đã
  // có chưa.
  const showAdd =
    canCreate && term === text.trim() && found.isSuccess && !found.isPlaceholderData && canQuickAdd(text, found.data);
  const err =
    create.error === null
      ? null
      : create.error instanceof ApiError && create.error.code === "supplier_name_taken"
        ? TEXT_NAME_TAKEN
        : create.message;

  const pick = (s: Supplier) => {
    setText(s.name);
    setOpen(false);
    onChange(s);
  };

  const quickAdd = async () => {
    const r = await create.run({ name: text.trim() });
    if (!r.ok) return;
    await qc.invalidateQueries({ queryKey: SUPPLIERS_KEY });
    pick(r.data);
  };

  const onBlur = (e: FocusEvent<HTMLDivElement>) => {
    if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) setOpen(false);
  };

  const expanded = open && (options.length > 0 || showAdd);
  return (
    <div className="field combo" onBlur={onBlur}>
      <label htmlFor={inputId}>{label}</label>
      <input
        id={inputId}
        className={err === null ? "input" : "input is-err"}
        role="combobox"
        aria-expanded={expanded}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-invalid={err !== null}
        aria-describedby={err === null ? undefined : errId}
        autoComplete="off"
        maxLength={NAME_MAX}
        value={text}
        onFocus={() => {
          setOpen(true);
        }}
        onChange={(e) => {
          setText(e.target.value);
          setOpen(true);
          create.reset();
          if (value !== null) onChange(null);
        }}
        onKeyDown={(e) => {
          if (e.key === "Escape") setOpen(false);
        }}
      />
      {expanded && (
        <div
          className="combo-list"
          role="listbox"
          id={listId}
          aria-label={label}
          // Giữ focus ở ô gõ khi bấm một dòng: Safari không đặt focus vào nút khi bấm, blur sẽ đóng danh sách trước click.
          onMouseDown={(e) => {
            e.preventDefault();
          }}
        >
          {options.map((s) => (
            <button
              key={s.id}
              type="button"
              className="combo-opt"
              role="option"
              aria-selected={value?.id === s.id}
              onClick={() => {
                pick(s);
              }}
            >
              <span className="wrap-any">{s.name}</span>
            </button>
          ))}
          {showAdd && (
            <button
              type="button"
              className="combo-opt is-add"
              role="option"
              aria-selected={false}
              disabled={create.pending}
              onClick={() => void quickAdd()}
            >
              <span className="wrap-any">{`+ Thêm nhà cung cấp "${text.trim()}"`}</span>
            </button>
          )}
        </div>
      )}
      {err !== null && (
        <span id={errId} className="field-err">
          {err}
        </span>
      )}
    </div>
  );
}
