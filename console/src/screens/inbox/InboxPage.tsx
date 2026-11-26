// Màn Hộp chat /inbox và /inbox/:conversationId (kế hoạch M4 Task 34, số toàn cục 174; mockup
// docs/mockups/m4/inbox.html trạng thái A–C; hành vi HV02–HV08 cùng các dòng sửa HV03, HV04, HV06 của README).
// - Cột danh sách: băng (mất kết nối realtime có "Tải lại ngay", tài khoản kênh chỉ xem / mất kết nối), chọn tài khoản
//   kênh có số chưa đọc, âm báo (tắt mặc định), bộ lọc, danh sách ảo, "Tải thêm 50".
// - Bộ lọc nằm trên URL; mỗi lần đổi được nhớ trên máy; mở /inbox không tham số lọc → dùng bộ lọc lần trước.
// - Máy tính: danh sách | hội thoại | khung khách (khi đang mở một hội thoại). Điện thoại: mỗi lúc một màn (HV02).
// Hội thoại (Task 175) và khung bên phải (Task 176) nạp lười.
import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { useParams, useSearchParams } from "react-router";
import {
  countConversations,
  getMe,
  listChannelAccounts,
  listConversationTags,
  listStaff,
  type ConversationSummary,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { hasPermission } from "../../app/nav";
import { queryKey } from "../../app/queryClient";
import { useRealtimeState } from "../../shell/realtimeState";
import { ChannelBanners } from "./ChannelBanners";
import { ConversationList } from "./ConversationList";
import { Filters } from "./Filters";
import {
  DEFAULT_FILTERS,
  hasFilterParams,
  isDefault,
  readFilters,
  writeFilters,
  type InboxFilters,
} from "./inboxRules";
import { PageColumn } from "./PageColumn";
import { beep, readSavedFilters, readSound, saveFilters, writeSound } from "./prefs";
import { CONVERSATION_TOPIC, useConversationList } from "./useConversationList";
import "./inbox.css";

const ConversationPane = lazy(() => import("./conversation/index"));
const SidePanel = lazy(() => import("./side-panel/index"));

/** Chủ đề realtime của tài khoản kênh (máy chủ: channel.TopicStatus). */
export const CHANNEL_TOPIC = "channel.status";

function emptyText(f: InboxFilters): string {
  if (f.unread) return "Không còn tin chưa đọc. Bỏ lọc để xem cả hội thoại đã đọc.";
  if (f.q.trim() !== "") return `Không có hội thoại nào của khách khớp "${f.q.trim()}".`;
  if (!isDefault(f)) return "Không có hội thoại nào khớp bộ lọc.";
  return "Chưa có tin nhắn hay comment nào. Tin mới sẽ tự hiện ở đây.";
}

/**
 * Bộ lọc của màn: URL. URL không có tham số lọc mà máy có bộ lọc đã lưu (HV06) → dùng bộ lọc đã lưu và ghi nó lên URL.
 * Mỗi lần người dùng đổi bộ lọc, lưu ngay (trước khi đổi URL) để "Bỏ lọc" không bị bộ lọc cũ lấy lại; mở bằng liên kết
 * có tham số lọc cũng được lưu.
 */
function useInboxFilters() {
  const [sp, setSp] = useSearchParams();
  const saved = hasFilterParams(sp) ? "" : readSavedFilters();
  const restoring = saved !== "";
  const filters = readFilters(restoring ? new URLSearchParams(saved) : sp);
  const text = writeFilters(filters).toString();
  useEffect(() => {
    if (restoring) setSp(new URLSearchParams(saved), { replace: true });
    else saveFilters(text);
  }, [restoring, saved, setSp, text]);
  const setFilters = (f: InboxFilters) => {
    const next = writeFilters(f);
    saveFilters(next.toString());
    setSp(next, { replace: true });
  };
  return { filters, setFilters, search: text === "" ? "" : `?${text}` };
}

/** Giờ hiện tại cho chữ "vừa xong", "n phút", làm mới mỗi 30 giây. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => {
      setNow(Date.now());
    }, 30_000);
    return () => {
      clearInterval(t);
    };
  }, []);
  return now;
}

export function InboxPage() {
  const { conversationId } = useParams();
  const { filters, setFilters, search } = useInboxFilters();
  const realtime = useRealtimeState();
  const now = useNow();
  // Cùng khóa với khung (src/shell/App.tsx) nên không gọi lại /api/v1/me.
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const grants = me.data?.grants ?? [];
  const meId = me.data?.userId ?? "";

  const [sound, setSound] = useState(readSound);
  const soundRef = useRef({ sound, meId });
  useEffect(() => {
    soundRef.current = { sound, meId };
  });
  // Một tiếng cho mỗi lần tải phần đổi có tin mới còn chưa đọc ở hội thoại của tôi hoặc chưa ai nhận.
  const onFresh = (rows: readonly ConversationSummary[]) => {
    const s = soundRef.current;
    const mine = (r: ConversationSummary) => r.assignee === undefined || r.assignee.id === s.meId;
    if (s.sound && rows.some((r) => r.unreadCount > 0 && mine(r))) beep();
  };
  const list = useConversationList(filters, { onFresh });

  const counts = useQuery({
    queryKey: queryKey(CONVERSATION_TOPIC, "counts"),
    queryFn: () => unwrap(countConversations()),
  });
  const accounts = useQuery({
    queryKey: queryKey(CHANNEL_TOPIC, "accounts"),
    queryFn: () => unwrap(listChannelAccounts()),
  });
  const tags = useQuery({
    queryKey: queryKey("conversation.tags", "list"),
    queryFn: () => unwrap(listConversationTags()),
  });
  const canStaff = hasPermission(grants, "identity.users.manage");
  const staff = useQuery({
    queryKey: queryKey("identity.staff"),
    enabled: canStaff,
    queryFn: () => unwrap(listStaff()),
  });
  const accountList = accounts.data?.items ?? [];
  const nameOf = (id: string) => accountList.find((a) => a.id === id)?.name ?? "";
  const others = (staff.data ?? []).filter((u) => u.id !== meId && u.status === "active");

  const open = conversationId !== undefined;
  const chatClass = open ? "chat has-conv" : "chat is-2";
  return (
    <div className="inbox">
      <div className="page-h">
        <div>
          <h1>Hộp chat</h1>
          <p className="sub">
            Tin nhắn và comment của mọi tài khoản kênh chung một chỗ. Danh sách tự cập nhật; hội thoại mới nhất lên đầu.
          </p>
        </div>
      </div>
      <div className={chatClass}>
        <div className="pane-list">
          <div className="pane-b">
            {realtime === "reconnecting" && (
              <div className="bnr bnr-warn" role="status">
                <span className="grow">
                  <b>Mất kết nối, đang nối lại.</b> Tin mới có thể chưa hiện. Trong lúc chờ, danh sách tự tải lại mỗi 20
                  giây.
                </span>
                <button type="button" className="btn btn-quiet" onClick={list.reload}>
                  Tải lại ngay
                </button>
              </div>
            )}
            <ChannelBanners accounts={accountList} canManage={hasPermission(grants, "channel.manage")} />
            <div className="row list-h">
              <b className="grow">Tài khoản kênh</b>
              <button
                type="button"
                className="toggle"
                aria-label="Âm báo tin mới"
                aria-pressed={sound}
                onClick={() => {
                  writeSound(!sound);
                  setSound(!sound);
                }}
              >
                Âm báo
              </button>
            </div>
            <PageColumn
              accounts={accountList}
              counts={counts.data}
              value={filters.page}
              onChange={(page) => {
                setFilters({ ...filters, page });
              }}
            />
            <Filters value={filters} onChange={setFilters} tags={tags.data?.items ?? []} staff={others} />
          </div>
          {list.isError && (
            <div className="pane-b">
              <div className="box box-err" role="alert">
                {errorMessage(list.error)}
              </div>
            </div>
          )}
          {list.isPending && (
            <div className="pane-b">
              <p className="muted">Đang tải hội thoại…</p>
            </div>
          )}
          {!list.isPending && !list.isError && list.rows.length === 0 && (
            <div className="pane-b">
              <div className="empty">
                <h2>Không có hội thoại nào</h2>
                <p className="muted small">{emptyText(filters)}</p>
                {!isDefault(filters) && (
                  <button
                    type="button"
                    className="btn btn-line"
                    onClick={() => {
                      setFilters(DEFAULT_FILTERS);
                    }}
                  >
                    Bỏ lọc
                  </button>
                )}
              </div>
            </div>
          )}
          {list.rows.length > 0 && (
            <>
              <ConversationList
                rows={list.rows}
                flash={list.flash}
                {...(conversationId === undefined ? {} : { selectedId: conversationId })}
                meId={meId}
                accountName={nameOf}
                search={search}
                now={now}
              />
              <div className="pane-b">
                <div className="more">
                  <span>{`Đang xem ${String(list.rows.length)} hội thoại`}</span>
                  {list.hasNextPage && (
                    <button
                      type="button"
                      className="btn btn-quiet"
                      disabled={list.isFetchingNextPage}
                      onClick={list.fetchNextPage}
                    >
                      Tải thêm 50
                    </button>
                  )}
                </div>
              </div>
            </>
          )}
        </div>
        <div className="pane-conv">
          {open ? (
            <Suspense fallback={<p className="pane-b muted">Đang mở hội thoại…</p>}>
              <ConversationPane conversationId={conversationId} backTo={`/inbox${search}`} />
            </Suspense>
          ) : (
            <div className="pane-b">
              <p className="muted">Chọn một hội thoại ở danh sách để xem tin và trả lời.</p>
            </div>
          )}
        </div>
        {open && (
          <div className="pane-side">
            <Suspense fallback={null}>
              <SidePanel conversationId={conversationId} />
            </Suspense>
          </div>
        )}
      </div>
    </div>
  );
}
