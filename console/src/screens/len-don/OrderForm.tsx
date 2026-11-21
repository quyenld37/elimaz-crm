// Form lên đơn nhanh (kế hoạch M2 Task M2-27; mockup docs/mockups/m2/len-don.html trạng thái A–H). Dùng lại nguyên khối
// ở M4 (cạnh hội thoại) qua src/shared. Gửi đơn bằng useWriteAction (G13): khóa Idempotency-Key sinh khi bấm lưu, giữ
// nguyên qua các lần tự gửi lại (503 retry_later, 409 request_in_progress, mất mạng) và khi bấm "Gửi lại" cùng nội dung;
// đơn tạo xong thì form trống, đơn sau là thao tác mới (khóa mới). Tiền hiện trước bằng pricing.ts; số máy chủ là số cuối.
// Chế độ sửa (prop edit, Task 85, HV23): cùng form điền sẵn từ đơn, khách cố định, phần ngoài editScope khóa kèm lý do;
// gửi updateOrder với version đã đọc và chỉ trường đã đổi (buildUpdate). 409 version_conflict: giữ nguyên phần đang
// gõ, câu báo và nút "Tải lại đơn" (bên gọi dựng lại form từ bản mới).
import { useQuery } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Link } from "react-router";
import {
  checkOrderDuplicates,
  createOrder,
  getMe,
  getOrderSettings,
  listTrafficSources,
  updateOrder,
  type CreateOrderRequest,
  type Order,
  type UpdateOrderRequest,
  type VariantSearchHit,
} from "../../api";
import { ApiError, ERROR_MESSAGES, NetworkError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { moneyInput, parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";
import { AddressFields } from "./AddressFields";
import { CouponField, useCouponCheck } from "./CouponField";
import { CustomerPicker, type CustomerChoice } from "./CustomerPicker";
import {
  EMPTY_ADDRESS,
  PENDING_TEXT,
  addressOf,
  buildRequest,
  buildUpdate,
  cleanPhone,
  saveBlock,
  warningText,
  type FormState,
  type Intent,
} from "./orderRules";
import { PricingError, compute, manualDiscountBP, type Totals } from "./pricing";
import { TotalsPanel } from "./TotalsPanel";
import { VariantPicker, optionText } from "./VariantPicker";
import "./len-don.css";

export const ORDER_TOPIC = "order.changed";
const MAX_QTY = 10000;
const MAX_BUSY_SHOWN = 3;

export const TEXT_BAD_MONEY = "Có ô tiền hoặc số lượng chưa đúng: sửa ô được đánh dấu.";

export interface OrderFormProps {
  /** Khách chọn sẵn (M4: khách của hội thoại). */
  initialCustomer?: { id: string; code: string; fullName: string };
  /** Gọi một lần khi máy chủ trả đơn đã tạo (201 hay 200 gửi lại). */
  onCreated?: (order: Order) => void;
  /** Sửa đơn có sẵn thay vì tạo (Task 85). */
  edit?: OrderEdit;
}

export interface OrderEdit {
  /** Đơn như đã đọc lúc mở form (version của nó đi kèm lần lưu). */
  order: Order;
  /** Gọi khi máy chủ trả đơn đã sửa. */
  onSaved: (order: Order) => void;
  /** Bấm "Tải lại đơn" sau 409 version_conflict: bên gọi đọc lại đơn và dựng lại form. */
  onReload: () => void;
}

export const TEXT_VERSION_CONFLICT = "Đơn đã thay đổi, tải lại để xem bản mới.";
export const TEXT_NO_CHANGE = "Chưa có thay đổi nào để lưu.";
const TEXT_LOCKED = "Không sửa được ở trạng thái này của đơn.";

interface EditBody {
  id: string;
  req: UpdateOrderRequest;
}

interface LineDraft {
  /** Dòng đang có của đơn đang sửa. */
  itemId?: string;
  variantId: string;
  sku: string;
  name: string;
  option: string;
  unitPrice: number;
  /** Có thể bán ở kho mặc định lúc chọn; null khi không đọc được. */
  available: number | null;
  qty: string;
  discount: string;
}

const parseQty = (s: string): number | null => {
  const t = s.trim();
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  return n >= 1 && n <= MAX_QTY ? n : null;
};
const parseOptMoney = (s: string): number | null => (s.trim() === "" ? 0 : parseMoney(s));

const PRICING_TEXT: Readonly<Record<string, string>> = {
  line_discount_exceeds: ERROR_MESSAGES.line_discount_exceeds ?? "",
  order_discount_exceeds: ERROR_MESSAGES.order_discount_exceeds ?? "",
  overflow: ERROR_MESSAGES.total_overflow ?? "",
  negative: ERROR_MESSAGES.invalid_input ?? "",
};

function tryCompute(...args: Parameters<typeof compute>): { totals: Totals | null; error: string | null } {
  try {
    return { totals: compute(...args), error: null };
  } catch (e) {
    if (e instanceof PricingError) return { totals: null, error: PRICING_TEXT[e.code] ?? null };
    throw e;
  }
}

// Lỗi mà máy chủ có thể đã làm hay chưa: nút "Gửi lại" gửi đúng đơn cũ cùng khóa.
const outcomeUnknown = (e: unknown) =>
  e instanceof NetworkError ||
  (e instanceof ApiError && (e.status >= 500 || (e.status === 409 && e.code === "request_in_progress")));

const percent = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 2 });
const bpText = (bp: number) => `${percent.format(bp / 100)}%`;

