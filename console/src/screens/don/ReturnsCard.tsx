// Thẻ phiếu trả của đơn (mockup docs/mockups/m2/don-chi-tiet.html trạng thái B). Nút theo trạng thái phiếu và quyền:
// Chờ nhận hàng → "Nhận hàng trả" (order.return.receive), "Hủy phiếu" (order.return; không có với phiếu hoàn của hãng và
// phiếu giao một phần: máy chủ từ chối, hàng chỉ về kho qua kho nhận — Task 442); Đã nhập kho → "Đóng phiếu"
// (order.return). Mọi thao tác gửi version của phiếu qua useWriteAction.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import {
  cancelReturn,
  closeReturn,
  receiveReturn,
  type Order,
  type ReceiveReturnRequest,
  type Return,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { formatMoney } from "../products/productRules";
import { RETURN_KIND_TEXT, RETURN_STATUS_TEXT } from "./labels";
import { returnCancellable } from "./returnRules";
import { ORDER_TOPIC, orderKey, timelineKey } from "./orderData";
import { ReceiveReturnDialog } from "./ReceiveReturnDialog";
import { queryKey } from "../../app/queryClient";

type ReturnAct =
  | { kind: "receive"; returnId: string; req: ReceiveReturnRequest }
  | { kind: "close" | "cancel"; returnId: string; version: number };

function sendAct(key: string, a: ReturnAct): Promise<Return> {
  const headers = { "Idempotency-Key": key };
  const path = { returnId: a.returnId };
  switch (a.kind) {
    case "receive":
      return unwrap(receiveReturn({ path, body: a.req, headers }));
    case "close":
      return unwrap(closeReturn({ path, body: { version: a.version }, headers }));
    case "cancel":
      return unwrap(cancelReturn({ path, body: { version: a.version }, headers }));
  }
}

const chipOf = (s: Return["status"]) => (s === "restocked" || s === "closed" ? "chip chip-ok" : "chip");

export interface ReturnsCardProps {
  order: Order;
  /** null khi còn phiếu chưa đọc xong. */
  returns: readonly Return[] | null;
  /** Lỗi đọc phiếu trả (null: không lỗi). */
  error: unknown;
  can: (perm: string) => boolean;
}

export function ReturnsCard({ order, returns, error, can }: ReturnsCardProps) {
  const qc = useQueryClient();
  const [receiving, setReceiving] = useState<Return | null>(null);
  const write = useWriteAction(sendAct);

  const run = async (a: ReturnAct) => {
    const r = await write.run(a);
    if (!r.ok) return;
    qc.setQueryData(queryKey(ORDER_TOPIC, "return", r.data.id), r.data);
    void qc.invalidateQueries({ queryKey: orderKey(order.id) });
    void qc.invalidateQueries({ queryKey: timelineKey(order.id) });
    setReceiving(null);
  };

  const itemCount = (r: Return) => r.items.reduce((s, i) => s + i.qty, 0);
  return (
    <section className="card" aria-label="Phiếu trả">
      <h2>Phiếu trả</h2>
      {returns === null && error !== null ? (
        <div className="box box-err">{errorMessage(error)}</div>
      ) : returns === null ? (
        <p className="muted">Đang tải phiếu trả…</p>
      ) : returns.length === 0 ? (
        <p className="muted">Đơn chưa có phiếu trả.</p>
      ) : (
        <div className="tbl-frame" style={{ marginTop: 12 }}>
          <table className="tbl">
            <thead>
              <tr>
                <th>Phiếu</th>
                <th>Loại</th>
                <th>Trạng thái</th>
                <th className="r">Hoàn dự kiến</th>
                <th>
                  <span className="sr">Thao tác</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {returns.map((r) => (
                <tr key={r.id} className="ret-row">
                  <td className="lead num bold">{r.code}</td>
                  <td data-label="Loại">
                    {`${RETURN_KIND_TEXT[r.kind]} · ${String(itemCount(r))} món`}
                    {r.replacementOrderId !== undefined && (
                      <>
                        {" → "}
                        <Link className="cell-a" to={`/don/${r.replacementOrderId}`}>
                          đơn đổi
                        </Link>
                      </>
                    )}
                  </td>
                  <td data-label="Trạng thái">
                    <span className={chipOf(r.status)}>{RETURN_STATUS_TEXT[r.status]}</span>
                  </td>
                  <td data-label="Hoàn dự kiến" className="r">
                    <span className="money">{formatMoney(r.refundAmount)}</span>
                  </td>
                  <td className="c-act">
                    <div className="row">
                      {r.status === "requested" && can("order.return.receive") && (
                        <button
                          type="button"
                          className="btn btn-line"
                          onClick={() => {
                            write.reset();
                            setReceiving(r);
                          }}
                        >
                          Nhận hàng trả
                        </button>
                      )}
                      {r.status === "requested" && returnCancellable(r) && can("order.return") && (
                        <button
                          type="button"
                          className="btn btn-quiet"
                          disabled={write.pending}
                          onClick={() => {
                            void run({ kind: "cancel", returnId: r.id, version: r.version });
                          }}
                        >
                          Hủy phiếu
                        </button>
                      )}
                      {r.status === "restocked" && can("order.return") && (
                        <button
                          type="button"
                          className="btn btn-line"
                          disabled={write.pending}
                          onClick={() => {
                            void run({ kind: "close", returnId: r.id, version: r.version });
                          }}
                        >
                          Đóng phiếu
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {receiving === null && write.message !== null && <div className="box box-err">{write.message}</div>}
      <p className="hint" style={{ marginTop: 8 }}>
        Tạo phiếu không đổi tồn; tồn chỉ tăng khi kho bấm nhận. Tiền hoàn của phiếu đổi được cấn trừ với đơn đổi ở M3.
      </p>
      {receiving !== null && (
        <ReceiveReturnDialog
          ret={receiving}
          items={order.items}
          pending={write.pending}
          error={write.message}
          onClose={() => {
            setReceiving(null);
          }}
          onReceive={(lines) => {
            void run({ kind: "receive", returnId: receiving.id, req: { version: receiving.version, lines } });
          }}
        />
      )}
    </section>
  );
}
