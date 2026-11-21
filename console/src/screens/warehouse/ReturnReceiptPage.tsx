// Nhận hàng hoàn của hãng /warehouse/returns/receive[/:returnId] (kế hoạch M3 Task 136; mockup docs/mockups/m3/kho.html
// trạng thái D; HV26, chủ shop OK 21/11; ca TD02, TD04). Trái: phiếu hoàn chờ nhận (listReturns của M2:
// carrier_return và partial_delivery, status requested; tải theo con trỏ). Phải: thẻ nhận theo món dùng lại
// ReceiveReturnDialog (ba ô Tốt / Hỏng / Thiếu). Hãng báo "đã hoàn về" chưa làm đổi tồn: tồn chỉ đổi khi bấm nhận.
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import {
  getOrder,
  getReturn,
  getShipment,
  listReturns,
  receiveReturn,
  type ReceiveReturnRequest,
  type Return,
  type ReturnKind,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC, orderKey, usePerms } from "../don/orderData";
import { ReceiveReturnCard } from "../don/ReceiveReturnDialog";
import { QTY_KEYS, QTY_TEXT } from "../don/receiveRules";
import { carrierName } from "./warehouseRules";
import "../don/don.css";
import "../len-don/len-don.css";
import "./warehouse.css";

const PAGE = 50;
const BASE = "/warehouse/returns/receive";

function usePending(kind: ReturnKind) {
  return useInfiniteQuery({
    queryKey: queryKey(ORDER_TOPIC, "returns", { kind, status: "requested" }),
    queryFn: ({ pageParam }) =>
      unwrap(
        listReturns({
          query: { kind, status: "requested", limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor,
  });
}

/** Đơn của phiếu (mã, dòng hàng); id rỗng thì chưa đọc. Cùng khóa với chi tiết đơn (tải lại theo order.changed). */
function useOrderOf(id: string) {
  return useQuery({
    queryKey: orderKey(id),
    queryFn: () => unwrap(getOrder({ path: { id } })),
    enabled: id !== "",
  });
}

/** Vận đơn của phiếu (hãng, mã hãng, trạng thái); chỉ đọc khi có quyền fulfillment.read. */
function useShipment(id: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: queryKey("fulfillment.shipment", id),
    queryFn: () => unwrap(getShipment({ path: { id: id ?? "" } })),
    enabled: enabled && id !== undefined,
  });
}

const itemCount = (r: Return) => r.items.reduce((s, i) => s + i.qty, 0);

function PendingRow({ ret, canShip, current }: { ret: Return; canShip: boolean; current: boolean }) {
  const order = useOrderOf(ret.orderId);
  const ship = useShipment(ret.shipmentId, canShip);
  const head = [order.data?.code ?? "…", ship.data === undefined ? null : carrierName(ship.data.carrierKey)]
    .filter((x) => x !== null)
    .join(" · ");
  const lines = `${String(ret.items.length)} dòng · ${String(itemCount(ret))} món`;
  const sub =
    ret.kind === "partial_delivery"
      ? `Giao một phần · ${String(itemCount(ret))} món về kho`
      : ship.data?.status === "returned"
        ? `${lines} · hãng báo đã hoàn về`
        : ship.data?.status === "returning"
          ? "Đang hoàn, hãng chưa báo về"
          : lines;
  return (
    <li>
      <span className="at num">{ret.code}</span>
      <div>
        <Link className="cell-a bold" to={`${BASE}/${ret.id}`} aria-current={current ? "true" : undefined}>
          {head}
        </Link>
        <br />
        <span className={ship.data?.status === "returning" ? "small muted" : "small"}>{sub}</span>
      </div>
    </li>
  );
}

function Received({ ret }: { ret: Return }) {
  const order = useOrderOf(ret.orderId);
  const skuOf = (id: string) => order.data?.items.find((i) => i.id === id)?.sku ?? "";
  return (
    <section className="card" aria-label={`Đã nhận · ${ret.code}`}>
      <div className="row-between">
        <h2>{`Nhận hàng hoàn · ${ret.code}`}</h2>
        <span className="chip chip-ok">Đã nhận vào kho</span>
      </div>
      {ret.items.map((it) => (
        <div key={it.id} className="rline">
          <div className="sp">
            <span className="code">{skuOf(it.orderItemId)}</span>
            <span className="xs">{`Gửi đi ${String(it.qty)}`}</span>
          </div>
          <p className="small num">
            {QTY_KEYS.map((k) => `${QTY_TEXT[k]} ${it[k] === undefined ? "—" : String(it[k])}`).join(" · ")}
          </p>
        </div>
      ))}
    </section>
  );
}

function sendReceive(key: string, a: { returnId: string; req: ReceiveReturnRequest }): Promise<Return> {
  return unwrap(receiveReturn({ path: { returnId: a.returnId }, body: a.req, headers: { "Idempotency-Key": key } }));
}

function ReceivePanel({ returnId, canShip }: { returnId: string; canShip: boolean }) {
  const qc = useQueryClient();
  const ret = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "return", returnId),
    queryFn: () => unwrap(getReturn({ path: { returnId } })),
  });
  const order = useOrderOf(ret.data?.orderId ?? "");
  const ship = useShipment(ret.data?.shipmentId, canShip);
  const write = useWriteAction(sendReceive);
  if (ret.isError) return <div className="box box-err">{errorMessage(ret.error)}</div>;
  if (ret.data === undefined || (order.data === undefined && !order.isError)) {
    return <p className="muted">Đang tải phiếu…</p>;
  }
  if (order.isError) return <div className="box box-err">{errorMessage(order.error)}</div>;
  const r = ret.data;
  if (r.status !== "requested") {
    return r.receivedAt !== undefined ? (
      <Received ret={r} />
    ) : (
      <div className="box box-info">{`Phiếu ${r.code} không còn chờ nhận.`}</div>
    );
  }
  const o = order.data;
  const sh = ship.data;
  const sub = [
    o.code,
    sh === undefined ? null : `${carrierName(sh.carrierKey)} ${sh.trackingCode ?? sh.code}`,
    `${String(itemCount(r))} món`,
  ]
    .filter((x) => x !== null)
    .join(" · ");
  return (
    <ReceiveReturnCard
      key={`${r.id}:${String(r.version)}`}
      ret={r}
      items={o.items}
      sub={sub}
      pending={write.pending}
      error={write.message}
      onReceive={(lines) => {
        void (async () => {
          const res = await write.run({ returnId: r.id, req: { version: r.version, lines } });
          if (!res.ok) return;
          qc.setQueryData(queryKey(ORDER_TOPIC, "return", r.id), res.data);
          void qc.invalidateQueries({ queryKey: queryKey(ORDER_TOPIC, "returns") });
          void qc.invalidateQueries({ queryKey: orderKey(r.orderId) });
        })();
      }}
    />
  );
}

