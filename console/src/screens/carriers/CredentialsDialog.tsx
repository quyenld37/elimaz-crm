// Hộp "Thay khóa" (mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái D; HV04): ô mật khẩu trống, khóa cũ bị
// thay hẳn khi lưu; máy chủ tự đặt "Đang kiểm tra" và ghi lệnh kiểm (putCarrierCredentials). Khóa chỉ nằm trong state của
// hộp; gửi xong là xóa và đóng hộp; không bao giờ vào localStorage. Khóa không bao giờ hiện lại (API không trả).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { putCarrierCredentials, type CarrierAccount } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog, RefusalBox } from "../staff/staffData";
import { accountsKey } from "./carrierData";
import { carrierName, credentialFields, credentialsBody } from "./carrierRules";

export function CredentialsDialog({ account, onClose }: { account: CarrierAccount; onClose: () => void }) {
  const qc = useQueryClient();
  const fields = credentialFields(account.carrierKey);
  const [values, setValues] = useState<Record<string, string>>({});
  const [missing, setMissing] = useState(false);
  const save = useWriteAction((key, credentials: Record<string, string>) =>
    unwrap(
      putCarrierCredentials({ path: { id: account.id }, body: { credentials }, headers: { "Idempotency-Key": key } }),
    ),
  );
  const onSave = async () => {
    const creds = credentialsBody(values);
    const complete = creds !== null && fields.every((f) => (creds[f.key] ?? "") !== "");
    setMissing(!complete);
    if (!complete) return;
    const r = await save.run(creds);
    if (!r.ok) return;
    setValues({});
    void qc.invalidateQueries({ queryKey: accountsKey });
    onClose();
  };
  return (
    <Dialog
      title={`Thay khóa · ${carrierName(account.carrierKey)}`}
      closeButton
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={save.pending}
            onClick={() => {
              void onSave();
            }}
          >
            Lưu khóa mới
          </button>
        </>
      }
    >
      <p className="small muted wrap-any">{account.name}</p>
      {fields.map((f) => (
        <label className="field" key={f.key}>
          <span>{f.label} *</span>
          <input
            className="input secret"
            type="password"
            autoComplete="new-password"
            aria-label={f.label}
            maxLength={4096}
            value={values[f.key] ?? ""}
            onChange={(e) => {
              setValues((v) => ({ ...v, [f.key]: e.target.value }));
            }}
          />
        </label>
      ))}
      {missing && <span className="field-err">Nhập đủ các ô khóa</span>}
      <p className="hint">Khóa cũ bị thay hẳn khi lưu. Lưu xong CRM tự kiểm tra kết nối.</p>
      {save.error !== null && <RefusalBox error={save.error} message={save.message} />}
    </Dialog>
  );
}
