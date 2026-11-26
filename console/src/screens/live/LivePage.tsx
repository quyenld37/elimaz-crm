// Màn Live (kế hoạch M4 Task 177; mockup docs/mockups/m4/live.html, HV40–HV48). Phiên hôm nay (listLiveSessions),
// comment realtime của phiên đang chọn (useLiveComments, danh sách ảo), khung chốt khi bấm một comment. Chỉ vẽ số máy
// chủ có (tệp task 177 Quyết định 4): nhận được, trên Facebook (phiên đã kết thúc), nguồn đọc comment.
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import {
  getMe,
  listChannelAccounts,
  listLiveSessions,
  listPendingDrafts,
  type LiveComment,
  type LiveSession,
} from "../../api";
import { unwrap } from "../../app/errors";
import { hasPermission } from "../../app/nav";
import { queryKey } from "../../app/queryClient";
import { ClaimPanel, PENDING_KEY } from "./ClaimPanel";
import { LiveCommentList } from "./LiveCommentList";
import { elapsedText, hourMinute, sessionTitle, startOfTodayVN } from "./liveRules";
import { SESSIONS_KEY, SessionPicker, sessionWhen } from "./SessionPicker";
import { useLiveComments, type LiveFilters } from "./useLiveComments";
import "../don/don.css";
import "../len-don/len-don.css";
import "./live.css";

/** Phiên hôm nay (since = đầu ngày giờ Việt Nam), đang live trước rồi mới bắt đầu trước. */
export function useTodaySessions() {
  const [since] = useState(() => startOfTodayVN(Date.now()));
  return useQuery({
    queryKey: queryKey(SESSIONS_KEY, since),
    queryFn: () => unwrap(listLiveSessions({ query: { since } })),
    select: (d) =>
      [...d.items].sort(
        (a, b) =>
          Number(b.status === "live") - Number(a.status === "live") ||
          Date.parse(b.startedAt) - Date.parse(a.startedAt),
      ),
  });
}

/** Giờ máy, đổi mỗi everyMs (số "đã live N phút"). */
export function useNow(everyMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => {
      setNow(Date.now());
    }, everyMs);
    return () => {
      clearInterval(t);
    };
  }, [everyMs]);
  return now;
}

/** Phiên đang chọn: mã trên URL, không có thì phiên mặc định (đang live, mới nhất); số đơn nháp thiếu địa chỉ của phiên. */
export function useSelectedSession() {
  const { sessionId } = useParams();
  const sessions = useTodaySessions();
  const list = sessions.data ?? [];
  const s = sessionId === undefined ? list[0] : list.find((x) => x.id === sessionId);
  const drafts = useQuery({
    queryKey: queryKey(PENDING_KEY, s?.id),
    queryFn: () => unwrap(listPendingDrafts({ path: { id: s?.id ?? "" } })),
    enabled: s !== undefined,
  });
  return { sessions, list, s, drafts };
}

/** Tên Page theo mã tài khoản kênh (listChannelAccounts). */
export function usePages() {
  const q = useQuery({ queryKey: queryKey("channel.accounts"), queryFn: () => unwrap(listChannelAccounts()) });
  const items = q.data?.items ?? [];
  return { items, name: (id: string) => items.find((p) => p.id === id)?.name ?? "Page" };
}

function SessionStats({ s, now }: { s: LiveSession; now: number }) {
  if (s.status === "ended") {
    const meta = s.metaCommentCount;
    const missing = meta === undefined ? 0 : meta - s.receivedCount;
    return (
      <div className="card">
        <div className="row-between">
          <h3>{sessionTitle(s)}</h3>
          <span className="chip">
            {s.endedAt === undefined ? "Đã kết thúc" : `Đã kết thúc ${hourMinute(s.endedAt)}`}
          </span>
        </div>
        <dl className="kv" style={{ marginTop: 8 }}>
          <dt>Thời gian</dt>
          <dd>{sessionWhen(s)}</dd>
          <dt>Nhận được</dt>
          <dd className="num">{`${s.receivedCount.toLocaleString("vi-VN")} comment`}</dd>
          {meta !== undefined && (
            <>
              <dt>Trên Facebook</dt>
              <dd className="num">
                {`${meta.toLocaleString("vi-VN")} comment`}
                {missing === 0 && (
                  <>
                    {" · "}
                    <span className="ok-text">khớp</span>
                  </>
                )}
              </dd>
            </>
          )}
        </dl>
        {missing > 0 && (
          <div className="box box-err small" style={{ marginTop: 8 }}>
            <b>{`Thiếu ${missing.toLocaleString("vi-VN")} comment.`}</b> CRM đã đọc lại cả bài sau khi kết thúc nhưng
            vẫn lệch; quản lý đã được báo. Comment bị Facebook lọc spam có thể không đọc được.
          </div>
        )}
      </div>
    );
  }
  return (
    <div className="lstats">
      <span>
        Bắt đầu <b>{hourMinute(s.startedAt)}</b> · đã live <b>{elapsedText(s.startedAt, now)}</b>
      </span>
      <span>
        Nhận được <b>{s.receivedCount.toLocaleString("vi-VN")}</b> comment
      </span>
      <span>
        Nguồn: <b>{s.activeSource === "poll" ? "hỏi định kỳ" : "luồng trực tiếp"}</b>
      </span>
    </div>
  );
}

