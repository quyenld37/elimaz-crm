// Màn Nhận đơn web /website/nhan-don-web (kế hoạch M5 Task 462, M5-50; mockup docs/mockups/m5/web-intake.html trạng
// thái A–D; HV23). Đọc getWebIntake, đổi bằng setWebIntake (Task 456): tạm dừng cần lý do 1–300 ký tự và xác nhận, mở
// lại cần xác nhận; gửi version đang thấy, 409 version_conflict → tải lại và báo, không tự gửi lại. Khóa truy vấn đứng
// đầu bằng chủ đề realtime "order.web_intake" (máy chủ báo khi trạng thái đổi). Lý do chỉ nằm trong bộ nhớ React Query.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { getWebIntake, setWebIntake, type SetWebIntakeRequest, type WebIntake } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { fullTime } from "../../app/time";
import { usePerms } from "../don/orderData";
import { Dialog } from "../staff/staffData";
import "../don/don.css";

export const INTAKE_KEY = queryKey("order.web_intake", "state");
export const MAX_REASON = 300;
export const TEXT_NO_PERMISSION = "Bạn không có quyền xem trạng thái nhận đơn web.";
const TEXT_VIEW_ONLY = 'Bạn chỉ xem được (thiếu quyền "Tạm dừng nhận đơn web").';

/** Câu báo sau 409: trạng thái vừa tải lại, ai, lúc nào, lý do. */
function reloadedText(w: WebIntake): string {
  const who = [w.changedByName, w.changedAt === undefined ? undefined : fullTime(w.changedAt)].filter(
    (x) => x !== undefined && x !== "",
  );
  if (w.paused && w.reason !== undefined) who.push(`lý do "${w.reason}"`);
  const detail = who.length === 0 ? "" : ` (${who.join(", ")})`;
  return `Đã tải lại: website ${w.paused ? "đang tạm dừng" : "đang nhận đơn"}${detail}. Chưa đổi gì thêm.`;
}

const isConflict = (e: unknown) => e instanceof ApiError && e.status === 409 && e.code === "version_conflict";

function ChangeDialog({
  current,
  onClose,
  onDone,
  onConflict,
}: {
  current: WebIntake;
  onClose: () => void;
  onDone: (w: WebIntake) => void;
  onConflict: () => Promise<void>;
}) {
  const pausing = !current.paused;
  const [reason, setReason] = useState("");
  const save = useWriteAction((key: string, body: SetWebIntakeRequest) =>
    unwrap(setWebIntake({ body, headers: { "Idempotency-Key": key } })),
  );
  const trimmed = reason.trim();
  const missing = pausing && trimmed === "";
  const go = async () => {
    if (missing) return;
    const body: SetWebIntakeRequest = pausing
      ? { paused: true, reason: trimmed, version: current.version }
      : { paused: false, version: current.version };
    const r = await save.run(body);
    if (r.ok) onDone(r.data);
    else if (isConflict(r.error)) await onConflict();
  };
  const title = pausing ? "Tạm dừng nhận đơn mới?" : "Mở lại nhận đơn?";
  const busy = save.pending;
  const goLabel = pausing ? "Xác nhận tạm dừng" : "Mở lại nhận đơn";
  const goButton = (
    <button
      type="button"
      className={pausing ? "btn btn-danger" : "btn btn-dark"}
      disabled={missing || busy}
      onClick={() => {
        void go();
      }}
    >
      {busy ? "Đang lưu…" : goLabel}
    </button>
  );
  return (
    <Dialog
      title={title}
      closeButton={!busy}
      onClose={() => {
        if (!busy) onClose();
      }}
      footer={
        <>
          <button type="button" className="btn btn-quiet" disabled={busy} onClick={onClose}>
            Đóng
          </button>
          {/* Cùng một khung với hay không có câu lý do: nút không bị dựng lại khi gõ. */}
          <div className="act" style={{ justifyItems: "end" }}>
            {goButton}
            {missing && <span className="reason">Nhập lý do để tạm dừng</span>}
          </div>
        </>
      }
    >
      {pausing ? (
        <>
          <p>
            Khách sẽ không đặt được đơn mới trên website cho tới khi bạn mở lại. Khách thấy câu: &quot;ELIMAZ đang tạm
            dừng nhận đơn trên website…&quot;.
          </p>
          <p className="small">
            Đơn khách đã bấm đặt trước lúc tạm dừng (kể cả đơn website đang gửi lại vì mạng chập chờn){" "}
            <b>vẫn vào CRM</b>.
          </p>
          <label className="field">
            <span>Lý do *</span>
            <textarea
              className="input"
              aria-label="Lý do"
              placeholder="Nhập lý do để đội ngũ cùng theo dõi"
              maxLength={MAX_REASON}
              value={reason}
              disabled={busy}
              onChange={(e) => {
                setReason(e.target.value);
              }}
            />
            <span className="hint num">{`${String(reason.length)} / ${String(MAX_REASON)}`}</span>
          </label>
        </>
      ) : (
        <p>Khách đặt được đơn mới trên website ngay (website đọc lại trạng thái trong vài giây).</p>
      )}
      {save.message !== null && !isConflict(save.error) && (
        <div className="box box-err" role="alert">
          {save.message}
        </div>
      )}
    </Dialog>
  );
}

