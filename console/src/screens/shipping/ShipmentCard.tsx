// Khối Vận chuyển sau khi gửi hãng (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html trạng thái D, E, G;
// HV12–HV16 chủ shop OK 21/11). Mã hãng, nhãn trạng thái, "Cập nhật lần cuối" (lastCarrierEventAt, kèm lần CRM hỏi
// lastSyncedAt và lỗi đồng bộ), băng kẹt "Hãng chưa cập nhật N giờ", băng đang yêu cầu hủy; nút Đồng bộ lại, In nhãn,
// Đổi địa chỉ, Hủy / đổi hãng; vận đơn nhập mã tay, tự giao: "Cập nhật trạng thái". Dưới thẻ: yêu cầu đổi địa chỉ đang
// mở, khung COD bốn ngăn. Nút theo quyền ở kho của đơn; máy chủ vẫn kiểm. Task 138 (HV29, HV35): "Ghi giao một phần"
// khi đang chuyển hay giao thất bại, "Đảo "Thành công"" khi đã giao — cần fulfillment.correct ở kho của đơn (người khác
// thấy nút mờ kèm lý do).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { syncShipment, type Order, type Shipment } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { hasPermission } from "../../app/nav";
import { shortTime } from "../../app/time";
import { canAt } from "../inventory/stocktakeRules";
import { formatMoney } from "../products/productRules";
import { AddressChangeBox, AddressChangeDialog } from "./AddressChangeDialog";
import { CancelDialog } from "./CancelDialog";
import { CodBox } from "./CodBox";
import { Act, TEXT_NO_SHIP_PERM } from "./CreateStateBanner";
import { ManualStatusDialog } from "./ManualStatusDialog";
import { CodAdjustmentDialog } from "../payments/CodAdjustmentDialog";
import { CorrectionDialog } from "../returns/CorrectionDialog";
import { PartialDeliveryDialog } from "../returns/PartialDeliveryDialog";
import { ADDRESS_CHANGE_STATUSES, hoursSilent, isClosed, statusChip, statusText, syncErrorText } from "./shipmentRules";
import { shipmentKey, useFulfillmentSettings, useGrants, useOpenAddressChange } from "./useShipment";

const account = (s: Shipment) => s.accountName ?? s.carrierKey;
export const TEXT_CANCEL_WAIT = "Đang chờ hãng xác nhận hủy — hàng chưa được trả về kho.";
const TEXT_PICKED_UP =
  "Hãng đã lấy hàng: không hủy được. Muốn dừng giao thì gọi hãng; khi hãng chuyển hoàn, đơn đi luồng hàng hoàn";

type Open = "cancel" | "address" | "manual" | "correct" | "partial" | "cod" | null;

export const TEXT_NO_CORRECT = 'Cần quyền "Sửa vận đơn" (Chủ, Quản lý)';
const PARTIAL_STATUSES: readonly Shipment["status"][] = ["shipping", "delivery_failed"];

/** Ghi giao một phần được không (fulfillment partial.Decide): đang chuyển, giao thất bại; chờ thu gom khi hãng đã báo
 * giao một phần (vận đơn ở "Cần xem"; máy chủ kiểm việc partial_reported còn mở). */
const partialAllowed = (s: Shipment) =>
  PARTIAL_STATUSES.includes(s.status) || (s.status === "awaiting_pickup" && s.needsReview === true);

/** Dòng "Cập nhật lần cuối lúc … (hãng) · CRM hỏi lúc …" của vận đơn nối API. */
function LastUpdate({ s }: { s: Shipment }) {
  // Cùng ngày với tin của hãng thì chỉ ghi giờ (mockup D: "CRM hỏi lúc 14:21").
  const askedAt = s.lastSyncedAt === undefined ? "" : shortTime(s.lastSyncedAt);
  const sameDay =
    s.lastCarrierEventAt !== undefined && askedAt.split(" ")[1] === shortTime(s.lastCarrierEventAt).split(" ")[1];
  const asked = askedAt === "" ? "" : ` · CRM hỏi lúc ${sameDay ? (askedAt.split(" ")[0] ?? askedAt) : askedAt}`;
  const err =
    s.lastSyncError === undefined || s.lastSyncError === ""
      ? ""
      : ` · lần hỏi gần nhất lỗi: ${syncErrorText(s.lastSyncError)}`;
  return (
    <p className="small">
      {s.lastCarrierEventAt === undefined ? (
        "Hãng chưa gửi cập nhật nào"
      ) : (
        <>
          Cập nhật lần cuối lúc <b>{shortTime(s.lastCarrierEventAt)}</b> (hãng)
        </>
      )}
      {asked}
      {err}
    </p>
  );
}

