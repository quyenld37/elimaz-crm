// Bảng biến thể màu × size của màn Sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, thẻ "Biến
// thể"). Thêm màu/size bằng Enter (hay rời ô) sinh dòng cho mọi tổ hợp còn thiếu; biến thể đã lưu không xóa, chỉ chuyển
// Ngừng bán; nút × của màu/size chỉ có khi không biến thể đã lưu nào dùng nó. Dưới 760 px mỗi dòng thành một thẻ.
import { useState, type KeyboardEvent } from "react";
import type { VariantStatus } from "../../api";
import {
  addOption,
  canRemoveOption,
  removeOption,
  setRow,
  type Draft,
  type OptionKind,
  type RowErrors,
  type RowPatch,
  type VariantRow,
} from "./editRules";

const KIND_TEXT: Record<OptionKind, string> = { color: "màu", size: "size" };
const VARIANT_STATUS: Record<VariantStatus, string> = { active: "Đang bán", discontinued: "Ngừng bán" };

export interface VariantGridProps {
  draft: Draft;
  /** Lỗi theo khóa dòng (kiểm trước khi gửi và lỗi của máy chủ). */
  errors: Readonly<Record<string, RowErrors>>;
  readOnly: boolean;
  onChange: (d: Draft) => void;
}

function OptionEditor({
  kind,
  draft,
  readOnly,
  onChange,
}: {
  kind: OptionKind;
  draft: Draft;
  readOnly: boolean;
  onChange: (d: Draft) => void;
}) {
  const [text, setText] = useState("");
  const list = kind === "color" ? draft.colors : draft.sizes;
  const word = KIND_TEXT[kind];
  const commit = () => {
    if (text.trim() === "") return;
    onChange(addOption(draft, kind, text));
    setText("");
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      commit();
    }
  };
  return (
    <div className="field">
      <span>{kind === "color" ? "Màu" : "Size"}</span>
      <div className="opt-row">
        {list.map((v) => (
          <span className="tag" key={v}>
            {v}
            {!readOnly && canRemoveOption(draft, kind, v) && (
              <button
                type="button"
                aria-label={`Bỏ ${word} ${v}`}
                onClick={() => {
                  onChange(removeOption(draft, kind, v));
                }}
              >
                ×
              </button>
            )}
          </span>
        ))}
        {!readOnly && (
          <input
            className="input"
            placeholder={`Thêm ${word}`}
            aria-label={`Thêm ${word}`}
            maxLength={50}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
            }}
            onKeyDown={onKey}
            onBlur={commit}
          />
        )}
      </div>
    </div>
  );
}

function Cell({
  label,
  name,
  value,
  error,
  placeholder,
  numeric,
  readOnly,
  onChange,
}: {
  label: string;
  name: string;
  value: string;
  error: string | undefined;
  placeholder?: string;
  numeric?: boolean;
  readOnly: boolean;
  onChange: (v: string) => void;
}) {
  const input = (
    <input
      className={`input${numeric === true ? " num" : ""}${error === undefined ? "" : " is-err"}`}
      aria-label={`${label} của ${name}`}
      aria-invalid={error === undefined ? undefined : true}
      inputMode={numeric === true ? "numeric" : undefined}
      placeholder={placeholder}
      value={value}
      disabled={readOnly}
      onChange={(e) => {
        onChange(e.target.value);
      }}
    />
  );
  return (
    <td data-label={label}>
      {error === undefined ? (
        input
      ) : (
        <span className="v-f">
          {input}
          <span className="field-err">{error}</span>
        </span>
      )}
    </td>
  );
}

function Row({
  row,
  errors,
  readOnly,
  onPatch,
}: {
  row: VariantRow;
  errors: RowErrors | undefined;
  readOnly: boolean;
  onPatch: (p: RowPatch) => void;
}) {
  const name = `${row.color} · ${row.size}`;
  const isNew = row.id === undefined;
  const cls = errors !== undefined ? "is-err" : row.status === "discontinued" ? "is-muted" : undefined;
  const cell = (
    label: string,
    field: "sku" | "barcode" | "price" | "costPrice",
    extra: { placeholder?: string; numeric?: boolean } = {},
  ) => (
    <Cell
      label={label}
      name={name}
      value={row[field]}
      error={errors?.[field]}
      readOnly={readOnly}
      onChange={(v) => {
        onPatch({ [field]: v });
      }}
      {...extra}
    />
  );
  return (
    <tr className={cls}>
      <td className="v-name lead">{name}</td>
      {cell("SKU", "sku")}
      {cell("Barcode", "barcode", { numeric: true, placeholder: isNew ? "Tự sinh khi lưu" : undefined })}
      {cell("Giá bán", "price", { numeric: true })}
      {cell("Giá nhập", "costPrice", { numeric: true, placeholder: "Không bắt buộc" })}
      <td data-label="Trạng thái">
        {isNew ? (
          <span className="small muted">Mới, chưa lưu</span>
        ) : (
          <span className="select-wrap">
            <select
              aria-label={`Trạng thái của ${name}`}
              value={row.status}
              disabled={readOnly}
              onChange={(e) => {
                const v = e.target.value;
                if (v === "active" || v === "discontinued") onPatch({ status: v });
              }}
            >
              {(["active", "discontinued"] as const).map((s) => (
                <option key={s} value={s}>
                  {VARIANT_STATUS[s]}
                </option>
              ))}
            </select>
          </span>
        )}
      </td>
    </tr>
  );
}

/** Câu đếm ở góc thẻ: "2 màu × 3 size = 6 biến thể" khi đủ tổ hợp, không thì "N biến thể". */
export function variantCountText(d: Draft): string {
  const n = d.rows.length;
  if (n > 0 && n === d.colors.length * d.sizes.length)
    return `${String(d.colors.length)} màu × ${String(d.sizes.length)} size = ${String(n)} biến thể`;
  return `${String(n)} biến thể`;
}

export function VariantGrid({ draft, errors, readOnly, onChange }: VariantGridProps) {
  return (
    <div className="card v-card">
      <div className="row-between">
        <h2>Biến thể</h2>
        <span className="small muted">{variantCountText(draft)}</span>
      </div>
      <div className="grid-f">
        <OptionEditor kind="color" draft={draft} readOnly={readOnly} onChange={onChange} />
        <OptionEditor kind="size" draft={draft} readOnly={readOnly} onChange={onChange} />
      </div>
      {draft.rows.length === 0 ? (
        <p className="muted v-empty">Thêm màu và size để có biến thể.</p>
      ) : (
        <div className="tbl-frame v-frame">
          <table className="tbl vtbl">
            <thead>
              <tr>
                <th>Biến thể</th>
                <th className="c-sku">SKU *</th>
                <th className="c-bar">Barcode</th>
                <th className="c-pr">Giá bán *</th>
                <th className="c-pr">Giá nhập</th>
                <th className="c-st">Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {draft.rows.map((r) => (
                <Row
                  key={r.key}
                  row={r}
                  errors={errors[r.key]}
                  readOnly={readOnly}
                  onPatch={(p) => {
                    onChange(setRow(draft, r.key, p));
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="hint v-hint">
        Barcode để trống thì CRM tự sinh mã EAN-13 bắt đầu bằng 2 khi lưu; sửa được từng biến thể. Biến thể đã lưu không
        xóa, chỉ chuyển "Ngừng bán" (không lên đơn mới được, đơn cũ vẫn tra được); nút × ở màu, size chỉ bỏ được biến
        thể chưa lưu.
      </p>
    </div>
  );
}
