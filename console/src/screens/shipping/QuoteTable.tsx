// Bảng so phí của khối Vận chuyển (mockup docs/mockups/m3/don-van-chuyen.html trạng thái A, B; HV08): một dòng mỗi
// tài khoản hãng (mỗi dịch vụ một dòng khi hãng trả nhiều dịch vụ). Dòng lỗi, đang hỏi, không dùng được luôn có chữ và
// không chọn được; không bao giờ hiện "0 ₫" thay cho lỗi (chỉ dòng ok có phí). Tài khoản nhập mã tay, tự giao hiện
// "Không báo phí" và chọn được.
import type { CarrierAccount, ShippingQuote } from "../../api";
import { shortTime } from "../../app/time";
import { formatMoney } from "../products/productRules";
import { SELF_CARRIER } from "./useShipment";

export type PickRow =
  { kind: "quote"; key: string; q: ShippingQuote } | { kind: "manual"; key: string; a: CarrierAccount };

/** Tên hãng trên nút gửi; khóa lạ → tên tài khoản. */
const CARRIER_NAME: Readonly<Record<string, string>> = {
  vtp: "Viettel Post",
  best: "Best Express",
  spx: "SPX",
  ghn: "GHN",
  self: "Shop tự giao",
};

export const carrierName = (key: string, fallback: string) =>
  Object.hasOwn(CARRIER_NAME, key) ? (CARRIER_NAME[key] ?? fallback) : fallback;

/** Câu của mã lỗi báo phí (ShippingQuote.errorCode); mã lạ → câu chung. */
const QUOTE_ERROR_TEXT: Readonly<Record<string, string>> = {
  timeout: "Hãng không trả lời",
  rate_limited: "Hãng đang bận, quá giới hạn yêu cầu",
  auth: "Tài khoản cần cấp quyền lại",
  address_unmapped: "Địa chỉ chưa khớp địa giới của hãng",
  rejected: "Hãng từ chối báo phí cho đơn này",
};

const quoteErrorText = (code: string | undefined) =>
  code !== undefined && Object.hasOwn(QUOTE_ERROR_TEXT, code)
    ? (QUOTE_ERROR_TEXT[code] ?? "")
    : "Hãng chưa báo được phí";

const quoteKey = (q: ShippingQuote) => `${q.carrierAccountId}|${q.service ?? ""}`;

/** Dòng của bảng: kết quả so phí theo thứ tự máy chủ (ok phí tăng dần, đang hỏi, lỗi), rồi tài khoản nhập tay chưa có. */
export function buildRows(quotes: readonly ShippingQuote[], manual: readonly CarrierAccount[]): PickRow[] {
  const seen = new Set(quotes.map((q) => q.carrierAccountId));
  return [
    ...quotes.map((q): PickRow => ({ kind: "quote", key: quoteKey(q), q })),
    ...manual.filter((a) => !seen.has(a.id)).map((a): PickRow => ({ kind: "manual", key: `${a.id}|`, a })),
  ];
}

/** Dòng ok có phí; chỉ dòng này mang giá. */
export const isPriced = (r: PickRow): r is { kind: "quote"; key: string; q: ShippingQuote & { feeTotal: number } } =>
  r.kind === "quote" && r.q.status === "ok" && r.q.feeTotal !== undefined;

export const selectable = (r: PickRow) => r.kind === "manual" || isPriced(r);

/** Dòng ok rẻ nhất (null khi không có giá nào). */
export function cheapest(rows: readonly PickRow[]): PickRow | null {
  let best: PickRow | null = null;
  let fee = Infinity;
  for (const r of rows) {
    if (isPriced(r) && r.q.feeTotal < fee) {
      best = r;
      fee = r.q.feeTotal;
    }
  }
  return best;
}

/**
 * Dòng đang chọn: đúng khóa đã chọn nếu còn chọn được; cùng tài khoản (dịch vụ khác) nếu có; không thì dòng rẻ nhất;
 * không có giá mà chỉ một dòng chọn được thì dòng đó; còn lại null (người xem tự chọn).
 */
export function resolvePick(rows: readonly PickRow[], picked: string | null): PickRow | null {
  if (picked !== null) {
    const same = rows.find((r) => r.key === picked && selectable(r));
    if (same !== undefined) return same;
    const account = picked.split("|")[0];
    const sameAccount = rows.find((r) => selectable(r) && r.key.split("|")[0] === account);
    if (sameAccount !== undefined) return sameAccount;
  }
  const c = cheapest(rows);
  if (c !== null) return c;
  const ok = rows.filter(selectable);
  return ok.length === 1 ? (ok[0] ?? null) : null;
}

export const isExpired = (r: PickRow, now: number) =>
  r.kind === "quote" && r.q.validUntil !== undefined && Date.parse(r.q.validUntil) <= now;

function RowBody({ r, best, now }: { r: PickRow; best: boolean; now: number }) {
  if (r.kind === "manual") {
    return (
      <>
        <span className="ellip">{r.a.name}</span>
        <span className="small muted">Không báo phí</span>
        <span className="sub">
          {r.a.carrierKey === SELF_CARRIER ? "Nhân viên shop giao, cập nhật tay" : "Tạo trên trang của hãng rồi dán mã"}
        </span>
      </>
    );
  }
  const q = r.q;
  if (isPriced(r)) {
    const expired = isExpired(r, now);
    const sub = [
      q.serviceName ?? q.service ?? "",
      q.expectedDays === undefined ? "" : `dự kiến ${String(q.expectedDays)} ngày`,
      best ? "rẻ nhất" : "",
      expired && q.validUntil !== undefined ? `báo phí hết hạn lúc ${shortTime(q.validUntil)}` : "",
    ].filter((x) => x !== "");
    return (
      <>
        <b className="ellip">{q.accountName}</b>
        <span className="money qt-fee">{formatMoney(r.q.feeTotal)}</span>
        {sub.length > 0 && <span className="sub">{sub.join(" · ")}</span>}
      </>
    );
  }
  if (q.status === "pending") {
    return (
      <>
        <span className="ellip">{q.accountName}</span>
        <span className="small">Đang hỏi hãng…</span>
      </>
    );
  }
  const auth = q.status === "unavailable" || q.errorCode === "auth";
  return (
    <>
      <span className="ellip">{q.accountName}</span>
      <span className="small">{auth ? "Không dùng được" : "Không có giá"}</span>
      <span className="sub">
        {q.status === "unavailable"
          ? "Tài khoản chưa sẵn sàng (chưa kết nối, tạm dừng hoặc lỗi khóa)"
          : quoteErrorText(q.errorCode)}
      </span>
    </>
  );
}

export function QuoteTable(props: {
  rows: readonly PickRow[];
  pick: PickRow | null;
  now: number;
  disabled: boolean;
  onPick: (key: string) => void;
}) {
  const best = cheapest(props.rows);
  return (
    <div className="qt" role="radiogroup" aria-label="Chọn tài khoản hãng">
      {props.rows.map((r) => {
        const can = selectable(r);
        const picked = props.pick?.key === r.key;
        const wait = r.kind === "quote" && r.q.status === "pending";
        const cls = ["qt-r", picked ? "is-pick" : "", can ? "" : wait ? "is-wait" : "is-err"];
        return (
          <label key={r.key} className={cls.filter((c) => c !== "").join(" ")}>
            <input
              type="radio"
              name="ship-pick"
              checked={picked}
              disabled={!can || props.disabled}
              onChange={() => {
                props.onPick(r.key);
              }}
            />
            <RowBody r={r} best={best?.key === r.key} now={props.now} />
          </label>
        );
      })}
    </div>
  );
}
