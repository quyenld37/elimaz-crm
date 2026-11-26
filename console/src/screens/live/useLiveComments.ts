// Comment của một phiên live (kế hoạch M4 Task 177; API listLiveComments của Task 172, báo hiệu live.comments).
//
// - Lần đầu, đổi lọc, resync và lưới an toàn 20 s của lớp realtime: tải cả danh sách (theo nextCursor tới hết). Tải cả
//   để thấy đúng trạng thái chốt của comment cũ hơn 30 s mà mốc after không lấy lại (tệp task 177 Quyết định 8).
// - Báo hiệu live.comments của đúng phiên (máy chủ gộp 300 ms): tải phần sau mốc after = comment cuối đang có (máy chủ
//   lùi 30 s), gộp theo messageId (bỏ trùng), xếp theo (sentAt, messageId). Đang tải thì không gửi thêm; báo hiệu tới
//   trong lúc đó gom thành đúng một lần tải sau khi lần đang chạy xong.
// Dữ liệu nằm trong một truy vấn TanStack (meta realtimeManual: báo hiệu changed không tải lại cả, hook tự xử lý).
import { useQuery, useQueryClient, type QueryClient, type QueryKey } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo } from "react";
import { listLiveComments, type LiveComment } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey, subscribeChanged, type RealtimeMeta } from "../../app/queryClient";
import { TOPIC_LIVE_COMMENTS, afterOf, mergeComments } from "./liveRules";

/** Số comment mỗi trang (trần của API). */
export const PAGE_LIMIT = 500;
/** Trần số trang mỗi lần tải (chống vòng lặp khi máy chủ trả nextCursor mãi). */
const MAX_PAGES = 100;

export interface LiveFilters {
  phoneOnly: boolean;
  unhandledOnly: boolean;
}

export interface LiveComments {
  /** Comment xếp theo thời gian tăng dần (mới nhất ở cuối). */
  items: LiveComment[];
  status: "pending" | "error" | "success";
  /** Sửa một comment tại chỗ (ví dụ vừa chốt xong ở máy này). */
  patch(messageId: string, fn: (c: LiveComment) => LiveComment): void;
}

async function loadFrom(sessionId: string, f: LiveFilters, after: string | undefined): Promise<LiveComment[]> {
  const out: LiveComment[] = [];
  let cursor = after;
  for (let i = 0; i < MAX_PAGES; i++) {
    const page = await unwrap(
      listLiveComments({
        path: { id: sessionId },
        query: {
          limit: PAGE_LIMIT,
          ...(cursor === undefined ? {} : { after: cursor }),
          ...(f.phoneOnly ? { phoneOnly: true } : {}),
          ...(f.unhandledOnly ? { unhandledOnly: true } : {}),
        },
      }),
    );
    out.push(...page.items);
    if (page.nextCursor === undefined || page.nextCursor === "") break;
    cursor = page.nextCursor;
  }
  return out;
}

/**
 * Tải cả: bản mới thay hẳn bản cũ (comment không còn khớp bộ lọc thì rời danh sách). Chỉ giữ thêm comment tới bằng
 * báo hiệu trong lúc đang tải (chưa có lúc bắt đầu), trừ comment đã chốt khi lọc "Chưa xử lý".
 */
async function loadAll(qc: QueryClient, key: QueryKey, sessionId: string, f: LiveFilters): Promise<LiveComment[]> {
  const before = new Set((qc.getQueryData<LiveComment[]>(key) ?? []).map((c) => c.messageId));
  const fresh = await loadFrom(sessionId, f, undefined);
  const cur = qc.getQueryData<LiveComment[]>(key) ?? [];
  const arrived = cur.filter((c) => !before.has(c.messageId) && !(f.unhandledOnly && c.claim !== undefined));
  return mergeComments(arrived, fresh);
}

const META: RealtimeMeta = { realtimeManual: true };

export function useLiveComments(sessionId: string, filters: LiveFilters): LiveComments {
  const qc = useQueryClient();
  const { phoneOnly, unhandledOnly } = filters;
  const f = useMemo(() => ({ phoneOnly, unhandledOnly }), [phoneOnly, unhandledOnly]);
  const key = useMemo(() => queryKey(TOPIC_LIVE_COMMENTS, sessionId, f), [sessionId, f]);
  const full = useQuery({
    queryKey: key,
    queryFn: () => loadAll(qc, key, sessionId, f),
    meta: META,
    enabled: sessionId !== "",
  });

  useEffect(() => {
    // Gom báo hiệu: running = đang tải phần sau mốc; again = có báo hiệu tới trong lúc đó. Riêng cho từng phiên, lọc.
    const st = { alive: true, running: false, again: false };
    const more = () => st.again && st.alive;
    const pull = async () => {
      if (st.running) {
        st.again = true;
        return;
      }
      st.running = true;
      try {
        do {
          st.again = false;
          const have = qc.getQueryData<LiveComment[]>(key);
          if (have === undefined) return; // lần tải cả chưa xong: nó sẽ có comment mới
          const last = have.at(-1);
          try {
            const page = await loadFrom(sessionId, f, last === undefined ? undefined : afterOf(last));
            if (!st.alive) return;
            qc.setQueryData<LiveComment[]>(key, (cur) => mergeComments(cur ?? [], page));
          } catch {
            // Lỗi tải phần mới: lưới an toàn 20 s (resync) tải lại cả; không báo lỗi cho cả danh sách.
          }
        } while (more());
      } finally {
        st.running = false;
      }
    };
    const off = subscribeChanged(qc, (e) => {
      if (e.topic === TOPIC_LIVE_COMMENTS && e.entity.id === sessionId) void pull();
    });
    return () => {
      st.alive = false;
      off();
    };
  }, [qc, key, sessionId, f]);

  const patch = useCallback(
    (messageId: string, fn: (c: LiveComment) => LiveComment) => {
      qc.setQueryData<LiveComment[]>(key, (cur) => cur?.map((c) => (c.messageId === messageId ? fn(c) : c)));
    },
    [qc, key],
  );

  return { items: full.data ?? [], status: full.status, patch };
}