function CommentsPane({
  s,
  canCreate,
  canUpdate,
  canReadStock,
}: {
  s: LiveSession;
  canCreate: boolean;
  canUpdate: boolean;
  canReadStock: boolean;
}) {
  const [filters, setFilters] = useState<LiveFilters>({ phoneOnly: false, unhandledOnly: false });
  const [selected, setSelected] = useState<LiveComment | null>(null);
  const live = useLiveComments(s.id, filters);
  const current = selected === null ? null : (live.items.find((c) => c.messageId === selected.messageId) ?? selected);
  return (
    <>
      {s.status === "live" && s.activeSource === "poll" && (
        <div className="bnr bnr-warn">
          <b>Comment đang lấy bằng cách hỏi định kỳ.</b>
          <span className="grow">
            Luồng trực tiếp của Facebook đang lỗi nên comment có thể chậm vài giây. CRM tự thử lại luồng trực tiếp.
          </span>
        </div>
      )}
      <div className="live">
        <LiveCommentList
          items={live.items}
          status={live.status}
          filters={filters}
          onFilters={setFilters}
          selectedId={current?.messageId ?? null}
          onSelect={setSelected}
        />
        {current === null ? (
          <aside className="pane-side" aria-label="Khung chốt">
            <div className="sec">
              <p className="small muted">Bấm một comment để chốt: tạo đơn nháp, giữ hàng và nhắn riêng xin địa chỉ.</p>
            </div>
          </aside>
        ) : (
          <ClaimPanel
            key={current.messageId}
            comment={current}
            pageId={s.pageId}
            canCreate={canCreate}
            canUpdate={canUpdate}
            canReadStock={canReadStock}
            onClaimed={(id, r) => {
              live.patch(id, (c) => ({ ...c, claim: r.claim }));
            }}
            onClose={() => {
              setSelected(null);
            }}
          />
        )}
      </div>
    </>
  );
}

export function LivePage() {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const grants = me.data?.grants ?? [];
  const { sessions, list, s, drafts: pendingQ } = useSelectedSession();
  const pages = usePages();
  const now = useNow(60_000);
  const canStart = hasPermission(grants, "live.operate");
  const pending = pendingQ.data?.items.length;

  return (
    <div className="stack live-page">
      <div className="page-h">
        <div>
          <p className="crumb">
            <Link to="/inbox">Hộp chat</Link> / Live
          </p>
          <h1>Live</h1>
          <p className="sub">
            Comment của phiên live hiện ngay khi khách gõ. Bấm một comment để chốt: tạo đơn nháp, giữ hàng và nhắn riêng
            xin địa chỉ.
          </p>
        </div>
        {s !== undefined && (
          <Link className="btn btn-quiet" to={`/live/${s.id}/thieu-dia-chi`}>
            {pending === undefined ? "Đã chốt, thiếu địa chỉ" : `Đã chốt, thiếu địa chỉ (${String(pending)})`}
          </Link>
        )}
      </div>
      {sessions.isError && <div className="box box-err">Chưa tải được phiên live. Thử lại sau ít giây.</div>}
      {sessions.isPending && <p className="small muted">Đang tải phiên live…</p>}
      {sessions.isSuccess && list.length === 0 && (
        <div className="empty">
          <h2>Chưa có phiên live hôm nay</h2>
          <p className="small muted">
            Khi Page bắt đầu live, CRM tự nhận trong vòng 30 giây và mở luồng comment. Không cần bấm gì.
          </p>
          <SessionPicker
            sessions={[]}
            selectedId={null}
            pageName={pages.name}
            pages={pages.items}
            canStart={canStart}
          />
        </div>
      )}
      {list.length > 0 && (
        <SessionPicker
          sessions={list}
          selectedId={s?.id ?? null}
          pageName={pages.name}
          pages={pages.items}
          canStart={canStart}
        />
      )}
      {sessions.isSuccess && list.length > 0 && s === undefined && (
        <div className="box box-info">Không thấy phiên này trong các phiên hôm nay. Chọn một phiên ở trên.</div>
      )}
      {s !== undefined && (
        <>
          <SessionStats s={s} now={now} />
          <CommentsPane
            key={s.id}
            s={s}
            canCreate={hasPermission(grants, "order.create")}
            canUpdate={hasPermission(grants, "order.update")}
            canReadStock={hasPermission(grants, "inventory.read")}
          />
        </>
      )}
    </div>
  );
}
