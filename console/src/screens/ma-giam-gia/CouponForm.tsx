// Ô điều khoản của mã (mockup ma-giam-gia.html trạng thái B, C, D) và hộp "Tạo mã". Ô % nhập theo %, gửi phần vạn;
// ô tiền theo đồng. Ô bị khóa (mã đã có lượt dùng, coupon_locked_after_use) hiện "· khóa" ở nhãn và không sửa được.
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { createCoupon, type CreateCouponRequest } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog, RefusalBox } from "../staff/staffData";
import { headers, reloadCoupons } from "./couponData";
import { CHANNELS, CODE_PATTERN, EMPTY_TERMS, readTerms, type TermsDraft, type TermsErrors } from "./couponRules";
import "./ma-giam-gia.css";

export interface FieldProps {
  label: string;
  /** Ô bị khóa vì mã đã dùng: nhãn có "· khóa". */
  locked?: boolean;
  hint?: string;
  error?: string | undefined;
  children: (p: { id: string; "aria-invalid"?: true }) => ReactNode;
}

/** Một ô có nhãn (gắn bằng htmlFor), gợi ý hoặc lỗi ngay dưới. */
export function Field({ label, locked = false, hint, error, children }: FieldProps) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>
        {label}
        {locked && <span className="locked"> · khóa</span>}
      </label>
      {children({ id, ...(error === undefined ? {} : { "aria-invalid": true as const }) })}
      {error !== undefined ? (
        <span className="field-err">{error}</span>
      ) : (
        hint !== undefined && <span className="hint">{hint}</span>
      )}
    </div>
  );
}

export interface TextFieldProps {
  label: string;
  value: string;
  onChange: (v: string) => void;
  hint?: string;
  error?: string | undefined;
  maxLength?: number;
  placeholder?: string;
  /** Ô số (chữ số căn phải, bàn phím số). */
  num?: boolean;
  disabled?: boolean;
}

/** Ô chữ một dòng có nhãn (tên, mã, tiền tố, số mã). */
export function TextField({
  label,
  value,
  onChange,
  hint,
  error,
  maxLength,
  placeholder,
  num = false,
  disabled,
}: TextFieldProps) {
  return (
    <Field label={label} hint={hint} error={error}>
      {(p) => (
        <input
          {...p}
          className={num ? "input num" : "input"}
          inputMode={num ? "numeric" : undefined}
          maxLength={maxLength}
          placeholder={placeholder}
          disabled={disabled}
          value={value}
          onChange={(e) => {
            onChange(e.target.value);
          }}
        />
      )}
    </Field>
  );
}

/** Chân hộp tạo (Hủy + nút chính), dùng cho Tạo mã và Tạo đợt mã. */
export function CreateFooter(o: { onClose: () => void; onSubmit: () => void; busy: boolean; label: string }) {
  return (
    <>
      <button type="button" className="btn btn-line" onClick={o.onClose}>
        Hủy
      </button>
      <button type="button" className="btn btn-dark" disabled={o.busy} onClick={o.onSubmit}>
        {o.label}
      </button>
    </>
  );
}

/** Hàng "kiểu giảm, giảm, giảm tối đa, đơn tối thiểu" (tạo mã, đợt mã, chi tiết mã chưa dùng). */
export function discountRow(f: ReturnType<typeof termFields>) {
  return (
    <div className="grid-f">
      {f.kind}
      {f.value}
      {f.maxDiscount}
      {f.minSubtotal}
    </div>
  );
}

export interface TermFieldsProps {
  draft: TermsDraft;
  set: (p: Partial<TermsDraft>) => void;
  errors: TermsErrors;
  /** Mã đã có lượt dùng: mọi ô trừ hạn cuối, tổng lượt bị khóa. */
  locked?: boolean;
  /** Không có quyền sửa: mọi ô chỉ xem. */
  readOnly?: boolean;
}

