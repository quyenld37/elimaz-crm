// Đổi địa chỉ sau khi gửi hãng (kế hoạch M3 Task 134; mockup docs/mockups/m3/don-van-chuyen.html trạng thái F; HV13,
// DH15). Hộp "Đổi địa chỉ giao" gửi requestAddressChange; địa chỉ trên đơn KHÔNG đổi tới khi hãng nhận. Hãng sửa qua
// API (mode api): CRM gửi lệnh, chờ hãng; không (mode manual), hay hãng chưa báo kết quả (sent, unknown): khung "Chờ hãng
// nhận" có "Hãng đã nhận" / "Hãng không đổi được" (resolveAddressChange). Yêu cầu đang mở đọc bằng
// listShipmentAddressChanges nên tải lại trang vẫn xác nhận được.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  requestAddressChange,
  resolveAddressChange,
  type AddressChange,
  type Order,
  type RequestAddressChangeRequest,
  type Shipment,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { reloadOrder } from "../don/orderData";
import { ProvinceField, WardField } from "../len-don/AddressFields";
import type { AddressInput } from "../len-don/orderRules";
import { Dialog } from "../staff/staffData";
import { DialogActions, DialogNotes } from "./dialogParts";
import { Act, TEXT_NO_SHIP_PERM } from "./CreateStateBanner";
import { shipmentCarrier } from "./CancelDialog";
import { addressChangeKey, shipmentKey } from "./useShipment";

export const TEXT_CALL_CARRIER = 'Gọi hãng đổi địa chỉ, sau đó bấm "Hãng đã nhận".';

/** Địa chỉ một dòng của đơn (số nhà, xã, tỉnh). */
export function addressLine(o: Order): string {
  const a = o.shipAddress;
  return a === undefined
    ? ""
    : [a.line, a.wardName, a.provinceName].filter((x) => x !== undefined && x !== "").join(", ");
}

interface Req {
  shipmentId: string;
  body: RequestAddressChangeRequest;
}

/** Hộp đổi địa chỉ giao của vận đơn s (đơn o): người nhận, SĐT giữ như đơn; đổi số nhà, tỉnh, xã. */
export function AddressChangeDialog({ s, order: o, onClose }: { s: Shipment; order: Order; onClose: () => void }) {
  const qc = useQueryClient();
  const cur = o.shipAddress;
  const [v, setV] = useState<AddressInput>({
    name: cur?.name ?? "",
    phone: cur?.phone ?? "",
    line: "",
    provinceCode: cur?.provinceCode ?? "",
    wardCode: "",
    wardName: "",
  });
  const write = useWriteAction((key, b: Req) =>
    unwrap(requestAddressChange({ path: { id: b.shipmentId }, body: b.body, headers: { "Idempotency-Key": key } })),
  );
  const line = v.line.trim();
  const why =
    line === ""
      ? "Nhập địa chỉ mới"
      : v.provinceCode === ""
        ? "Chọn tỉnh/thành"
        : v.wardCode === ""
          ? "Chọn xã/phường"
          : cur === undefined
            ? "Đơn chưa có người nhận"
            : null;
  const submit = async () => {
    const r = await write.run({
      shipmentId: s.id,
      body: {
        address: { name: v.name, phone: v.phone, line, provinceCode: v.provinceCode, wardCode: v.wardCode },
      },
    });
    if (!r.ok) return;
    qc.setQueryData(addressChangeKey(s.id), { items: [r.data] });
    void qc.invalidateQueries({ queryKey: addressChangeKey(s.id) });
    onClose();
  };
  const carrier = shipmentCarrier(s);
  return (
    <Dialog
      title="Đổi địa chỉ giao"
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Gửi yêu cầu cho hãng"
          why={why}
          pending={write.pending}
          onSubmit={() => void submit()}
          onClose={onClose}
        />
      }
    >
      <p className="small muted wrap-any">{`Đang giao tới: ${addressLine(o)}`}</p>
      <label className="field">
        <span>Địa chỉ mới *</span>
        <input
          className="input"
          maxLength={500}
          autoComplete="off"
          value={v.line}
          onChange={(e) => {
            setV({ ...v, line: e.target.value });
          }}
        />
      </label>
      <div className="grid-f">
        <ProvinceField label="Tỉnh *" value={v} onChange={setV} />
        <WardField value={v} onChange={setV} />
      </div>
      <p className="small">
        {`${carrier} sửa được qua CRM thì CRM gửi yêu cầu cho hãng; không thì CRM ghi yêu cầu, bạn gọi hãng rồi bấm "Hãng đã nhận". Hãng nhận thì địa chỉ trên đơn mới đổi; hãng từ chối thì giữ địa chỉ cũ.`}
      </p>
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}

