// Hộp kết quả "In nhãn" (mockup docs/mockups/m3/kho.html trạng thái B; HV23): mỗi lô một tài khoản hãng, một tệp PDF
// của hãng; đọc trạng thái lô tới khi ready/failed (báo hiệu label_batch.ready, thêm đọc lại mỗi 3 giây phòng mất báo
// hiệu); lô lỗi có "Thử lại lô …" (in lại đúng các vận đơn của tài khoản đó); vận đơn bị bỏ qua (tự giao, nhập tay
// không có nhãn hãng) có câu riêng. In không đổi cột "Đóng gói" (máy chủ, AT08).
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import {
  createLabelBatch,
  getLabelBatch,
  listCarrierAccounts,
  type LabelBatchCreated,
  type LabelBatchSummary,
  type OrderListItem,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { Dialog } from "../staff/staffData";
import { carrierName, safeFileUrl, skippedMessages } from "./warehouseRules";

/** Chủ đề báo hiệu lô nhãn xong (máy chủ: fulfillment signalLabelBatch). */
export const LABEL_BATCH_TOPIC = "label_batch.ready";
const POLL_MS = 3_000;
/** Liên kết ký sẵn sống 5 phút: đọc lại lô ready trước khi hết hạn để nút luôn mở được. */
const READY_REFRESH_MS = 240_000;

function sendRetry(key: string, shipmentIds: string[]): Promise<LabelBatchCreated> {
  return unwrap(createLabelBatch({ body: { shipmentIds }, headers: { "Idempotency-Key": key } }));
}

function BatchRow({
  batch,
  name,
  retrying,
  onRetry,
}: {
  batch: LabelBatchSummary;
  name: string;
  retrying: boolean;
  onRetry: () => void;
}) {
  const q = useQuery({
    queryKey: queryKey(LABEL_BATCH_TOPIC, batch.id),
    queryFn: () => unwrap(getLabelBatch({ path: { id: batch.id } })),
    meta: { realtimeEntity: batch.id },
    refetchInterval: (query) => {
      const st = query.state.data?.status;
      return st === undefined || st === "pending" ? POLL_MS : st === "ready" ? READY_REFRESH_MS : false;
    },
  });
  const count = `${String(batch.count)} vận đơn`;
  if (q.data?.status === "failed") {
    return (
      <div className="box box-err small">
        <b>{`${name}: hãng không trả nhãn.`}</b> Thử lại sau ít phút; các lô khác không ảnh hưởng.{" "}
        <button type="button" className="btn btn-line" style={{ marginTop: 8 }} disabled={retrying} onClick={onRetry}>
          {`Thử lại lô ${name}`}
        </button>
      </div>
    );
  }
  const url = q.data?.status === "ready" ? safeFileUrl(q.data.url) : null;
  return (
    <div className="oitem lb-item">
      <span className="grow">
        <b>{name}</b>
        <br />
        {url !== null ? (
          <span className="small">{`${count} · sẵn sàng`}</span>
        ) : q.isError ? (
          <span className="small field-err">{errorMessage(q.error)}</span>
        ) : (
          <span className="small muted">{`${count} · đang lấy nhãn từ hãng…`}</span>
        )}
      </span>
      {url !== null ? (
        <a className="btn btn-dark" href={url} target="_blank" rel="noopener noreferrer">
          Mở tệp nhãn (PDF)
        </a>
      ) : (
        <button type="button" className="btn btn-line" disabled>
          Mở tệp nhãn
        </button>
      )}
    </div>
  );
}

export interface LabelBatchDialogProps {
  created: LabelBatchCreated;
  /** Các đơn đã chọn khi bấm In nhãn (để biết mã đơn, hãng, tài khoản của từng vận đơn). */
  rows: readonly OrderListItem[];
  canReadAccounts: boolean;
  onClose: () => void;
}

export function LabelBatchDialog({ created, rows, canReadAccounts, onClose }: LabelBatchDialogProps) {
  const [batches, setBatches] = useState<readonly LabelBatchSummary[]>(created.batches);
  const retry = useWriteAction(sendRetry);
  const accounts = useQuery({
    queryKey: queryKey("fulfillment.carrier_accounts", "list"),
    queryFn: () => unwrap(listCarrierAccounts({ query: { limit: 100 } })),
    enabled: canReadAccounts,
  });
  const byShipment = new Map(rows.flatMap((r) => (r.shipment?.id === undefined ? [] : [[r.shipment.id, r] as const])));
  const skippedIds = new Set(created.skipped.map((s) => s.shipmentId));
  const nameOf = (accountId: string) => {
    const acc = accounts.data?.items.find((a) => a.id === accountId);
    if (acc !== undefined) return acc.name;
    const row = rows.find((r) => r.shipment?.carrierAccountId === accountId);
    return carrierName(row?.shipment?.carrierKey ?? "");
  };
  const skipped = skippedMessages(
    created.skipped,
    (id) => byShipment.get(id)?.shipment?.carrierKey ?? "",
    (id) => byShipment.get(id)?.code ?? id,
  );

  const runRetry = async (b: LabelBatchSummary) => {
    const ids = rows.flatMap((r) =>
      r.shipment?.id !== undefined &&
      r.shipment.carrierAccountId === b.carrierAccountId &&
      !skippedIds.has(r.shipment.id)
        ? [r.shipment.id]
        : [],
    );
    if (ids.length === 0) return;
    const res = await retry.run(ids);
    if (!res.ok) return;
    setBatches((cur) => [...cur.filter((x) => x.carrierAccountId !== b.carrierAccountId), ...res.data.batches]);
  };

  return (
    <Dialog
      title={`In nhãn · ${String(rows.length)} vận đơn`}
      onClose={onClose}
      footer={
        <button type="button" className="btn btn-quiet" onClick={onClose}>
          Đóng
        </button>
      }
    >
      {batches.map((b) => (
        <BatchRow
          key={b.id}
          batch={b}
          name={nameOf(b.carrierAccountId)}
          retrying={retry.pending}
          onRetry={() => void runRetry(b)}
        />
      ))}
      {skipped.length > 0 && (
        <div className="box box-info small">
          {skipped.map((t) => (
            <b key={t} style={{ display: "block" }}>
              {t}
            </b>
          ))}
          In phiếu nhặt để dán mã đơn lên gói.
        </div>
      )}
      {batches.length === 0 && skipped.length === 0 && <p className="muted">Không có vận đơn nào để in.</p>}
      {retry.message !== null && <div className="box box-err">{retry.message}</div>}
      <p className="hint">
        Nhãn đúng mẫu của hãng; không ghép tệp giữa các hãng. Liên kết tệp hết hạn sau 5 phút, mở lại thì lấy liên kết
        mới. In lại không đổi cột &quot;Đóng gói&quot;.
      </p>
    </Dialog>
  );
}
