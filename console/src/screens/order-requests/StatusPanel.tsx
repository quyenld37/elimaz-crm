// Thẻ "Xử lý yêu cầu" (mockup docs/mockups/m5/order-requests.html trạng thái B, C, D, F; HV08). Ô chọn chỉ có trạng thái
// kế tiếp theo sơ đồ của máy chủ. Đang kiểm tra, Đã nhận hàng gửi về, Đã xác nhận đổi (kèm hướng dẫn gửi trả) lưu ngay;
// Cần bổ sung, Từ chối, Hoàn tất mở hộp riêng. Gửi version đã đọc; 409 version_conflict → báo, tải lại yêu cầu, giữ chữ
// đang gõ, không tự gửi lại. Đang ghi (kể cả đang gửi tin) thì khóa mọi thao tác.
import { useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import {
  setOrderRequestStatus,
  type OrderRequest,
  type OrderRequestStatus,
  type SetOrderRequestStatusRequest,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog } from "../staff/staffData";
import { headers, reloadRequests } from "./requestData";
import {
  MSG_MAX,
  REASON_MAX,
  STATUS_TEXT,
  TRACKING_MAX,
  isDialogStatus,
  messageError,
  nextStatuses,
  runeCount,
  shipBackErrors,
  vnToday,
  type DialogStatus,
  type ShipBackInput,
} from "./requestRules";

interface StatusBody {
  id: string;
  req: SetOrderRequestStatusRequest;
}

const isConflict = (e: unknown) => e instanceof ApiError && e.status === 409 && e.code === "version_conflict";

function Field(o: { label: string; error?: string | null; hint?: ReactNode; children: ReactNode }) {
  return (
    // Lỗi, gợi ý nằm ngoài <label> để tên của ô nhập chỉ là nhãn (trình đọc màn hình, test theo tên).
    <div className="field">
      <label className="field">
        <span>{o.label}</span>
        {o.children}
      </label>
      {o.error !== undefined && o.error !== null ? <span className="field-err">{o.error}</span> : o.hint}
    </div>
  );
}

const DIALOG_TITLE: Readonly<Record<DialogStatus, string>> = {
  needs_info: "Cần khách bổ sung",
  rejected: "Từ chối yêu cầu này?",
  resolved: "Hoàn tất yêu cầu này?",
};
const DIALOG_GO: Readonly<Record<DialogStatus, string>> = {
  needs_info: "Gửi và chuyển trạng thái",
  rejected: "Từ chối yêu cầu",
  resolved: "Hoàn tất yêu cầu",
};

/** Ô nhập của hộp thoại; giữ ở thẻ cha để 409 không làm mất chữ đang gõ. */
interface DialogInput {
  /** Lời nhắn của Cần bổ sung: cùng ô "Lời nhắn cho khách" của thẻ. */
  message: string;
  reason: string;
  tracking: string;
}

function StatusDialog(o: {
  status: DialogStatus;
  input: DialogInput;
  onInput: (p: Partial<DialogInput>) => void;
  pending: boolean;
  error: string | null;
  onGo: () => void;
  onClose: () => void;
}) {
  const { status, input } = o;
  const msgErr = messageError(input.message, true);
  const reasonLen = runeCount(input.reason.trim());
  const trackLen = runeCount(input.tracking.trim());
  const invalid =
    status === "needs_info"
      ? msgErr !== null
      : status === "rejected"
        ? reasonLen === 0 || reasonLen > REASON_MAX
        : trackLen > TRACKING_MAX;
  return (
    <Dialog
      title={DIALOG_TITLE[status]}
      closeButton
      onClose={o.onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={o.onClose}>
            Đóng
          </button>
          <button
            type="button"
            className={status === "rejected" ? "btn btn-danger" : "btn btn-dark"}
            disabled={invalid || o.pending}
            onClick={o.onGo}
          >
            {DIALOG_GO[status]}
          </button>
        </>
      }
    >
      {status === "needs_info" && (
        <>
          <Field label="Lời nhắn cho khách *" error={input.message.trim() === "" ? null : msgErr}>
            <textarea
              className="input"
              maxLength={MSG_MAX + 200}
              value={input.message}
              onChange={(e) => {
                o.onInput({ message: e.target.value });
              }}
            />
          </Field>
          <p className="hint">
            Khách thấy lời nhắn ở trang đơn hàng và gửi thêm thông tin; yêu cầu quay về &quot;Đang kiểm tra&quot; khi
            nhân viên xem lại.
          </p>
        </>
      )}
      {status === "rejected" && (
        <>
          <p>
            Khách sẽ thấy lý do từ chối. Yêu cầu đóng lại và <b>không mở lại được</b>.
          </p>
          <Field
            label="Lý do từ chối *"
            error={reasonLen > REASON_MAX ? "Lý do tối đa 500 ký tự." : null}
            hint={<span className="hint num">{`${String(reasonLen)} / ${String(REASON_MAX)}`}</span>}
          >
            <textarea
              className="input"
              value={input.reason}
              onChange={(e) => {
                o.onInput({ reason: e.target.value });
              }}
            />
          </Field>
        </>
      )}
      {status === "resolved" && (
        <>
          <p>
            Yêu cầu đóng lại và <b>không mở lại được</b>.
          </p>
          <Field
            label="Mã vận đơn đơn đổi (nếu có)"
            error={trackLen > TRACKING_MAX ? "Mã vận đơn tối đa 60 ký tự." : null}
            hint={<span className="hint">Tối đa 60 ký tự; khách thấy để tra đơn đổi.</span>}
          >
            <input
              className="input num"
              value={input.tracking}
              onChange={(e) => {
                o.onInput({ tracking: e.target.value });
              }}
            />
          </Field>
        </>
      )}
      {o.error !== null && <div className="box box-err">{o.error}</div>}
    </Dialog>
  );
}

function ShipBackFields(o: { v: ShipBackInput; onChange: (p: Partial<ShipBackInput>) => void; today: string }) {
  const err = shipBackErrors(o.v, o.today);
  // Chỉ báo lỗi ô đã gõ (ô trống lúc mới chọn không đỏ; nút Lưu vẫn khóa).
  const shown = (field: keyof ShipBackInput) => (o.v[field] === "" ? null : err[field]);
  return (
    <fieldset className="rq-ship stack">
      <legend className="label">Hướng dẫn gửi hàng về</legend>
      <Field label="Địa chỉ nhận hàng *" error={shown("address")}>
        <input
          className="input"
          maxLength={400}
          value={o.v.address}
          onChange={(e) => {
            o.onChange({ address: e.target.value });
          }}
        />
      </Field>
      <div className="grid-f">
        <Field
          label="Mã ghi trên kiện hàng *"
          error={shown("packageCode")}
          hint={<span className="hint">3–40 ký tự, khách ghi ngoài kiện</span>}
        >
          <input
            className="input"
            maxLength={60}
            value={o.v.packageCode}
            onChange={(e) => {
              o.onChange({ packageCode: e.target.value });
            }}
          />
        </Field>
        <Field label="Gửi trước ngày (giờ Việt Nam) *" error={shown("deadline")}>
          <input
            className="input num"
            type="date"
            min={o.today}
            value={o.v.deadline}
            onChange={(e) => {
              o.onChange({ deadline: e.target.value });
            }}
          />
        </Field>
      </div>
      <p className="hint">Khách thấy hướng dẫn này ở trang đơn hàng; hạn tính tới 23:59 của ngày đã chọn.</p>
    </fieldset>
  );
}

const EMPTY_SHIP: ShipBackInput = { address: "", packageCode: "", deadline: "" };
const EMPTY_DLG = { reason: "", tracking: "" };

/** Thân gửi máy chủ: chỉ trường của trạng thái đã chọn; chữ đã cắt khoảng trắng; trường rỗng không gửi. */
function bodyFor(
  r: OrderRequest,
  status: OrderRequestStatus,
  message: string,
  ship: ShipBackInput,
  dlg: Omit<DialogInput, "message">,
): SetOrderRequestStatusRequest {
  const req: SetOrderRequestStatusRequest = { status, version: r.version };
  const msg = message.trim();
  if (status !== "rejected" && status !== "resolved" && msg !== "") req.message = msg;
  if (status === "confirmed") {
    req.shipBack = { address: ship.address.trim(), packageCode: ship.packageCode.trim(), deadline: ship.deadline };
  }
  if (status === "rejected") req.outcome = { reason: dlg.reason.trim() };
  if (status === "resolved" && dlg.tracking.trim() !== "") req.outcome = { trackingCode: dlg.tracking.trim() };
  return req;
}

/** Thẻ xử lý của người có order.requests.manage, yêu cầu chưa đóng. busy: đang gửi tin ở thẻ Trao đổi. */
export function StatusPanel(o: { r: OrderRequest; busy: boolean; onPending: (p: boolean) => void }) {
  const { r } = o;
  const qc = useQueryClient();
  const [next, setNext] = useState<OrderRequestStatus | "">("");
  const [message, setMessage] = useState("");
  const [ship, setShip] = useState<ShipBackInput>(EMPTY_SHIP);
  const [dlg, setDlg] = useState<Omit<DialogInput, "message">>(EMPTY_DLG);
  const [open, setOpen] = useState<DialogStatus | null>(null);
  const [conflict, setConflict] = useState(false);
  const [today] = useState(() => vnToday(Date.now()));
  const write = useWriteAction((key, b: StatusBody) =>
    unwrap(setOrderRequestStatus({ path: { id: b.id }, body: b.req, headers: headers(key) })),
  );
  const options = nextStatuses(r.kind, r.status);
  // Sau khi tải lại (409, báo hiệu), trạng thái đã chọn có thể không còn hợp lệ: coi như "Giữ nguyên".
  const chosen = next !== "" && options.includes(next) ? next : "";
  const locked = write.pending || o.busy;

  const submit = async (status: OrderRequestStatus) => {
    setConflict(false);
    o.onPending(true);
    const res = await write.run({ id: r.id, req: bodyFor(r, status, message, ship, dlg) });
    o.onPending(false);
    if (res.ok) {
      setNext("");
      setMessage("");
      setShip(EMPTY_SHIP);
      setDlg(EMPTY_DLG);
      setOpen(null);
      reloadRequests(qc);
      return;
    }
    if (isConflict(res.error)) {
      setOpen(null);
      setConflict(true);
      write.reset();
      reloadRequests(qc);
    }
  };

  const msgErr = messageError(message, false);
  const shipInvalid = chosen === "confirmed" && Object.values(shipBackErrors(ship, today)).some((e) => e !== null);
  const canSave = chosen !== "" && !locked && (isDialogStatus(chosen) || (msgErr === null && !shipInvalid));
  const onSave = () => {
    if (chosen === "") return;
    if (isDialogStatus(chosen)) {
      write.reset();
      setOpen(chosen);
      return;
    }
    void submit(chosen);
  };

  return (
    <section className="card">
      <h2>Xử lý yêu cầu</h2>
      {conflict && (
        <div className="box box-err stack-sm">
          <b>Yêu cầu vừa được người khác đổi trạng thái.</b>
          <p>{`Đã tải lại: yêu cầu giờ là "${STATUS_TEXT[r.status]}". Chữ bạn đang gõ vẫn giữ trong ô; xem lại rồi lưu.`}</p>
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
      <Field label="Trạng thái tiếp theo">
        <span className="select-wrap">
          <select
            value={chosen}
            disabled={locked}
            onChange={(e) => {
              setNext(options.find((s) => s === e.target.value) ?? "");
            }}
          >
            <option value="">Giữ nguyên, chỉ trả lời</option>
            {options.map((s) => (
              <option key={s} value={s}>
                {STATUS_TEXT[s]}
              </option>
            ))}
          </select>
        </span>
      </Field>
      <p className="hint">
        {`Từ "${STATUS_TEXT[r.status]}" chuyển được sang: ${options.map((s) => STATUS_TEXT[s]).join(", ")}. Hoàn tất và Từ chối hỏi xác nhận vì không mở lại được.`}
      </p>
      {chosen === "confirmed" && (
        <ShipBackFields
          v={ship}
          today={today}
          onChange={(p) => {
            setShip((s) => ({ ...s, ...p }));
          }}
        />
      )}
      {chosen !== "rejected" && chosen !== "resolved" && (
        <Field label="Lời nhắn cho khách" error={msgErr}>
          <textarea
            className="input"
            placeholder="Không bắt buộc; bắt buộc khi chọn Cần bổ sung"
            maxLength={MSG_MAX + 200}
            value={message}
            disabled={locked}
            onChange={(e) => {
              setMessage(e.target.value);
            }}
          />
        </Field>
      )}
      {open === null && write.message !== null && <div className="box box-err">{write.message}</div>}
      <div className="savebar">
        <span className="xs muted grow">Thay đổi được ghi vào nhật ký thao tác.</span>
        <button type="button" className="btn btn-dark" disabled={!canSave} onClick={onSave}>
          Lưu xử lý
        </button>
      </div>
      {open !== null && (
        <StatusDialog
          status={open}
          input={{ ...dlg, message }}
          onInput={({ message: m, ...p }) => {
            if (m !== undefined) setMessage(m);
            setDlg((d) => ({ ...d, ...p }));
          }}
          pending={write.pending}
          error={write.message}
          onGo={() => {
            void submit(open);
          }}
          onClose={() => {
            if (!write.pending) setOpen(null);
          }}
        />
      )}
    </section>
  );
}
