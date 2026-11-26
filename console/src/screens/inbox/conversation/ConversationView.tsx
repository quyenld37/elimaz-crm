// Cột giữa của /inbox/:conversationId (Task 175; mockup docs/mockups/m4/hoi-thoai.html trạng thái A–D). Đầu hội thoại
// (tên khách, tài khoản kênh, loại, người phụ trách, trạng thái, "Nhận" khi chưa ai nhận), dải ai đang xem, dòng tin, ô
// gõ. Điện thoại: màn riêng, nút "‹" về danh sách (giữ bộ lọc), nút "Khách" xuống khung khách (HV02). "Giao cho…" và
// thẻ khách ở khung bên phải (Task 176). Chi tiết hội thoại tải lại khi có báo hiệu conversation.changed của nó.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import {
  claimConversation,
  getConversation,
  getMe,
  setConversationStatus,
  type ConversationStatus,
  type PresenceState,
} from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { queryKey, type RealtimeMeta } from "../../../app/queryClient";
import type { UploadOptions } from "../../products/useUpload";
import { useNow } from "../../live/LivePage";
import { STATUS_OPTIONS } from "../inboxRules";
import { Composer } from "./Composer";
import { canOnPage } from "./conversationRules";
import { MessageList } from "./MessageList";
import { detailKey, idem, refreshConversation } from "./messageWrites";
import { PresenceBar } from "./PresenceBar";
import { usePresenceHeartbeat } from "./usePresenceHeartbeat";
import "./conversation.css";

/** Thôi gõ bao lâu thì nhịp tim về "đang xem". */
const TYPING_IDLE_MS = 5_000;

/** "đang xem" ↔ "đang trả lời": gõ chữ → typing, thôi gõ 5 giây (hay ô trống, đã gửi) → viewing. */
function usePresenceState(): [PresenceState, (typing: boolean) => void] {
  const [state, setState] = useState<PresenceState>("viewing");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );
  const onTyping = (typing: boolean) => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
    if (!typing) {
      setState("viewing");
      return;
    }
    setState("typing");
    timer.current = setTimeout(() => {
      setState("viewing");
    }, TYPING_IDLE_MS);
  };
  return [state, onTyping];
}

const STATUSES = STATUS_OPTIONS.filter((o): o is [ConversationStatus, string] => o[0] !== "all");

export interface ConversationViewProps {
  conversationId: string;
  backTo: string;
  uploadOptions?: UploadOptions;
}

export function ConversationView({ conversationId, backTo, uploadOptions }: ConversationViewProps) {
  const qc = useQueryClient();
  const nowMs = useNow(30_000);
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const meta: RealtimeMeta = { realtimeEntity: conversationId };
  const q = useQuery({
    queryKey: detailKey(conversationId),
    queryFn: () => unwrap(getConversation({ path: { id: conversationId } })),
    meta,
  });
  const [presence, onTyping] = usePresenceState();
  usePresenceHeartbeat(conversationId, presence);

  const claim = useWriteAction((key, b: { id: string }) =>
    unwrap(claimConversation({ path: { id: b.id }, headers: idem(key) })),
  );
  const status = useWriteAction((key, b: { id: string; status: ConversationStatus; expectedVersion: number }) =>
    unwrap(
      setConversationStatus({
        path: { id: b.id },
        body: { status: b.status, expectedVersion: b.expectedVersion },
        headers: idem(key),
      }),
    ),
  );
  const refresh = () => {
    refreshConversation(qc, conversationId);
  };

  const back = (
    <Link className="ibtn only-phone" to={backTo} aria-label="Về danh sách hội thoại">
      ‹
    </Link>
  );
  if (q.isPending)
    return (
      <div className="pane-h">
        {back}
        <span className="muted">Đang mở hội thoại…</span>
      </div>
    );
  if (q.isError)
    return (
      <>
        <div className="pane-h">{back}</div>
        <div className="pane-b">
          <div className="box box-err" role="alert">
            {errorMessage(q.error)}
          </div>
        </div>
      </>
    );

  const d = q.data;
  const c = d.conversation;
  const grants = me.data?.grants ?? [];
  const meId = me.data?.userId ?? "";
  const canReply = canOnPage(grants, "conversation.reply", c.pageId);
  const readOnly = d.page.sendMode === "read_only";
  const who =
    c.assignee === undefined
      ? "Chưa ai nhận"
      : c.assignee.id === meId
        ? "Bạn phụ trách"
        : `${c.assignee.name} phụ trách`;
  const err = claim.error ?? status.error;

  return (
    <div className="conv" data-conversation={conversationId}>
      <div className="conv-h">
        {back}
        <div className="conv-t">
          <b className="ellip">{c.customer.name}</b>
          <span className="small muted">
            {`${d.page.name} · ${c.kind === "comment" ? "Comment" : "Tin nhắn"} · ${who}`}
          </span>
        </div>
        <a className="btn btn-quiet only-phone" href="#khach">
          Khách
        </a>
        {canReply && (
          <div className="acts">
            <span className="select-wrap conv-st">
              <select
                aria-label="Trạng thái hội thoại"
                value={c.status}
                disabled={status.pending}
                onChange={(e) => {
                  const s = STATUSES.find(([k]) => k === e.target.value);
                  if (s === undefined) return;
                  void status.run({ id: conversationId, status: s[0], expectedVersion: c.version }).then(refresh);
                }}
              >
                {STATUSES.map(([k, label]) => (
                  <option key={k} value={k}>
                    {label}
                  </option>
                ))}
              </select>
            </span>
            {c.assignee === undefined && (
              <>
                <button
                  type="button"
                  className="btn btn-dark"
                  disabled={claim.pending}
                  onClick={() => void claim.run({ id: conversationId }).then(refresh)}
                >
                  Nhận
                </button>
                <span className="reason">
                  Chưa ai nhận: bấm "Nhận" để thành người phụ trách (hội thoại vào "Của tôi").
                </span>
              </>
            )}
          </div>
        )}
        {err !== null && (
          <p className="small err-text conv-err" role="alert">
            {errorMessage(err)}
          </p>
        )}
      </div>
      <PresenceBar conversationId={conversationId} meId={meId} />
      <MessageList
        ctx={{ conversationId, canReply, readOnly, features: d.page.features, nowMs, onChanged: refresh }}
        unreadCount={c.unreadCount}
        post={c.post}
      />
      <Composer
        key={conversationId}
        detail={d}
        canReply={canReply}
        nowMs={nowMs}
        onTyping={onTyping}
        onSent={refresh}
        {...(uploadOptions === undefined ? {} : { uploadOptions })}
      />
    </div>
  );
}
