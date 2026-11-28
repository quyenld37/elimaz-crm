// Khối "Tiền đã thu" của chi tiết đơn (kế hoạch M3 Task 138, TD10; mockup docs/mockups/m3/tien-tren-don.html trạng thái
// A; HV33): phiếu thu, cọc, cấn trừ, hoàn tiền của đơn (listOrderPayments, mới nhất trước, theo trang), Tổng đơn / Đã
// thu / Thu hộ (COD), nút "Ghi phiếu thu" (mờ kèm lý do khi đơn đã gửi hãng hay không còn nhận tiền). Chỉ người có
// cash.payments.record thấy khối (cùng quyền của hai operation). Đơn đổi (Task 116): phiếu đổi đã nhận mà tiền hoàn
// cuối nhỏ hơn số đã cấn trừ (kho nhận thiếu) → cảnh báo chênh lệch và cách xử lý; CRM không tự sửa phiếu cấn trừ.
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { listOrderPayments, type Order, type Payment, type PaymentPage } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { ORDER_TOPIC, entityMeta, reloadOrder } from "../don/orderData";
import { formatMoney } from "../products/productRules";
import { Act } from "../shipping/CreateStateBanner";
import { useReplacedReturn } from "./exchangeData";
import { RecordPaymentDialog } from "./RecordPaymentDialog";
import { METHOD_TEXT, paymentBlock, shortCredit } from "./paymentRules";

const PAGE = 50;
export const paymentsKey = (orderId: string) => queryKey(ORDER_TOPIC, "payments", orderId);

/** Tiền có dấu: âm ghi "−" (hoàn tiền cho khách). */
const signed = (v: number) => (v < 0 ? `−${formatMoney(-v)}` : formatMoney(v));

function PaymentRow({ p }: { p: Payment }) {
  const sub = [p.reference ?? "", p.actor?.name ?? ""].filter((x) => x !== "").join(" · ");
  return (
    <li>
      <span className="at">{shortTime(p.receivedAt)}</span>
      <div>
        <b>{METHOD_TEXT[p.method]}</b> <span className="money">{signed(p.amount)}</span>
        {sub !== "" && (
          <>
            <br />
            <span className="small wrap-any">{sub}</span>
          </>
        )}
      </div>
    </li>
  );
}

/** Cảnh báo kho nhận thiếu của đơn đổi o (Task 116); không có gì để báo → null. */
function ShortCreditNote({ o, payments, complete }: { o: Order; payments: readonly Payment[]; complete: boolean }) {
  const { rid, ret } = useReplacedReturn(o);
  const r = ret.data;
  if (rid === undefined || !complete || r === undefined) return null;
  if (r.status !== "restocked" && r.status !== "closed") return null;
  const credited = payments
    .filter((p) => p.method === "exchange_credit" && p.reference === `exchange:${rid}`)
    .reduce((s, p) => s + p.amount, 0);
  const short = shortCredit(credited, r.refundAmount);
  if (short === 0) return null;
  const how =
    paymentBlock(o) === null
      ? `thu thêm ${formatMoney(short)} của khách bằng "Ghi phiếu thu" trước khi gửi hãng`
      : `đơn đã gửi hãng nên COD không đổi được; kế toán thu riêng ${formatMoney(short)} của khách, hoặc ghi chú vào đơn nếu shop bỏ qua phần này`;
  return (
    <div className="box box-err small" role="alert">
      {`Kho nhận thiếu hàng của phiếu đổi ${r.code}: đã cấn trừ ${formatMoney(credited)} nhưng tiền hoàn cuối chỉ ${formatMoney(r.refundAmount)} — khách được cấn nhiều hơn ${formatMoney(short)}. CRM không tự sửa phiếu cấn trừ (sổ phiếu chỉ ghi thêm). Cách xử lý: ${how}.`}
    </div>
  );
}

export function PaymentsBox({ order: o }: { order: Order }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const list = useInfiniteQuery({
    queryKey: paymentsKey(o.id),
    meta: entityMeta(o.id),
    initialPageParam: "",
    getNextPageParam: (last: PaymentPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(
        listOrderPayments({
          path: { id: o.id },
          query: pageParam === "" ? { limit: PAGE } : { limit: PAGE, cursor: pageParam },
        }),
      ),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const why = paymentBlock(o);
  return (
    <section className="card" aria-label="Tiền đã thu">
      <div className="row-between">
        <h2>Tiền đã thu</h2>
        <Act reason={why}>
          <button
            type="button"
            className="btn btn-line"
            disabled={why !== null}
            onClick={() => {
              setOpen(true);
            }}
          >
            Ghi phiếu thu
          </button>
        </Act>
      </div>
      {list.isPending ? (
        <p className="small muted">Đang tải phiếu thu…</p>
      ) : list.isError && list.data === undefined ? (
        <div className="box box-err small">{errorMessage(list.error)}</div>
      ) : rows.length === 0 ? (
        <p className="small muted">Chưa có phiếu thu, cọc nào.</p>
      ) : (
        <ul className="tl" aria-label="Phiếu thu của đơn">
          {rows.map((p) => (
            <PaymentRow key={p.id} p={p} />
          ))}
        </ul>
      )}
      {list.hasNextPage && (
        <button
          type="button"
          className="btn btn-quiet"
          disabled={list.isFetchingNextPage}
          onClick={() => {
            void list.fetchNextPage();
          }}
        >
          Xem thêm phiếu
        </button>
      )}
      <ShortCreditNote o={o} payments={rows} complete={list.isSuccess && !list.hasNextPage} />
      <div className="sum" style={{ marginTop: 8 }}>
        <div className="sum-r" aria-label="Tổng đơn">
          <span>Tổng đơn</span>
          <span className="money">{formatMoney(o.grandTotal)}</span>
        </div>
        <div className="sum-r" aria-label="Đã thu">
          <span>Đã thu</span>
          <span className="money">{formatMoney(o.prepaidTotal ?? 0)}</span>
        </div>
        <div className="sum-r is-total" aria-label="Thu hộ (COD)">
          <span>Thu hộ (COD)</span>
          <span className="money">{formatMoney(o.codAmount)}</span>
        </div>
      </div>
      {open && (
        <RecordPaymentDialog
          order={o}
          onClose={() => {
            setOpen(false);
          }}
          onDone={() => {
            setOpen(false);
            reloadOrder(qc, o.id);
            void qc.invalidateQueries({ queryKey: paymentsKey(o.id) });
          }}
        />
      )}
    </section>
  );
}
