// Giữ hàng quá hạn /ops/aged-holds (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html trạng thái F; KH09).
// Chỉ đọc: đang giữ quá 20 ngày, đang giao quá 180 ngày. KHÔNG có nút nhả hàng: người xử lý mở đơn (hủy đơn thì hàng
// về "có thể bán", hay liên hệ hãng). Chỉ hiện kho người xem có quyền xem (máy chủ lọc).
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listAgedReservations, listWarehouses } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { agedKey, useOpsPaged, usePerms } from "./opsData";
import { agedState, dayMonth } from "./opsRules";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { ListState, OpsFrame, Seg } from "./OpsTabs";
import "./ops.css";

type State = "" | "held" | "in_transit";
const STATE_FILTERS: readonly { key: State; label: string }[] = [
  { key: "", label: "Tất cả" },
  { key: "held", label: "Đang giữ" },
  { key: "in_transit", label: "Đang giao" },
];

export function AgedHoldsPage() {
  const perms = usePerms();
  const [state, setState] = useState<State>("");
  const paged = useOpsPaged(agedKey(state), (query) =>
    unwrap(listAgedReservations({ query: { ...query, ...(state === "" ? {} : { state }) } })),
  );
  // Tên kho: cần inventory.read; không có thì hiện mã ngắn của kho.
  const wh = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    enabled: perms.can("inventory.read"),
    queryFn: () => unwrap(listWarehouses()),
  });
  const name = (id: string) => wh.data?.find((w) => w.id === id)?.name ?? `Kho ${id.slice(0, 8)}`;
  const { list } = paged;
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const failed = list.isError && list.data === undefined ? errorMessage(list.error) : null;
  return (
    <OpsFrame title="Giữ hàng quá hạn" sub="Hàng đang giữ cho đơn quá 20 ngày, hay đang giao quá 180 ngày; chỉ để xem.">
      <div className="ops-filters">
        <Seg label="Trạng thái hàng" value={state} options={STATE_FILTERS} onChange={setState} />
      </div>
      <ListState
        pending={list.isPending}
        error={failed}
        empty={!list.isPending && failed === null && rows.length === 0}
        emptyTitle="Không có hàng giữ quá hạn"
        emptyText="Hàng giữ hay đang giao quá lâu sẽ hiện ở đây."
      />
      {rows.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl ops-t">
            <thead>
              <tr>
                <th>Đơn</th>
                <th>Mẫu · kho</th>
                <th>Trạng thái</th>
                <th className="r">SL</th>
                <th>Từ ngày</th>
                <th className="c-act" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.reservationId}>
                  <td className="lead">
                    <Link className="num bold" to={`/don/${r.orderId}`}>
                      {r.orderCode}
                    </Link>
                  </td>
                  <td data-label="Mẫu · kho" className="small wrap-any">{`${r.sku} · ${name(r.warehouseId)}`}</td>
                  <td data-label="Trạng thái">
                    <span className="chip">{agedState(r)}</span>
                  </td>
                  <td data-label="SL" className="r num">
                    {r.quantity}
                  </td>
                  <td data-label="Từ ngày" className="num">
                    {dayMonth(r.since)}
                  </td>
                  <td className="c-act">
                    <Link className="btn btn-line" to={`/don/${r.orderId}`}>
                      Mở đơn
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && <MoreBar text={`${String(rows.length)} dòng đã tải`} pages={list} />}
      <p className="hint">
        Hệ thống không tự trả hàng về kho. Người xử lý mở đơn: hủy đơn (hàng về "có thể bán") hoặc liên hệ hãng về kiện
        đang giao quá lâu. Chỉ hiện kho bạn có quyền xem.
      </p>
    </OpsFrame>
  );
}
