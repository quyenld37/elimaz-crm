// Ô nhập của khối Vận chuyển trước khi gửi (mockup docs/mockups/m3/don-van-chuyen.html trạng thái A, G): cân nặng, khai
// giá, cho xem hàng, phí khi khách từ chối, ghi chú cho hãng (tài khoản API); mã vận đơn và phí hãng báo (tài khoản nhập
// mã tay, HV16). Cân nặng, khai giá, tùy chọn gửi đúng như lúc so phí (máy chủ so input_hash): đổi sau khi so phí thì
// phải so phí lại.
import type { CarrierOptions, Order } from "../../api";
import { moneyInput, parseMoney } from "../products/editRules";

export interface ParcelForm {
  weight: string;
  declared: string;
  /** "" = theo mặc định của tài khoản; "none" = không cho xem hàng (mã duy nhất các hãng đã nhận). */
  inspection: "" | "none";
  feeOnRefuse: string;
  note: string;
}

/** Thân chung của so phí và gửi hãng: trường bỏ trống thì máy chủ dùng mặc định (cài đặt, tài khoản hãng). */
export interface Params {
  weightGrams?: number;
  declaredValue?: number;
  options?: CarrierOptions;
}

export const MAX_WEIGHT = 1_000_000;

/**
 * Ô ban đầu: khai giá = tổng đơn, còn lại trống. Tùy chọn (cho xem, phí khi từ chối, ghi chú) không điền sẵn: máy chủ
 * coi `options` là trọn bộ — có một ô thì bỏ toàn bộ tùy chọn mặc định của tài khoản hãng (bên trả phí, lấy hàng…).
 */
export function initialForm(o: Order): ParcelForm {
  return { weight: "", declared: moneyInput(o.grandTotal), inspection: "", feeOnRefuse: "", note: "" };
}

/** Đọc ô nhập; ô sai dạng → null. Tùy chọn chỉ gửi khi có ô được điền (không thì tùy chọn mặc định của tài khoản). */
export function toParams(f: ParcelForm): Params | null {
  const out: Params = {};
  const w = f.weight.trim();
  if (w !== "") {
    if (!/^\d+$/.test(w) || Number(w) < 1 || Number(w) > MAX_WEIGHT) return null;
    out.weightGrams = Number(w);
  }
  if (f.declared.trim() !== "") {
    const d = parseMoney(f.declared);
    if (d === null) return null;
    out.declaredValue = d;
  }
  const opts: CarrierOptions = {};
  if (f.inspection !== "") opts.inspection = f.inspection;
  if (f.feeOnRefuse.trim() !== "") {
    const v = parseMoney(f.feeOnRefuse);
    if (v === null) return null;
    opts.feeOnRefuse = v;
  }
  if (f.note.trim() !== "") opts.note = f.note.trim();
  if (Object.keys(opts).length > 0) out.options = opts;
  return out;
}

export const paramsKey = (p: Params) => JSON.stringify(p);

