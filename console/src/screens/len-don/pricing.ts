// Tính tiền đơn để HIỆN TRƯỚC trên màn Lên đơn (kế hoạch M2 Task M2-27). Cùng thuật toán với
// internal/modules/order/internal/pricing (Task M2-07): tiền dòng, giảm dòng, giảm cấp đơn và mã chia về dòng (làm tròn
// xuống, phần lẻ vào dòng có base lớn nhất, không quá base, phần còn lại sang dòng lớn kế tiếp — Q4, Q-T64-1), phí giao,
// tổng đơn, tỷ lệ giảm tay. Số máy chủ trả là số cuối. Kiểm bằng cùng tệp testdata/vectors.json (pricing.test.ts).
// Tiền là số nguyên đồng trong [0, MAX_MONEY]; phép nhân, chia tỷ lệ tính bằng BigInt (như tích 128 bit của Go).

/** 2^53 − 1 đồng, bằng pricing.MaxMoney của Go. */
export const MAX_MONEY = Number.MAX_SAFE_INTEGER;

export interface Line {
  key: number;
  qty: number;
  unitPrice: number;
  lineDiscount: number;
}

export interface Input {
  lines: Line[];
  orderDiscount: number;
  couponDiscount: number;
  shippingFee: number;
}

export interface LineTotals {
  key: number;
  gross: number;
  base: number;
  allocated: number;
  net: number;
}

export interface Totals {
  subtotal: number;
  lineDiscountTotal: number;
  orderDiscount: number;
  couponDiscount: number;
  discountTotal: number;
  shippingFee: number;
  grandTotal: number;
  lines: LineTotals[];
}

/** Lỗi của compute, cùng thứ tự kiểm với pricing.Compute: số âm, số vượt giới hạn, rồi từng dòng, rồi cấp đơn. */
export type PricingErrorCode = "negative" | "overflow" | "line_discount_exceeds" | "order_discount_exceeds";

export class PricingError extends Error {
  readonly code: PricingErrorCode;
  constructor(code: PricingErrorCode) {
    super(`pricing: ${code}`);
    this.name = "PricingError";
    this.code = code;
  }
}

function addMoney(a: number, b: number): number {
  if (a > MAX_MONEY - b) throw new PricingError("overflow");
  return a + b;
}

function validate(in_: Input): void {
  const nums = [
    ...in_.lines.flatMap((l) => [l.qty, l.unitPrice, l.lineDiscount]),
    in_.orderDiscount,
    in_.couponDiscount,
    in_.shippingFee,
  ];
  if (nums.some((x) => x < 0)) throw new PricingError("negative");
  // Số lẻ hay vượt 2^53 − 1 không phải tiền đồng (Go là int64 nên không có số lẻ): coi là vượt giới hạn.
  if (nums.some((x) => !Number.isSafeInteger(x))) throw new PricingError("overflow");
}

/** Tính tiền đơn; sai thì ném PricingError. */
export function compute(in_: Input): Totals {
  validate(in_);
  let subtotal = 0;
  let lineDiscountTotal = 0;
  const lines: LineTotals[] = [];
  for (const l of in_.lines) {
    const g = BigInt(l.qty) * BigInt(l.unitPrice);
    if (g > BigInt(MAX_MONEY)) throw new PricingError("overflow");
    const gross = Number(g);
    if (l.lineDiscount > gross) throw new PricingError("line_discount_exceeds");
    subtotal = addMoney(subtotal, gross);
    lineDiscountTotal += l.lineDiscount; // ≤ subtotal
    lines.push({ key: l.key, gross, base: gross - l.lineDiscount, allocated: 0, net: 0 });
  }
  const orderLevel = addMoney(in_.orderDiscount, in_.couponDiscount);
  if (orderLevel > subtotal - lineDiscountTotal) throw new PricingError("order_discount_exceeds");
  const alloc = allocate(
    lines.map((l) => l.base),
    orderLevel,
  );
  lines.forEach((l, i) => {
    l.allocated = alloc[i] ?? 0;
    l.net = l.base - l.allocated;
  });
  const discountTotal = lineDiscountTotal + orderLevel;
  return {
    subtotal,
    lineDiscountTotal,
    orderDiscount: in_.orderDiscount,
    couponDiscount: in_.couponDiscount,
    discountTotal,
    shippingFee: in_.shippingFee,
    grandTotal: addMoney(subtotal - discountTotal, in_.shippingFee),
    lines,
  };
}

/**
 * Chia amount về các dòng theo tỷ lệ bases (như pricing.Allocate): floor(base × amount / Σ base); phần dư vào dòng có
 * base lớn nhất (bằng nhau thì chỉ số nhỏ nhất), không quá base, còn lại sang dòng lớn kế tiếp. Σ = amount.
 * Điều kiện của bên gọi: base ≥ 0, 0 ≤ amount ≤ Σ base ≤ MAX_MONEY (sai thì ném Error, lỗi lập trình).
 */
export function allocate(bases: readonly number[], amount: number): number[] {
  const sum = bases.reduce((s, b) => s + b, 0);
  if (bases.some((b) => b < 0) || sum > MAX_MONEY || amount < 0 || amount > sum) {
    throw new Error("pricing.allocate: ngoài điều kiện");
  }
  const out = bases.map(() => 0);
  if (amount === 0) return out;
  let rest = amount;
  bases.forEach((b, i) => {
    out[i] = Number((BigInt(b) * BigInt(amount)) / BigInt(sum));
    rest -= out[i] ?? 0;
  });
  // Sắp xếp ổn định theo base giảm dần (Array.prototype.sort ổn định từ ES2019), như slices.SortStableFunc.
  const order = bases.map((_, i) => i).sort((i, j) => (bases[j] ?? 0) - (bases[i] ?? 0));
  for (const i of order) {
    if (rest === 0) break;
    const add = Math.min(rest, (bases[i] ?? 0) - (out[i] ?? 0));
    out[i] = (out[i] ?? 0) + add;
    rest -= add;
  }
  return out;
}

/**
 * Tỷ lệ giảm tay (giảm dòng + giảm cấp đơn, không tính mã; Q3) trên tạm tính, đơn vị 1/10.000, làm tròn lên, như
 * pricing.ManualDiscountBP. Không giảm tay → 0; có giảm mà tạm tính 0 → vô cực (luôn vượt ngưỡng).
 */
export function manualDiscountBP(t: Pick<Totals, "subtotal" | "lineDiscountTotal" | "orderDiscount">): number {
  const manual = BigInt(t.lineDiscountTotal) + BigInt(t.orderDiscount);
  if (manual === 0n) return 0;
  if (t.subtotal <= 0) return Number.POSITIVE_INFINITY;
  const s = BigInt(t.subtotal);
  const num = manual * 10000n;
  return Number(num / s + (num % s === 0n ? 0n : 1n));
}
