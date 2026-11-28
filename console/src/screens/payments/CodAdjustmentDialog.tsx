// Điều chỉnh COD hai bước (kế hoạch M3 Task 138; mockup docs/mockups/m3/tien-tren-don.html trạng thái C; HV32, HV35):
// bước 1 chọn cách (đảo một bút toán đã ghi / chuyển tiền giữa hai ngăn), số, lý do → previewCodAdjustment (không đổi
// dữ liệu); bước 2 số dư trước/sau của các ngăn đổi, cảnh báo (ngăn âm vẫn cho xác nhận) → createCodAdjustment gửi
// token của bản xem trước. 409 preview_stale (sổ vừa đổi, ví dụ bảng kê vừa chốt) → tự xem trước lại, hiện bản mới
// kèm câu TEXT_NEW_PREVIEW. Cần cash.adjust (máy chủ kiểm). Không xóa bút toán cũ.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  createCodAdjustment,
  previewCodAdjustment,
  type CodAccount,
  type CodAdjustmentPreview,
  type CodAdjustmentRequest,
  type CodBalances,
  type CodTransfer,
  type Shipment,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { parseMoney } from "../products/editRules";
import { formatMoney } from "../products/productRules";
import { DialogNotes } from "../shipping/dialogParts";
import { SHIPMENT_TOPIC, shipmentKey, useShipmentCod } from "../shipping/useShipment";
import { Dialog } from "../staff/staffData";
import { ACCOUNTS, ACCOUNT_TEXT, TEXT_NEW_PREVIEW } from "./paymentRules";
import { reversible, transferText } from "./codLedger";

const BAL_KEY: Record<CodAccount, keyof CodBalances> = {
  to_collect: "toCollect",
  carrier_holding: "carrierHolding",
  shop_received: "shopReceived",
  carrier_fee: "carrierFee",
  sales_clearing: "salesClearing",
};

const signed = (v: number) => (v < 0 ? `−${formatMoney(-v)}` : formatMoney(v));

export interface CodAdjustmentDialogProps {
  s: Shipment;
  orderCode: string;
  onClose: () => void;
}

interface Form {
  mode: CodAdjustmentRequest["mode"];
  transferId: string;
  from: CodAccount;
  to: CodAccount;
  amount: string;
  reason: string;
}

/** Yêu cầu của form f (null kèm lý do khi chưa đủ). */
function toRequest(shipmentId: string, f: Form): { req: CodAdjustmentRequest | null; why: string | null } {
  const reason = f.reason.trim();
  if (f.mode === "reverse") {
    if (f.transferId === "") return { req: null, why: "Chọn bút toán cần đảo" };
    if (reason === "") return { req: null, why: "Ghi lý do điều chỉnh" };
    return { req: { shipmentId, mode: "reverse", reversesTransferId: f.transferId, reason }, why: null };
  }
  const n = parseMoney(f.amount);
  if (f.from === f.to) return { req: null, why: "Hai ngăn phải khác nhau" };
  if (n === null || n <= 0) return { req: null, why: "Nhập số tiền (đồng)" };
  if (reason === "") return { req: null, why: "Ghi lý do điều chỉnh" };
  return { req: { shipmentId, mode: "move", from: f.from, to: f.to, amount: n, reason }, why: null };
}

function Step1({ f, set, transfers }: { f: Form; set: (p: Partial<Form>) => void; transfers: readonly CodTransfer[] }) {
  const options = reversible(transfers);
  return (
    <>
      <div className="radios" role="radiogroup" aria-label="Cách điều chỉnh">
        {(
          [
            ["reverse", "Đảo một bút toán đã ghi"],
            ["move", "Chuyển tiền giữa hai ngăn"],
          ] as const
        ).map(([m, text]) => (
          <button
            key={m}
            type="button"
            role="radio"
            className="toggle"
            aria-checked={f.mode === m}
            aria-pressed={f.mode === m}
            onClick={() => {
              set({ mode: m });
            }}
          >
            {text}
          </button>
        ))}
      </div>
      {f.mode === "reverse" ? (
        <label className="field">
          <span>Bút toán cần đảo *</span>
          <span className="select-wrap">
            <select
              aria-label="Bút toán cần đảo"
              value={f.transferId}
              onChange={(e) => {
                set({ transferId: e.target.value });
              }}
            >
              <option value="">Chọn bút toán</option>
              {options.map((t) => (
                <option key={t.id} value={t.id}>
                  {transferText(t)}
                </option>
              ))}
            </select>
          </span>
        </label>
      ) : (
        <>
          {(["from", "to"] as const).map((k) => (
            <label key={k} className="field">
              <span>{k === "from" ? "Từ ngăn" : "Sang ngăn"}</span>
              <span className="select-wrap">
                <select
                  aria-label={k === "from" ? "Từ ngăn" : "Sang ngăn"}
                  value={f[k]}
                  onChange={(e) => {
                    const v = ACCOUNTS.find((a) => a === e.target.value);
                    if (v !== undefined) set(k === "from" ? { from: v } : { to: v });
                  }}
                >
                  {ACCOUNTS.map((a) => (
                    <option key={a} value={a}>
                      {ACCOUNT_TEXT[a]}
                    </option>
                  ))}
                </select>
              </span>
            </label>
          ))}
          <label className="field">
            <span>Số tiền *</span>
            <input
              className="input"
              inputMode="numeric"
              aria-label="Số tiền"
              value={f.amount}
              onChange={(e) => {
                set({ amount: e.target.value });
              }}
            />
          </label>
        </>
      )}
      <label className="field">
        <span>Lý do *</span>
        <input
          className="input"
          maxLength={500}
          aria-label="Lý do"
          value={f.reason}
          onChange={(e) => {
            set({ reason: e.target.value });
          }}
        />
      </label>
    </>
  );
}

