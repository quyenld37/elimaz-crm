// Ghi giao một phần (kế hoạch M3 Task 138, TD09; mockup docs/mockups/m3/tra-doi.html trạng thái C; HV29): mỗi dòng đơn
// số khách nhận (0…số gửi), tiền shipper thực thu (0…COD gửi hãng), lý do; câu tác động trước khi ghi. Như máy chủ
// (fulfillment partial.Decide): mọi dòng đúng một lần, 0 < tổng nhận < tổng gửi, 0 ≤ tiền ≤ COD. Phần còn lại thành
// phiếu hoàn "giao một phần", kho nhận khi hàng về. CRM không tự ghi theo tin của hãng. Cần fulfillment.correct.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { recordPartialDelivery, type Order, type RecordPartialDeliveryRequest, type Shipment } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { reloadOrder } from "../don/orderData";
import { parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";
import { DialogActions, DialogNotes } from "../shipping/dialogParts";
import { shipmentKey } from "../shipping/useShipment";
import { Dialog } from "../staff/staffData";

export interface PartialDeliveryDialogProps {
  s: Shipment;
  order: Order;
  onClose: () => void;
}

export function PartialDeliveryDialog({ s, order: o, onClose }: PartialDeliveryDialogProps) {
  const qc = useQueryClient();
  const [got, setGot] = useState<ReadonlyMap<string, number>>(() => new Map(o.items.map((i) => [i.id, i.qty])));
  const [cash, setCash] = useState("");
  const [reason, setReason] = useState("");
  const write = useWriteAction((key, b: { id: string; req: RecordPartialDeliveryRequest }) =>
    unwrap(recordPartialDelivery({ path: { id: b.id }, body: b.req, headers: { "Idempotency-Key": key } })),
  );
  const qtyOf = (id: string, max: number) => Math.min(Math.max(got.get(id) ?? max, 0), max);
  const sent = o.items.reduce((n, i) => n + i.qty, 0);
  const delivered = o.items.reduce((n, i) => n + qtyOf(i.id, i.qty), 0);
  const rest = o.items.filter((i) => qtyOf(i.id, i.qty) < i.qty);
  const n = parseMoney(cash);
  const why =
    delivered === sent
      ? "Khách nhận đủ mọi món thì không phải giao một phần; chờ hãng báo Thành công"
      : delivered === 0
        ? "Khách không nhận món nào: đây là giao thất bại, không phải giao một phần"
        : n === null
          ? "Nhập tiền shipper thực thu (đồng, 0 nếu không thu)"
          : n > s.codAmount
            ? `Vượt COD gửi hãng ${formatMoney(s.codAmount)}`
            : reason.trim() === ""
              ? "Ghi lý do"
              : null;
  const set = (id: string, v: number) => {
    setGot((m) => new Map(m).set(id, v));
  };
  const submit = async () => {
    if (why !== null || n === null) return;
    const req: RecordPartialDeliveryRequest = {
      items: o.items.map((i) => ({ orderItemId: i.id, deliveredQty: qtyOf(i.id, i.qty) })),
      codCollected: n,
      reason: reason.trim(),
    };
    const r = await write.run({ id: s.id, req });
    if (!r.ok) return;
    void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
    reloadOrder(qc, o.id);
    onClose();
  };
  const restQty = sent - delivered;
  return (
    <Dialog
      title={`Giao một phần · ${o.code}`}
      className="dlg-wide"
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Ghi giao một phần"
          why={why}
          pending={write.pending}
          onClose={onClose}
          onSubmit={() => void submit()}
        />
      }
    >
      <p className="small muted">
        {`${s.accountName ?? s.carrierKey} ${s.trackingCode ?? s.code} · COD gửi hãng ${formatMoney(s.codAmount)}`}
      </p>
      <div className="tbl-frame">
        <table className="tbl">
          <thead>
            <tr>
              <th>Dòng</th>
              <th className="r">Đã gửi</th>
              <th>Khách nhận</th>
            </tr>
          </thead>
          <tbody>
            {o.items.map((i) => {
              const q = qtyOf(i.id, i.qty);
              return (
                <tr key={i.id}>
                  <td className="lead">
                    <span className="code">{i.sku}</span>
                  </td>
                  <td data-label="Đã gửi" className="r num">
                    {i.qty}
                  </td>
                  <td data-label="Khách nhận">
                    <span className="stepper">
                      <button
                        type="button"
                        className="ibtn"
                        aria-label="Bớt 1"
                        disabled={q <= 0}
                        onClick={() => {
                          set(i.id, q - 1);
                        }}
                      >
                        −
                      </button>
                      <span className="num" aria-label={`Khách nhận ${i.sku}`}>
                        {q}
                      </span>
                      <button
                        type="button"
                        className="ibtn"
                        aria-label="Thêm 1"
                        disabled={q >= i.qty}
                        onClick={() => {
                          set(i.id, q + 1);
                        }}
                      >
                        +
                      </button>
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <label className="field">
        <span>Tiền shipper thực thu *</span>
        <input
          className="input"
          inputMode="numeric"
          aria-label="Tiền shipper thực thu"
          value={cash}
          onChange={(e) => {
            setCash(e.target.value);
          }}
        />
        <span className="hint">Hỏi khách hoặc xem app của hãng. Có thể khác giá món.</span>
      </label>
      <label className="field">
        <span>Lý do *</span>
        <input
          className="input"
          maxLength={500}
          aria-label="Lý do"
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      {delivered > 0 && delivered < sent && (
        <ul className="fx small" aria-label="Sẽ xảy ra">
          <li>{`Đơn thành công một phần: ${String(delivered)} món xuất kho`}</li>
          <li>{`${String(restQty)} món (${rest.map((i) => i.sku).join(", ")}) thành phiếu hoàn "giao một phần", kho nhận khi hàng về`}</li>
          {n !== null && n <= s.codAmount && (
            <li>{`Sổ COD ghi thu ${formatMoney(n)}, phần còn lại ${formatMoney(s.codAmount - n)} không thu`}</li>
          )}
        </ul>
      )}
      <DialogNotes why={why} message={write.message} />
      <p className="hint">CRM không tự ghi giao một phần theo tin của hãng: tin đó chỉ đưa đơn vào "Cần xem".</p>
    </Dialog>
  );
}