interface Resolve {
  id: string;
  result: "confirmed" | "rejected";
}

/** Câu của yêu cầu đang mở c (vận đơn s) và nhân viên có phải xác nhận không. */
function changeNote(c: AddressChange, carrier: string): { lead: string; text: string; confirm: boolean } {
  if (c.mode === "manual") {
    return {
      lead: `${carrier} không cho sửa địa chỉ qua phần mềm.`,
      text: ` ${TEXT_CALL_CARRIER} Địa chỉ trên đơn vẫn là địa chỉ cũ tới khi bấm.`,
      confirm: true,
    };
  }
  if (c.status === "pending") {
    return {
      lead: `Đã gửi yêu cầu cho ${carrier}.`,
      text: " Hãng nhận thì địa chỉ trên đơn mới đổi; hãng từ chối thì giữ địa chỉ cũ.",
      confirm: false,
    };
  }
  return {
    lead: c.status === "unknown" ? `Không rõ ${carrier} đã nhận yêu cầu chưa.` : `${carrier} chưa báo kết quả.`,
    text: ` Kiểm trên trang hãng (hoặc gọi hãng), rồi bấm một nút. Địa chỉ trên đơn vẫn là địa chỉ cũ tới khi bấm.`,
    confirm: true,
  };
}

/** Khung "Đổi địa chỉ · Chờ hãng nhận" của yêu cầu đang mở c; canShip: quyền fulfillment.ship ở kho của đơn. */
export function AddressChangeBox(props: { c: AddressChange; s: Shipment; orderId: string; canShip: boolean }) {
  const { c, s } = props;
  const qc = useQueryClient();
  const write = useWriteAction((key, b: Resolve) =>
    unwrap(
      resolveAddressChange({ path: { id: b.id }, body: { result: b.result }, headers: { "Idempotency-Key": key } }),
    ),
  );
  const n = changeNote(c, shipmentCarrier(s));
  const run = async (result: Resolve["result"]) => {
    const r = await write.run({ id: c.id, result });
    if (!r.ok) {
      void qc.invalidateQueries({ queryKey: addressChangeKey(s.id) });
      return;
    }
    qc.setQueryData(addressChangeKey(s.id), { items: [] });
    void qc.invalidateQueries({ queryKey: addressChangeKey(s.id) });
    void qc.invalidateQueries({ queryKey: shipmentKey(s.id) });
    reloadOrder(qc, props.orderId);
  };
  const why = props.canShip ? null : TEXT_NO_SHIP_PERM;
  return (
    <section className="card ship" aria-label="Đổi địa chỉ">
      <div className="row-between">
        <h2>Đổi địa chỉ</h2>
        <span className="chip">Chờ hãng nhận</span>
      </div>
      <div className="box box-soft small">
        <b>{n.lead}</b>
        {n.text}
      </div>
      {n.confirm && (
        <div className="row">
          <Act reason={why}>
            <button
              type="button"
              className="btn btn-dark"
              disabled={why !== null || write.pending}
              onClick={() => void run("confirmed")}
            >
              Hãng đã nhận
            </button>
          </Act>
          <button
            type="button"
            className="btn btn-line"
            disabled={why !== null || write.pending}
            onClick={() => void run("rejected")}
          >
            Hãng không đổi được
          </button>
        </div>
      )}
      {write.message !== null && <div className="box box-err small">{write.message}</div>}
    </section>
  );
}
