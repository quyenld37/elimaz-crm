// Chi tiết đơn /don/:id (kế hoạch M2 Task M2-28; mockup docs/mockups/m2/don-chi-tiet.html trạng thái A–F; HV16–HV24).
// Nút hiện theo trạng thái đơn, editScope và quyền trong /api/v1/me; máy chủ vẫn chặn. Nút áp dụng nhưng đang bị chặn
// hiện mờ kèm lý do bằng chữ. Đổi trạng thái và sửa gửi version đã đọc; 409 version_conflict → câu báo và "Tải lại
// đơn", không tự ghi đè. Đơn, lịch sử, phiếu trả chỉ tải lại khi báo hiệu order.changed đúng đơn này.
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Link, useParams } from "react-router";
import {
  addOrderNote,
  approveOrderDiscount,
  confirmOrder,
  getOrderTimeline,
  getReturn,
  listStaff,
  setOrderPending,
  updateOrder,
  type Order,
  type Return,
  type TimelineItem,
  type UpdateOrderRequest,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { displayPhone } from "../len-don/orderRules";
import { manualDiscountBP } from "../len-don/pricing";
import { formatMoney } from "../products/productRules";
import { CancelDialog } from "./CancelDialog";
import { EditOrderDialog } from "./EditOrderDialog";
import { FIELD_TEXT, LABEL_TEXT, PENDING_REASON_TEXT, STATUS_TEXT, historyReasonText, labelChip } from "./labels";
import {
  ORDER_TOPIC,
  entityMeta,
  reloadOrder,
  timelineKey,
  useOrder,
  useOrderSettings,
  usePerms,
  useSources,
} from "./orderData";
import { ReturnDialog } from "./ReturnDialog";
import { ReturnsCard } from "./ReturnsCard";
import { DeliveryStatsBadge } from "../len-don/DeliveryStatsBadge";
import { OrderRefundsCard } from "../payments/OrderRefundsCard";
import { PaymentsBox } from "../payments/PaymentsBox";
import { ShippingPanel } from "../shipping/ShippingPanel";
import { OrderShipmentCards } from "../shipping/Timeline";
import "../len-don/len-don.css";
import "./don.css";

export const TEXT_CONFLICT = "Đơn đã thay đổi, tải lại để xem bản mới.";

type Act =
  | { kind: "approve" | "confirm"; id: string; version: number }
  | { kind: "pending"; id: string; version: number; reason?: "awaiting_customer" }
  | { kind: "update"; id: string; req: UpdateOrderRequest };

function sendAct(key: string, a: Act): Promise<Order> {
  const headers = { "Idempotency-Key": key };
  const path = { id: a.id };
  switch (a.kind) {
    case "approve":
      return unwrap(approveOrderDiscount({ path, body: { version: a.version }, headers }));
    case "confirm":
      return unwrap(confirmOrder({ path, body: { version: a.version }, headers }));
    case "pending":
      return unwrap(
        setOrderPending({
          path,
          body: { version: a.version, ...(a.reason === undefined ? {} : { reason: a.reason }) },
          headers,
        }),
      );
    case "update":
      return unwrap(updateOrder({ path, body: a.req, headers }));
  }
}

const timeText = (iso: string) => shortTime(iso).replace(" ", " · ");
const percent = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 2 });
const bpText = (bp: number) => `${percent.format(bp / 100)}%`;
const minus = (v: number) => (v === 0 ? formatMoney(0) : `−${formatMoney(v)}`);
const FORM_SCOPE = ["lines", "discounts", "coupon", "address", "shipping", "carrierNote"] as const;

// Chữ của một khóa trong bảng; khóa lạ (chuỗi mở của máy chủ) → "".
const textOf = (m: Readonly<Record<string, string>>, k: string | undefined) =>
  k !== undefined && Object.hasOwn(m, k) ? (m[k] ?? "") : "";

function statusTitle(s: Readonly<Record<string, string>>): string {
  const to = textOf(STATUS_TEXT, s.to);
  const pending = textOf(PENDING_REASON_TEXT, s.toPending);
  if (s.from === undefined) return pending === "" ? `Tạo đơn → ${to}` : `Tạo đơn → ${to} · ${pending}`;
  if (s.from === s.to) return pending === "" ? "Bỏ lý do chờ" : `Chờ: ${pending}`;
  const from = textOf(STATUS_TEXT, s.from);
  return pending === "" ? `${from} → ${to}` : `${from} → ${to} · ${pending}`;
}