export function ReturnReceiptPage() {
  const { returnId } = useParams();
  const perms = usePerms();
  const canShip = perms.can("fulfillment.read");
  const carrier = usePending("carrier_return");
  const partial = usePending("partial_delivery");
  const lists = [carrier, partial];
  const err = lists.find((l) => l.isError)?.error;
  const loading = lists.some((l) => l.data === undefined && !l.isError);
  const rets = lists
    .flatMap((l) => l.data?.pages.flatMap((p) => p.items) ?? [])
    .sort((a, b) => b.code.localeCompare(a.code));
  const more = lists.some((l) => l.hasNextPage);
  const fetching = lists.some((l) => l.isFetchingNextPage);

  return (
    <div className="stack">
      <div className="page-h">
        <div>
          <p className="crumb">Kho / Nhận hàng hoàn</p>
          <h1>Nhận hàng hoàn</h1>
          <p className="sub">
            Đếm từng món của kiện hoàn: Tốt / Hỏng / Thiếu. Tồn chỉ đổi khi bấm &quot;Nhận vào kho&quot;.
          </p>
        </div>
      </div>
      <div className="cols">
        <div className="stack">
          <section className="card" aria-label="Phiếu hoàn chờ nhận">
            <div className="row-between">
              <h2>Phiếu hoàn chờ nhận</h2>
              {!loading && err === undefined && (
                <span className="small muted">{`${String(rets.length)}${more ? "+" : ""} phiếu`}</span>
              )}
            </div>
            {err !== undefined ? (
              <div className="box box-err">{errorMessage(err)}</div>
            ) : loading ? (
              <p className="muted">Đang tải phiếu…</p>
            ) : rets.length === 0 ? (
              <p className="muted">Không có phiếu hoàn nào chờ nhận.</p>
            ) : (
              <ul className="tl" style={{ marginTop: 8 }}>
                {rets.map((r) => (
                  <PendingRow key={r.id} ret={r} canShip={canShip} current={r.id === returnId} />
                ))}
              </ul>
            )}
            {more && (
              <button
                type="button"
                className="btn btn-line"
                style={{ marginTop: 8 }}
                disabled={fetching}
                onClick={() => {
                  for (const l of lists) if (l.hasNextPage) void l.fetchNextPage();
                }}
              >
                {fetching ? "Đang tải…" : "Tải thêm"}
              </button>
            )}
          </section>
          <div className="box box-info small">
            <b>Hãng báo &quot;đã hoàn về shop&quot; chưa làm đổi tồn.</b> Tồn chỉ tăng khi kho đếm hàng và bấm
            &quot;Nhận vào kho&quot;.
          </div>
        </div>
        <div className="stack">
          {returnId === undefined ? (
            <p className="muted">Chọn một phiếu ở danh sách để đếm hàng và nhận vào kho.</p>
          ) : (
            <ReceivePanel key={returnId} returnId={returnId} canShip={canShip} />
          )}
        </div>
      </div>
    </div>
  );
}
