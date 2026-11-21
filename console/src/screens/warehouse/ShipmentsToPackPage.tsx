// Màn Đóng gói /warehouse/pack (kế hoạch M3 Task 136; mockup docs/mockups/m3/kho.html trạng thái A, B, E; HV23, HV24,
// chủ shop OK 21/11). Vận đơn "Chờ thu gom" của một kho; chọn nhiều dòng → thanh đen: In nhãn (hộp kết quả theo lô),
// In phiếu nhặt (trang in, tab mới), Đang đóng gói, Đã đóng gói (chỉ đi tới; máy chủ trả kết quả từng đơn). Danh sách
// tải theo con trỏ, tự tải lại khi có báo hiệu order.changed.
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import {
  createLabelBatch,
  listOrders,
  listWarehouses,
  setPackingStatus,
  type LabelBatchCreated,
  type OrderListItem,
  type PackingResultList,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC, usePerms } from "../don/orderData";
import { WarehouseSelect } from "../inventory/WarehouseSelect";
import { formatMoney } from "../products/productRules";
import { LabelBatchDialog } from "./LabelBatchDialog";
import {
  MAX_SELECT,
  PACK_CHIP,
  PACK_FILTERS,
  PACK_FILTER_TEXT,
  PACK_TEXT,
  carrierName,
  labelCell,
  packFilterQuery,
  packingMessage,
  selectionText,
  type PackFilter,
  type PackTo,
} from "./warehouseRules";
import "../don/don.css";
import "./warehouse.css";

const PAGE = 50;

type Act = { kind: "label"; shipmentIds: string[] } | { kind: "pack"; orderIds: string[]; to: PackTo };
type ActResult = { kind: "label"; data: LabelBatchCreated } | { kind: "pack"; data: PackingResultList };

async function sendAct(key: string, a: Act): Promise<ActResult> {
  const headers = { "Idempotency-Key": key };
  if (a.kind === "label") {
    const data = await unwrap(createLabelBatch({ body: { shipmentIds: a.shipmentIds }, headers }));
    return { kind: "label", data };
  }
  const data = await unwrap(setPackingStatus({ body: { orderIds: a.orderIds, to: a.to }, headers }));
  return { kind: "pack", data };
}

/** Đường trang in phiếu nhặt của các vận đơn đã chọn (mở ở tab mới). */
export function pickingListPath(shipmentIds: readonly string[], warehouseId: string): string {
  const q = new URLSearchParams();
  for (const id of shipmentIds) q.append("shipmentIds", id);
  if (warehouseId !== "") q.set("warehouseId", warehouseId);
  return `/warehouse/picking-list?${q.toString()}`;
}

