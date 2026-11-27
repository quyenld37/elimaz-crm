// Hộp xử lý việc "Cần xem": "Đã xử lý · DH…" đòi ghi cách xử lý (resolveShipmentAnomaly; 409 với hủy tay/không rõ đã hủy
// → hiện câu lỗi, xử lý ở đơn) và "Đã tạo"/"Chưa tạo" của loại "Không rõ đã tạo" (resolveShipmentCreate; "Đã tạo" đòi mã
// hãng). Sau khi ghi tải lại mọi danh sách (đóng anomaly không phát shipment.changed, Task 111).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { resolveShipmentAnomaly, resolveShipmentCreate, type ShipmentReviewItem } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../shared";
import { DialogActions, DialogNotes } from "../shipping/dialogParts";
import { Dialog } from "../staff/staffData";
import { headers, reloadOps } from "./opsData";

function Reason({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  return (
    <label className="field">
      <span>{label}</span>
      <textarea
        className="input"
        rows={3}
        maxLength={1000}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
    </label>
  );
}

export function ResolveAnomalyDialog({ item, onClose }: { item: ShipmentReviewItem; onClose: () => void }) {
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const write = useWriteAction((key, resolution: string) =>
    unwrap(resolveShipmentAnomaly({ path: { id: item.anomalyId }, body: { resolution }, headers: headers(key) })),
  );
  const why = text.trim() === "" ? "Ghi cách đã xử lý" : null;
  const submit = async () => {
    const r = await write.run(text.trim());
    reloadOps(qc);
    if (r.ok) onClose();
  };
  return (
    <Dialog
      title={`Đã xử lý · ${item.orderCode}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions label="Lưu" why={why} pending={write.pending} onSubmit={() => void submit()} onClose={onClose} />
      }
    >
      <Reason value={text} onChange={setText} label="Đã làm gì *" />
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}

interface CreateReq {
  result: "created" | "not_created";
  trackingCode?: string;
  reason: string;
}

export function ResolveCreateDialog(props: {
  item: ShipmentReviewItem;
  result: "created" | "not_created";
  onClose: () => void;
}) {
  const { item, result, onClose } = props;
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const [text, setText] = useState("");
  const write = useWriteAction((key, b: CreateReq) =>
    unwrap(resolveShipmentCreate({ path: { id: item.shipmentId }, body: b, headers: headers(key) })),
  );
  const why =
    result === "created" && code.trim() === ""
      ? "Nhập mã vận đơn của hãng"
      : text.trim() === ""
        ? "Ghi đã kiểm ở đâu"
        : null;
  const submit = async () => {
    const body: CreateReq = { result, reason: text.trim() };
    if (result === "created") body.trackingCode = code.trim();
    const r = await write.run(body);
    reloadOps(qc);
    if (r.ok) onClose();
  };
  return (
    <Dialog
      title={`${result === "created" ? "Hãng đã tạo" : "Hãng chưa tạo"} · ${item.orderCode}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label={result === "created" ? "Ghi đã tạo" : "Ghi chưa tạo, gửi lại"}
          why={why}
          pending={write.pending}
          onSubmit={() => void submit()}
          onClose={onClose}
        />
      }
    >
      {result === "created" && (
        <label className="field">
          <span>Mã vận đơn của hãng *</span>
          <input
            className="input"
            maxLength={64}
            autoComplete="off"
            value={code}
            onChange={(e) => {
              setCode(e.target.value);
            }}
          />
        </label>
      )}
      <Reason value={text} onChange={setText} label="Đã kiểm ở đâu *" />
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
