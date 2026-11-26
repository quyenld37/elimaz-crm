// Kênh kết nối /settings/channels (kế hoạch M4 Task 178; mockup docs/mockups/m4/ket-noi-page.html trạng thái A, C;
// HV32, HV34–HV38). Mỗi dòng là một tài khoản kênh (M4 chỉ Facebook Page): tính năng kênh, trạng thái, chế độ gửi, lần
// kiểm, webhook; công tắc chỉ xem (tắt phải xác nhận), Human Agent (chỉ bật khi Meta đã cấp), kiểm tra, ngắt (hỏi lại).
// "Kết nối Page Facebook" mở Facebook trong cùng tab (startFacebookOAuth → window.location.assign).
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import {
  disconnectChannel,
  runChannelHealthCheck,
  setChannelHumanAgent,
  setChannelSendMode,
  startFacebookOAuth,
  type ChannelAccount,
  type ChannelSendMode,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { fullTime } from "../../app/time";
import { RefusalBox } from "../staff/staffData";
import { ConfirmDialog } from "./settingsKit";
import { CHANNELS_KEY, useChannels } from "./kenhData";
import {
  FEATURE_LABELS,
  WEBHOOK_FIELDS_TOTAL,
  channelIcon,
  channelKind,
  featuresOf,
  isFacebookAuthorizeUrl,
} from "./kenhRules";
import { SystemTokenForm } from "./SystemTokenForm";
import "./kenh.css";

export const TEXT_BAD_AUTHORIZE_URL = "Địa chỉ đăng nhập Facebook không hợp lệ. Thử lại sau ít phút";
/** Chờ kết quả kiểm tra kết nối (báo hiệu channel.status) tối đa chừng này. */
export const HEALTH_WAIT_MS = 60_000;

/** Bắt đầu đăng nhập Facebook rồi chuyển cả tab sang Facebook (HV35). */
export function useStartFacebookOAuth() {
  const start = useWriteAction((key: string) => unwrap(startFacebookOAuth({ headers: { "Idempotency-Key": key } })));
  const [badUrl, setBadUrl] = useState(false);
  const run = async () => {
    setBadUrl(false);
    const r = await start.run(null);
    if (!r.ok) return;
    if (!isFacebookAuthorizeUrl(r.data.authorizeUrl)) {
      setBadUrl(true);
      return;
    }
    window.location.assign(r.data.authorizeUrl);
  };
  const message = badUrl ? TEXT_BAD_AUTHORIZE_URL : start.message;
  return { run, pending: start.pending, message };
}

/** Câu giải thích lý do `degraded` (Task 150 quyết định 7). */
function degradedText(reason: string | undefined): { chip: string; title: string; detail: string } {
  if (reason?.startsWith("meta_policy") === true) {
    return {
      chip: "Bị Meta chặn",
      title: "Meta đang chặn một thao tác của Page.",
      detail: "CRM vẫn nhận và lưu tin, comment mới. Chặn được gỡ khi Meta báo hết hạn chế.",
    };
  }
  const why: Readonly<Record<string, string>> = {
    token_invalid: "Mã truy cập không còn hiệu lực (quản trị Page đổi mật khẩu hoặc gỡ quyền app).",
    auth_failed: "Mã truy cập không còn hiệu lực (quản trị Page đổi mật khẩu hoặc gỡ quyền app).",
    missing_permissions: "Tài khoản đã kết nối không còn đủ quyền nhắn tin và kiểm duyệt comment.",
    subscribe_failed: "CRM chưa đăng ký nhận tin với Facebook được.",
  };
  return {
    chip: "Mất quyền",
    title: "Page mất quyền.",
    detail: `${reason !== undefined && Object.hasOwn(why, reason) ? `${why[reason] ?? ""} ` : ""}CRM vẫn nhận và lưu tin, comment mới nhưng không gửi được. Cần một quản trị của Page kết nối lại.`,
  };
}

function StatusChip({ a }: { a: ChannelAccount }) {
  if (a.status === "active") return <span className="chip chip-ok">Đang hoạt động</span>;
  if (a.status === "disconnected") return <span className="chip">Đã ngắt</span>;
  return <span className="chip chip-err">{degradedText(a.statusReason).chip}</span>;
}

type Ask = "enable-send" | "disconnect" | null;

function ChannelRow({
  a,
  onReconnect,
  reconnecting,
}: {
  a: ChannelAccount;
  onReconnect: () => void;
  reconnecting: boolean;
}) {
  const qc = useQueryClient();
  const reload = () => {
    void qc.invalidateQueries({ queryKey: CHANNELS_KEY });
  };
  const done = (x: ChannelAccount) => {
    qc.setQueryData(CHANNELS_KEY, (old: { items: ChannelAccount[] } | undefined) =>
      old === undefined ? old : { items: old.items.map((i) => (i.id === x.id ? x : i)) },
    );
    reload();
  };
  const sendMode = useWriteAction((key, b: { id: string; mode: ChannelSendMode; expectedVersion: number }) =>
    unwrap(
      setChannelSendMode({
        path: { id: b.id },
        body: { mode: b.mode, expectedVersion: b.expectedVersion },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const humanAgent = useWriteAction((key, b: { id: string; enabled: boolean; expectedVersion: number }) =>
    unwrap(
      setChannelHumanAgent({
        path: { id: b.id },
        body: { enabled: b.enabled, expectedVersion: b.expectedVersion },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const disconnect = useWriteAction((key, b: { id: string }) =>
    unwrap(disconnectChannel({ path: { id: b.id }, headers: { "Idempotency-Key": key } })),
  );
  const health = useWriteAction((key, b: { id: string; nonce: number }) =>
    unwrap(runChannelHealthCheck({ path: { id: b.id }, headers: { "Idempotency-Key": key } })),
  );
  const [ask, setAsk] = useState<Ask>(null);
  // Kiểm tra kết nối: nhớ tokenCheckedAt (giờ máy chủ) lúc bấm; xong khi máy chủ ghi một lần kiểm khác (kết quả về
  // qua báo hiệu channel.status) hoặc hết giờ chờ. Không so với giờ máy nhân viên (hai đồng hồ lệch nhau).
  const [checkFrom, setCheckFrom] = useState<{ before: string | undefined } | null>(null);
  const [checkTimedOut, setCheckTimedOut] = useState(false);
  const checkDone = checkFrom !== null && a.tokenCheckedAt !== undefined && a.tokenCheckedAt !== checkFrom.before;
  const checking = checkFrom !== null && !checkDone && !checkTimedOut;
  useEffect(() => {
    if (!checking) return;
    const t = setTimeout(() => {
      setCheckTimedOut(true);
    }, HEALTH_WAIT_MS);
    return () => {
      clearTimeout(t);
    };
  }, [checking]);

  const readOnly = a.sendMode === "read_only";
  const connected = a.status !== "disconnected";
  const f = featuresOf(a);
  const lastError = [sendMode, humanAgent, disconnect, health].find((w) => w.error !== null);
  const deg = a.status === "degraded" ? degradedText(a.statusReason) : null;

  const setMode = async (mode: ChannelSendMode) => {
    const r = await sendMode.run({ id: a.id, mode, expectedVersion: a.version });
    setAsk(null);
    if (r.ok) done(r.data);
  };

  return (
    <div className="chrow">
      <span className="ch-ic" aria-hidden="true">
        {channelIcon(a.channel)}
      </span>
      <div className="stack-sm">
        <div className="row" style={{ gap: 6 }}>
          <b className="ellip">{a.name}</b>
          <StatusChip a={a} />
          {connected &&
            (readOnly ? <span className="chip">Chỉ xem</span> : <span className="chip chip-dark">Đang gửi thật</span>)}
        </div>
        <span className="small muted num">
          {channelKind(a.channel)} · Page ID {a.externalId}
        </span>
        <div className="feats" aria-label="Tính năng kênh hỗ trợ">
          {FEATURE_LABELS.filter((x) => f[x.key]).map((x) => (
            <span key={x.key} className="chip chip-line">
              ✓ {x.label}
            </span>
          ))}
        </div>
        {deg !== null && (
          <div className="box box-err small" role="alert">
            <b>{deg.title}</b> {deg.detail}
          </div>
        )}
        <dl className="kv">
          <dt>Lần kiểm gần nhất</dt>
          <dd>
            {a.tokenCheckedAt === undefined
              ? "Chưa kiểm"
              : `${fullTime(a.tokenCheckedAt)} · ${a.status === "active" ? "mã truy cập còn hiệu lực · đủ quyền" : "có lỗi"}`}
          </dd>
          <dt>Webhook</dt>
          <dd>
            Đã đăng ký {String(a.subscribedFields.length)}/{String(WEBHOOK_FIELDS_TOTAL)} trường
          </dd>
        </dl>
        {checkDone && a.status === "active" && (
          <div className="box box-ok small" role="status">
            <b className="ok-text">
              {a.name} ổn · {a.tokenCheckedAt === undefined ? "" : fullTime(a.tokenCheckedAt)}
            </b>
            <br />
            Mã truy cập còn hiệu lực, đủ quyền, {String(a.subscribedFields.length)}/{String(WEBHOOK_FIELDS_TOTAL)}{" "}
            trường webhook.
          </div>
        )}
        {checkTimedOut && !checkDone && (
          <div className="box box-soft small" role="status">
            Chưa có kết quả kiểm tra sau 60 giây. CRM tự kiểm mỗi giờ; tải lại trang để xem kết quả mới.
          </div>
        )}
        {lastError !== undefined && (
          <RefusalBox
            error={lastError.error}
            message={lastError.message}
            conflictTitle="Có người vừa đổi cài đặt Page này, tải lại"
            onReload={reload}
          />
        )}
      </div>
      <div className="stack-sm c-end">
        {!connected || a.status === "degraded" ? (
          <button type="button" className="btn btn-dark" disabled={reconnecting} onClick={onReconnect}>
            Kết nối lại
          </button>
        ) : null}
        {connected && (
          <>
            <button
              type="button"
              className="toggle"
              aria-pressed={readOnly}
              disabled={sendMode.pending}
              onClick={() => {
                if (readOnly) setAsk("enable-send");
                else void setMode("read_only");
              }}
            >
              Chỉ xem: {readOnly ? "bật" : "tắt"}
            </button>
            {f.humanAgent && (
              <div className="act">
                <button
                  type="button"
                  className="toggle"
                  aria-pressed={a.humanAgentEnabled}
                  disabled={humanAgent.pending}
                  onClick={() => {
                    void humanAgent
                      .run({ id: a.id, enabled: !a.humanAgentEnabled, expectedVersion: a.version })
                      .then((r) => {
                        if (r.ok) done(r.data);
                      });
                  }}
                >
                  Human Agent: {a.humanAgentEnabled ? "bật" : "tắt"}
                </button>
                <span className="reason">
                  Chỉ bật khi Meta đã cấp. Bật: trả lời được tới 7 ngày sau tin cuối của khách.
                </span>
              </div>
            )}
            <div className="row">
              <button
                type="button"
                className="btn btn-quiet"
                disabled={checking || health.pending}
                onClick={() => {
                  const before = a.tokenCheckedAt;
                  setCheckTimedOut(false);
                  setCheckFrom(null);
                  // nonce: mỗi lần bấm là một thao tác mới (khóa chống trùng mới).
                  void health.run({ id: a.id, nonce: Date.now() }).then((r) => {
                    if (r.ok) {
                      setCheckFrom({ before });
                      reload();
                    }
                  });
                }}
              >
                {checking || health.pending ? "Đang kiểm tra…" : "Kiểm tra kết nối"}
              </button>
              <button
                type="button"
                className="btn btn-quiet btn-danger"
                onClick={() => {
                  setAsk("disconnect");
                }}
              >
                Ngắt kết nối
              </button>
            </div>
          </>
        )}
      </div>
      {ask === "enable-send" && (
        <ConfirmDialog
          title={`Bật gửi thật cho Page ${a.name}?`}
          confirmLabel="Bật gửi thật"
          pending={sendMode.pending}
          onCancel={() => {
            setAsk(null);
          }}
          onConfirm={() => {
            void setMode("enabled");
          }}
        >
          <p>Khách sẽ nhận tin từ CRM.</p>
          <p className="small muted">
            Quy tắc tự động đang bật (tự ẩn, tự trả lời comment, tin chào) chạy với tin và comment mới từ lúc này, không
            chạy lại cho tin cũ.
          </p>
        </ConfirmDialog>
      )}
      {ask === "disconnect" && (
        <ConfirmDialog
          title={`Ngắt kết nối Page ${a.name}?`}
          confirmLabel="Ngắt kết nối"
          danger
          pending={disconnect.pending}
          onCancel={() => {
            setAsk(null);
          }}
          onConfirm={() => {
            void disconnect.run({ id: a.id }).then((r) => {
              setAsk(null);
              if (r.ok) done(r.data);
            });
          }}
        >
          <p className="small">
            CRM hủy đăng ký nhận tin với Facebook rồi xóa mã truy cập của Page. Hội thoại, tin và comment đã nhận vẫn
            giữ. Kết nối lại bất cứ lúc nào.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

/** Trang Kênh kết nối; `children` là phần vẽ thêm trên trang (hộp chọn Page của callback Facebook). */
export function ChannelsPage({ children }: { children?: ReactNode }) {
  const channels = useChannels();
  const oauth = useStartFacebookOAuth();
  const qc = useQueryClient();
  const [systemUser, setSystemUser] = useState(false);
  const items = channels.data?.items ?? [];
  return (
    <div className="chat-set">
      <div className="page-h">
        <div>
          <h1>Kênh kết nối</h1>
          <p className="sub">
            Mỗi dòng là một tài khoản của một kênh nhắn tin, đổ tin vào Hộp chat. Hiện có Facebook Page.
          </p>
        </div>
        <div className="row">
          <button
            type="button"
            className="btn btn-dark"
            disabled={oauth.pending}
            onClick={() => {
              void oauth.run();
            }}
          >
            Kết nối Page Facebook
          </button>
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              setSystemUser(true);
            }}
          >
            Kết nối bằng mã system user
          </button>
        </div>
      </div>
      {oauth.message !== null && (
        <div className="box box-err small" role="alert" style={{ marginBottom: 16 }}>
          {oauth.message}
        </div>
      )}
      {channels.isError && <div className="box box-err">{errorMessage(channels.error)}</div>}
      {channels.isPending && <p className="muted">Đang tải…</p>}
      {channels.data !== undefined &&
        (items.length === 0 ? (
          <div className="card">
            <p className="muted">Chưa có tài khoản kênh nào. Bấm "Kết nối Page Facebook" để kết nối Page đầu tiên.</p>
          </div>
        ) : (
          <div className="card" style={{ padding: 0 }}>
            {items.map((a) => (
              <ChannelRow
                key={a.id}
                a={a}
                reconnecting={oauth.pending}
                onReconnect={() => {
                  void oauth.run();
                }}
              />
            ))}
          </div>
        ))}
      {children}
      {systemUser && (
        <SystemTokenForm
          onClose={() => {
            setSystemUser(false);
          }}
          onConnected={() => {
            setSystemUser(false);
            void qc.invalidateQueries({ queryKey: CHANNELS_KEY });
          }}
        />
      )}
    </div>
  );
}
