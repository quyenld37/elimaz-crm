// Quay về từ Facebook: /settings/channels/facebook/callback?code=…&state=… (kế hoạch M4 Task 178; mockup
// ket-noi-page.html trạng thái B, HV35). Đọc code, state rồi xóa chúng khỏi thanh địa chỉ (history.replaceState) trước
// khi gọi exchangeFacebookOAuth; hiện hộp chọn Page trên trang Kênh kết nối; chọn → connectFacebookPage. Page thiếu
// quyền hiện mờ kèm lý do; Page mới luôn ở chế độ chỉ xem (Q22).
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import {
  connectFacebookPage,
  exchangeFacebookOAuth,
  type FacebookOAuthPages,
  type FacebookPageOption,
} from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction, type WriteResult } from "../../app/idempotency";
import { Dialog } from "../staff/staffData";
import { ChannelsPage } from "./ChannelsPage";
import { ConnectError } from "./connectErrors";
import { CHANNELS_KEY } from "./kenhData";

export const TEXT_NO_CODE = "Facebook không trả mã đăng nhập (có thể bạn đã hủy).";
const CHANNELS_PATH = "/settings/channels";

/** code, state Facebook gửi về trên thanh địa chỉ; thiếu một trong hai → null. */
function readOAuthParams(): { code: string; state: string } | null {
  const q = new URLSearchParams(window.location.search);
  const code = q.get("code") ?? "";
  const state = q.get("state") ?? "";
  return code === "" || state === "" ? null : { code, state };
}

/** Xóa mọi tham số khỏi thanh địa chỉ (chỉ giữ đường dẫn), trước khi gửi mã đi đổi. */
function clearOAuthParams() {
  if (window.location.search !== "") window.history.replaceState(window.history.state, "", window.location.pathname);
}

function missingRight(p: FacebookPageOption): string | null {
  if (!p.canMessage && !p.canModerate) return "thiếu quyền nhắn tin và kiểm duyệt comment";
  if (!p.canMessage) return "thiếu quyền nhắn tin";
  if (!p.canModerate) return "thiếu quyền kiểm duyệt comment";
  return null;
}

function PagePicker({ result, onClose }: { result: FacebookOAuthPages; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const usable = result.pages.filter((p) => missingRight(p) === null);
  const [pageId, setPageId] = useState(usable[0]?.pageId ?? "");
  const connect = useWriteAction((key, b: { pageId: string; sessionId: string }) =>
    unwrap(connectFacebookPage({ body: b, headers: { "Idempotency-Key": key } })),
  );
  const chosen = result.pages.find((p) => p.pageId === pageId);
  return (
    <Dialog
      title="Chọn Page để kết nối"
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
            disabled={chosen === undefined || connect.pending}
            onClick={() => {
              void connect.run({ pageId, sessionId: result.sessionId }).then((r) => {
                if (!r.ok) return;
                void qc.invalidateQueries({ queryKey: CHANNELS_KEY });
                void navigate(CHANNELS_PATH, { replace: true });
              });
            }}
          >
            {chosen === undefined ? "Kết nối" : `Kết nối ${chosen.name}`}
          </button>
        </>
      }
    >
      {result.pages.length === 0 ? (
        <p className="small">Tài khoản Facebook này không quản trị Page nào.</p>
      ) : (
        <>
          <p className="small muted">
            Facebook cho CRM thấy {String(result.pages.length)} Page bạn quản trị. Chọn một Page.
          </p>
          <div className="radios" role="radiogroup" aria-label="Page">
            {result.pages.map((p) => {
              const missing = missingRight(p);
              return (
                <label key={p.pageId} className={missing === null ? "check" : "check is-off"}>
                  <input
                    type="radio"
                    name="fb-page"
                    checked={p.pageId === pageId}
                    disabled={missing !== null}
                    onChange={() => {
                      setPageId(p.pageId);
                    }}
                  />{" "}
                  {p.name} · {missing ?? "đủ quyền nhắn tin và kiểm duyệt"}
                </label>
              );
            })}
          </div>
        </>
      )}
      <div className="box box-soft small">
        <b>Page mới kết nối ở chế độ chỉ xem.</b> CRM nhận tin và comment nhưng không gửi gì ra Facebook (kể cả tin tự
        động) cho tới khi bạn tắt chỉ xem.
      </div>
      <p className="hint">Danh sách này hết hạn sau 30 phút; quá hạn thì bấm "Kết nối Page Facebook" lại.</p>
      {connect.error !== null && <ConnectError error={connect.error} pageName={`Page "${chosen?.name ?? pageId}"`} />}
    </Dialog>
  );
}

export function FacebookCallback() {
  const navigate = useNavigate();
  const exchange = useWriteAction((key, b: { code: string; state: string }) =>
    unwrap(exchangeFacebookOAuth({ body: b, headers: { "Idempotency-Key": key } })),
  );
  const [result, setResult] = useState<FacebookOAuthPages | null>(null);
  // Đọc một lần lúc dựng trang (không đổi thanh địa chỉ ở đây); hiệu ứng dưới xóa chúng rồi mới gọi API.
  const [params] = useState(readOAuthParams);
  const missing = params === null;
  const started = useRef(false);
  const runRef = useRef<((b: { code: string; state: string }) => Promise<WriteResult<FacebookOAuthPages>>) | null>(
    null,
  );
  useEffect(() => {
    runRef.current = (b) => exchange.run(b);
  });
  useEffect(() => {
    // Một lần cho mỗi lần quay về (StrictMode chạy hiệu ứng hai lần; mã đăng nhập chỉ dùng được một lần).
    if (started.current) return;
    started.current = true;
    clearOAuthParams();
    if (params === null || runRef.current === null) return;
    void runRef.current(params).then((r) => {
      if (r.ok) setResult(r.data);
    });
  }, [params]);

  const close = () => {
    void navigate(CHANNELS_PATH, { replace: true });
  };
  const failed = missing || exchange.error !== null;
  return (
    <ChannelsPage>
      {exchange.pending && (
        <p className="muted" role="status" style={{ marginTop: 16 }}>
          Đang lấy danh sách Page từ Facebook…
        </p>
      )}
      {failed && (
        <div className="card" style={{ marginTop: 16 }}>
          <h3>Lỗi khi kết nối</h3>
          <div className="stack-sm" style={{ marginTop: 8 }}>
            {missing ? (
              <div className="box box-err small" role="alert">
                <b>{TEXT_NO_CODE}</b> Bấm "Kết nối Page Facebook" để làm lại.
              </div>
            ) : (
              <ConnectError error={exchange.error} pageName="Page" />
            )}
          </div>
        </div>
      )}
      {result !== null && <PagePicker result={result} onClose={close} />}
    </ChannelsPage>
  );
}