function StateCard({
  w,
  canManage,
  permsLoaded,
  onChange,
}: {
  w: WebIntake;
  canManage: boolean;
  permsLoaded: boolean;
  onChange: () => void;
}) {
  const button = (
    <button
      type="button"
      className={canManage ? (w.paused ? "btn btn-dark" : "btn btn-danger") : "btn"}
      disabled={!canManage}
      onClick={onChange}
    >
      {w.paused ? "Mở lại nhận đơn" : "Tạm dừng nhận đơn"}
    </button>
  );
  const when = w.changedAt === undefined ? "" : fullTime(w.changedAt);
  return (
    <section className="card stack-sm" style={{ maxWidth: 760 }}>
      <div className="row-between">
        <h2>{w.paused ? "Website đang tạm dừng nhận đơn mới" : "Website đang nhận đơn"}</h2>
        <span className={w.paused ? "chip chip-err" : "chip chip-ok"}>
          {w.paused ? "Đang tạm dừng" : "Đang nhận đơn"}
        </span>
      </div>
      {w.paused ? (
        <div className="box box-soft">
          <b className="wrap-any">Lý do: {w.reason ?? "không ghi"}</b>
          {when !== "" && (
            <>
              <br />
              <span className="small">{[w.changedByName, when].filter((x) => x !== undefined).join(" · ")}</span>
            </>
          )}
        </div>
      ) : (
        when !== "" && (
          <p className="small muted">
            Mở lại lúc {when}
            {w.changedByName === undefined ? "" : ` bởi ${w.changedByName}`}.
          </p>
        )
      )}
      {canManage || !permsLoaded ? (
        <div>{button}</div>
      ) : (
        <div className="act">
          {button}
          <span className="reason">{TEXT_VIEW_ONLY}</span>
        </div>
      )}
    </section>
  );
}

export function WebIntakeScreen() {
  const qc = useQueryClient();
  const perms = usePerms();
  const intake = useQuery({ queryKey: INTAKE_KEY, queryFn: () => unwrap(getWebIntake()) });
  const [editing, setEditing] = useState(false);
  const [conflict, setConflict] = useState(false);
  const canManage = perms.can("order.web_intake.manage");

  const onConflict = async () => {
    setEditing(false);
    await qc.refetchQueries({ queryKey: INTAKE_KEY });
    setConflict(true);
  };

  let body;
  if (intake.isError) {
    body =
      intake.error instanceof ApiError && intake.error.status === 403 ? (
        <div className="box box-err stack-sm">
          <b>{TEXT_NO_PERMISSION}</b>
          <p>Nhờ Chủ shop cấp quyền xem đơn cho vai trò của bạn.</p>
        </div>
      ) : (
        <div className="box box-err stack-sm">
          <b>Chưa tải được trạng thái nhận đơn web.</b>
          <p>{errorMessage(intake.error)}</p>
          <div>
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                void intake.refetch();
              }}
            >
              Thử lại
            </button>
          </div>
        </div>
      );
  } else if (intake.isPending) {
    body = <p className="muted">Đang tải trạng thái nhận đơn web…</p>;
  } else {
    const w = intake.data;
    body = (
      <>
        {conflict && (
          <div className="box box-err stack-sm" role="alert">
            <b>Trạng thái vừa được người khác đổi.</b>
            <p className="wrap-any">{reloadedText(w)}</p>
            <div>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  setConflict(false);
                }}
              >
                Đã hiểu
              </button>
            </div>
          </div>
        )}
        <StateCard
          w={w}
          canManage={canManage}
          permsLoaded={perms.loaded}
          onChange={() => {
            setConflict(false);
            setEditing(true);
          }}
        />
        {editing && (
          <ChangeDialog
            current={w}
            onClose={() => {
              setEditing(false);
            }}
            onDone={(next) => {
              qc.setQueryData(INTAKE_KEY, next);
              setEditing(false);
            }}
            onConflict={onConflict}
          />
        )}
      </>
    );
  }

  return (
    <div className="stack">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Website / Nhận đơn web</p>
          <h1>Nhận đơn web</h1>
          <p className="sub">
            Tạm dừng khi cần (kiểm kho, sự cố giao hàng…): khách vẫn xem sản phẩm nhưng không đặt được đơn mới trên
            website. Đơn tại cửa hàng và đơn nhân viên lên không bị ảnh hưởng.
          </p>
        </div>
      </div>
      {body}
    </div>
  );
}
