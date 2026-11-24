// Hộp thoại của màn Chiến dịch giảm giá (mockup docs/mockups/m5/web-campaigns.html trạng thái C, D; HV16, HV17):
// Kích hoạt (tự xem trước giá của revision hiện tại khi mở; nút khóa tới khi có bản xem trước và lý do; gửi đúng số đã
// thấy), Tạm dừng, Sửa khi đang chạy (tạm dừng rồi mở form), Xóa (lý do, cảnh báo banner). Lý do 1–300 ký tự.
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  deleteWebCampaign,
  previewWebCampaign,
  setWebCampaignState,
  type WebCampaign,
  type WebCampaignPreview,
  type WebCampaignRevision,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { formatMoney } from "../products/productRules";
import { Dialog } from "../staff/staffData";
import { headers, reloadCampaigns } from "./campaignData";
import { REASON_MAX, TEXT_RAISE_CAP, percentTooHighText, periodText, runeCount, scopeText } from "./campaignRules";

export type StateMode = "enable" | "pause" | "pauseEdit";

const TITLE: Readonly<Record<StateMode, string>> = { enable: "Kích hoạt", pause: "Tạm dừng", pauseEdit: "Sửa" };
const GO: Readonly<Record<StateMode, string>> = {
  enable: "Kích hoạt",
  pause: "Tạm dừng",
  pauseEdit: "Tạm dừng và sửa",
};

const isCode = (e: unknown, code: string) => e instanceof ApiError && e.code === code;
const reasonOk = (r: string) => {
  const n = runeCount(r.trim());
  return n >= 1 && n <= REASON_MAX;
};

function ReasonField(o: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const n = runeCount(o.value.trim());
  return (
    <div className="field">
      <label className="field">
        <span>Lý do *</span>
        <input
          className="input"
          value={o.value}
          maxLength={REASON_MAX + 100}
          placeholder={o.placeholder}
          onChange={(e) => {
            o.onChange(e.target.value);
          }}
        />
      </label>
      {n > REASON_MAX ? (
        <span className="field-err">Lý do tối đa 300 ký tự.</span>
      ) : (
        <span className="hint">1–300 ký tự, ghi vào nhật ký thao tác</span>
      )}
    </div>
  );
}

interface PreviewBody {
  id: string;
  revision: number;
}

/** Xem trước giá của revision đang có (ghi số đếm ở máy chủ để bật). */
export function usePreview() {
  return useWriteAction((key: string, b: PreviewBody) =>
    unwrap(
      previewWebCampaign({
        path: { campaignId: b.id },
        body: { revision: b.revision } satisfies WebCampaignRevision,
        headers: headers(key),
      }),
    ),
  );
}

function PreviewSummary({ p }: { p: WebCampaignPreview }) {
  const s = p.samples[0];
  return (
    <div className="box box-info">
      <b>{`${String(p.modelCount)} mẫu · ${String(p.variantCount)} biến thể`}</b> sẽ đổi giá trên website trong khoảng
      thời gian đã đặt.
      {s !== undefined && (
        <>
          {` Ví dụ: ${s.productName} `}
          <span className="money">{formatMoney(s.listPrice)}</span> →{" "}
          <span className="money">{formatMoney(s.salePrice)}</span>.
        </>
      )}
    </div>
  );
}

interface StateBody {
  id: string;
  revision: number;
  state: "enabled" | "paused";
  reason: string;
  preview?: { revision: number; modelCount: number; variantCount: number };
}