function Step2({ p, fresh }: { p: CodAdjustmentPreview; fresh: boolean }) {
  const changed = ACCOUNTS.filter((a) => p.before[BAL_KEY[a]] !== p.after[BAL_KEY[a]]);
  return (
    <>
      {fresh && (
        <div className="box box-soft small" role="status">
          {TEXT_NEW_PREVIEW} Xem lại trước khi xác nhận.
        </div>
      )}
      <div className="tbl-frame">
        <table className="tbl">
          <thead>
            <tr>
              <th>Ngăn</th>
              <th className="r">Trước</th>
              <th className="r">Sau</th>
            </tr>
          </thead>
          <tbody>
            {changed.map((a) => (
              <tr key={a}>
                <td className="lead">{ACCOUNT_TEXT[a]}</td>
                <td data-label="Trước" className="r">
                  <span className="money">{signed(p.before[BAL_KEY[a]])}</span>
                </td>
                <td data-label="Sau" className="r">
                  <span className="money">{signed(p.after[BAL_KEY[a]])}</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {p.warnings.map((w) => (
        <div key={w} className="box box-err small">
          {`${w}. Kiểm lại số tiền; vẫn xác nhận được nếu đúng.`}
        </div>
      ))}
      <p className="hint">Ghi một bút toán điều chỉnh hai vế, kèm lý do và người làm. Không xóa bút toán cũ.</p>
    </>
  );
}

export function CodAdjustmentDialog({ s, orderCode, onClose }: CodAdjustmentDialogProps) {
  const qc = useQueryClient();
  const cod = useShipmentCod(s.id, true);
  const [f, setF] = useState<Form>({
    mode: "reverse",
    transferId: "",
    from: "carrier_holding",
    to: "carrier_fee",
    amount: "",
    reason: "",
  });
  const [preview, setPreview] = useState<{ p: CodAdjustmentPreview; req: CodAdjustmentRequest } | null>(null);
  const [fresh, setFresh] = useState(false);
  const look = useWriteAction((key, req: CodAdjustmentRequest) =>
    unwrap(previewCodAdjustment({ body: req, headers: { "Idempotency-Key": key } })),
  );
  const save = useWriteAction((key, b: CodAdjustmentRequest & { token: string }) =>
    unwrap(createCodAdjustment({ body: b, headers: { "Idempotency-Key": key } })),
  );
  const { req, why } = toRequest(s.id, f);

  const runPreview = async (r: CodAdjustmentRequest, isFresh: boolean) => {
    const res = await look.run(r);
    if (!res.ok) return;
    setPreview({ p: res.data, req: r });
    setFresh(isFresh);
  };
  const confirm = async () => {
    if (preview === null) return;
    const res = await save.run({ ...preview.req, token: preview.p.token });
    if (res.ok) {
      void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
      void qc.invalidateQueries({ queryKey: queryKey(SHIPMENT_TOPIC, "cod", s.id) });
      onClose();
      return;
    }
    if (res.error instanceof ApiError && res.error.code === "preview_stale") {
      save.reset();
      await runPreview(preview.req, true);
    }
  };
  const busy = look.pending || save.pending;
  const where = `${s.accountName ?? s.carrierKey} ${s.trackingCode ?? s.code}`;
  return (
    <Dialog
      title={`Điều chỉnh COD · ${orderCode}`}
      closeButton
      onClose={onClose}
      footer={
        preview === null ? (
          <>
            <button type="button" className="btn btn-quiet" onClick={onClose}>
              Đóng
            </button>
            <button
              type="button"
              className="btn btn-dark"
              disabled={req === null || busy}
              onClick={() => {
                if (req !== null) void runPreview(req, false);
              }}
            >
              Xem trước
            </button>
          </>
        ) : (
          <>
            <button
              type="button"
              className="btn btn-quiet"
              disabled={busy}
              onClick={() => {
                setPreview(null);
                setFresh(false);
                save.reset();
              }}
            >
              Quay lại
            </button>
            <button type="button" className="btn btn-dark" disabled={busy} onClick={() => void confirm()}>
              Xác nhận điều chỉnh
            </button>
          </>
        )
      }
    >
      {preview === null ? (
        <>
          <p className="small muted">{`Bước 1/2 · ${where}`}</p>
          {cod.isError ? (
            <div className="box box-err small">{errorMessage(cod.error)}</div>
          ) : (
            <Step1
              f={f}
              set={(p) => {
                setF((x) => ({ ...x, ...p }));
              }}
              transfers={cod.data?.transfers ?? []}
            />
          )}
          <DialogNotes why={why} message={look.message} />
        </>
      ) : (
        <>
          <p className="small muted">{`Bước 2/2 · chưa ghi gì · ${where}`}</p>
          <Step2 p={preview.p} fresh={fresh} />
          <DialogNotes why={null} message={save.message ?? look.message} />
        </>
      )}
    </Dialog>
  );
}
