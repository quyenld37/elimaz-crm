// Thẻ "Nghĩa vụ hoàn" trên chi tiết đơn (Task 524, từ QĐ-138-1; mockup docs/mockups/m3/tien-tren-don.html trạng thái
// B; HV34): nghĩa vụ hoàn của đúng đơn này (listRefundObligations?orderId, cash.cod.read), mỗi nghĩa vụ một khối: nguồn,
// phải hoàn, đã chi, trạng thái; nghĩa vụ mở "Chưa chi — chưa tính là đã hoàn" kèm "Ghi phiếu chi"
// (cash.refunds.record) và "Miễn hoàn" (cash.refunds.waive; người khác thấy nút mờ kèm lý do). Đơn không có nghĩa vụ →
// không vẽ thẻ; đang tải → không vẽ (không giả là rỗng). Máy chủ không báo hiệu thay đổi nghĩa vụ: đọc lại sau mỗi lần
// ghi ở đây, và cùng chủ đề với trang Nghĩa vụ hoàn.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listRefundObligations, type Order, type RefundObligation } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { reloadOrder, usePerms } from "../don/orderData";
import { formatMoney } from "../products/productRules";
import { Act } from "../shipping/CreateStateBanner";
import { paymentsKey } from "./PaymentsBox";
import { PayRefundDialog, WaiveRefundDialog } from "./RefundDialogs";
import { OBLIGATION_TOPIC } from "./RefundObligationsPage";
import { SOURCE_TEXT, TEXT_NOT_COUNTED, obligationState } from "./paymentRules";

const LIMIT = 50;
export const orderObligationsKey = (orderId: string) => queryKey(OBLIGATION_TOPIC, "order", orderId);

const chipOf = (o: RefundObligation) =>
  o.status === "paid" ? "chip chip-ok" : o.status === "waived" ? "chip chip-line" : "chip chip-err";

function Obligation({
  ob,
  onPay,
  onWaive,
  waiveReason,
}: {
  ob: RefundObligation;
  onPay: (() => void) | null;
  onWaive: (() => void) | null;
  /** Lý do Miễn hoàn mờ (thiếu cash.refunds.waive, khi quyền đã tải); null: không câu. */
  waiveReason: string | null;
}) {
  const st = obligationState(ob);
  return (
    <div className="stack-sm" aria-label={`Nghĩa vụ ${SOURCE_TEXT[ob.source]}`}>
      <div className="row-between">
        <h3>{`Nghĩa vụ hoàn · ${ob.orderCode ?? "đơn"}`}</h3>
        <span className={chipOf(ob)}>{st.chip}</span>
      </div>
      <p className="small">
        {`${SOURCE_TEXT[ob.source]}. Phải hoàn `}
        <span className="money">{formatMoney(ob.amount)}</span>
        {", đã chi "}
        <span className="money">{formatMoney(ob.paidAmount)}</span>.
      </p>
      {ob.status === "open" ? (
        <div className="box box-err small">
          {ob.paidAmount === 0 ? <b>{`${TEXT_NOT_COUNTED}.`}</b> : <b>{`${st.note}.`}</b>} Còn phải hoàn{" "}
          <span className="money">{formatMoney(st.remaining)}</span>.
        </div>
      ) : (
        <p className="small muted wrap-any">{st.note}</p>
      )}
      {ob.status === "open" && (
        <div className="acts">
          {onPay !== null && (
            <div className="act">
              <button type="button" className="btn btn-dark" onClick={onPay}>
                Ghi phiếu chi
              </button>
            </div>
          )}
          <Act reason={waiveReason}>
            <button type="button" className="btn btn-line" disabled={onWaive === null} onClick={onWaive ?? undefined}>
              Miễn hoàn
            </button>
          </Act>
        </div>
      )}
    </div>
  );
}

export function OrderRefundsCard({ order: o }: { order: Order }) {
  const qc = useQueryClient();
  const { can, loaded } = usePerms();
  const [open, setOpen] = useState<{ kind: "pay" | "waive"; ob: RefundObligation } | null>(null);
  const list = useQuery({
    queryKey: orderObligationsKey(o.id),
    queryFn: () => unwrap(listRefundObligations({ query: { orderId: o.id, limit: LIMIT } })),
  });
  if (list.isPending) return null;
  if (list.isError && list.data === undefined) {
    return (
      <section className="card" aria-label="Nghĩa vụ hoàn">
        <h2>Nghĩa vụ hoàn</h2>
        <div className="box box-err small">{errorMessage(list.error)}</div>
      </section>
    );
  }
  const items = list.data.items;
  if (items.length === 0) return null;
  const canPay = can("cash.refunds.record");
  const canWaive = can("cash.refunds.waive");
  const waiveReason = loaded && !canWaive ? "Chỉ Chủ được miễn hoàn" : null;
  const done = () => {
    setOpen(null);
    void qc.invalidateQueries({ queryKey: [OBLIGATION_TOPIC] });
    void qc.invalidateQueries({ queryKey: paymentsKey(o.id) });
    reloadOrder(qc, o.id);
  };
  const close = () => {
    setOpen(null);
  };
  return (
    <section className="card stack-sm" aria-label="Nghĩa vụ hoàn">
      {items.map((ob) => (
        <Obligation
          key={ob.id}
          ob={ob}
          onPay={
            canPay
              ? () => {
                  setOpen({ kind: "pay", ob });
                }
              : null
          }
          waiveReason={waiveReason}
          onWaive={
            canWaive
              ? () => {
                  setOpen({ kind: "waive", ob });
                }
              : null
          }
        />
      ))}
      {list.data.nextCursor !== undefined && (
        <p className="hint">
          Đơn còn nghĩa vụ khác: xem ở <Link to="/refund-obligations">trang Nghĩa vụ hoàn</Link>.
        </p>
      )}
      <p className="hint">
        Nghĩa vụ chỉ đóng khi đã chi đủ hoặc được miễn kèm lý do; chi một phần thì nghĩa vụ vẫn mở.
      </p>
      {open?.kind === "pay" && <PayRefundDialog ob={open.ob} onClose={close} onDone={done} />}
      {open?.kind === "waive" && <WaiveRefundDialog ob={open.ob} onClose={close} onDone={done} />}
    </section>
  );
}
