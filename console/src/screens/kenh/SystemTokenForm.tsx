// Kết nối Page bằng mã system user (mockup ket-noi-page.html trạng thái C; HV35). Mã chỉ sống trong ô mật khẩu (không
// điều khiển bằng state) và trong biến cục bộ của một lần gửi: đọc bằng ref, xóa ô ngay khi gửi, không vào
// localStorage, sessionStorage, bộ đệm truy vấn hay ref nhớ thân yêu cầu của useWriteAction (Tái dùng của tệp task).
// Mỗi lần bấm một khóa chống trùng mới; nối lại cùng Page là lũy đẳng ở máy chủ (Task 149 quyết định 6).
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { connectFacebookPage, type ChannelAccount } from "../../api";
import { unwrap } from "../../app/errors";
import { Dialog } from "../staff/staffData";
import { CHANNELS_KEY } from "./kenhData";
import { ConnectError } from "./connectErrors";

export interface SystemTokenFormProps {
  onClose: () => void;
  onConnected: (a: ChannelAccount) => void;
}

export function SystemTokenForm({ onClose, onConnected }: SystemTokenFormProps) {
  const qc = useQueryClient();
  const tokenRef = useRef<HTMLInputElement>(null);
  const [pageId, setPageId] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [fieldErr, setFieldErr] = useState<{ token?: string; pageId?: string }>({});

  const submit = async () => {
    const input = tokenRef.current;
    if (input === null || pending) return;
    const token = input.value;
    const errs: { token?: string; pageId?: string } = {};
    if (token.trim() === "") errs.token = "Nhập mã truy cập system user.";
    if (!/^\d{1,32}$/.test(pageId.trim())) errs.pageId = "Page ID chỉ gồm chữ số.";
    setFieldErr(errs);
    if (errs.token !== undefined || errs.pageId !== undefined) return;
    input.value = "";
    setPending(true);
    setError(null);
    try {
      const a = await unwrap(
        connectFacebookPage({
          body: { pageId: pageId.trim(), systemUserToken: token },
          headers: { "Idempotency-Key": crypto.randomUUID() },
        }),
      );
      void qc.invalidateQueries({ queryKey: CHANNELS_KEY });
      onConnected(a);
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog
      title="Kết nối bằng mã system user"
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
            disabled={pending}
            onClick={() => {
              void submit();
            }}
          >
            {pending ? "Đang kết nối…" : "Kết nối"}
          </button>
        </>
      }
    >
      <label className="field">
        <span>Mã truy cập system user *</span>
        <input
          ref={tokenRef}
          className={fieldErr.token === undefined ? "input" : "input is-err"}
          type="password"
          autoComplete="off"
          spellCheck={false}
          {...(fieldErr.token === undefined ? {} : { "aria-invalid": true })}
        />
        {fieldErr.token !== undefined && <span className="field-err">{fieldErr.token}</span>}
      </label>
      <label className="field">
        <span>Page ID *</span>
        <input
          className={fieldErr.pageId === undefined ? "input num" : "input num is-err"}
          inputMode="numeric"
          value={pageId}
          {...(fieldErr.pageId === undefined ? {} : { "aria-invalid": true })}
          onChange={(e) => {
            setPageId(e.target.value);
          }}
        />
        {fieldErr.pageId !== undefined && <span className="field-err">{fieldErr.pageId}</span>}
      </label>
      <p className="hint">
        Dùng khi shop quản lý Page bằng Business Manager. Mã chỉ dùng một lần để lấy mã của Page, CRM không lưu; ô tự
        xóa ngay sau khi gửi.
      </p>
      {error !== null && <ConnectError error={error} pageName={`Page ID ${pageId.trim()}`} />}
    </Dialog>
  );
}