export function ParcelFields(props: {
  form: ParcelForm;
  onChange: (f: ParcelForm) => void;
  disabled: boolean;
  /** Gợi ý cân nặng mặc định: tổng món × gam mỗi món theo cài đặt (null khi chưa đọc được cài đặt). */
  weightHint: { items: number; perItem: number } | null;
  /** Ghi chú hãng của đơn (gợi ý chép vào ô; không tự gửi). */
  orderNote: string;
}) {
  const { form: f, onChange, disabled } = props;
  const set = (p: Partial<ParcelForm>) => {
    onChange({ ...f, ...p });
  };
  const h = props.weightHint;
  const badWeight = f.weight.trim() !== "" && !/^\d+$/.test(f.weight.trim());
  // Có ô tùy chọn nào được điền thì máy chủ không dùng tùy chọn mặc định của tài khoản cho ô trống nữa.
  const own = f.inspection !== "" || f.feeOnRefuse.trim() !== "" || f.note.trim() !== "";
  const blank = own ? "Không" : "Theo tài khoản";
  return (
    <>
      <div className="grid-f" style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))" }}>
        <label className="field">
          <span>Cân nặng</span>
          <span className="unit">
            <input
              className={badWeight ? "input is-err" : "input"}
              aria-label="Cân nặng"
              inputMode="numeric"
              disabled={disabled}
              placeholder={h === null ? "" : String(Math.max(h.items * h.perItem, 1))}
              value={f.weight}
              onChange={(e) => {
                set({ weight: e.target.value });
              }}
            />
            <span>gam</span>
          </span>
          <span className="hint">
            {h === null ? "Bỏ trống: theo cài đặt" : `${String(h.items)} món × ${String(h.perItem)} g`}
          </span>
        </label>
        <label className="field">
          <span>Khai giá</span>
          <input
            className="input num"
            inputMode="numeric"
            disabled={disabled}
            value={f.declared}
            onChange={(e) => {
              set({ declared: e.target.value });
            }}
          />
        </label>
        <label className="field">
          <span>Cho xem hàng</span>
          <span className="select-wrap">
            <select
              disabled={disabled}
              value={f.inspection}
              onChange={(e) => {
                set({ inspection: e.target.value === "none" ? "none" : "" });
              }}
            >
              <option value="">{blank}</option>
              <option value="none">Không cho xem</option>
            </select>
          </span>
        </label>
        <label className="field">
          <span>Thu phí khi khách từ chối</span>
          <input
            className="input num"
            inputMode="numeric"
            disabled={disabled}
            placeholder={own ? "Không thu" : "Theo tài khoản"}
            value={f.feeOnRefuse}
            onChange={(e) => {
              set({ feeOnRefuse: e.target.value });
            }}
          />
        </label>
      </div>
      <label className="field">
        <span>Ghi chú cho hãng</span>
        <textarea
          className="input"
          style={{ minHeight: 64 }}
          maxLength={500}
          disabled={disabled}
          value={f.note}
          onChange={(e) => {
            set({ note: e.target.value });
          }}
        />
        <span className="hint">
          {props.orderNote === ""
            ? "Hãng in trên nhãn; không ghi thông tin nội bộ."
            : `Hãng in trên nhãn; không ghi thông tin nội bộ. Ghi chú hãng của đơn: "${props.orderNote}" — chỉ gửi khi chép vào ô này.`}
        </span>
      </label>
      {own && (
        <div className="box box-info small">
          Đã điền tùy chọn gửi: CRM gửi đúng các ô trên, không dùng tùy chọn mặc định của tài khoản hãng (bên trả phí,
          hãng tới lấy hay mang ra bưu cục, cho xem, phí khi từ chối). Để trống cả ba ô thì theo tài khoản.
        </div>
      )}
    </>
  );
}

export interface ManualForm {
  code: string;
  fee: string;
}

export function ManualFields(props: {
  carrier: string;
  accountName: string;
  form: ManualForm;
  onChange: (f: ManualForm) => void;
  disabled: boolean;
}) {
  const { form: f, onChange, carrier } = props;
  return (
    <>
      <p className="small">
        <b>{props.accountName}.</b> {`Tạo vận đơn trên trang ${carrier}, rồi dán mã vào đây.`}
      </p>
      <label className="field">
        <span>{`Mã vận đơn của ${carrier} *`}</span>
        <input
          className="input num"
          maxLength={64}
          disabled={props.disabled}
          value={f.code}
          onChange={(e) => {
            onChange({ ...f, code: e.target.value });
          }}
        />
      </label>
      <label className="field">
        <span>{`Phí ${carrier} báo`}</span>
        <input
          className="input num"
          inputMode="numeric"
          disabled={props.disabled}
          value={f.fee}
          onChange={(e) => {
            onChange({ ...f, fee: e.target.value });
          }}
        />
      </label>
      <p className="hint">Mã đã gắn với vận đơn khác của cùng hãng thì CRM báo lỗi, không lưu: kiểm lại mã.</p>
    </>
  );
}
