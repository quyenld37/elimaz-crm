// Đợt mã (mockup ma-giam-gia.html trạng thái C): hộp "Tạo đợt mã" (tên, tiền tố, số mã, điều khoản chung) và thẻ tiến
// độ trên danh sách. Máy chủ sinh cả đợt trong một yêu cầu (createCouponBatch, một transaction), không báo tiến độ giữa
// chừng: lúc đang gửi thẻ hiện thanh chạy, xong hiện "n / n" và nút tải danh sách mã (CSV, tải theo trang 100 mã).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { createCouponBatch, type CouponBatch, type CreateCouponBatchRequest } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog, RefusalBox } from "../staff/staffData";
import { batchCodes, headers, reloadCoupons, saveTextFile } from "./couponData";
import { CreateFooter, MAX_NAME, TextField, discountRow, termFields } from "./CouponForm";
import {
  EMPTY_TERMS,
  MAX_BATCH,
  PREFIX_PATTERN,
  cleanPrefix,
  codesCsv,
  readTerms,
  type TermsDraft,
  type TermsErrors,
} from "./couponRules";
import "../imports/imports.css";

const NUM = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });

type BatchErrors = TermsErrors & { name?: string; prefix?: string; quantity?: string };

/** Hộp "Tạo đợt mã": kiểm ô rồi trao thân gửi cho onSubmit (thẻ tiến độ của danh sách gửi và theo dõi). */
export function BatchDialog({
  onClose,
  onSubmit,
}: {
  onClose: () => void;
  onSubmit: (b: CreateCouponBatchRequest) => void;
}) {
  const [name, setName] = useState("");
  const [prefix, setPrefix] = useState("");
  const [quantity, setQuantity] = useState("");
  const [draft, setDraft] = useState<TermsDraft>(EMPTY_TERMS);
  const [errors, setErrors] = useState<BatchErrors>({});
  const f = termFields({
    draft,
    set: (p) => {
      setDraft((d) => ({ ...d, ...p }));
    },
    errors,
  });
  const qty = /^\d{1,7}$/.test(quantity.trim()) ? Number(quantity.trim()) : null;

  const submit = () => {
    const r = readTerms(draft);
    const e: BatchErrors = { ...r.errors };
    const p = cleanPrefix(prefix);
    if (name.trim() === "") e.name = "Điền tên đợt";
    if (!PREFIX_PATTERN.test(p)) e.prefix = "Chữ in hoa và số, tối đa 8 ký tự";
    if (qty === null || qty < 1 || qty > MAX_BATCH) e.quantity = "Từ 1 đến 100.000 mã";
    setErrors(e);
    if (r.terms === null || qty === null || Object.keys(e).length > 0) return;
    // Mẫu của đợt (CouponTemplate) không có tổng lượt, trả lượt: mỗi mã dùng một lần.
    const t = r.terms;
    onSubmit({
      name: name.trim(),
      prefix: p,
      quantity: qty,
      template: {
        name: name.trim(),
        discountKind: t.discountKind,
        value: t.value,
        ...(t.maxDiscount === undefined ? {} : { maxDiscount: t.maxDiscount }),
        minSubtotal: t.minSubtotal,
        channels: t.channels,
        validFrom: t.validFrom,
        validTo: t.validTo,
      },
    });
  };

  return (
    <Dialog
      title="Tạo đợt mã"
      className="dlg-coupon"
      onClose={onClose}
      footer={
        <CreateFooter
          onClose={onClose}
          onSubmit={submit}
          busy={false}
          label={qty === null ? "Tạo mã" : `Tạo ${NUM.format(qty)} mã`}
        />
      }
    >
      <div className="stack">
        <TextField label="Tên đợt" error={errors.name} maxLength={MAX_NAME} value={name} onChange={setName} />
        <div className="grid-f">
          <TextField
            label="Tiền tố"
            hint="Chữ in hoa và số, tối đa 8; mã có dạng SN-7KQ2M9XA"
            error={errors.prefix}
            maxLength={12}
            value={prefix}
            onChange={setPrefix}
          />
          <TextField
            label="Số mã"
            hint="1–100.000; mỗi mã dùng một lần"
            error={errors.quantity}
            num
            value={quantity}
            onChange={setQuantity}
          />
        </div>
        <p className="small muted">Điều khoản dưới đây áp cho mọi mã của đợt.</p>
        {discountRow(f)}
        <div className="grid-f">
          {f.from}
          {f.to}
        </div>
        {f.channels}
      </div>
    </Dialog>
  );
}

