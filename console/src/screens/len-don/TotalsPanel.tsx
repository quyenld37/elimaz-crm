// Khối "Tiền" của màn Lên đơn (mockup docs/mockups/m2/len-don.html trạng thái A). Số tính trước trên máy bằng
// pricing.ts; số máy chủ trả là số cuối.
import { useId, type KeyboardEvent, type ReactNode } from "react";
import { formatMoney } from "../products/productRules";
import type { Totals } from "./pricing";

export interface MoneyFieldProps {
  label: string;
  value: string;
  onChange: (s: string) => void;
  invalid: boolean;
  onEnter: (e: KeyboardEvent<HTMLInputElement>) => void;
}

/** Ô tiền trong khối tổng (giảm cấp đơn, phí ship). */
function MoneyField({ label, value, onChange, invalid, onEnter }: MoneyFieldProps) {
  const id = useId();
  return (
    <div className="sum-r">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        className={invalid ? "input num is-err" : "input num"}
        inputMode="numeric"
        autoComplete="off"
        placeholder="0"
        aria-invalid={invalid}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.ctrlKey && !e.metaKey) onEnter(e);
        }}
      />
    </div>
  );
}

export interface TotalsPanelProps {
  /** null khi số nhập chưa tính được (lỗi hiện ở ô tương ứng). */
  totals: Totals | null;
  itemCount: number;
  orderDiscount: string;
  onOrderDiscount: (s: string) => void;
  orderDiscountInvalid: boolean;
  shippingFee: string;
  onShippingFee: (s: string) => void;
  shippingFeeInvalid: boolean;
  onEnter: (e: KeyboardEvent<HTMLInputElement>) => void;
  /** Ô mã giảm giá (CouponField). */
  coupon: ReactNode;
  /** Câu lỗi tính tiền (giảm quá tiền hàng…). */
  error: string | null;
  /** Sửa đơn giữ mã này mà tạm tính sau giảm đã đổi: máy chủ tính lại mã khi lưu, totals là tổng chưa trừ mã. */
  couponRecalc?: string | null;
}

const minus = (v: number) => (v === 0 ? formatMoney(0) : `−${formatMoney(v)}`);

export function TotalsPanel(p: TotalsPanelProps) {
  const id = useId();
  const t = p.totals;
  const recalc = p.couponRecalc ?? null;
  return (
    <section className="card" aria-labelledby={id}>
      <h2 id={id}>Tiền</h2>
      <div className="sum">
        <div className="sum-r">
          <span>Tạm tính ({p.itemCount} món)</span>
          <span className="money">{t === null ? "—" : formatMoney(t.subtotal)}</span>
        </div>
        <div className="sum-r">
          <span>Giảm trên dòng</span>
          <span className="money">{t === null ? "—" : minus(t.lineDiscountTotal)}</span>
        </div>
        <MoneyField
          label="Giảm cấp đơn"
          value={p.orderDiscount}
          onChange={p.onOrderDiscount}
          invalid={p.orderDiscountInvalid}
          onEnter={p.onEnter}
        />
        {p.coupon}
        <div className="sum-r">
          <span>Giảm theo mã</span>
          <span className="money">
            {recalc !== null ? "Tính lại khi lưu" : t === null ? "—" : minus(t.couponDiscount)}
          </span>
        </div>
        <MoneyField
          label="Phí ship báo khách"
          value={p.shippingFee}
          onChange={p.onShippingFee}
          invalid={p.shippingFeeInvalid}
          onEnter={p.onEnter}
        />
        <div className="sum-r is-total">
          <span>{recalc === null ? "Tổng" : "Tổng (chưa trừ mã)"}</span>
          <span className="money">{t === null ? "—" : formatMoney(t.grandTotal)}</span>
        </div>
        <div className="sum-r">
          <span>{recalc === null ? "Thu hộ (COD)" : "Thu hộ (COD, chưa trừ mã)"}</span>
          <span className="money">{t === null ? "—" : formatMoney(t.grandTotal)}</span>
        </div>
      </div>
      {p.error !== null && <p className="field-err">{p.error}</p>}
      {recalc !== null && (
        <p className="hint">
          Tiền hàng hoặc giảm giá đã đổi: mã {recalc} sẽ được tính lại khi lưu; đơn dưới mức tối thiểu của mã thì không
          lưu được.
        </p>
      )}
      <p className="hint">
        Số hiện ở đây là tính trước trên máy; số máy chủ trả về là số cuối. Mã giảm giá kiểm ngay khi gõ xong, không tốn
        lượt; lượt chỉ bị trừ khi lưu đơn.
      </p>
    </section>
  );
}
