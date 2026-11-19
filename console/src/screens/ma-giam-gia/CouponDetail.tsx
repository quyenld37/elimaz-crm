// Chi tiết mã /ma-giam-gia/:id (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/ma-giam-gia.html trạng thái D, E).
// Mã đã có lượt dùng (usedCount > 0, hoặc có lượt dù đã trả: máy chủ coi là đã dùng, Q-T71-4) chỉ đổi được tên, hạn
// cuối, tổng lượt (không dưới số đã dùng) và bật/tắt; ô khác khóa và chỉ gửi bốn trường đó (coupon_locked_after_use).
// API chưa cho bỏ trống một mức đã đặt (giảm tối đa, tổng lượt, lượt mỗi khách): ô đó không để trống được.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";
import { updateCoupon, type Coupon, type UpdateCouponRequest } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { formatMoney } from "../products/productRules";
import { Dialog, RefusalBox } from "../staff/staffData";
import { headers, reloadCoupons, useCoupon, usePerms, useRedemptions } from "./couponData";
import { MoreBar } from "./MoreBar";
import { MAX_NAME, TEXT_NEED_NAME, TextField, discountRow, termFields } from "./CouponForm";
import { STATE_CHIP, couponState, readTerms, termsOf, type TermsDraft, type TermsErrors } from "./couponRules";
import "../don/don.css";
import "./ma-giam-gia.css";

const TEXT_CONFLICT = "Người khác vừa sửa mã này, tải lại";
const TEXT_CANT_CLEAR = "Mã đã đặt mức này, không bỏ trống được";

type Errors = TermsErrors & { name?: string };

function usesLine(c: Coupon): string {
  if (c.maxUses === undefined) return `${String(c.usedCount)} lượt đã dùng · không giới hạn`;
  return `${String(c.usedCount)} / ${String(c.maxUses)} lượt · còn ${String(c.remainingUses ?? Math.max(c.maxUses - c.usedCount, 0))}`;
}

/** Ô đã có giá trị trên mã mà bị xóa trắng: API không có cách bỏ một mức đã đặt. */
function clearedErrors(c: Coupon, d: TermsDraft): Errors {
  const e: Errors = {};
  if (c.maxDiscount !== undefined && d.kind === "percent" && d.maxDiscount.trim() === "")
    e.maxDiscount = TEXT_CANT_CLEAR;
  if (c.maxUses !== undefined && d.maxUses.trim() === "") e.maxUses = TEXT_CANT_CLEAR;
  if (c.perCustomerLimit !== undefined && d.perCustomer.trim() === "") e.perCustomer = TEXT_CANT_CLEAR;
  return e;
}

interface EditCardProps {
  c: Coupon;
  locked: boolean;
  canManage: boolean;
  /** Version máy chủ trả sau lần lưu gần nhất; thẻ dựng lại theo version nên câu "Đã lưu." do màn cha nhớ. */
  savedVersion: number | null;
  onSaved: (version: number) => void;
}