/** Băng "Hãng chưa cập nhật N giờ" của vận đơn kẹt (stuckSince có giá trị). */
function StuckNote({ s }: { s: Shipment }) {
  const settings = useFulfillmentSettings(true);
  const [now] = useState(() => Date.now());
  const limit = settings.data?.stuckHours[s.status];
  const last = s.lastCarrierEventAt ?? s.lastStatusAt;
  const failed =
    s.lastSyncError === undefined || s.lastSyncError === ""
      ? ""
      : `; lần hỏi${s.lastSyncedAt === undefined ? "" : ` ${shortTime(s.lastSyncedAt)}`} lỗi: ${syncErrorText(s.lastSyncError)}`;
  return (
    <div className="box box-err small">
      <b>{`Hãng chưa cập nhật ${String(hoursSilent(s, now))} giờ`}</b>
      {`${limit === undefined ? "" : ` (ngưỡng ${String(limit)} giờ)`}.${
        last === undefined ? "" : ` Cập nhật lần cuối ${shortTime(last)}.`
      } CRM vẫn hỏi hãng theo lịch${failed}.`}
    </div>
  );
}

/** Lý do mờ nút "Hủy / đổi hãng" (null: bấm được). */
function cancelReason(s: Shipment, allowed: boolean): string | null {
  if (s.status === "cancel_requested") return "Đang chờ hãng trả lời yêu cầu hủy";
  if (s.status !== "awaiting_pickup") return TEXT_PICKED_UP;
  return allowed ? null : "Bạn không có quyền hủy vận đơn hay hủy đơn";
}

/** Thẻ dưới khối Vận chuyển: yêu cầu đổi địa chỉ đang mở, khung COD của vận đơn s. */
export function ShipmentExtras({ s, order: o }: { s: Shipment; order: Order }) {
  const { loaded, grants } = useGrants();
  const change = useOpenAddressChange(s.id);
  return (
    <>
      {change.data !== null && change.data !== undefined && (
        <AddressChangeBox
          c={change.data}
          s={s}
          orderId={o.id}
          canShip={canAt(grants, "fulfillment.ship", o.warehouseId ?? "")}
        />
      )}
      <CodBox
        s={s}
        orderCode={o.code}
        canRead={loaded ? hasPermission(grants, "cash.cod.read") : null}
        canAdjust={hasPermission(grants, "cash.adjust")}
      />
    </>
  );
}