/** Từng ô điều khoản, để mỗi màn tự xếp (tạo mã, đợt mã, chi tiết mã đã dùng). */
export function termFields({ draft, set, errors, locked = false, readOnly = false }: TermFieldsProps) {
  const lockedOff = locked || readOnly;
  const text = (
    key: keyof TermsDraft,
    label: string,
    o: { hint?: string; num?: boolean; isLocked?: boolean; type?: string } = {},
  ) => {
    const v = draft[key];
    return (
      <Field label={label} locked={o.isLocked === true && locked} hint={o.hint} error={errors[key]}>
        {(p) => (
          <input
            {...p}
            className={o.num === false ? "input" : "input num"}
            type={o.type ?? "text"}
            inputMode={
              o.type === undefined ? (key === "value" && draft.kind === "percent" ? "decimal" : "numeric") : undefined
            }
            disabled={o.isLocked === true ? lockedOff : readOnly}
            value={typeof v === "string" ? v : ""}
            onChange={(e) => {
              set({ [key]: e.target.value });
            }}
          />
        )}
      </Field>
    );
  };
  return {
    kind: (
      <div className="field">
        <span>Kiểu giảm{locked && <span className="locked"> · khóa</span>}</span>
        <div className="row">
          {(["percent", "fixed"] as const).map((k) => (
            <button
              key={k}
              type="button"
              className="toggle"
              aria-pressed={draft.kind === k}
              disabled={lockedOff}
              onClick={() => {
                set({ kind: k, value: "" });
              }}
            >
              {k === "percent" ? "Phần trăm" : "Số tiền"}
            </button>
          ))}
        </div>
      </div>
    ),
    value:
      draft.kind === "percent"
        ? text("value", "Giảm (%)", { hint: "0,01–100%, làm tròn xuống tới đồng", isLocked: true })
        : text("value", "Giảm (đồng)", { isLocked: true }),
    maxDiscount: draft.kind === "percent" ? text("maxDiscount", "Giảm tối đa (đồng)", { isLocked: true }) : null,
    minSubtotal: text("minSubtotal", "Đơn tối thiểu (đồng)", {
      hint: "Tính sau giảm dòng và giảm tay cấp đơn",
      isLocked: true,
    }),
    from: text("from", "Từ", { type: "datetime-local", isLocked: true, hint: "Giờ Việt Nam" }),
    to: text("to", "Đến", { type: "datetime-local", hint: "Giờ Việt Nam" }),
    maxUses: (label: string, hint?: string) => text("maxUses", label, hint === undefined ? {} : { hint }),
    perCustomer: text("perCustomer", "Lượt mỗi khách", { isLocked: true, hint: "Để trống: không giới hạn" }),
    channels: (
      <div className="field">
        <span>Kênh{locked && <span className="locked"> · khóa</span>}</span>
        <div className="row">
          {CHANNELS.map((c) => (
            <label key={c.key} className="check">
              <input
                type="checkbox"
                disabled={lockedOff}
                checked={draft.channels.includes(c.key)}
                onChange={(e) => {
                  set({
                    channels: e.target.checked
                      ? CHANNELS.map((x) => x.key).filter((k) => k === c.key || draft.channels.includes(k))
                      : draft.channels.filter((k) => k !== c.key),
                  });
                }}
              />{" "}
              {c.label}
            </label>
          ))}
        </div>
        <span className="hint">Không chọn kênh nào = mọi kênh</span>
      </div>
    ),
    release: (
      <label className="check">
        <input
          type="checkbox"
          disabled={lockedOff}
          checked={draft.releaseOnCancel}
          onChange={(e) => {
            set({ releaseOnCancel: e.target.checked });
          }}
        />{" "}
        Trả lại lượt khi đơn bị hủy trước khi giao{locked && <span className="locked"> · khóa</span>}
      </label>
    ),
  };
}

export const MAX_NAME = 120;
export const TEXT_NEED_NAME = "Điền tên mã cho nhân viên";
const TEXT_BAD_CODE = "Chữ, số, dấu -; 3–32 ký tự";

/** Hộp "Tạo mã" (trạng thái B): mã để trống thì máy chủ sinh; trùng → 409 code_taken báo theo mã đã nhập. */
export function CreateCouponDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<TermsDraft>(EMPTY_TERMS);
  const [errors, setErrors] = useState<TermsErrors & { code?: string; name?: string }>({});
  const [sentCode, setSentCode] = useState("");
  const create = useWriteAction((key, body: CreateCouponRequest) =>
    unwrap(createCoupon({ body, headers: headers(key) })),
  );
  const set = (p: Partial<TermsDraft>) => {
    setDraft((d) => ({ ...d, ...p }));
  };
  const f = termFields({ draft, set, errors });

  const save = async () => {
    const r = readTerms(draft);
    const c = code.trim();
    const e = { ...r.errors };
    const extra: { code?: string; name?: string } = {};
    if (c !== "" && !CODE_PATTERN.test(c)) extra.code = TEXT_BAD_CODE;
    if (name.trim() === "") extra.name = TEXT_NEED_NAME;
    setErrors({ ...e, ...extra });
    if (r.terms === null || Object.keys(extra).length > 0) return;
    setSentCode(c);
    const res = await create.run({ ...(c === "" ? {} : { code: c }), name: name.trim(), ...r.terms });
    if (!res.ok) return;
    reloadCoupons(qc);
    void navigate(`/ma-giam-gia/${res.data.id}`);
  };
  const taken = create.error instanceof ApiError && create.error.code === "code_taken";

  return (
    <Dialog
      title="Tạo mã"
      className="dlg-coupon"
      onClose={onClose}
      footer={
        <CreateFooter
          onClose={onClose}
          busy={create.pending}
          label="Tạo mã"
          onSubmit={() => {
            void save();
          }}
        />
      }
    >
      <div className="stack">
        <div className="grid-f">
          <TextField
            label="Mã"
            hint="Chữ, số, dấu -; 3–32 ký tự; không phân biệt hoa thường"
            error={errors.code}
            maxLength={32}
            placeholder="Để trống để tự sinh 8 ký tự"
            value={code}
            onChange={setCode}
          />
          <TextField
            label="Tên (cho nhân viên)"
            error={errors.name}
            maxLength={MAX_NAME}
            value={name}
            onChange={setName}
          />
        </div>
        {discountRow(f)}
        <div className="grid-f">
          {f.from}
          {f.to}
          {f.maxUses("Tổng lượt (để trống: không giới hạn)")}
          {f.perCustomer}
        </div>
        {f.channels}
        {f.release}
        {taken ? (
          <div className="box box-err" role="alert">
            <b>{`Mã ${sentCode} đã có.`}</b> Chọn mã khác hoặc để trống để tự sinh.
          </div>
        ) : (
          create.error !== null && <RefusalBox error={create.error} message={create.message} />
        )}
      </div>
    </Dialog>
  );
}
