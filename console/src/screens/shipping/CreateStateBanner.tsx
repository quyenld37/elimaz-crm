// Kết quả lệnh tạo vận đơn ở hãng (mockup docs/mockups/m3/don-van-chuyen.html trạng thái B "đang gửi", C; HV11; Q-5):
// pending "Đang gửi hãng…"; unknown "Không rõ hãng đã tạo chưa" với hai nút "Hãng đã tạo" (đòi mã hãng) / "Hãng chưa
// tạo", không có "Gửi lại"; not_sent "Chưa gửi được" + "Gửi lại" do người bấm (CRM không tự gửi); rejected "Hãng từ
// chối". Vận đơn đã gửi (created, manual) là ShipmentCard (Task 134).
import { useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { resendShipment, resolveShipmentCreate, type Shipment } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { reloadOrder } from "../don/orderData";
import { shipmentKey } from "./useShipment";

export const TEXT_NO_SHIP_PERM = 'Bạn không có quyền gửi hãng ở kho của đơn này (quyền "Gửi hãng" theo kho)';

function Head({ chip, chipClass }: { chip: string; chipClass: string }) {
  return (
    <div className="row-between">
      <h2>Vận chuyển</h2>
      <span className={chipClass}>{chip}</span>
    </div>
  );
}

/** Nút kèm lý do mờ (kiểu .act/.reason của chi tiết đơn); wide: nút rộng hết khối. */
export function Act({ children, reason, wide }: { children: ReactNode; reason?: string | null; wide?: boolean }) {
  return (
    <div className="act" style={wide === true ? { maxWidth: "none" } : undefined}>
      {children}
      {reason !== undefined && reason !== null && <span className="reason">{reason}</span>}
    </div>
  );
}

const account = (s: Shipment) => s.accountName ?? s.carrierKey;

interface Resolve {
  id: string;
  result: "created" | "not_created";
  trackingCode?: string;
  reason: string;
}

/** Sau một lệnh ghi thành công: đặt vận đơn mới vào bộ nhớ, đọc lại đơn (trạng thái, vận đơn hiện hành). */
function useAfterWrite(orderId: string) {
  const qc = useQueryClient();
  return (s: Shipment) => {
    qc.setQueryData(shipmentKey(s.id), s);
    void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
    reloadOrder(qc, orderId);
  };
}

function Unknown({ s, canShip, orderId }: { s: Shipment; canShip: boolean; orderId: string }) {
  const [code, setCode] = useState("");
  const [reason, setReason] = useState("");
  const after = useAfterWrite(orderId);
  const resolve = useWriteAction((key, b: Resolve) =>
    unwrap(
      resolveShipmentCreate({
        path: { id: b.id },
        body: {
          result: b.result,
          reason: b.reason,
          ...(b.trackingCode === undefined ? {} : { trackingCode: b.trackingCode }),
        },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const run = async (result: Resolve["result"]) => {
    const r = await resolve.run({
      id: s.id,
      result,
      reason: reason.trim(),
      ...(result === "created" ? { trackingCode: code.trim() } : {}),
    });
    if (r.ok) after(r.data);
  };
  const noReason = reason.trim() === "";
  const madeWhy = !canShip
    ? TEXT_NO_SHIP_PERM
    : code.trim() === ""
      ? "Nhập mã vận đơn của hãng"
      : noReason
        ? "Ghi chú đã kiểm ở đâu"
        : null;
  const notMadeWhy = !canShip
    ? TEXT_NO_SHIP_PERM
    : noReason
      ? "Ghi chú đã kiểm ở đâu"
      : 'Vận đơn về "Chưa gửi được", rồi bấm Gửi lại';
  return (
    <>
      <Head chip="Không rõ hãng đã tạo chưa" chipClass="chip chip-err" />
      <div className="box box-err small">
        <b>{`Không rõ ${account(s)} đã tạo vận đơn chưa.`}</b>
        {` Gửi lúc ${shortTime(s.createdAt)}; hãng chưa trả lời chắc chắn. Vào trang hãng tìm mã `}
        <span className="num">{s.code}</span>
        {" rồi chọn một nút. Không gửi lại khi chưa chắc: khách có thể nhận hai kiện."}
      </div>
      <label className="field">
        <span>Mã vận đơn của hãng (khi đã tạo)</span>
        <input
          className="input num"
          maxLength={64}
          placeholder="Ví dụ 1234567890123"
          value={code}
          onChange={(e) => {
            setCode(e.target.value);
          }}
        />
      </label>
      <label className="field">
        <span>Ghi chú *</span>
        <input
          className="input"
          maxLength={1000}
          placeholder="Đã kiểm trên trang hãng lúc…"
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </label>
      <div className="acts">
        <Act reason={madeWhy}>
          <button
            type="button"
            className="btn btn-line"
            disabled={madeWhy !== null || resolve.pending}
            onClick={() => void run("created")}
          >
            Hãng đã tạo
          </button>
        </Act>
        <Act reason={notMadeWhy}>
          <button
            type="button"
            className="btn btn-line"
            disabled={!canShip || noReason || resolve.pending}
            onClick={() => void run("not_created")}
          >
            Hãng chưa tạo
          </button>
        </Act>
      </div>
      {resolve.message !== null && <div className="box box-err small">{resolve.message}</div>}
    </>
  );
}

function NotSent({ s, canShip, orderId }: { s: Shipment; canShip: boolean; orderId: string }) {
  const after = useAfterWrite(orderId);
  const resend = useWriteAction((key, b: { id: string }) =>
    unwrap(resendShipment({ path: { id: b.id }, headers: { "Idempotency-Key": key } })),
  );
  return (
    <>
      <Head chip="Chưa gửi được" chipClass="chip chip-err" />
      <div className="box box-err small">
        <b>Hãng xác nhận chưa tạo vận đơn.</b>
        {` ${account(s)} · mã CRM `}
        <span className="num">{s.code}</span>
        {". Phí và COD có thể đã đổi: kiểm lại rồi gửi lại."}
      </div>
      <Act wide reason={canShip ? null : TEXT_NO_SHIP_PERM}>
        <button
          type="button"
          className="btn btn-dark btn-wide"
          disabled={!canShip || resend.pending}
          onClick={() => {
            void resend.run({ id: s.id }).then((r) => {
              if (r.ok) after(r.data);
            });
          }}
        >
          Gửi lại
        </button>
      </Act>
      {resend.message !== null && <div className="box box-err small">{resend.message}</div>}
      <p className="hint">
        CRM không tự gửi lại. "Gửi lại" dùng đúng tài khoản, cân nặng, tùy chọn đã chọn; muốn đổi thì chờ hãng xác nhận
        hủy rồi báo phí lại.
      </p>
    </>
  );
}

/** Câu "Hãng từ chối" của vận đơn s (mã là trạng thái phụ hãng trả, nếu có). */
export function RejectedNote({ s, onEdit }: { s: Shipment; onEdit?: (() => void) | undefined }) {
  const code = s.subStatus ?? "";
  return (
    <>
      <div className="box box-err small">
        <b>{code === "" ? "Hãng từ chối vận đơn." : `Hãng từ chối: ${code}.`}</b>
        {" Vận đơn "}
        <span className="num">{s.code}</span>
        {` (${account(s)}) đã hủy. Sửa đơn rồi báo phí, gửi lại.`}
      </div>
      {onEdit !== undefined && (
        <button type="button" className="btn btn-line btn-wide" onClick={onEdit}>
          Sửa đơn
        </button>
      )}
    </>
  );
}

/** Khối của vận đơn hiện hành s đang tạo (creating) theo trạng thái lệnh tạo; vận đơn đã gửi là ShipmentCard. */
export function CreateStateBanner(props: {
  s: Shipment;
  canShip: boolean;
  orderId: string;
  onEdit?: (() => void) | undefined;
}) {
  const { s } = props;
  if (s.status === "creating") {
    switch (s.createState) {
      case "unknown":
        return <Unknown s={s} canShip={props.canShip} orderId={props.orderId} />;
      case "not_sent":
        return <NotSent s={s} canShip={props.canShip} orderId={props.orderId} />;
      case "rejected":
        return (
          <>
            <Head chip="Chưa gửi hãng" chipClass="chip" />
            <RejectedNote s={s} onEdit={props.onEdit} />
          </>
        );
      default:
        return (
          <>
            <Head chip="Đang gửi hãng…" chipClass="chip chip-dark" />
            <p className="small">
              {`${account(s)} · gửi lúc ${shortTime(s.createdAt)} · `}
              <span className="num">{s.code}</span>
            </p>
            <div className="bar" role="progressbar" aria-label="Đang gửi">
              <i style={{ width: "40%" }} />
            </div>
            <p className="hint">Chưa có mã của hãng. Không bấm gửi lại: CRM tự hỏi hãng theo mã E… nếu quá giờ.</p>
          </>
        );
    }
  }
  return null;
}