function TimelineRow({ t }: { t: TimelineItem }) {
  const s = t.summary ?? {};
  let title: string;
  let detail: string | null = null;
  if (t.kind === "status") {
    title = statusTitle(s);
    detail = historyReasonText(s.reason);
  } else if (t.kind === "edit") {
    title = "Sửa đơn";
    const fields = Object.keys(FIELD_TEXT).filter((k) => s[k] !== undefined);
    detail = fields.length === 0 ? null : fields.map((k) => `${FIELD_TEXT[k] ?? k}: đã đổi`).join(" · ");
  } else if (t.kind === "note") {
    title = "Ghi chú nội bộ";
  } else {
    return null;
  }
  return (
    <li>
      <span className="at">{timeText(t.at)}</span>
      <div>
        <b>{title}</b>
        {t.actor !== undefined && (
          <>
            {" · "}
            <span className="who">{t.actor.name}</span>
          </>
        )}
        {detail !== null && (
          <>
            <br />
            <span className="small">{detail}</span>
          </>
        )}
      </div>
    </li>
  );
}

function ActSlot({ children, reason }: { children: ReactNode; reason?: string | null }) {
  return (
    <div className="act">
      {children}
      {reason !== undefined && reason !== null && <span className="reason">{reason}</span>}
    </div>
  );
}

export function OrderDetail() {
  const { id = "" } = useParams();
  const order = useOrder(id);
  if (order.isError) {
    return (
      <div>
        <p className="crumb">
          <Link to="/don">Đơn hàng</Link>
        </p>
        <div className="box box-err">{errorMessage(order.error)}</div>
      </div>
    );
  }
  if (order.data === undefined) return <p className="muted">Đang tải đơn…</p>;
  return <OrderView o={order.data} refetch={() => void order.refetch()} />;
}

/** Mọi phiếu trả của đơn; items null khi còn phiếu chưa đọc xong; error là lỗi đọc đầu tiên (nếu có). */
function useReturns(o: Order): { items: readonly Return[] | null; error: unknown } {
  return useQueries({
    queries: (o.returns ?? []).map((r) => ({
      queryKey: queryKey(ORDER_TOPIC, "return", r.id),
      meta: entityMeta(o.id),
      queryFn: () => unwrap(getReturn({ path: { returnId: r.id } })),
    })),
    // Chỉ có items khi đủ mọi phiếu (tiền hoàn dự kiến cần đủ phiếu đang mở); thiếu một phiếu → null.
    combine: (rs) => {
      const error = rs.find((r) => r.isError)?.error ?? null;
      const out: Return[] = [];
      for (const r of rs) {
        if (r.data === undefined) return { items: null, error };
        out.push(r.data);
      }
      return { items: out, error };
    },
  });
}

