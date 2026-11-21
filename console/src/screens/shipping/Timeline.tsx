// Dòng thời gian vận đơn và "Vận đơn của đơn này" (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html
// trạng thái D; HV14). Mỗi sự kiện có giờ, nhãn, chip nguồn (Hãng / Nhân viên / Hệ thống theo source; ShipmentEvent chưa
// có tên người làm); sự kiện bị bỏ qua vẫn hiện, nhạt, kèm lý do. Còn trang sau thì có "Xem thêm", số sự kiện không
// được coi là đủ.
import type { Order, Shipment, ShipmentEvent } from "../../api";
import { errorMessage } from "../../app/errors";
import { shortTime } from "../../app/time";
import { carrierName } from "./QuoteTable";
import { cancelledText, ignoredText, sourceText, statusText } from "./shipmentRules";
import { useOrderShipments, useShipmentEvents } from "./useShipment";
import "./shipping.css";

const at = (iso: string) => shortTime(iso).replace(" ", " · ");

function EventRow({ e }: { e: ShipmentEvent }) {
  const src = sourceText(e.source);
  const ign = ignoredText(e);
  return (
    <li className={ign === null ? undefined : "is-ign"}>
      <span className="at">{at(e.occurredAt)}</span>
      <div>
        <b>{e.label ?? e.rawStatus}</b> · <span className={src.staff ? "src is-staff" : "src"}>{src.text}</span>
        {ign !== null && (
          <>
            <br />
            <span className="small">{ign}</span>
          </>
        )}
      </div>
    </li>
  );
}

/** Thẻ dòng thời gian của vận đơn id. */
export function ShipmentTimeline({ id }: { id: string }) {
  const q = useShipmentEvents(id);
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  const more = q.hasNextPage;
  return (
    <section className="card" aria-label="Dòng thời gian vận đơn">
      <div className="row-between">
        <h2>Dòng thời gian vận đơn</h2>
        {q.isSuccess && (
          <span className="small muted">{`${String(items.length)}${more ? "+" : ""} sự kiện · mới nhất trên đầu`}</span>
        )}
      </div>
      {q.isError ? (
        <div className="box box-err small">{errorMessage(q.error)}</div>
      ) : q.isPending ? (
        <p className="small muted">Đang tải dòng thời gian…</p>
      ) : items.length === 0 ? (
        <p className="small muted">Chưa có sự kiện nào.</p>
      ) : (
        <ul className="tl" aria-label="Sự kiện vận đơn">
          {items.map((e, i) => (
            <EventRow key={e.id ?? `${e.recordedAt}-${String(i)}`} e={e} />
          ))}
        </ul>
      )}
      {more && (
        <button
          type="button"
          className="btn btn-quiet"
          disabled={q.isFetchingNextPage}
          onClick={() => void q.fetchNextPage()}
        >
          {q.isFetchingNextPage ? "Đang tải…" : "Xem thêm"}
        </button>
      )}
      {q.isFetchNextPageError && <div className="box box-err small">Chưa tải được trang sau. Bấm "Xem thêm" lại.</div>}
      <p className="hint">
        Nguồn: Hãng (API) = CRM hỏi hãng (sau tin webhook hoặc tra theo lịch; tin webhook chỉ là tín hiệu, không bao giờ
        tự đổi trạng thái); Nhân viên; Hệ thống. Sự kiện bị bỏ qua vẫn lưu, hiện nhạt kèm lý do.
      </p>
    </section>
  );
}

function HistoryRow({ s, current }: { s: Shipment; current: boolean }) {
  const name = carrierName(s.carrierKey, s.accountName ?? s.carrierKey);
  return (
    <li>
      <span className="at num">{s.code}</span>
      {s.status === "cancelled" ? (
        <div className="muted">{`${name} · ${cancelledText(s.cancelKind)} · ${shortTime(s.lastStatusAt ?? s.createdAt)}`}</div>
      ) : (
        <div>
          {`${name} · `}
          <b>{statusText(s)}</b>
          {current ? " (hiện hành)" : ""}
        </div>
      )}
    </li>
  );
}

/** Thẻ "Vận đơn của đơn này": mọi vận đơn của đơn o, cả đã hủy (đổi hãng). */
export function ShipmentHistory({ order: o }: { order: Order }) {
  // Đơn nháp, đơn mới chưa bao giờ có vận đơn: không đọc.
  const q = useOrderShipments(o.id, o.status !== "draft" && o.status !== "new");
  const items = q.data?.items ?? [];
  if (q.isPending || (q.isSuccess && items.length === 0)) return null; // isPending cả khi không đọc
  return (
    <section className="card" aria-label="Vận đơn của đơn này">
      <h2>Vận đơn của đơn này</h2>
      {q.isError ? (
        <div className="box box-err small">Chưa đọc được các vận đơn của đơn. Tải lại trang.</div>
      ) : (
        <ul className="tl" style={{ marginTop: 8 }}>
          {items.map((s) => (
            <HistoryRow key={s.id} s={s} current={s.id === o.currentShipmentId} />
          ))}
        </ul>
      )}
      {q.data?.nextCursor !== undefined && (
        <p className="hint">Chỉ hiện các vận đơn mới nhất; vận đơn cũ hơn xem ở danh sách vận đơn.</p>
      )}
    </section>
  );
}

/** Cột trái chi tiết đơn: dòng thời gian của vận đơn hiện hành và các vận đơn của đơn. */
export function OrderShipmentCards({ order: o }: { order: Order }) {
  return (
    <>
      {o.currentShipmentId !== undefined && <ShipmentTimeline id={o.currentShipmentId} />}
      <ShipmentHistory order={o} />
    </>
  );
}
