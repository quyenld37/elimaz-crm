// Bảng biến thể màu × size của màn Sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, thẻ "Biến
// thể"). Thêm màu/size bằng Enter (hay rời ô) sinh dòng cho mọi tổ hợp còn thiếu; biến thể đã lưu không xóa, chỉ chuyển
// Ngừng bán; nút × của màu/size chỉ có khi không biến thể đã lưu nào dùng nó. Dưới 760 px mỗi dòng thành một thẻ.
// Cột Thứ tự (Task 513; mockup docs/mockups/m5/variant-order.html): kéo dòng (HTML5 như ImageManager), phím ↑ ↓ trên tay
// nắm, nút ↑ ↓ cho điện thoại; chỉ đổi bản nháp, nút Lưu gửi một lần sắp lại.
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { VariantStatus } from "../../api";
import {
  addOption,
  canRemoveOption,
  moveRow,
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
  /** Một lần lưu đang chạy: khóa cả kéo thả (fieldset disabled chỉ khóa nút, ô). */
  busy?: boolean;
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

interface RowOrder {
  /** Không kéo được (chỉ xem hay đang lưu). */
  locked: boolean;
  index: number;
  count: number;
  moved: boolean;
  dragging: boolean;
  /** Dời dòng này tới chỗ to; keyboard: dời bằng phím trên tay nắm (giữ tiêu điểm). */
  onMove: (to: number, keyboard: boolean) => void;
  onDragStart: () => void;
  onDragEnd: () => void;
  /** Đang kéo một dòng khác: thả vào đây. */
  onDrop: (() => void) | null;
}

function OrderCell({ name, readOnly, o }: { name: string; readOnly: boolean; o: RowOrder }) {
  if (readOnly)
    return (
      <td data-label="Thứ tự" className="c-ord">
        <span className="ord">{o.index + 1}</span>
      </td>
    );
  const onKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
    e.preventDefault();
    o.onMove(o.index + (e.key === "ArrowUp" ? -1 : 1), true);
  };
  return (
    <td data-label="Thứ tự" className="c-ord">
      <span className="v-ord">
        <button type="button" className="ibtn v-grip" aria-label={`Kéo để dời ${name}`} onKeyDown={onKey}>
          ⠿
        </button>
        <button
          type="button"
          className="ibtn"
          aria-label={`Đưa ${name} lên`}
          disabled={o.index === 0}
          onClick={() => {
            o.onMove(o.index - 1, false);
          }}
        >
          ↑
        </button>
        <button
          type="button"
          className="ibtn"
          aria-label={`Đưa ${name} xuống`}
          disabled={o.index === o.count - 1}
          onClick={() => {
            o.onMove(o.index + 1, false);
          }}
        >
          ↓
        </button>
      </span>
    </td>
  );
}

function Row({
  row,
  errors,
  readOnly,
  order,
  onPatch,
}: {
  row: VariantRow;
  errors: RowErrors | undefined;
  readOnly: boolean;
  order: RowOrder;
  onPatch: (p: RowPatch) => void;
}) {
  const name = `${row.color} · ${row.size}`;
  const isNew = row.id === undefined;
  const cls =
    [
      errors !== undefined ? "is-err" : row.status === "discontinued" ? "is-muted" : "",
      order.moved ? "is-moved" : "",
      order.dragging ? "is-drag" : "",
    ]
      .filter((c) => c !== "")
      .join(" ") || undefined;
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
  const drop = order.onDrop;
  return (
    <tr
      className={cls}
      data-key={row.key}
      draggable={order.locked ? undefined : true}
      onDragStart={order.locked ? undefined : order.onDragStart}
      onDragEnd={order.locked ? undefined : order.onDragEnd}
      onDragOver={
        drop === null
          ? undefined
          : (e) => {
              e.preventDefault();
            }
      }
      onDrop={
        drop === null
          ? undefined
          : (e) => {
              e.preventDefault();
              drop();
            }
      }
    >
      <OrderCell name={name} readOnly={readOnly} o={order} />
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

export function VariantGrid({ draft, errors, readOnly, busy = false, onChange }: VariantGridProps) {
  const [dragging, setDragging] = useState<string | null>(null);
  // Dời bằng phím: React dời nút trong DOM có thể làm mất tiêu điểm; đặt lại vào tay nắm của dòng vừa dời.
  const focusKey = useRef<string | null>(null);
  const body = useRef<HTMLTableSectionElement>(null);
  useEffect(() => {
    const k = focusKey.current;
    if (k === null) return;
    focusKey.current = null;
    const tr = Array.from(body.current?.rows ?? []).find((r) => r.dataset.key === k);
    tr?.querySelector<HTMLButtonElement>(".v-grip")?.focus();
  });
  const moved = new Set(draft.moved ?? []);
  const locked = readOnly || busy;
  const orderOf = (r: VariantRow, i: number): RowOrder => ({
    locked,
    index: i,
    count: draft.rows.length,
    moved: moved.has(r.key),
    dragging: dragging === r.key,
    onMove: (to, keyboard) => {
      onChange(moveRow(draft, i, to));
      if (keyboard) focusKey.current = r.key;
    },
    onDragStart: () => {
      setDragging(r.key);
    },
    onDragEnd: () => {
      setDragging(null);
    },
    onDrop:
      locked || dragging === null || dragging === r.key
        ? null
        : () => {
            const from = draft.rows.findIndex((x) => x.key === dragging);
            if (from >= 0) onChange(moveRow(draft, from, i));
            setDragging(null);
          },
  });
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
                <th className="c-ord">Thứ tự</th>
                <th>Biến thể</th>
                <th className="c-sku">SKU *</th>
                <th className="c-bar">Barcode</th>
                <th className="c-pr">Giá bán *</th>
                <th className="c-pr">Giá nhập</th>
                <th className="c-st">Trạng thái</th>
              </tr>
            </thead>
            <tbody ref={body}>
              {draft.rows.map((r, i) => (
                <Row
                  key={r.key}
                  row={r}
                  errors={errors[r.key]}
                  readOnly={readOnly}
                  order={orderOf(r, i)}
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
        {readOnly
          ? ""
          : "Thứ tự ở đây là thứ tự khách thấy trên website. Kéo ⠿ hoặc bấm ↑ ↓ để đổi (trên ⠿ dùng được phím mũi tên lên, xuống); thứ tự lưu cùng nút Lưu. "}
        Barcode để trống thì CRM tự sinh mã EAN-13 bắt đầu bằng 2 khi lưu; sửa được từng biến thể. Biến thể đã lưu không
        xóa, chỉ chuyển "Ngừng bán" (không lên đơn mới được, đơn cũ vẫn tra được); nút × ở màu, size chỉ bỏ được biến
        thể chưa lưu.
      </p>
    </div>
  );
}
