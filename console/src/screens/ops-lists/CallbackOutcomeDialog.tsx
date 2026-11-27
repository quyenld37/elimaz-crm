// Hộp "Ghi kết quả cuộc gọi" (mockup docs/mockups/m3/van-hanh.html trạng thái B): năm kết quả; "Không nghe máy" và
// "Khách hẹn gọi lại" có ô "Gọi lại lúc" (việc sang Đang xử lý); "Giao lại tới địa chỉ mới" đòi địa chỉ (người nhận và
// SĐT lấy từ đơn). Gửi `version` đã xem: 409 → câu "người khác vừa sửa" và tải lại danh sách.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { getOrder, recordCallbackOutcome, type CallbackTask, type RecordCallbackOutcomeRequest } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { useWriteAction } from "../../shared";
import { ProvinceField, WardField } from "../len-don/AddressFields";
import type { AddressInput } from "../len-don/orderRules";
import { DialogActions, DialogNotes } from "../shipping/dialogParts";
import { fromVnLocal } from "../shipping/shipmentRules";
import { Dialog } from "../staff/staffData";
import { headers, reloadOps, usePerms } from "./opsData";
import { OUTCOMES, needsCallTime, outcomeHelp, type OutcomeKey } from "./opsRules";

interface Req {
  id: string;
  body: RecordCallbackOutcomeRequest;
}

export function CallbackOutcomeDialog({ task, onClose }: { task: CallbackTask; onClose: () => void }) {
  const qc = useQueryClient();
  const perms = usePerms();
  const [outcome, setOutcome] = useState<OutcomeKey | "">("");
  const [when, setWhen] = useState("");
  const [note, setNote] = useState("");
  const [addr, setAddr] = useState<AddressInput>({
    name: "",
    phone: "",
    line: "",
    provinceCode: "",
    wardCode: "",
    wardName: "",
  });
  const newAddress = outcome === "redeliver_new_address";
  // Người nhận và SĐT của địa chỉ mới lấy từ đơn (cần quyền xem đơn); chỉ đọc khi chọn kết quả này.
  const order = useQuery({
    queryKey: queryKey("order.changed", "callback-address", task.orderId),
    enabled: newAddress && perms.can("order.read"),
    gcTime: 0,
    queryFn: () => unwrap(getOrder({ path: { id: task.orderId } })),
  });
  const write = useWriteAction((key, r: Req) =>
    unwrap(recordCallbackOutcome({ path: { id: r.id }, body: r.body, headers: headers(key) })),
  );
  const recipient = order.data?.shipAddress;
  const whenIso = when === "" ? null : fromVnLocal(when);
  const line = addr.line.trim();
  const why =
    outcome === ""
      ? "Chọn kết quả cuộc gọi"
      : outcome === "reschedule" && whenIso === null
        ? "Chọn giờ gọi lại"
        : newAddress && !perms.can("order.read")
          ? "Không có quyền xem đơn để lấy người nhận"
          : newAddress && recipient === undefined
            ? "Đang đọc người nhận của đơn"
            : newAddress && line === ""
              ? "Nhập địa chỉ mới"
              : newAddress && addr.provinceCode === ""
                ? "Chọn tỉnh/thành"
                : newAddress && addr.wardCode === ""
                  ? "Chọn xã/phường"
                  : null;
  const submit = async () => {
    if (outcome === "") return;
    const body: RecordCallbackOutcomeRequest = { outcome, version: task.version };
    if (note.trim() !== "") body.note = note.trim();
    if (needsCallTime(outcome) && whenIso !== null) body.nextCallAt = whenIso;
    if (newAddress && recipient !== undefined) {
      body.newAddress = {
        name: recipient.name,
        phone: recipient.phone,
        line,
        provinceCode: addr.provinceCode,
        wardCode: addr.wardCode,
      };
    }
    const r = await write.run({ id: task.id, body });
    // Thành công hay 409 (việc đã đổi): tải lại để thấy bản mới; chỉ thành công mới đóng hộp.
    reloadOps(qc);
    if (r.ok) onClose();
  };
  return (
    <Dialog
      title={`Kết quả gọi · ${task.orderCode}`}
      closeButton
      onClose={onClose}
      footer={
        <DialogActions
          label="Lưu kết quả"
          why={why}
          pending={write.pending}
          onSubmit={() => void submit()}
          onClose={onClose}
        />
      }
    >
      <fieldset className="ops-radios">
        <legend className="sr-only">Kết quả cuộc gọi</legend>
        {OUTCOMES.map((o) => (
          <label key={o.key} className="ops-radio">
            <input
              type="radio"
              name="outcome"
              checked={outcome === o.key}
              onChange={() => {
                setOutcome(o.key);
              }}
            />
            <span>{o.label}</span>
          </label>
        ))}
      </fieldset>
      {outcome !== "" && <p className="small muted">{outcomeHelp(outcome)}</p>}
      {outcome !== "" && needsCallTime(outcome) && (
        <label className="field">
          <span>{outcome === "reschedule" ? "Gọi lại lúc *" : "Gọi lại lúc"}</span>
          <input
            className="input"
            type="datetime-local"
            value={when}
            onChange={(e) => {
              setWhen(e.target.value);
            }}
          />
        </label>
      )}
      {newAddress && (
        <>
          <label className="field">
            <span>Địa chỉ mới *</span>
            <input
              className="input"
              maxLength={500}
              autoComplete="off"
              value={addr.line}
              onChange={(e) => {
                setAddr({ ...addr, line: e.target.value });
              }}
            />
          </label>
          <div className="grid-f">
            <ProvinceField label="Tỉnh *" value={addr} onChange={setAddr} />
            <WardField value={addr} onChange={setAddr} />
          </div>
        </>
      )}
      <label className="field">
        <span>Ghi chú</span>
        <textarea
          className="input"
          maxLength={1000}
          rows={3}
          value={note}
          onChange={(e) => {
            setNote(e.target.value);
          }}
        />
      </label>
      <DialogNotes why={why} message={write.message} />
    </Dialog>
  );
}