function OrderView({ o, refetch }: { o: Order; refetch: () => void }) {
  const qc = useQueryClient();
  const perms = usePerms();
  const { can } = perms;
  const sources = useSources();
  const settings = useOrderSettings();
  const timeline = useQuery({
    queryKey: timelineKey(o.id),
    meta: entityMeta(o.id),
    queryFn: () => unwrap(getOrderTimeline({ path: { id: o.id } })),
  });
  const { items: returns, error: returnsError } = useReturns(o);
  const canStaff = can("identity.users.manage");
  const staff = useQuery({
    queryKey: queryKey("identity.staff"),
    enabled: canStaff,
    queryFn: () => unwrap(listStaff()),
  });
  const [dialog, setDialog] = useState<"edit" | "cancel" | "return" | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [noteText, setNoteText] = useState("");

  const act = useWriteAction(sendAct);
  const note = useWriteAction((key, b: { id: string; body: string }) =>
    unwrap(addOrderNote({ path: { id: b.id }, body: { body: b.body }, headers: { "Idempotency-Key": key } })),
  );
  const run = async (a: Act) => {
    setNotice(null);
    const r = await act.run(a);
    if (r.ok) reloadOrder(qc, o.id);
  };
  const version = o.version ?? 0;

  const st = o.status;
  const pr = o.pendingReason;
  const scope = o.editScope;
  const label = o.label ?? "new";
  const open = st === "draft" || st === "new";
  const waitsApproval = pr === "discount_approval";

  const buttons: ReactNode[] = [];
  if (open && waitsApproval && can("order.discount.approve")) {
    buttons.push(
      <ActSlot key="approve">
        <button
          type="button"
          className="btn btn-dark"
          disabled={act.pending}
          onClick={() => void run({ kind: "approve", id: o.id, version })}
        >
          {st === "new" ? "Duyệt giảm giá và xác nhận" : "Duyệt giảm giá"}
        </button>
      </ActSlot>,
    );
  }
  const canEdit = can("order.update") && scope !== undefined && FORM_SCOPE.some((k) => scope[k]);
  if (canEdit) {
    buttons.push(
      <ActSlot key="edit">
        <button
          type="button"
          className="btn btn-line"
          onClick={() => {
            setDialog("edit");
          }}
        >
          Sửa đơn
        </button>
      </ActSlot>,
    );
  }
  if (open && can("order.confirm")) {
    const why = waitsApproval
      ? "Cần duyệt giảm giá trước"
      : st === "draft" && o.addressComplete !== true
        ? "Cần đủ địa chỉ giao (tỉnh, xã/phường) trước"
        : null;
    buttons.push(
      <ActSlot key="confirm" reason={why}>
        <button
          type="button"
          className={buttons.length === 0 ? "btn btn-dark" : "btn btn-line"}
          disabled={why !== null || act.pending}
          onClick={() => void run({ kind: "confirm", id: o.id, version })}
        >
          Xác nhận
        </button>
      </ActSlot>,
    );
  }
  if (st === "new" && can("order.confirm")) {
    const waiting = pr === "awaiting_customer" || pr === "out_of_stock";
    buttons.push(
      <ActSlot key="pending" reason={waitsApproval ? "Đơn đang chờ duyệt giảm giá" : null}>
        <button
          type="button"
          className="btn btn-line"
          disabled={waitsApproval || act.pending}
          onClick={() =>
            void run(
              waiting
                ? { kind: "pending", id: o.id, version }
                : { kind: "pending", id: o.id, version, reason: "awaiting_customer" },
            )
          }
        >
          {waiting ? "Bỏ chờ" : "Chờ khách"}
        </button>
      </ActSlot>,
    );
  }
  if ((open || st === "confirmed") && can("order.cancel")) {
    buttons.push(
      <ActSlot key="cancel">
        <button
          type="button"
          className="btn btn-danger"
          onClick={() => {
            setDialog("cancel");
          }}
        >
          Hủy đơn
        </button>
      </ActSlot>,
    );
  }
  if (st === "delivered" && can("order.return")) {
    buttons.push(
      <ActSlot
        key="return"
        reason={returns !== null ? null : returnsError === null ? "Đang tải phiếu trả" : "Chưa đọc được phiếu trả"}
      >
        <button
          type="button"
          className="btn btn-dark"
          disabled={returns === null}
          onClick={() => {
            setDialog("return");
          }}
        >
          Tạo phiếu trả/đổi
        </button>
      </ActSlot>,
    );
  }

  const conflict = act.error instanceof ApiError && act.error.code === "version_conflict";
  const t = { subtotal: o.subtotal, lineDiscountTotal: o.lineDiscountTotal ?? 0, orderDiscount: o.orderDiscount ?? 0 };
  const bp = manualDiscountBP(t);
  const notes = (timeline.data?.items ?? []).filter((x) => x.kind === "note");
  const items = o.items;
  const qtyTotal = items.reduce((s, i) => s + i.qty, 0);
  const a = o.shipAddress;
  const where =
    a === undefined ? "" : [a.line, a.wardName, a.provinceName].filter((x) => x !== undefined && x !== "").join(", ");
  const stats = o.customer?.stats;
  const canCare = can("order.update") && scope?.careBy === true;
  const canSource = can("order.update") && scope?.trafficSource === true;
  const careOptions = [
    ...(o.careBy === undefined ? [] : [{ id: o.careBy.id, name: o.careBy.name }]),
    ...(perms.userId !== "" && perms.userId !== o.careBy?.id
      ? [{ id: perms.userId, name: "Tôi (người đang xem)" }]
      : []),
    ...(staff.data ?? [])
      .filter((u) => u.status === "active" && u.id !== o.careBy?.id && u.id !== perms.userId)
      .map((u) => ({ id: u.id, name: u.displayName })),
  ];
  const sourceOptions = sources.items.filter((s) => s.active || s.id === o.trafficSourceId);
  const channel = o.channel === "manual" ? "Nhập tay" : (o.channel ?? "");
  const sub = [
    `Tạo ${timeText(o.createdAt)}${o.createdBy === undefined ? "" : ` bởi ${o.createdBy.name}`}`,
    o.trafficSourceId === undefined ? "" : `Nguồn ${sources.nameOf(o.trafficSourceId)}`,
    channel,
  ].filter((x) => x !== "" && x !== "Nguồn ");

  return (
    <div>
      <p className="crumb">
        <Link to="/don">Đơn hàng</Link> / {o.code}
      </p>
      <div className="page-h">
        <div className="grow">
          <div className="row">
            <h1 className="num">{o.code}</h1>
            <span className={labelChip(label)}>{LABEL_TEXT[label]}</span>
            {o.duplicateSuspect === true && (
              <Link className="chip chip-err" to="/don/trung">
                Nghi trùng
              </Link>
            )}
          </div>
          <p className="sub">{sub.join(" · ")}</p>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 24 }}>
        <div className="acts" role="group" aria-label="Thao tác đơn" data-ready={perms.loaded ? "true" : "false"}>
          {buttons}
        </div>
        {perms.loaded && buttons.length === 0 && (
          <p className="muted">Không còn thao tác nào cho đơn ở trạng thái này với quyền của bạn.</p>
        )}
        {waitsApproval && perms.loaded && !can("order.discount.approve") && (
          <p className="reason" style={{ marginTop: 8 }}>
            Đang chờ Chủ hoặc Quản lý duyệt giảm giá
          </p>
        )}
        <p className="hint" style={{ marginTop: 8 }}>
          Nút hiện theo trạng thái đơn và quyền của người xem; máy chủ vẫn kiểm lại.
        </p>
        {conflict ? (
          <div className="box box-err" style={{ marginTop: 8 }}>
            <b>{TEXT_CONFLICT}</b> Thao tác chưa được lưu.
            <div className="row" style={{ marginTop: 8 }}>
              <button
                type="button"
                className="btn btn-dark"
                onClick={() => {
                  act.reset();
                  refetch();
                }}
              >
                Tải lại đơn
              </button>
            </div>
          </div>
        ) : (
          act.message !== null && (
            <div className="box box-err" style={{ marginTop: 8 }}>
              {act.message}
            </div>
          )
        )}
        {notice !== null && (
          <div className="box box-ok" style={{ marginTop: 8 }}>
            {notice}
          </div>
        )}
      </div>

      <div className="cols">
        <div className="stack">
          <section className="card" aria-label="Sản phẩm">
            <div className="row-between">
              <h2>Sản phẩm</h2>
              <span className="small muted">{`${String(items.length)} dòng · ${String(qtyTotal)} món`}</span>
            </div>
            <div className="tbl-frame">
              <table className="tbl ltbl">
                <thead>
                  <tr>
                    <th className="c-sp">Sản phẩm</th>
                    <th className="c-gia r">Đơn giá</th>
                    <th className="c-sl r">SL</th>
                    <th className="c-giam r">Giảm dòng</th>
                    <th className="c-tt r">Thành tiền</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((it) => (
                    <tr key={it.id}>
                      <td className="c-sp">
                        <div className="sp">
                          <span className="code">{it.sku}</span>
                          <span className="nm clamp2">{it.name}</span>
                        </div>
                      </td>
                      <td data-label="Đơn giá" className="r">
                        <span className="money">{formatMoney(it.unitPrice)}</span>
                      </td>
                      <td data-label="SL" className="r num">
                        {it.qty}
                      </td>
                      <td data-label="Giảm dòng" className="r">
                        <span className="money">{formatMoney(it.lineDiscount ?? 0)}</span>
                      </td>
                      <td data-label="Thành tiền" className="r">
                        <span className="money">{formatMoney(it.netAmount + (it.allocatedOrderDiscount ?? 0))}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="sum" style={{ marginTop: 12, maxWidth: 420, marginLeft: "auto" }}>
              <div className="sum-r">
                <span>Tạm tính</span>
                <span className="money">{formatMoney(o.subtotal)}</span>
              </div>
              <div className="sum-r">
                <span>Giảm trên dòng</span>
                <span className="money">{minus(o.lineDiscountTotal ?? 0)}</span>
              </div>
              <div className="sum-r">
                <span>Giảm cấp đơn</span>
                <span className="money">{minus(o.orderDiscount ?? 0)}</span>
              </div>
              <div className="sum-r">
                <span>Mã giảm giá</span>
                {o.couponCode === undefined || o.couponCode === "" ? (
                  <span className="small muted">Không dùng</span>
                ) : (
                  <span className="money">{`${o.couponCode} ${minus(o.couponDiscount ?? 0)}`}</span>
                )}
              </div>
              <div className="sum-r">
                <span>Phí ship báo khách</span>
                <span className="money">{formatMoney(o.shippingFee)}</span>
              </div>
              <div className="sum-r is-total">
                <span>Tổng</span>
                <span className="money">{formatMoney(o.grandTotal)}</span>
              </div>
              <div className="sum-r">
                <span>Đã trả trước</span>
                <span className="money">{formatMoney(o.prepaidTotal ?? 0)}</span>
              </div>
              <div className="sum-r">
                <span>Thu hộ (COD)</span>
                <span className="money">{formatMoney(o.codAmount)}</span>
              </div>
            </div>
            <p className="hint">
              {waitsApproval && settings.data !== undefined
                ? `Giảm tay ${formatMoney(t.lineDiscountTotal + t.orderDiscount)} = ${bpText(bp)} tạm tính, vượt mức ${bpText(settings.data.discountApprovalThresholdBp)} của shop. `
                : ""}
              Mỗi dòng giữ giá lúc đặt kể cả khi giá sản phẩm đổi sau.
            </p>
          </section>

          {(st === "delivered" || (o.returns ?? []).length > 0) && (
            <ReturnsCard order={o} returns={returns} error={returnsError} can={can} />
          )}
          <OrderShipmentCards order={o} />
          <section className="card">
            <div className="row-between">
              <h2>Lịch sử thao tác</h2>
              <span className="small muted">Ai làm, lúc nào, đổi gì</span>
            </div>
            {timeline.isError ? (
              <div className="box box-err">{errorMessage(timeline.error)}</div>
            ) : (
              <ul className="tl" aria-label="Lịch sử thao tác">
                {(timeline.data?.items ?? []).map((x) => (
                  <TimelineRow key={`${x.kind}-${x.refId}`} t={x} />
                ))}
              </ul>
            )}
            <p className="hint">Chỉ ghi tên trường đã đổi, không ghi giá trị cũ của địa chỉ, SĐT.</p>
          </section>

          <section className="card">
            <h2>Ghi chú nội bộ</h2>
            <div className="stack-sm">
              {notes.length === 0 && <p className="small muted">Chưa có ghi chú.</p>}
              {notes.map((n) => (
                <div key={n.refId} className="note-box">
                  <p className="small muted">{`${n.actor?.name ?? ""} · ${timeText(n.at)}`}</p>
                  <p>{n.note ?? ""}</p>
                </div>
              ))}
              {can("order.update") && (
                <>
                  <label className="field">
                    <span className="sr">Thêm ghi chú</span>
                    <textarea
                      className="input"
                      style={{ minHeight: 72 }}
                      maxLength={2000}
                      placeholder="Ghi chú cho người trong shop, không gửi hãng vận chuyển"
                      value={noteText}
                      onChange={(e) => {
                        setNoteText(e.target.value);
                      }}
                    />
                  </label>
                  {note.message !== null && <div className="box box-err">{note.message}</div>}
                  <div className="row" style={{ justifyContent: "flex-end" }}>
                    <button
                      type="button"
                      className="btn btn-line"
                      disabled={noteText.trim() === "" || note.pending}
                      onClick={() => {
                        void note.run({ id: o.id, body: noteText.trim() }).then((r) => {
                          if (!r.ok) return;
                          setNoteText("");
                          void qc.invalidateQueries({ queryKey: timelineKey(o.id) });
                        });
                      }}
                    >
                      Thêm ghi chú
                    </button>
                  </div>
                </>
              )}
            </div>
          </section>
        </div>

        <div className="stack">
          <section className="card">
            <div className="row-between">
              <h2>Khách</h2>
              {o.customer !== undefined && (
                <Link className="btn btn-quiet" to={`/khach/${o.customer.rootId}`}>
                  Hồ sơ
                </Link>
              )}
            </div>
            <p className="bold clamp2">{o.customer?.fullName ?? ""}</p>
            <p className="small muted">{o.customer?.code ?? ""}</p>
            <DeliveryStatsBadge customerId={o.customerId} />
            {stats !== undefined && (
              <>
                <div className="kpis kh-kpis">
                  <div className="kpi">
                    <p className="small muted">Đơn</p>
                    <p className="v">{stats.orderCount}</p>
                    <p className="xs muted">{`${String(stats.deliveredCount)} thành công · ${String(stats.returnedCount)} hoàn`}</p>
                  </div>
                  <div className="kpi">
                    <p className="small muted">Tỷ lệ hoàn</p>
                    <p className="v">{bpText(stats.returnRateBp)}</p>
                    <p className="xs muted">trên đơn đã kết thúc</p>
                  </div>
                </div>
                <p className="small" style={{ marginTop: 8 }}>
                  Đã chi <span className="money">{formatMoney(stats.totalSpent)}</span>
                  {stats.lastOrderAt === undefined
                    ? ""
                    : ` · mua gần nhất ${shortTime(stats.lastOrderAt).split(" ")[1] ?? ""}`}
                </p>
              </>
            )}
          </section>
          <section className="card">
            <h2>Giao tới</h2>
            {a === undefined ? (
              <p className="small muted">Chưa có địa chỉ giao.</p>
            ) : (
              <>
                <p>
                  <b>{a.name}</b> · <span className="num">{displayPhone(a.phone)}</span>
                </p>
                <p className="small">{where}</p>
              </>
            )}
            {o.carrierNote !== undefined && o.carrierNote !== "" && (
              <>
                <p className="label" style={{ marginTop: 12 }}>
                  Ghi chú cho hãng
                </p>
                <p className="small">{o.carrierNote}</p>
              </>
            )}
          </section>
          <ShippingPanel
            order={o}
            {...(canEdit
              ? {
                  onEdit: () => {
                    setDialog("edit");
                  },
                }
              : {})}
          />
          {can("cash.payments.record") && <PaymentsBox order={o} />}
          {can("cash.cod.read") && <OrderRefundsCard order={o} />}
          <section className="card">
            <h2>Phụ trách, nguồn</h2>
            <div className="stack-sm">
              <label className="field">
                <span>Người phụ trách</span>
                <span className="select-wrap">
                  <select
                    disabled={!canCare || act.pending}
                    value={o.careBy?.id ?? ""}
                    onChange={(e) => {
                      if (e.target.value !== "")
                        void run({ kind: "update", id: o.id, req: { version, careBy: e.target.value } });
                    }}
                  >
                    {o.careBy === undefined && <option value="">Chưa giao ai</option>}
                    {careOptions.map((u) => (
                      <option key={u.id} value={u.id}>
                        {u.name}
                      </option>
                    ))}
                  </select>
                </span>
              </label>
              <label className="field">
                <span>Nguồn đơn</span>
                <span className="select-wrap">
                  <select
                    disabled={!canSource || act.pending}
                    value={o.trafficSourceId ?? ""}
                    onChange={(e) => {
                      if (e.target.value !== "")
                        void run({ kind: "update", id: o.id, req: { version, trafficSourceId: e.target.value } });
                    }}
                  >
                    {o.trafficSourceId === undefined && <option value="">Chưa có</option>}
                    {sourceOptions.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                </span>
              </label>
              {!canStaff && canCare && (
                <p className="hint">Chỉ người quản lý nhân viên thấy đủ danh sách; bạn có thể nhận đơn về mình.</p>
              )}
            </div>
          </section>
        </div>
      </div>

      {dialog === "edit" && (
        <EditOrderDialog
          order={o}
          onClose={() => {
            setDialog(null);
          }}
        />
      )}
      {dialog === "cancel" && (
        <CancelDialog
          order={o}
          onClose={() => {
            setDialog(null);
          }}
        />
      )}
      {dialog === "return" && returns !== null && (
        <ReturnDialog
          order={o}
          returns={returns}
          timeline={timeline.data?.items ?? []}
          canReadStock={can("inventory.read")}
          canExchange={can("order.create")}
          onClose={() => {
            setDialog(null);
          }}
          onCreated={(r) => {
            setNotice(`Đã tạo phiếu ${r.code}, hoàn dự kiến ${formatMoney(r.refundAmount)}.`);
          }}
        />
      )}
    </div>
  );
}