/** Việc tạo đợt đang theo dõi trên danh sách: thân đã gửi và lệnh ghi (giữ khóa chống trùng cho lần gửi lại). */
export function useBatchJob() {
  const qc = useQueryClient();
  const [body, setBody] = useState<CreateCouponBatchRequest | null>(null);
  const [done, setDone] = useState<CouponBatch | null>(null);
  const act = useWriteAction((key, b: CreateCouponBatchRequest) =>
    unwrap(createCouponBatch({ body: b, headers: headers(key) })),
  );
  const start = async (b: CreateCouponBatchRequest) => {
    setBody(b);
    setDone(null);
    const r = await act.run(b);
    if (!r.ok) return;
    setDone(r.data);
    reloadCoupons(qc);
  };
  return { body, done, act, start };
}

export type BatchJob = ReturnType<typeof useBatchJob>;

/** Thẻ tiến độ của đợt vừa tạo; onShow lọc danh sách theo đợt. */
export function BatchProgress({ job, onShow }: { job: BatchJob; onShow: (batchId: string) => void }) {
  const [loaded, setLoaded] = useState<number | null>(null);
  const [dlError, setDlError] = useState<unknown>(null);
  const { body, done, act } = job;
  if (body === null) return null;
  const total = done?.quantity ?? body.quantity;
  const failed = !act.pending && done === null && act.error !== null;

  const download = async () => {
    if (done === null) return;
    setDlError(null);
    setLoaded(0);
    try {
      const codes = await batchCodes(done.id, setLoaded);
      saveTextFile(
        `ma-${done.prefix === "" ? "dot" : done.prefix}-${done.id.slice(0, 8)}.csv`,
        codesCsv(codes),
        "text/csv;charset=utf-8",
      );
    } catch (e) {
      setDlError(e);
    } finally {
      setLoaded(null);
    }
  };

  return (
    <div className="card stack-sm" style={{ marginBottom: 16 }}>
      <div className="row-between">
        <h2>{body.name}</h2>
        <span className="small muted num">
          {done === null ? `${NUM.format(total)} mã` : `${NUM.format(total)} / ${NUM.format(total)}`}
        </span>
      </div>
      {failed ? (
        <>
          <RefusalBox error={act.error} message={act.message} />
          <div className="row">
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                void job.start(body);
              }}
            >
              Gửi lại
            </button>
          </div>
        </>
      ) : (
        <>
          <div
            className={done === null ? "bar is-busy" : "bar"}
            role="progressbar"
            aria-label="Tiến độ sinh mã"
            {...(done === null ? {} : { "aria-valuenow": 100, "aria-valuemin": 0, "aria-valuemax": 100 })}
          >
            <i style={done === null ? undefined : { width: "100%" }} />
          </div>
          <p className="small muted">
            {done === null
              ? `Đang sinh ${NUM.format(total)} mã…`
              : `Đã sinh xong ${NUM.format(total)} mã, mỗi mã dùng một lần.`}
          </p>
          {done === null && <p className="small muted">Giữ trang này mở; xong sẽ có nút tải.</p>}
          <div className="row">
            <button
              type="button"
              className="btn btn-line"
              disabled={done === null || loaded !== null}
              onClick={() => {
                void download();
              }}
            >
              Tải danh sách mã (.csv)
            </button>
            {done !== null && (
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  onShow(done.id);
                }}
              >
                Xem các mã của đợt
              </button>
            )}
          </div>
          {done === null && <span className="reason">Có khi sinh xong</span>}
          {loaded !== null && (
            <p className="small muted num">{`Đang tải ${NUM.format(loaded)} / ${NUM.format(total)} mã`}</p>
          )}
          {dlError !== null && (
            <div className="box box-err" role="alert">
              {errorMessage(dlError)}
            </div>
          )}
        </>
      )}
    </div>
  );
}