export function ShipmentsToPackPage() {
  const qc = useQueryClient();
  const perms = usePerms();
  const warehouses = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    queryFn: () => unwrap(listWarehouses()),
  });
  const active = (warehouses.data ?? []).filter((w) => w.status === "active");
  const [picked, setPicked] = useState("");
  const warehouseId = picked !== "" ? picked : (active[0]?.id ?? "");
  const [filter, setFilter] = useState<PackFilter>("all");
  const [selected, setSelected] = useState<ReadonlyMap<string, OrderListItem>>(new Map());
  const [toast, setToast] = useState<{ text: string; first?: string } | null>(null);
  const [labels, setLabels] = useState<{ created: LabelBatchCreated; rows: OrderListItem[] } | null>(null);
  const write = useWriteAction(sendAct);

  const params = { shipmentStatus: ["awaiting_pickup" as const], warehouseId, limit: PAGE, ...packFilterQuery(filter) };
  const list = useInfiniteQuery({
    queryKey: queryKey(ORDER_TOPIC, "pack", params),
    queryFn: ({ pageParam }) =>
      unwrap(listOrders({ query: { ...params, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor,
    enabled: warehouseId !== "",
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const busy = write.pending;

  const changeView = (f: () => void) => {
    f();
    setSelected(new Map());
    setToast(null);
  };
  const toggle = (o: OrderListItem) => {
    setSelected((m) => {
      const n = new Map(m);
      if (n.has(o.id)) n.delete(o.id);
      else if (n.size < MAX_SELECT) n.set(o.id, o);
      return n;
    });
  };
  const allOn = rows.length > 0 && rows.slice(0, MAX_SELECT).every((r) => selected.has(r.id));
  const chosen = [...selected.values()];
  const shipmentIds = chosen.flatMap((o) => (o.shipment?.id === undefined ? [] : [o.shipment.id]));

  const printLabels = async () => {
    setToast(null);
    const r = await write.run({ kind: "label", shipmentIds });
    if (r.ok && r.data.kind === "label") setLabels({ created: r.data.data, rows: chosen });
  };
  const pack = async (to: PackTo) => {
    setToast(null);
    const byId = new Map(selected);
    const r = await write.run({ kind: "pack", orderIds: chosen.map((o) => o.id), to });
    if (!r.ok || r.data.kind !== "pack") return;
    const further = r.data.data.items.find((i) => i.result === "already_further");
    setToast({ text: packingMessage(to, r.data.data.items, byId), first: further?.orderId });
    setSelected(new Map());
    void qc.invalidateQueries({ queryKey: queryKey(ORDER_TOPIC, "pack") });
  };

  return (
    <div className="stack">
      <div className="page-h">
        <div>
          <p className="crumb">Kho / Đóng gói</p>
          <h1>Đóng gói và in nhãn</h1>
          <p className="sub">
            Vận đơn &quot;Chờ thu gom&quot; của kho bạn có quyền. Chọn nhiều dòng để in nhãn theo lô, in phiếu nhặt,
            đánh dấu đóng gói.
          </p>
        </div>
        <div className="select-wrap" style={{ minWidth: 180 }}>
          <WarehouseSelect
            ariaLabel="Kho"
            warehouses={active}
            value={warehouseId}
            onChange={(id) => {
              changeView(() => {
                setPicked(id);
              });
            }}
          />
        </div>
      </div>

      <div className="scroll-x">
        <div className="lblbar" role="group" aria-label="Lọc vận đơn">
          {PACK_FILTERS.map((f) => (
            <button
              key={f}
              type="button"
              className="lbl"
              aria-pressed={filter === f}
              onClick={() => {
                changeView(() => {
                  setFilter(f);
                });
              }}
            >
              {PACK_FILTER_TEXT[f]}
            </button>
          ))}
        </div>
      </div>

      {chosen.length > 0 && (
        <div className="selbar" role="toolbar" aria-label="Thao tác với vận đơn đã chọn">
          <span className="grow">{selectionText(chosen)}</span>
          {perms.can("fulfillment.print") && (
            <>
              <button type="button" className="btn" disabled={busy} onClick={() => void printLabels()}>
                In nhãn
              </button>
              <button
                type="button"
                className="btn"
                disabled={busy}
                onClick={() => {
                  window.open(pickingListPath(shipmentIds, warehouseId), "_blank", "noopener");
                }}
              >
                In phiếu nhặt
              </button>
            </>
          )}
          {perms.can("fulfillment.pack") && (
            <>
              <button type="button" className="btn" disabled={busy} onClick={() => void pack("picking")}>
                Đang đóng gói
              </button>
              <button type="button" className="btn btn-dark" disabled={busy} onClick={() => void pack("packed")}>
                Đã đóng gói
              </button>
            </>
          )}
        </div>
      )}
      {labels === null && write.message !== null && <div className="box box-err">{write.message}</div>}
      {toast !== null && (
        <div className="toast" role="status">
          <span className="grow">{toast.text}</span>
          {toast.first !== undefined && (
            <Link className="btn" to={`/don/${toast.first}`}>
              Xem đơn
            </Link>
          )}
        </div>
      )}

      {warehouses.isError ? (
        <div className="box box-err">{errorMessage(warehouses.error)}</div>
      ) : list.isError ? (
        <div className="box box-err">{errorMessage(list.error)}</div>
      ) : warehouses.isSuccess && active.length === 0 ? (
        <div className="empty">
          <h2>Không có kho đang dùng</h2>
          <p className="small muted">Bạn chưa được giao kho nào, hoặc mọi kho đã đóng.</p>
        </div>
      ) : list.data === undefined ? (
        <p className="muted">Đang tải vận đơn…</p>
      ) : rows.length === 0 ? (
        <div className="empty">
          <h2>Không có đơn chờ đóng gói</h2>
          <p className="small muted">Đơn gửi hãng xong sẽ vào đây.</p>
        </div>
      ) : (
        <>
          <div className="tbl-frame">
            <table className="tbl">
              <thead>
                <tr>
                  <th className="c-chk">
                    <label className="check">
                      <input
                        type="checkbox"
                        aria-label="Chọn cả trang"
                        checked={allOn}
                        disabled={busy}
                        onChange={() => {
                          setSelected(
                            allOn ? new Map() : new Map(rows.slice(0, MAX_SELECT).map((r) => [r.id, r] as const)),
                          );
                        }}
                      />
                    </label>
                  </th>
                  <th>Đơn · vận đơn</th>
                  <th className="r">Món</th>
                  <th>Nhãn</th>
                  <th>Đóng gói</th>
                  <th className="r">Thu hộ</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((o) => {
                  const lbl = labelCell(o);
                  const st = o.packingStatus ?? "none";
                  return (
                    <tr key={o.id}>
                      <td className="c-chk lead">
                        <label className="check">
                          <input
                            type="checkbox"
                            aria-label={`Chọn ${o.code}`}
                            checked={selected.has(o.id)}
                            disabled={busy || (!selected.has(o.id) && selected.size >= MAX_SELECT)}
                            onChange={() => {
                              toggle(o);
                            }}
                          />
                        </label>
                      </td>
                      <td data-label="Đơn · vận đơn">
                        <span className="who-c">
                          <Link className="cell-a num bold" to={`/don/${o.id}`}>
                            {o.code}
                          </Link>
                          <span className="small muted num">
                            {`${carrierName(o.shipment?.carrierKey ?? "")} · ${o.shipment?.trackingCode ?? o.shipment?.code ?? ""}`}
                          </span>
                        </span>
                      </td>
                      <td data-label="Món" className="r num">
                        {o.itemCount}
                      </td>
                      <td data-label="Nhãn">
                        <span className={lbl.muted ? "small muted" : "small"}>{lbl.text}</span>
                      </td>
                      <td data-label="Đóng gói">
                        <span className={PACK_CHIP[st]}>{PACK_TEXT[st]}</span>
                      </td>
                      <td data-label="Thu hộ" className="r">
                        <span className="money">{formatMoney(o.codAmount)}</span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="more">
            <span>
              {list.hasNextPage
                ? `Đang hiện ${String(rows.length)} vận đơn, còn nữa`
                : `${String(rows.length)} vận đơn chờ thu gom`}
            </span>
            {list.hasNextPage && (
              <button
                type="button"
                className="btn btn-line"
                disabled={list.isFetchingNextPage}
                onClick={() => void list.fetchNextPage()}
              >
                {list.isFetchingNextPage ? "Đang tải…" : "Tải thêm"}
              </button>
            )}
            <span>{`Chọn tối đa ${String(MAX_SELECT)} dòng mỗi lần`}</span>
          </div>
        </>
      )}

      {labels !== null && (
        <LabelBatchDialog
          created={labels.created}
          rows={labels.rows}
          canReadAccounts={perms.can("fulfillment.carrier_accounts.read")}
          onClose={() => {
            setLabels(null);
            write.reset();
          }}
        />
      )}
    </div>
  );
}