/** Nội dung khối Vận chuyển của vận đơn s (đã gửi hãng) của đơn o, kèm các hộp thoại của nó. */
export function ShipmentCard({ s, order: o }: { s: Shipment; order: Order }) {
  const qc = useQueryClient();
  const { loaded, grants } = useGrants();
  const wh = o.warehouseId ?? "";
  const canShip = canAt(grants, "fulfillment.ship", wh);
  const canReplace = canAt(grants, "fulfillment.cancel", wh);
  const canManual = canAt(grants, "fulfillment.manual_status", wh);
  const canPrint = canAt(grants, "fulfillment.print", wh);
  const canCorrect = canAt(grants, "fulfillment.correct", wh);
  const correctWhy = !loaded ? "Đang tải quyền" : canCorrect ? null : TEXT_NO_CORRECT;
  const canCancelOrder = hasPermission(grants, "order.cancel");
  const change = useOpenAddressChange(s.id);
  const [open, setOpen] = useState<Open>(null);
  const [synced, setSynced] = useState(false);
  const sync = useWriteAction((key, b: { id: string }) =>
    unwrap(syncShipment({ path: { id: b.id }, headers: { "Idempotency-Key": key } })),
  );

  const manual = s.mode === "manual";
  const closed = isClosed(s.status);
  const service = s.service === undefined || s.service === "" ? "" : ` · ${s.service}`;
  const canAddress = ADDRESS_CHANGE_STATUSES.includes(s.status);
  const addressWhy = !loaded
    ? "Đang tải quyền"
    : !canShip
      ? TEXT_NO_SHIP_PERM
      : change.isPending
        ? "Đang đọc yêu cầu đổi địa chỉ"
        : change.isError
          ? "Chưa đọc được yêu cầu đổi địa chỉ. Tải lại trang"
          : change.data !== null
            ? "Đang chờ hãng nhận yêu cầu đổi địa chỉ trước"
            : null;
  const cancelWhy = loaded ? cancelReason(s, canReplace || canCancelOrder) : "Đang tải quyền";
  const manualWhy = loaded && !canManual ? "Bạn không có quyền cập nhật trạng thái tay ở kho của đơn này" : null;
  const wantOrderCancel = o.status === "cancel_requested";

  return (
    <>
      <div className="ship-h">
        <div className="grow">
          <p className="small muted wrap-any">{`${account(s)}${service}`}</p>
          <p className="ship-code">{s.trackingCode ?? s.code}</p>
          <p className="xs muted num">{`Mã CRM ${s.code}`}</p>
        </div>
        <span className={statusChip(s.status)}>{statusText(s)}</span>
      </div>
      {manual ? (
        <p className="small">Hãng không báo trạng thái cho CRM. Cập nhật tay khi biết.</p>
      ) : (
        <LastUpdate s={s} />
      )}
      {s.stuckSince !== undefined && !closed && <StuckNote s={s} />}
      {s.status === "cancel_requested" && (
        <div className="box box-soft small">
          <b>{TEXT_CANCEL_WAIT}</b>
          {wantOrderCancel
            ? ' Hãng xác nhận thì đơn hủy hẳn, hàng về "có thể bán".'
            : ' Hãng xác nhận thì đơn về "Đã xác nhận" (hàng vẫn giữ cho đơn), gửi bằng hãng khác được.'}
          {manual &&
            ' Hủy trên trang hãng rồi bấm "Cập nhật trạng thái": Đã hủy (hãng đã hủy) hoặc Đang chuyển (hãng không cho hủy).'}
        </div>
      )}
      {!closed && (
        <div className="row">
          {!manual && (
            <button
              type="button"
              className="btn btn-line"
              disabled={sync.pending}
              onClick={() => {
                setSynced(false);
                void sync.run({ id: s.id }).then((r) => {
                  if (!r.ok) return;
                  setSynced(true);
                  void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
                });
              }}
            >
              Đồng bộ lại
            </button>
          )}
          {canPrint && !manual && (
            <Link className="btn btn-line" to="/warehouse/pack">
              In nhãn
            </Link>
          )}
          {manual && (
            <Act reason={manualWhy}>
              <button
                type="button"
                className="btn btn-line"
                disabled={manualWhy !== null || !loaded}
                onClick={() => {
                  setOpen("manual");
                }}
              >
                Cập nhật trạng thái
              </button>
            </Act>
          )}
          {canAddress && (
            <Act reason={addressWhy}>
              <button
                type="button"
                className="btn btn-line"
                disabled={addressWhy !== null}
                onClick={() => {
                  setOpen("address");
                }}
              >
                Đổi địa chỉ
              </button>
            </Act>
          )}
          {partialAllowed(s) && (
            <Act reason={correctWhy}>
              <button
                type="button"
                className="btn btn-line"
                disabled={correctWhy !== null}
                onClick={() => {
                  setOpen("partial");
                }}
              >
                Ghi giao một phần
              </button>
            </Act>
          )}
          <Act reason={cancelWhy}>
            <button
              type="button"
              className="btn btn-danger"
              disabled={cancelWhy !== null}
              onClick={() => {
                setOpen("cancel");
              }}
            >
              Hủy / đổi hãng
            </button>
          </Act>
        </div>
      )}
      {s.status === "delivered" && (
        <div className="row">
          <Act reason={correctWhy}>
            <button
              type="button"
              className="btn btn-line"
              disabled={correctWhy !== null}
              onClick={() => {
                setOpen("correct");
              }}
            >
              Đảo &quot;Thành công&quot;
            </button>
          </Act>
        </div>
      )}
      {synced && <p className="hint">Đã nhờ CRM hỏi hãng ngay; kết quả hiện ở đây khi hãng trả lời.</p>}
      {sync.message !== null && <div className="box box-err small">{sync.message}</div>}
      <dl className="kv">
        {s.fee !== undefined && (
          <>
            <dt>Phí hãng báo</dt>
            <dd>
              <span className="money">{formatMoney(s.fee)}</span>
            </dd>
          </>
        )}
        {s.weightGrams !== undefined && (
          <>
            <dt>Cân nặng</dt>
            <dd>{`${String(s.weightGrams)} g${s.options?.inspection === "none" ? " · không cho xem hàng" : ""}`}</dd>
          </>
        )}
        <dt>Thu hộ</dt>
        <dd>
          <span className="money">{formatMoney(s.codAmount)}</span>
        </dd>
      </dl>
      {open === "cancel" && (
        <CancelDialog
          s={s}
          order={o}
          canReplace={canReplace}
          canCancelOrder={canCancelOrder}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "address" && (
        <AddressChangeDialog
          s={s}
          order={o}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "correct" && (
        <CorrectionDialog
          s={s}
          order={o}
          canAdjust={hasPermission(grants, "cash.adjust")}
          onClose={() => {
            setOpen(null);
          }}
          onAdjust={() => {
            setOpen("cod");
          }}
        />
      )}
      {open === "cod" && (
        <CodAdjustmentDialog
          s={s}
          orderCode={o.code}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "partial" && (
        <PartialDeliveryDialog
          s={s}
          order={o}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
      {open === "manual" && (
        <ManualStatusDialog
          s={s}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
    </>
  );
}