const STATUS_TEXT: Readonly<Record<string, string>> = {
  confirmed: "Đơn đã xác nhận, hàng đã giữ.",
  new: "Đơn mới, hàng đã giữ; xác nhận sau.",
  draft: "Đơn nháp, hàng đã giữ tới khi hết hạn.",
};

// Dòng của đơn đang sửa thành dòng nháp của form (giữ đơn giá lúc đặt).
const draftOf = (o: Order): LineDraft[] =>
  o.items.map((it) => ({
    itemId: it.id,
    variantId: it.variantId,
    sku: it.sku,
    name: it.name,
    option: "",
    unitPrice: it.unitPrice,
    available: null,
    qty: String(it.qty),
    discount: (it.lineDiscount ?? 0) > 0 ? moneyInput(it.lineDiscount) : "",
  }));

const moneyOrEmpty = (v: number | undefined) => ((v ?? 0) > 0 ? moneyInput(v) : "");

export function OrderForm({ initialCustomer, onCreated, edit }: OrderFormProps) {
  const id = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const customerRef = useRef<HTMLInputElement>(null);
  const productRef = useRef<HTMLInputElement>(null);
  const eo = edit?.order;
  const scope = eo?.editScope;
  const can = (k: keyof NonNullable<typeof scope>) => eo === undefined || scope?.[k] === true;
  const initial: CustomerChoice | null =
    eo !== undefined
      ? {
          kind: "existing",
          id: eo.customer?.id ?? eo.customerId,
          code: eo.customer?.code ?? "",
          fullName: eo.customer?.fullName ?? "",
        }
      : initialCustomer === undefined
        ? null
        : { kind: "existing", ...initialCustomer };

  const [customer, setCustomer] = useState<CustomerChoice | null>(initial);
  const [lines, setLines] = useState<LineDraft[]>(() => (eo === undefined ? [] : draftOf(eo)));
  const [address, setAddress] = useState(() => (eo === undefined ? EMPTY_ADDRESS : addressOf(eo.shipAddress)));
  const [phoneIsCustomers, setPhoneIsCustomers] = useState(true);
  const [orderDiscount, setOrderDiscount] = useState(() => moneyOrEmpty(eo?.orderDiscount));
  const [ship, setShip] = useState(() => (eo === undefined ? "" : moneyInput(eo.shippingFee)));
  const [couponText, setCouponText] = useState(eo?.couponCode ?? "");
  const [couponCode, setCouponCode] = useState(eo?.couponCode ?? "");
  const [sourceId, setSourceId] = useState<string | null>(eo?.trafficSourceId ?? null);
  const [carrierNote, setCarrierNote] = useState(eo?.carrierNote ?? "");
  const [lastEdit, setLastEdit] = useState<EditBody | null>(null);
  const [created, setCreated] = useState<Order | null>(null);
  const [lastBody, setLastBody] = useState<CreateOrderRequest | null>(null);
  const [pendingIntent, setPendingIntent] = useState<Intent | null>(null);
  const [attempt, setAttempt] = useState(0);
  const attemptRef = useRef(0);

  const write = useWriteAction((key, body: CreateOrderRequest) => {
    attemptRef.current += 1;
    setAttempt(attemptRef.current);
    return unwrap(createOrder({ body, headers: { "Idempotency-Key": key } }));
  });
  const editWrite = useWriteAction((key, b: EditBody) => {
    attemptRef.current += 1;
    setAttempt(attemptRef.current);
    return unwrap(updateOrder({ path: { id: b.id }, body: b.req, headers: { "Idempotency-Key": key } }));
  });
  const w = eo === undefined ? write : editWrite;

  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const grants = me.data?.grants ?? [];
  const canApprove = hasPermission(grants, "order.discount.approve");
  const canReadStock = hasPermission(grants, "inventory.read");
  const settings = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "settings"),
    queryFn: () => unwrap(getOrderSettings()),
  });
  const sources = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "traffic-sources"),
    queryFn: () => unwrap(listTrafficSources()),
  });
  const activeSources = (sources.data?.items ?? []).filter((s) => s.active).sort((a, b) => a.position - b.position);
  const defaultSource = activeSources.find((s) => s.key === "manual") ?? activeSources[0];
  // Sửa đơn: đơn chưa có nguồn (đơn đổi hàng do máy chủ tạo) thì ô để trống tới khi người dùng chọn, không tự gán
  // nguồn mặc định (sẽ thành một thay đổi không ai yêu cầu).
  const trafficSourceId = sourceId ?? (eo === undefined ? (defaultSource?.id ?? "") : "");

  // Số nhập → số tiền; null là ô sai.
  const qtys = lines.map((l) => parseQty(l.qty));
  const lineDiscounts = lines.map((l) => parseOptMoney(l.discount));
  const orderDiscountN = parseOptMoney(orderDiscount);
  const shipN = parseOptMoney(ship);
  const inputsOk =
    qtys.every((q) => q !== null) &&
    lineDiscounts.every((d) => d !== null) &&
    orderDiscountN !== null &&
    shipN !== null;
  const pricingLines = lines.map((l, i) => ({
    key: i,
    qty: qtys[i] ?? 0,
    unitPrice: l.unitPrice,
    lineDiscount: lineDiscounts[i] ?? 0,
  }));
  const before = tryCompute({
    lines: pricingLines,
    orderDiscount: orderDiscountN ?? 0,
    couponDiscount: 0,
    shippingFee: shipN ?? 0,
  });
  const base = before.totals === null ? null : before.totals.subtotal - before.totals.discountTotal;

  const customerId = customer?.kind === "existing" ? customer.id : null;
  const phone = cleanPhone(address.phone);
  const phoneOrNull = phone.replace(/\D/g, "").length >= 8 ? phone : null;
  // Sửa đơn giữ mã cũ: không kiểm lại (lượt đã dùng cho chính đơn này). Máy chủ giữ số giảm đang lưu khi tạm tính sau
  // giảm không đổi, đổi thì tính lại mã (mã %, mức tối thiểu; edit.go applyCoupon): khi đó màn không trừ số cũ mà báo
  // "tính lại khi lưu", tổng hiện là tổng chưa trừ mã.
  const keepCoupon = eo !== undefined && couponCode === (eo.couponCode ?? "");
  const savedBase = eo === undefined ? null : eo.subtotal - (eo.lineDiscountTotal ?? 0) - (eo.orderDiscount ?? 0);
  const couponRecalc = keepCoupon && couponCode !== "" && base !== null && base !== savedBase;
  const coupon = useCouponCheck({ code: keepCoupon ? "" : couponCode, base, customerId, phone: phoneOrNull });
  const couponDiscount = keepCoupon ? (eo.couponDiscount ?? 0) : coupon.check?.ok === true ? coupon.check.discount : 0;
  const after =
    before.totals === null || couponRecalc
      ? before
      : tryCompute({
          lines: pricingLines,
          orderDiscount: orderDiscountN ?? 0,
          couponDiscount,
          shippingFee: shipN ?? 0,
        });
  const totals = after.totals;
  const itemCount = qtys.reduce<number>((s, q) => s + (q ?? 0), 0);

  const form: FormState = {
    customer:
      customer === null ? null : customer.kind === "new" ? { kind: "new" } : { kind: "existing", id: customer.id },
    lines: lines.map((l, i) => ({
      ...(l.itemId === undefined ? {} : { itemId: l.itemId }),
      variantId: l.variantId,
      qty: qtys[i] ?? 0,
      lineDiscount: lineDiscounts[i] ?? 0,
    })),
    address,
    phoneIsCustomers,
    orderDiscount: orderDiscountN ?? 0,
    // Gửi mã đang hiện trong ô (lưu bằng phím khi chưa rời ô vẫn đúng mã người dùng thấy); máy chủ kiểm lại.
    couponCode: couponText,
    shippingFee: shipN ?? 0,
    trafficSourceId,
    carrierNote,
  };
  const extraBlock = !inputsOk ? TEXT_BAD_MONEY : after.error;
  const block = (intent: Intent) => saveBlock(form, intent) ?? extraBlock;
  // Sửa đơn: đơn đã xác nhận cần địa chỉ đủ như lúc xác nhận; không đổi gì thì không gửi.
  const editReq = eo === undefined || scope === undefined ? null : buildUpdate(form, eo, scope);
  const editBlock =
    eo === undefined
      ? null
      : (block(eo.status === "confirmed" ? "confirmed" : "new") ?? (editReq === null ? TEXT_NO_CHANGE : null));

  const variantIds = [...new Set(lines.map((l) => l.variantId))].sort();
  const dupKey = customerId ?? (customer?.kind === "new" ? phoneOrNull : null);
  const duplicates = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "duplicate-check", { customerId, phone: phoneOrNull, variantIds }),
    enabled: eo === undefined && dupKey !== null && variantIds.length > 0,
    queryFn: () =>
      unwrap(
        checkOrderDuplicates({
          query: {
            variantIds,
            ...(customerId === null ? {} : { customerId }),
            ...(customerId === null && phoneOrNull !== null ? { phone: phoneOrNull } : {}),
          },
        }),
      ),
  });
  const dupItems = eo === undefined && dupKey !== null && variantIds.length > 0 ? (duplicates.data?.items ?? []) : [];

  const manualBP = totals === null ? 0 : manualDiscountBP(totals);
  const thresholdBP = settings.data?.discountApprovalThresholdBp;
  const overThreshold = me.isSuccess && !canApprove && thresholdBP !== undefined && manualBP > thresholdBP;

  const err = w.error;
  const outOfStock = err instanceof ApiError && err.code === "out_of_stock" ? err : null;
  const errVariant = typeof outOfStock?.details.variantId === "string" ? outOfStock.details.variantId : null;
  // Lỗi coupon_rejected của lần lưu cuối chỉ hiện dưới ô mã tới khi người dùng đổi hay bỏ mã.
  const [dismissedErr, setDismissedErr] = useState<unknown>(null);
  const couponRejected = err instanceof ApiError && err.code === "coupon_rejected" ? err : null;
  const rejectedReason =
    couponRejected === null || dismissedErr === couponRejected
      ? null
      : typeof couponRejected.details.reason === "string"
        ? couponRejected.details.reason
        : undefined;

  const resetForm = () => {
    setCustomer(initial);
    setLines([]);
    setAddress(EMPTY_ADDRESS);
    setPhoneIsCustomers(true);
    setOrderDiscount("");
    setShip("");
    setCouponText("");
    setCouponCode("");
    setSourceId(null);
    setCarrierNote("");
  };

  const send = async (body: CreateOrderRequest, intent: Intent) => {
    attemptRef.current = 0;
    setAttempt(0);
    setPendingIntent(intent);
    setCreated(null);
    setLastBody(body);
    const r = await write.run(body);
    setPendingIntent(null);
    if (!r.ok) return;
    setCreated(r.data);
    setLastBody(null);
    resetForm();
    onCreated?.(r.data);
  };

  const sendEdit = async (b: EditBody) => {
    if (edit === undefined) return;
    attemptRef.current = 0;
    setAttempt(0);
    setPendingIntent("new");
    setLastEdit(b);
    const r = await editWrite.run(b);
    setPendingIntent(null);
    if (!r.ok) return;
    setLastEdit(null);
    edit.onSaved(r.data);
  };

  const save = (intent: Intent) => {
    if (eo !== undefined) {
      if (editWrite.pending || editBlock !== null || editReq === null) return;
      void sendEdit({ id: eo.id, req: editReq });
      return;
    }
    if (write.pending || block(intent) !== null) return;
    void send(buildRequest(form, intent), intent);
  };

  const saveRef = useRef(save);
  useEffect(() => {
    saveRef.current = save;
  });
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (e.key === "F2") {
        e.preventDefault();
        customerRef.current?.focus();
      } else if (e.key === "F3") {
        e.preventDefault();
        productRef.current?.focus();
      } else if (mod && (e.key === "s" || e.key === "S")) {
        e.preventDefault();
        saveRef.current("new");
      } else if (mod && e.key === "Enter") {
        e.preventDefault();
        saveRef.current("confirmed");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  // Enter trong ô số chỉ sang ô kế tiếp, không gửi đơn (HV03).
  const focusNext = (e: KeyboardEvent<HTMLInputElement>) => {
    e.preventDefault();
    const root = rootRef.current;
    if (root === null) return;
    const els = Array.from(
      root.querySelectorAll<HTMLElement>(
        'input:not([disabled]):not([type="checkbox"]):not([type="search"]), select:not([disabled]), textarea',
      ),
    );
    els[els.indexOf(e.currentTarget) + 1]?.focus();
  };
  const onNumberKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && !e.ctrlKey && !e.metaKey) focusNext(e);
  };

  const pickCustomer = (v: CustomerChoice | null, text: string) => {
    setCustomer(v);
    if (v?.kind !== "new" || text === "") return;
    // Khách mới: chữ đã gõ điền sẵn vào người nhận (số → SĐT, chữ → tên) khi ô đó còn trống.
    if (/^[\d\s.+-]+$/.test(text)) {
      if (address.phone === "") setAddress({ ...address, phone: text });
    } else if (address.name === "") setAddress({ ...address, name: text });
  };

  const addLine = (h: VariantSearchHit, available: number | null) => {
    setLines((ls) => {
      const i = ls.findIndex((l) => l.variantId === h.variantId);
      if (i >= 0) {
        return ls.map((l, j) => (j === i ? { ...l, qty: String((parseQty(l.qty) ?? 0) + 1) } : l));
      }
      return [
        ...ls,
        {
          variantId: h.variantId,
          sku: h.sku,
          name: h.productName,
          option: optionText(h),
          unitPrice: h.price,
          available,
          qty: "1",
          discount: "",
        },
      ];
    });
  };
  const setLine = (i: number, patch: Partial<LineDraft>) => {
    setLines((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));
  };

  const blockDraft = block("draft");
  const blockConfirm = block("confirmed");
  const busy = w.pending;
  const btnText = (intent: Intent, label: string) => {
    if (pendingIntent !== intent || !busy) return label;
    const retry = attempt - 1;
    if (retry <= 0) return "Đang lưu…";
    return retry <= MAX_BUSY_SHOWN
      ? `Đang lưu… (gửi lại lần ${String(retry)}/${String(MAX_BUSY_SHOWN)})`
      : `Đang lưu… (gửi lại lần ${String(retry)})`;
  };

  let alert: ReactNode = null;
  if (!busy && err !== null) {
    if (outOfStock !== null) {
      const l = lines.find((x) => x.variantId === errVariant);
      const n = outOfStock.details.available;
      alert = (
        <div className="box box-err">
          <b>Chưa lưu đơn: không đủ hàng.</b>{" "}
          {l !== undefined && typeof n === "number"
            ? `${l.sku} chỉ còn ${String(n)} có thể bán (người khác vừa lấy). `
            : ""}
          Giảm số lượng hoặc bỏ dòng rồi lưu lại; các dòng khác chưa bị giữ.
        </div>
      );
    } else if (couponRejected !== null) {
      alert = <div className="box box-err">Chưa lưu đơn: bỏ mã hoặc đổi mã giảm giá rồi lưu lại.</div>;
    } else if (err instanceof ApiError && err.status === 403 && err.code === "forbidden" && edit === undefined) {
      // Task 411: lên đơn bị chặn khi kho giữ hàng (bỏ trống là kho chính) ngoài phạm vi order.create của người gọi.
      alert = (
        <div className="box box-err">
          Chưa lưu đơn: bạn chưa được giao kho giữ hàng của đơn. Nhờ chủ shop gán quyền lên đơn ở kho này hoặc toàn
          shop.
        </div>
      );
    } else if (err instanceof ApiError && err.code === "version_conflict" && edit !== undefined) {
      alert = (
        <div className="box box-err">
          <b>{TEXT_VERSION_CONFLICT}</b> Thay đổi của bạn chưa được lưu và vẫn còn trong ô; chép lại sau khi tải nếu
          cần.
          <div className="row" style={{ marginTop: 8 }}>
            <button type="button" className="btn btn-dark" onClick={edit.onReload}>
              Tải lại đơn
            </button>
          </div>
        </div>
      );
    } else if (outcomeUnknown(err) && lastEdit !== null) {
      const b = lastEdit;
      alert = (
        <div className="box box-err">
          <b>{`${w.message ?? ""}.`.replace(/\.\.$/, ".")}</b> Chưa chắc đã lưu. Bấm "Gửi lại" sẽ gửi đúng thay đổi cũ.
          <div className="row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-dark"
              onClick={() => {
                void sendEdit(b);
              }}
            >
              Gửi lại
            </button>
          </div>
        </div>
      );
    } else if (outcomeUnknown(err) && lastBody !== null) {
      const body = lastBody;
      alert = (
        <div className="box box-err">
          <b>{`${write.message ?? ""}.`.replace(/\.\.$/, ".")}</b> Đơn chưa chắc đã lưu. Bấm "Gửi lại" sẽ gửi đúng đơn
          cũ: nếu máy chủ đã lưu thì nhận lại đơn đó, không tạo đơn mới.
          <div className="row" style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn btn-dark"
              onClick={() => {
                void send(body, body.intent ?? "confirmed");
              }}
            >
              Gửi lại
            </button>
          </div>
        </div>
      );
    } else {
      alert = <div className="box box-err">{w.message}</div>;
    }
  }

  return (
    <div className="ld cols" ref={rootRef}>
      <div className="stack">
        {eo === undefined ? (
          <CustomerPicker value={customer} onChange={pickCustomer} inputRef={customerRef} />
        ) : (
          <section className="card" aria-labelledby={`${id}-kh`}>
            <h2 id={`${id}-kh`}>Khách</h2>
            <p className="bold clamp2">{eo.customer?.fullName ?? ""}</p>
            <p className="small muted">{eo.customer?.code ?? ""} · Khách của đơn không đổi khi sửa.</p>
          </section>
        )}
        <fieldset className="lock" disabled={!can("address")}>
          <AddressFields
            value={address}
            onChange={setAddress}
            customer={customer}
            phoneIsCustomers={phoneIsCustomers}
            onPhoneIsCustomers={setPhoneIsCustomers}
          />
          {!can("address") && <p className="reason">{TEXT_LOCKED}</p>}
        </fieldset>
        <fieldset className="lock" disabled={!can("lines")}>
          <section className="card" aria-labelledby={`${id}-sp`}>
            <div className="row-between">
              <h2 id={`${id}-sp`}>Sản phẩm</h2>
              <span className="small muted">
                {lines.length === 0 ? "0 dòng" : `${String(lines.length)} dòng · ${String(itemCount)} món`}
              </span>
            </div>
            <VariantPicker onPick={addLine} inputRef={productRef} canReadStock={canReadStock} />
            {lines.length === 0 ? (
              <div className="empty" style={{ marginTop: 12 }}>
                <p className="muted">Chưa có sản phẩm. Gõ mã hoặc tên ở ô trên, hoặc quét barcode.</p>
              </div>
            ) : (
              <div className="tbl-frame" style={{ marginTop: 12 }}>
                <table className="tbl ltbl">
                  <thead>
                    <tr>
                      <th className="c-sp">Sản phẩm</th>
                      <th className="c-gia r">Đơn giá</th>
                      <th className="c-sl">Số lượng</th>
                      <th className="c-giam">Giảm dòng</th>
                      <th className="c-tt r">Thành tiền</th>
                      <th className="c-x">
                        <span className="sr">Bỏ dòng</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {lines.map((l, i) => {
                      const q = qtys[i] ?? null;
                      const d = lineDiscounts[i] ?? null;
                      const lt = totals?.lines[i];
                      const isErr = l.variantId === errVariant;
                      const avail = isErr
                        ? errorMessage(err)
                        : l.available === null
                          ? null
                          : `Có thể bán: ${String(l.available)}`;
                      return (
                        <tr key={l.variantId} className={isErr ? "is-err" : undefined}>
                          <td className="c-sp">
                            <div className="sp">
                              <span className="code">{[l.sku, l.option].filter(Boolean).join(" · ")}</span>
                              <span className="nm clamp2">{l.name}</span>
                              {avail !== null && (
                                <span
                                  className={
                                    isErr || (l.available !== null && l.available <= 2) ? "avail is-low" : "avail"
                                  }
                                >
                                  {avail}
                                </span>
                              )}
                            </div>
                          </td>
                          <td data-label="Đơn giá" className="r">
                            <span className="money">{formatMoney(l.unitPrice)}</span>
                          </td>
                          <td data-label="Số lượng">
                            <span className="stepper">
                              <button
                                type="button"
                                className="ibtn"
                                aria-label={`Bớt 1 ${l.sku}`}
                                disabled={q === null || q <= 1}
                                onClick={() => {
                                  setLine(i, { qty: String((q ?? 2) - 1) });
                                }}
                              >
                                −
                              </button>
                              <input
                                className={q === null ? "input is-err" : "input"}
                                inputMode="numeric"
                                autoComplete="off"
                                aria-label={`Số lượng ${l.sku}`}
                                aria-invalid={q === null}
                                value={l.qty}
                                onChange={(e) => {
                                  setLine(i, { qty: e.target.value });
                                }}
                                onKeyDown={onNumberKey}
                              />
                              <button
                                type="button"
                                className="ibtn"
                                aria-label={`Thêm 1 ${l.sku}`}
                                disabled={q === null || q >= MAX_QTY}
                                onClick={() => {
                                  setLine(i, { qty: String((q ?? 0) + 1) });
                                }}
                              >
                                +
                              </button>
                            </span>
                          </td>
                          <td data-label="Giảm dòng">
                            <input
                              className={d === null ? "input num is-err" : "input num"}
                              inputMode="numeric"
                              autoComplete="off"
                              placeholder="0"
                              aria-label={`Giảm dòng ${l.sku} (đồng)`}
                              aria-invalid={d === null}
                              disabled={!can("discounts")}
                              value={l.discount}
                              onChange={(e) => {
                                setLine(i, { discount: e.target.value });
                              }}
                              onBlur={() => {
                                if (d !== null && d > 0) setLine(i, { discount: moneyInput(d) });
                              }}
                              onKeyDown={onNumberKey}
                            />
                          </td>
                          <td data-label="Thành tiền" className="r">
                            <span className="money">{lt === undefined ? "—" : formatMoney(lt.base)}</span>
                          </td>
                          <td className="c-act">
                            <button
                              type="button"
                              className="ibtn"
                              aria-label={`Bỏ dòng ${l.sku}`}
                              onClick={() => {
                                setLines((ls) => ls.filter((_, j) => j !== i));
                              }}
                            >
                              ×
                            </button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
            <p className="hint" style={{ marginTop: 8 }}>
              {eo === undefined
                ? 'Đơn giá lấy giá bán hiện hành, không sửa tay; muốn bớt thì giảm dòng. "Có thể bán" của kho mặc định, đỏ khi còn 1–2. Enter trong ô số lượng chỉ sang ô kế tiếp, không gửi đơn.'
                : "Dòng đang có giữ giá lúc đặt; dòng thêm mới lấy giá bán hiện hành. Enter trong ô số lượng chỉ sang ô kế tiếp."}
            </p>
            {!can("lines") && <p className="reason">{TEXT_LOCKED}</p>}
          </section>
        </fieldset>
      </div>

      <div className="stack">
        <fieldset className="lock" disabled={!can("discounts") && !can("shipping") && !can("coupon")}>
          <TotalsPanel
            totals={totals}
            itemCount={itemCount}
            orderDiscount={orderDiscount}
            onOrderDiscount={setOrderDiscount}
            orderDiscountInvalid={orderDiscountN === null}
            shippingFee={ship}
            onShippingFee={setShip}
            shippingFeeInvalid={shipN === null}
            onEnter={focusNext}
            error={inputsOk ? after.error : null}
            couponRecalc={couponRecalc ? couponCode : null}
            coupon={
              <CouponField
                text={couponText}
                onText={(s) => {
                  setCouponText(s);
                  setDismissedErr(couponRejected);
                }}
                onCommit={() => {
                  setCouponCode(couponText.trim().toUpperCase());
                }}
                onClear={() => {
                  setCouponText("");
                  setCouponCode("");
                  setDismissedErr(couponRejected);
                }}
                check={coupon.check}
                failed={coupon.failed}
                rejectedReason={rejectedReason}
              />
            }
          />
        </fieldset>
        <section className="card" aria-labelledby={`${id}-tt`}>
          <h2 id={`${id}-tt`}>Thông tin đơn</h2>
          <div className="stack">
            <label className="field">
              <span>Nguồn đơn *</span>
              <span className="select-wrap">
                <select
                  disabled={!can("trafficSource")}
                  value={trafficSourceId}
                  onChange={(e) => {
                    setSourceId(e.target.value);
                  }}
                >
                  {eo !== undefined && trafficSourceId === "" && <option value="">Chưa có</option>}
                  {activeSources.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </span>
            </label>
            <label className="field">
              <span>Ghi chú cho hãng vận chuyển</span>
              <textarea
                className="input"
                style={{ minHeight: 72 }}
                maxLength={500}
                disabled={!can("carrierNote")}
                value={carrierNote}
                onChange={(e) => {
                  setCarrierNote(e.target.value);
                }}
              />
              <span className="hint">
                {eo === undefined
                  ? "In lên phiếu gửi hãng. Ghi chú nội bộ viết ở trang đơn sau khi lưu."
                  : "In lên phiếu gửi hãng. Ghi chú nội bộ thêm ở trang đơn."}
              </span>
            </label>
          </div>
        </section>

        {dupItems.length > 0 && (
          <div className="box box-soft">
            <b>Có thể trùng đơn.</b> {customerId === null ? "SĐT này" : "Khách này"} có {dupItems.length} đơn
            {settings.data === undefined
              ? " gần đây"
              : ` trong ${percent.format(settings.data.duplicateWindowMinutes / 60)} giờ qua`}{" "}
            có cùng sản phẩm:{" "}
            {dupItems.map((o, i) => (
              <span key={o.id}>
                {i > 0 && ", "}
                <Link className="cell-a" to={`/don/${o.id}`}>
                  {o.code}
                </Link>{" "}
                ({shortTime(o.createdAt)}, <span className="money">{formatMoney(o.grandTotal)}</span>)
              </span>
            ))}
            . Vẫn lưu được; đơn sẽ có dấu "Nghi trùng".
          </div>
        )}
        {overThreshold && (
          <div className="box box-soft">
            <b>{`Giảm tay ${bpText(manualBP)} tạm tính, vượt mức ${bpText(thresholdBP)}.`}</b> Lưu được, nhưng đơn sẽ
            "Chờ duyệt giảm giá" và chưa xác nhận cho tới khi Chủ hoặc Quản lý duyệt. Mã giảm giá không tính vào mức
            này.
          </div>
        )}
        {alert}
        {created !== null && (
          <div className="box box-ok">
            <b className="ok-text">{`Đã tạo đơn ${created.code}.`}</b>{" "}
            <span>
              {created.pendingReason === undefined
                ? (STATUS_TEXT[created.status] ?? "")
                : PENDING_TEXT[created.pendingReason]}
            </span>{" "}
            {(created.warnings ?? []).map((w) => {
              const t = warningText(w);
              return t === null ? null : <span key={`${w.code}-${w.refId ?? ""}`}>{t} </span>;
            })}
            <Link className="cell-a" to={`/don/${created.id}`}>
              Mở đơn
            </Link>
            {(created.warnings ?? []).some((w) => w.code === "possible_duplicate") && (
              <>
                {" · "}
                <Link className="cell-a" to="/don/trung">
                  Xem đơn trùng
                </Link>
              </>
            )}
          </div>
        )}

        {eo !== undefined ? (
          <div className="card stack-sm">
            <button
              type="button"
              className="btn btn-dark btn-wide"
              disabled={busy || editBlock !== null}
              onClick={() => {
                save("new");
              }}
            >
              {btnText("new", "Lưu thay đổi")}
            </button>
            {busy && attempt > 1 ? (
              <p className="reason">Mạng chập chờn. Đang gửi lại đúng thay đổi này, không lưu hai lần.</p>
            ) : (
              editBlock !== null && <p className="reason">{editBlock}</p>
            )}
            <p className="hint">
              Ctrl + S hoặc Ctrl + Enter: lưu thay đổi. Giảm tay vượt mức duyệt thì đơn mới, đơn nháp chuyển "Chờ duyệt
              giảm giá"; đơn đã xác nhận phải nhờ người có quyền duyệt.
            </p>
          </div>
        ) : (
          <div className="card stack-sm">
            <button
              type="button"
              className="btn btn-dark btn-wide"
              disabled={busy || blockConfirm !== null}
              onClick={() => {
                save("confirmed");
              }}
            >
              {btnText("confirmed", "Lưu và xác nhận")}
            </button>
            <div className="row">
              <button
                type="button"
                className="btn btn-line grow"
                disabled={busy || block("new") !== null}
                onClick={() => {
                  save("new");
                }}
              >
                {btnText("new", "Lưu (mới)")}
              </button>
              <button
                type="button"
                className="btn btn-line grow"
                disabled={busy || blockDraft !== null}
                onClick={() => {
                  save("draft");
                }}
              >
                {btnText("draft", "Lưu nháp")}
              </button>
            </div>
            {busy && attempt > 1 ? (
              <p className="reason">
                Mạng chập chờn. Đang gửi lại đúng đơn này; dù bấm lại cũng không tạo đơn thứ hai.
              </p>
            ) : (
              (blockDraft ?? blockConfirm) !== null && <p className="reason">{blockDraft ?? blockConfirm}</p>
            )}
            <p className="hint">
              {`Lưu và xác nhận: giữ hàng, đơn "Đã xác nhận". Lưu (mới): giữ hàng, chờ xác nhận sau. Lưu nháp: giữ hàng ${
                settings.data === undefined
                  ? "tới khi hết hạn nháp"
                  : `${percent.format(settings.data.draftTtlMinutes / 60)} giờ`
              } khi còn thiếu địa chỉ; quá hạn tự hủy và nhả hàng.`}
            </p>
          </div>
        )}
        <div className="card keys-desk">
          <h2>Phím tắt</h2>
          <div className="keys" style={{ marginTop: 12 }}>
            <span>
              <span className="kbd">F2</span>
            </span>
            <span>Tìm khách</span>
            <span>
              <span className="kbd">F3</span>
            </span>
            <span>Thêm dòng (ô tìm sản phẩm)</span>
            <span>
              <span className="kbd">Tab</span>
            </span>
            <span>Sang ô kế tiếp; Shift + Tab lùi lại</span>
            <span>
              <span className="kbd">Enter</span>
            </span>
            <span>Chọn gợi ý đang sáng; trong ô số chỉ sang ô kế</span>
            <span>
              <span className="kbd">Ctrl</span> <span className="kbd">S</span>
            </span>
            <span>Lưu (mới)</span>
            <span>
              <span className="kbd">Ctrl</span> <span className="kbd">Enter</span>
            </span>
            <span>Lưu và xác nhận</span>
            <span>
              <span className="kbd">Esc</span>
            </span>
            <span>Đóng danh sách gợi ý</span>
          </div>
          <p className="hint" style={{ marginTop: 8 }}>
            Trên Mac: ⌘ thay Ctrl. Không phím nào gửi đơn ngoài hai phím lưu.
          </p>
        </div>
      </div>
    </div>
  );
}
