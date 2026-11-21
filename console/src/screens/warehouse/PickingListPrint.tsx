// Phiếu nhặt hàng /warehouse/picking-list?shipmentIds=…&warehouseId=… (mockup docs/mockups/m3/kho.html trạng thái C;
// HV25, chủ shop OK 21/11): trang in A4 đen trắng, gom theo mẫu rồi kê theo đơn; API không trả địa chỉ, SĐT khách nên
// trang không có. Nút "In" gọi hộp in của trình duyệt; @media print (warehouse.css) chỉ in tờ phiếu.
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import { getPickingList, listWarehouses } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { formatMoney } from "../products/productRules";
import { MAX_SELECT, carrierName } from "./warehouseRules";
import "./warehouse.css";

export function PickingListPrint() {
  const [sp] = useSearchParams();
  const ids = sp.getAll("shipmentIds").slice(0, MAX_SELECT);
  const warehouseId = sp.get("warehouseId") ?? "";
  const [printedAt] = useState(() => new Date().toISOString());
  const list = useQuery({
    queryKey: queryKey("fulfillment.picking-list", ids),
    queryFn: () => unwrap(getPickingList({ query: { shipmentIds: ids } })),
    enabled: ids.length > 0,
  });
  const warehouses = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    queryFn: () => unwrap(listWarehouses()),
    enabled: warehouseId !== "",
  });
  const whName = warehouses.data?.find((w) => w.id === warehouseId)?.name;
  const back = (
    <Link className="btn btn-quiet no-print" to="/warehouse/pack">
      Về Đóng gói
    </Link>
  );

  if (ids.length === 0) {
    return (
      <div className="empty">
        <h2>Chưa chọn vận đơn</h2>
        <p className="small muted">Chọn vận đơn ở màn Đóng gói rồi bấm &quot;In phiếu nhặt&quot;.</p>
        {back}
      </div>
    );
  }
  if (list.isError) {
    return (
      <div className="stack-sm">
        <div className="box box-err">{errorMessage(list.error)}</div>
        {back}
      </div>
    );
  }
  if (list.data === undefined) return <p className="muted">Đang tải phiếu nhặt…</p>;
  const { lines, orders } = list.data;
  const items = orders.reduce((s, o) => s + o.itemCount, 0);
  return (
    <div className="stack">
      <div className="no-print">{back}</div>
      <div className="paper" aria-label="Phiếu nhặt hàng">
        <div className="row-between">
          <div>
            <h2>{`Phiếu nhặt hàng${whName === undefined ? "" : ` · ${whName}`}`}</h2>
            <p className="small">{`In ${shortTime(printedAt)} · ${String(orders.length)} đơn · ${String(items)} món`}</p>
          </div>
          <button
            type="button"
            className="btn btn-line no-print"
            onClick={() => {
              window.print();
            }}
          >
            In
          </button>
        </div>
        <div className="tbl-frame">
          <table className="tbl" aria-label="Gom theo mẫu">
            <thead>
              <tr>
                <th>Mẫu</th>
                <th>Tên</th>
                <th className="r">SL</th>
                <th>Đơn</th>
              </tr>
            </thead>
            <tbody>
              {lines.map((l) => (
                <tr key={l.sku}>
                  <td className="lead num bold">{l.sku}</td>
                  <td data-label="Tên">
                    <span className="clamp2">{l.variant === undefined ? l.name : `${l.name} · ${l.variant}`}</span>
                  </td>
                  <td data-label="SL" className="r num">
                    {l.qty}
                  </td>
                  <td data-label="Đơn">
                    <span className="small num">{l.orderCodes.join(", ")}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="tbl-frame">
          <table className="tbl" aria-label="Kê theo đơn">
            <thead>
              <tr>
                <th>Đơn</th>
                <th>Vận đơn</th>
                <th>Hãng</th>
                <th className="r">Món</th>
                <th className="r">Thu hộ</th>
              </tr>
            </thead>
            <tbody>
              {orders.map((o) => (
                <tr key={o.code}>
                  <td className="lead num bold">{o.code}</td>
                  <td data-label="Vận đơn" className="num">
                    {o.trackingCode ?? o.shipmentCode}
                  </td>
                  <td data-label="Hãng">{o.carrier === "self" ? "Tự giao" : carrierName(o.carrier)}</td>
                  <td data-label="Món" className="r num">
                    {o.itemCount}
                  </td>
                  <td data-label="Thu hộ" className="r">
                    <span className="money">{formatMoney(o.codAmount)}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="xs">Trang in theo khổ A4, chữ đen; không in địa chỉ, SĐT khách (nhãn hãng đã có).</p>
      </div>
    </div>
  );
}