/** Hộp Kích hoạt, Tạm dừng, Sửa khi đang chạy; onDone nhận chiến dịch sau khi đổi. */
export function StateDialog(o: {
  campaign: WebCampaign;
  mode: StateMode;
  onDone: (c: WebCampaign) => void;
  onClose: () => void;
}) {
  const { campaign: c, mode } = o;
  const qc = useQueryClient();
  const [reason, setReason] = useState("");
  const [seen, setSeen] = useState<WebCampaignPreview | null>(null);
  const preview = usePreview();
  const setState = useWriteAction((key: string, b: StateBody) => {
    const { id, ...body } = b;
    return unwrap(setWebCampaignState({ path: { campaignId: id }, body, headers: headers(key) }));
  });
  const loadPreview = () => {
    setSeen(null);
    setState.reset();
    void preview.run({ id: c.id, revision: c.revision }).then((r) => {
      if (r.ok) setSeen(r.data);
    });
  };
  // Kích hoạt: xem trước lại trên danh mục hiện tại ngay khi mở (mockup C). run ổn định (useCallback); giữ qua ref
  // để effect chỉ chạy lại khi đổi chiến dịch, revision.
  const previewRef = useRef(preview);
  useEffect(() => {
    previewRef.current = preview;
  });
  useEffect(() => {
    if (mode !== "enable") return;
    void previewRef.current.run({ id: c.id, revision: c.revision }).then((r) => {
      if (r.ok) setSeen(r.data);
    });
  }, [mode, c.id, c.revision]);

  const needPreview = isCode(setState.error, "preview_required");
  const tooHigh = isCode(setState.error, "percent_too_high");
  const ready = mode !== "enable" || (seen !== null && !needPreview);
  const busy = preview.pending || setState.pending;

  const go = () => {
    void setState
      .run({
        id: c.id,
        revision: c.revision,
        state: mode === "enable" ? "enabled" : "paused",
        reason: reason.trim(),
        ...(mode === "enable" && seen !== null
          ? { preview: { revision: seen.revision, modelCount: seen.modelCount, variantCount: seen.variantCount } }
          : {}),
      })
      .then((r) => {
        if (r.ok) o.onDone(r.data);
        else if (isCode(r.error, "version_conflict")) reloadCampaigns(qc);
      });
  };

  return (
    <Dialog
      title={`${TITLE[mode]}: ${c.name}`}
      className="wc-dlg"
      closeButton
      onClose={o.onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={o.onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-dark" disabled={!ready || !reasonOk(reason) || busy} onClick={go}>
            {GO[mode]}
          </button>
        </>
      }
    >
      {mode === "enable" && (
        <>
          <p>
            <b>{`Giảm ${String(c.percent)}%`}</b>
            {` · ${scopeText(c.scope, c.targetIds.length)}${c.scope === "all" ? "" : " được chọn"} · ${periodText(c)}`}
          </p>
          {preview.pending && <p className="small muted">Đang xem trước giá…</p>}
          {seen !== null && !needPreview && <PreviewSummary p={seen} />}
          {preview.message !== null && (
            <div className="box box-err stack-sm">
              <p>{preview.message}</p>
              <div>
                <button type="button" className="btn btn-line" disabled={busy} onClick={loadPreview}>
                  Xem trước lại
                </button>
              </div>
            </div>
          )}
          {needPreview && (
            <div className="box box-err stack-sm">
              <b>Danh sách sản phẩm đã đổi kể từ lúc xem trước.</b>
              <p>Xem trước lại rồi mới kích hoạt.</p>
              <div>
                <button type="button" className="btn btn-line" disabled={busy} onClick={loadPreview}>
                  Xem trước lại
                </button>
              </div>
            </div>
          )}
          {tooHigh && (
            <div className="box box-err stack-sm">
              <b>{percentTooHighText(c.percent)}</b>
              <p>{TEXT_RAISE_CAP}</p>
            </div>
          )}
        </>
      )}
      {mode === "pause" && <p>Giá trên website trở về giá gốc ngay. Đơn đã đặt giữ giá lúc đặt.</p>}
      {mode === "pauseEdit" && (
        <p>
          Chiến dịch đang chạy. <b>Tạm dừng để sửa</b>: trong lúc sửa, giá trên website trở về giá gốc. Sửa xong bấm
          Kích hoạt để chạy lại.
        </p>
      )}
      <ReasonField
        value={reason}
        onChange={setReason}
        placeholder={mode === "pause" ? "Ví dụ: đổi phạm vi trước ngày chạy" : undefined}
      />
      {setState.message !== null && !needPreview && !tooHigh && <div className="box box-err">{setState.message}</div>}
    </Dialog>
  );
}

interface DeleteBody {
  id: string;
  revision: number;
  reason: string;
}

/** Hộp Xóa chiến dịch (không đang bật): lý do bắt buộc; banner trỏ chiến dịch tự ẩn (tệp task 458, Quyết định 5). */
export function DeleteDialog(o: { campaign: WebCampaign; onDone: () => void; onClose: () => void }) {
  const { campaign: c } = o;
  const qc = useQueryClient();
  const [reason, setReason] = useState("");
  const del = useWriteAction((key: string, b: DeleteBody) =>
    unwrap(
      deleteWebCampaign({
        path: { campaignId: b.id },
        body: { revision: b.revision, reason: b.reason },
        headers: headers(key),
      }),
    ),
  );
  const go = () => {
    void del.run({ id: c.id, revision: c.revision, reason: reason.trim() }).then((r) => {
      if (r.ok) o.onDone();
      else if (isCode(r.error, "version_conflict")) reloadCampaigns(qc);
    });
  };
  return (
    <Dialog
      title={`Xóa chiến dịch "${c.name}"?`}
      className="wc-dlg"
      closeButton
      onClose={o.onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={o.onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-danger" disabled={!reasonOk(reason) || del.pending} onClick={go}>
            Xóa chiến dịch
          </button>
        </>
      }
    >
      <p>Chiến dịch biến mất khỏi danh sách. Đơn đã đặt vẫn giữ giá lúc đặt; nhật ký thao tác vẫn lưu.</p>
      <div className="box box-soft">
        <b>Banner trang chủ nào đang dùng chiến dịch này</b> sẽ tự ẩn (&quot;Chưa chọn chiến dịch, hoặc chiến dịch đã bị
        xóa&quot;).
      </div>
      <ReasonField value={reason} onChange={setReason} />
      {del.message !== null && <div className="box box-err">{del.message}</div>}
    </Dialog>
  );
}
