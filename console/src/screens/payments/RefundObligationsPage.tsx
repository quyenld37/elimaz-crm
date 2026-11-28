// Nghĩa vụ hoàn /refund-obligations (kế hoạch M3 Task 138, TI09, TD10; mockup docs/mockups/m3/tien-tren-don.html trạng
// thái B; HV34): ba mục Chưa xong / Đã chi đủ / Đã miễn (listRefundObligations theo status), cột Đơn / Nguồn / Phải
// hoàn / Đã chi / Trạng thái. Nghĩa vụ mở "Chưa chi — chưa tính là đã hoàn"; "Ghi phiếu chi" cần cash.refunds.record,
// "Miễn hoàn" cần cash.refunds.waive (người khác thấy nút mờ kèm lý do). Máy chủ không báo hiệu thay đổi nghĩa vụ:
// danh sách đọc lại sau mỗi lần ghi ở màn này.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listRefundObligations, type RefundObligation } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { Paged, usePaged } from "../cod/Paged";
import { usePerms } from "../don/orderData";
import { formatMoney } from "../products/productRules";
import { Act } from "../shipping/CreateStateBanner";
import { PayRefundDialog, WaiveRefundDialog } from "./RefundDialogs";
import { SOURCE_TEXT, obligationState } from "./paymentRules";
import "../cod/cod.css";

type Status = RefundObligation["status"];
const TABS: readonly [Status, string][] = [
  ["open", "Chưa xong"],
  ["paid", "Đã chi đủ"],
  ["waived", "Đã miễn"],
];

/** Chủ đề cục bộ của danh sách nghĩa vụ (không có báo hiệu realtime). */
export const OBLIGATION_TOPIC = "cash.refund_obligations";
const listKey = (status: Status) => queryKey(OBLIGATION_TOPIC, "list", status);

const chipOf = (o: RefundObligation) =>
  o.status === "paid" ? "chip chip-ok" : o.status === "waived" ? "chip chip-line" : "chip";

export function RefundObligationsPage() {
  const qc = useQueryClient();
  const { can, loaded } = usePerms();
  const [status, setStatus] = useState<Status>("open");
  const [open, setOpen] = useState<{ kind: "pay" | "waive"; ob: RefundObligation } | null>(null);
  const paged = usePaged(listKey(status), (q) => unwrap(listRefundObligations({ query: { status, ...q } })));
  const canPay = can("cash.refunds.record");
  const canWaive = can("cash.refunds.waive");
  const done = () => {
    setOpen(null);
    void qc.invalidateQueries({ queryKey: [OBLIGATION_TOPIC] });
  };
  return (
    <div className="cod">
      <div className="page-h">
        <div>
          <p className="crumb">Tiền / Nghĩa vụ hoàn</p>
          <h1>Nghĩa vụ hoàn</h1>
          <p className="sub">Tiền shop còn phải trả lại khách: hủy đơn đã cọc, khách trả hàng, đổi hàng dư tiền.</p>
        </div>
      </div>
      <div className="scroll-x">
        <div className="tabs" role="tablist" aria-label="Trạng thái nghĩa vụ">
          {TABS.map(([t, label]) => (
            <button
              key={t}
              type="button"
              className="tab"
              role="tab"
              aria-selected={t === status}
              onClick={() => {
                setStatus(t);
              }}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <Paged
        paged={paged}
        emptyTitle="Không có nghĩa vụ hoàn nào ở mục này."
        emptyText="Hủy đơn đã thu tiền, kho nhận hàng khách trả, đổi hàng dư tiền sẽ mở nghĩa vụ ở đây."
        countText={(n) => `${String(n)} nghĩa vụ`}
      >
        <div className="tbl-frame">
          <table className="tbl cod-t">
            <thead>
              <tr>
                <th>Đơn</th>
                <th>Nguồn</th>
                <th className="r">Phải hoàn</th>
                <th className="r">Đã chi</th>
                <th>Trạng thái</th>
                <th>
                  <span className="sr">Thao tác</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {paged.rows.map((o) => {
                const st = obligationState(o);
                return (
                  <tr key={o.id}>
                    <td className="lead">
                      <Link className="cell-a num bold" to={`/don/${o.orderId}`}>
                        {o.orderCode ?? "Đơn"}
                      </Link>
                    </td>
                    <td data-label="Nguồn">{SOURCE_TEXT[o.source]}</td>
                    <td data-label="Phải hoàn" className="r">
                      <span className="money">{formatMoney(o.amount)}</span>
                    </td>
                    <td data-label="Đã chi" className="r">
                      <span className="money">{formatMoney(o.paidAmount)}</span>
                    </td>
                    <td data-label="Trạng thái">
                      <span className={chipOf(o)}>{st.chip}</span>
                      {o.status !== "paid" && <span className="xs muted wrap-any">{` ${st.note}`}</span>}
                    </td>
                    <td className="c-act">
                      {o.status === "open" && (
                        <div className="row">
                          {canPay && (
                            <button
                              type="button"
                              className="btn btn-line"
                              onClick={() => {
                                setOpen({ kind: "pay", ob: o });
                              }}
                            >
                              Ghi phiếu chi
                            </button>
                          )}
                          <Act reason={loaded && !canWaive ? "Chỉ Chủ được miễn hoàn" : null}>
                            <button
                              type="button"
                              className="btn btn-quiet"
                              disabled={!canWaive}
                              onClick={() => {
                                setOpen({ kind: "waive", ob: o });
                              }}
                            >
                              Miễn hoàn
                            </button>
                          </Act>
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Paged>
      <p className="hint">
        Nghĩa vụ chỉ đóng khi đã chi đủ hoặc được miễn kèm lý do; chi một phần thì nghĩa vụ vẫn mở.
      </p>
      {open?.kind === "pay" && (
        <PayRefundDialog
          ob={open.ob}
          onClose={() => {
            setOpen(null);
          }}
          onDone={done}
        />
      )}
      {open?.kind === "waive" && (
        <WaiveRefundDialog
          ob={open.ob}
          onClose={() => {
            setOpen(null);
          }}
          onDone={done}
        />
      )}
    </div>
  );
}