function EditCard({ c, locked, canManage, savedVersion, onSaved }: EditCardProps) {
  const qc = useQueryClient();
  const [name, setName] = useState(c.name);
  const [draft, setDraft] = useState<TermsDraft>(() => termsOf(c));
  const [errors, setErrors] = useState<Errors>({});
  const [edited, setEdited] = useState(false);
  const saved = !edited && savedVersion === c.version;
  const [askOff, setAskOff] = useState(false);
  const save = useWriteAction((key, body: UpdateCouponRequest) =>
    unwrap(updateCoupon({ path: { couponId: c.id }, body, headers: headers(key) })),
  );
  const toggle = useWriteAction((key, body: UpdateCouponRequest) =>
    unwrap(updateCoupon({ path: { couponId: c.id }, body, headers: headers(key) })),
  );
  const readOnly = !canManage;
  const f = termFields({
    draft,
    set: (p) => {
      setDraft((d) => ({ ...d, ...p }));
      setEdited(true);
    },
    errors,
    locked,
    readOnly,
  });

  const onSave = async () => {
    const r = readTerms(draft);
    const e: Errors = { ...r.errors, ...clearedErrors(c, draft) };
    if (name.trim() === "") e.name = TEXT_NEED_NAME;
    if (r.terms?.maxUses !== undefined && r.terms.maxUses < c.usedCount)
      e.maxUses = `Tổng lượt không được thấp hơn số đã dùng (${String(c.usedCount)}).`;
    setErrors(e);
    if (r.terms === null || Object.keys(e).length > 0) return;
    const { validTo, maxUses } = r.terms;
    const body: UpdateCouponRequest = locked
      ? { version: c.version, name: name.trim(), validTo, ...(maxUses === undefined ? {} : { maxUses }) }
      : { version: c.version, name: name.trim(), ...r.terms };
    const res = await save.run(body);
    if (!res.ok) return;
    setEdited(false);
    onSaved(res.data.version);
    reloadCoupons(qc);
  };

  const setStatus = async (status: "active" | "disabled") => {
    const res = await toggle.run({ version: c.version, status });
    setAskOff(false);
    if (res.ok) reloadCoupons(qc);
  };
  const failed = save.error ?? toggle.error;
  const failedMsg = save.error !== null ? save.message : toggle.message;

  const nameField = (
    <TextField
      label="Tên"
      error={errors.name}
      maxLength={MAX_NAME}
      disabled={readOnly}
      value={name}
      onChange={(v) => {
        setName(v);
        setEdited(true);
      }}
    />
  );

  return (
    <div className="card">
      {locked && (
        <div className="box box-info" style={{ marginTop: 12 }}>
          {`Mã đã có lượt dùng: chỉ đổi được tên, ngày hết hạn, tổng lượt (không dưới ${String(c.usedCount)}) và tắt mã. Muốn đổi mức giảm thì tạo mã mới.`}
        </div>
      )}
      {locked ? (
        <>
          <div className="grid-f" style={{ marginTop: 16 }}>
            {nameField}
            {f.to}
            {f.maxUses("Tổng lượt", "Để trống: không giới hạn")}
          </div>
          <div className="grid-f" style={{ marginTop: 16 }}>
            {f.kind}
            {f.value}
            {f.maxDiscount}
            {f.minSubtotal}
            {f.from}
            {f.perCustomer}
          </div>
        </>
      ) : (
        <>
          <div className="grid-f" style={{ marginTop: 16 }}>
            {nameField}
          </div>
          <div style={{ marginTop: 16 }}>{discountRow(f)}</div>
          <div className="grid-f" style={{ marginTop: 16 }}>
            {f.from}
            {f.to}
            {f.maxUses("Tổng lượt", "Để trống: không giới hạn")}
            {f.perCustomer}
          </div>
        </>
      )}
      <div className="stack-sm" style={{ marginTop: 16 }}>
        {f.channels}
        {f.release}
      </div>
      {canManage && (
        <div className="savebar" style={{ marginTop: 16 }}>
          {c.status === "active" ? (
            <button
              type="button"
              className="btn btn-danger"
              disabled={toggle.pending}
              onClick={() => {
                setAskOff(true);
              }}
            >
              Tắt mã
            </button>
          ) : (
            <button
              type="button"
              className="btn btn-line"
              disabled={toggle.pending}
              onClick={() => {
                void setStatus("active");
              }}
            >
              Bật lại
            </button>
          )}
          <button
            type="button"
            className="btn btn-dark"
            disabled={save.pending}
            onClick={() => {
              void onSave();
            }}
          >
            Lưu
          </button>
        </div>
      )}
      {saved && (
        <p className="small muted" style={{ marginTop: 8 }}>
          Đã lưu.
        </p>
      )}
      {failed !== null && (
        <div style={{ marginTop: 12 }}>
          <RefusalBox
            error={failed}
            message={failedMsg}
            conflictTitle={TEXT_CONFLICT}
            onReload={() => {
              reloadCoupons(qc);
            }}
          />
        </div>
      )}
      {askOff && (
        <Dialog
          title={`Tắt mã ${c.code}?`}
          onClose={() => {
            setAskOff(false);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setAskOff(false);
                }}
              >
                Quay lại
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={toggle.pending}
                onClick={() => {
                  void setStatus("disabled");
                }}
              >
                Tắt mã
              </button>
            </>
          }
        >
          <p>{`Đơn mới không dùng được mã này nữa. ${String(c.usedCount)} đơn đã dùng giữ nguyên phần giảm. Bật lại được bất cứ lúc nào.`}</p>
        </Dialog>
      )}
    </div>
  );
}

function Redemptions({ id }: { id: string }) {
  const r = useRedemptions(id);
  const rows = r.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <div className="card">
      <h2>Lượt dùng</h2>
      {r.isError && <div className="box box-err">{errorMessage(r.error)}</div>}
      {r.isSuccess && rows.length === 0 && <p className="muted">Chưa có đơn nào dùng mã này.</p>}
      {rows.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Đơn</th>
                <th className="r">Giảm</th>
                <th>Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((x) => (
                <tr key={x.id} className={x.status === "released" ? "is-muted" : undefined}>
                  <td className="lead">
                    <Link className="cell-a num" to={`/don/${x.orderId}`}>
                      {x.orderCode}
                    </Link>
                  </td>
                  <td data-label="Giảm" className="r">
                    <span className="money">{formatMoney(x.discount)}</span>
                  </td>
                  <td data-label="Trạng thái">
                    {x.status === "active" ? (
                      <span className="chip chip-ok">Đang tính</span>
                    ) : (
                      <span className="chip">Đã trả lượt (đơn hủy)</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > 0 && <MoreBar text={`${String(rows.length)} lượt mới nhất`} pages={r} />}
    </div>
  );
}

export function CouponDetail() {
  const id = useParams().id ?? "";
  const perms = usePerms();
  const coupon = useCoupon(id);
  const red = useRedemptions(id);
  const [now] = useState(() => Date.now());
  const [savedVersion, setSavedVersion] = useState<number | null>(null);
  const c = coupon.data;
  const hasRedemption = (red.data?.pages[0]?.items.length ?? 0) > 0;
  const chip = c === undefined ? undefined : STATE_CHIP[couponState(c, now)];

  return (
    <div>
      <p className="small" style={{ marginBottom: 8 }}>
        <Link className="cell-a" to="/ma-giam-gia">
          ← Mã giảm giá
        </Link>
      </p>
      {coupon.isError && <div className="box box-err">{errorMessage(coupon.error)}</div>}
      {c !== undefined && chip !== undefined && (
        <>
          <div className="row-between" style={{ marginBottom: 12 }}>
            <div className="row">
              <h2 className="num">{c.code}</h2>
              <span className={chip.className}>{chip.label}</span>
            </div>
            <span className="small muted">{usesLine(c)}</span>
          </div>
          <div className="cols">
            {(red.isSuccess || red.isError || c.usedCount > 0) && perms.loaded ? (
              <EditCard
                key={c.version}
                c={c}
                locked={c.usedCount > 0 || hasRedemption}
                canManage={perms.can("promotion.manage")}
                savedVersion={savedVersion}
                onSaved={setSavedVersion}
              />
            ) : (
              <div className="card muted">Đang tải…</div>
            )}
            <Redemptions id={id} />
          </div>
        </>
      )}
    </div>
  );
}
