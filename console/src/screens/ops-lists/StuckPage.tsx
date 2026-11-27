// Đơn kẹt /ops/stuck (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html trạng thái D; VC16). Vận đơn hãng lâu
// không cập nhật quá ngưỡng của từng trạng thái, hoặc lệnh gửi hãng không chạy xong. "Đồng bộ lại" tra hãng ngay (một
// việc mỗi phút mỗi vận đơn); "Mở đơn" sang trang đơn.
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { listStuckShipments, syncShipment, type StuckShipment } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../shared";
import { carrierName } from "../shipping/QuoteTable";
import { statusText, syncErrorText } from "../shipping/shipmentRules";
import { clock, dayMonth } from "./opsRules";
import { headers, reloadOps, stuckKey, useOpsPaged, usePerms } from "./opsData";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { ListState, OpsFrame } from "./OpsTabs";
import "./ops.css";

function SyncCell({ s }: { s: StuckShipment }) {
  if (s.status === "creating") return <span className="small">Lệnh gửi không chạy xong</span>;
  if (s.lastSyncedAt === undefined) return <span className="small muted">Chưa hỏi hãng</span>;
  const err = s.lastSyncError === undefined || s.lastSyncError === "" ? "đạt" : syncErrorText(s.lastSyncError);
  return <span className="small num">{`${clock(s.lastSyncedAt)} · ${dayMonth(s.lastSyncedAt)} · ${err}`}</span>;
}

function Row({
  s,
  canSync,
  onSync,
  busy,
}: {
  s: StuckShipment;
  canSync: boolean;
  onSync: (s: StuckShipment) => void;
  busy: boolean;
}) {
  const creating = s.status === "creating";
  return (
    <tr>
      <td className="lead">
        <span className="who-c">
          {s.orderId === undefined ? (
            <span className="num bold">{s.orderCode}</span>
          ) : (
            <Link className="num bold" to={`/don/${s.orderId}`}>
              {s.orderCode}
            </Link>
          )}
          <span className="xs muted num">
            {[carrierName(s.carrierKey, s.carrierKey), s.trackingCode ?? (creating ? "Chưa có mã hãng" : "")]
              .filter((x) => x !== "")
              .join(" · ")}
          </span>
        </span>
      </td>
      <td data-label="Trạng thái">
        <span className="chip">{statusText(s)}</span>
      </td>
      <td data-label="Không cập nhật" className="num">{`${String(s.hoursSinceUpdate)} giờ`}</td>
      <td data-label="Lần CRM hỏi hãng">
        <SyncCell s={s} />
      </td>
      <td className="c-act">
        {canSync && !creating && (
          <button
            type="button"
            className="btn btn-line"
            disabled={busy}
            onClick={() => {
              onSync(s);
            }}
          >
            Đồng bộ lại
          </button>
        )}
        {s.orderId !== undefined && (
          <Link className="btn btn-line" to={`/don/${s.orderId}`}>
            {creating ? "Mở đơn, xử lý" : "Mở đơn"}
          </Link>
        )}
      </td>
    </tr>
  );
}

export function StuckPage() {
  const qc = useQueryClient();
  const perms = usePerms();
  const paged = useOpsPaged(stuckKey(), (query) => unwrap(listStuckShipments({ query })));
  const { list } = paged;
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const sync = useWriteAction((key, id: string) => unwrap(syncShipment({ path: { id }, headers: headers(key) })));
  const failed = list.isError && list.data === undefined ? errorMessage(list.error) : null;
  const onSync = (s: StuckShipment) => {
    void sync.run(s.shipmentId).then(() => {
      reloadOps(qc);
    });
  };
  return (
    <OpsFrame
      title="Đơn kẹt"
      sub="Hãng lâu không cập nhật quá ngưỡng, hoặc lệnh gửi hãng không chạy xong; lâu nhất đứng đầu."
    >
      <ListState
        pending={list.isPending}
        error={failed}
        empty={!list.isPending && failed === null && rows.length === 0}
        emptyTitle="Không có đơn kẹt"
        emptyText="Vận đơn nào hãng lâu không cập nhật sẽ vào đây."
      />
      {sync.message !== null && (
        <div className="box box-err small" role="alert">
          {sync.message}
        </div>
      )}
      {rows.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl ops-t">
            <thead>
              <tr>
                <th>Đơn · vận đơn</th>
                <th>Trạng thái</th>
                <th>Không cập nhật</th>
                <th>Lần CRM hỏi hãng</th>
                <th className="c-act" />
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <Row
                  key={s.shipmentId}
                  s={s}
                  canSync={perms.can("fulfillment.read")}
                  onSync={onSync}
                  busy={sync.pending}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && <MoreBar text={`${String(rows.length)} vận đơn đã tải`} pages={list} />}
      <p className="hint">
        Ngưỡng theo trạng thái ở Cài đặt vận chuyển. Mỗi ngày một cảnh báo Telegram chỉ mang số lượng, không có tên
        khách.
      </p>
    </OpsFrame>
  );
}
