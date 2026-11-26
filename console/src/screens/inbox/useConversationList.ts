// Danh sách hội thoại của màn Hộp chat (Task 174, kế hoạch M4-34; CH01 phía màn hình).
// - Trang đầu theo bộ lọc; "Tải thêm 50" theo con trỏ; tự tải lại cả danh sách mỗi 20 s (lưới an toàn của M0, vẫn chạy
//   khi lớp realtime đang nối lại) và khi resync (onRealtimeResync tải lại mọi truy vấn đang mở).
// - Báo hiệu conversation.changed: một lần tải phần đổi listConversations(updatedSince = lần tải trước − 30 s), gộp theo
//   id (bản tải sau thắng), sắp lại theo (lastMessageAt, id); dòng có tin mới sáng nền 5 giây. Báo hiệu dồn dập trong
//   lúc đang tải gộp thành đúng một lần tải kế tiếp. Máy chủ lọc updatedSince theo last_message_at: hội thoại đang hiện
//   mà lần tải phần đổi không trả (giao, nhãn, đã đọc, không có tin mới) → tải lại cả danh sách một lần.
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { listConversations, type ConversationSummary } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey, subscribeChanged } from "../../app/queryClient";
import { listQuery, mergeRows, type InboxFilters, type Stamped } from "./inboxRules";

/** Chủ đề realtime của hội thoại (máy chủ: conversation.TopicConversationChanged). */
export const CONVERSATION_TOPIC = "conversation.changed";
/** Lùi 30 giây cho transaction commit muộn (kế hoạch; máy chủ cũng tự lùi query.SinceSlack). */
export const SINCE_SLACK_MS = 30_000;
export const REFRESH_MS = 20_000;
export const FLASH_MS = 5_000;
/** Số dòng tối đa của một lần tải phần đổi. */
const DELTA_LIMIT = 100;

export interface ConversationListOptions {
  /** Hội thoại vừa có tin mới (lastMessageAt mới hơn bản đang hiện, hay chưa hiện) qua lần tải phần đổi (âm báo HV04). */
  /** Gọi một lần cho mỗi lần tải phần đổi có ít nhất một hội thoại như vậy. */
  onFresh?: (rows: readonly ConversationSummary[]) => void;
}

interface StampedPage extends Stamped {
  nextCursor?: string;
}

export function useConversationList(filters: InboxFilters, opts: ConversationListOptions = {}) {
  const qc = useQueryClient();
  const query = listQuery(filters);
  const key = queryKey(CONVERSATION_TOPIC, "list", query);
  const keyText = JSON.stringify(key);

  const list = useInfiniteQuery({
    queryKey: key,
    meta: { realtimeManual: true },
    refetchInterval: REFRESH_MS,
    initialPageParam: "",
    getNextPageParam: (last: StampedPage) => last.nextCursor,
    queryFn: async ({ pageParam }): Promise<StampedPage> => {
      const at = Date.now();
      const p = await unwrap(
        listConversations({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } }),
      );
      return { at, rows: p.items, ...(p.nextCursor === undefined ? {} : { nextCursor: p.nextCursor }) };
    },
  });

  // Các lần tải phần đổi của đúng bộ lọc đang xem (đổi bộ lọc → bỏ).
  const [deltas, setDeltas] = useState<{ key: string; loads: Stamped[] }>({ key: keyText, loads: [] });
  const [flash, setFlash] = useState<ReadonlySet<string>>(new Set());

  const pages = list.data?.pages;
  // Lần tải lại cả danh sách gần nhất bắt đầu lúc trang đầu được gửi; phần đổi gửi trước đó đã nằm trong nó.
  const baseAt = pages?.[0]?.at ?? 0;
  const own = useMemo(
    () => (deltas.key === keyText ? deltas.loads.filter((d) => d.at > baseAt) : []),
    [deltas, keyText, baseAt],
  );
  const loads = useMemo(() => [...(pages ?? []), ...own], [pages, own]);
  const rows = useMemo(() => mergeRows(loads), [loads]);

  // Mốc của lần tải trước: lần tải lại cả danh sách hay lần tải phần đổi sau nó, cái nào gửi sau (trang 2, 3 tải thêm
  // không làm mới phần đầu danh sách nên không tính).
  const lastAt = Math.max(baseAt, ...own.map((d) => d.at));
  const ctx = useRef({ query, keyText, key, rows, lastAt, baseAt, onFresh: opts.onFresh });
  useEffect(() => {
    ctx.current = { query, keyText, key, rows, lastAt, baseAt, onFresh: opts.onFresh };
  });

  useEffect(() => {
    let running = false;
    let pending = new Set<string>();
    let stopped = false;
    // Đọc qua hàm: giá trị đổi trong lúc chờ await (rời màn).
    const isStopped = () => stopped;
    const timers = new Set<ReturnType<typeof setTimeout>>();
    const mark = (ids: readonly string[]) => {
      if (ids.length === 0) return;
      setFlash((s) => new Set([...s, ...ids]));
      const t = setTimeout(() => {
        timers.delete(t);
        setFlash((s) => new Set([...s].filter((id) => !ids.includes(id))));
      }, FLASH_MS);
      timers.add(t);
    };

    const run = async () => {
      running = true;
      while (pending.size > 0 && !isStopped()) {
        const ids = pending;
        pending = new Set();
        const c = ctx.current;
        if (c.lastAt === 0) continue; // trang đầu chưa về: lần tải đó đã có phần đổi.
        const at = Date.now();
        try {
          const p = await unwrap(
            listConversations({
              query: {
                ...c.query,
                limit: DELTA_LIMIT,
                updatedSince: new Date(c.lastAt - SINCE_SLACK_MS).toISOString(),
              },
            }),
          );
          if (isStopped() || ctx.current.keyText !== c.keyText) continue;
          const known = new Map(ctx.current.rows.map((r) => [r.id, r]));
          const fresh = p.items.filter((r) => {
            const old = known.get(r.id);
            return old === undefined || Date.parse(r.lastMessageAt) > Date.parse(old.lastMessageAt);
          });
          // Chỉ giữ phần đổi gửi sau lần tải lại cả danh sách gần nhất (cũ hơn đã nằm trong nó): tab mở cả ngày
          // không giữ mãi các lần tải cũ.
          const base = ctx.current.baseAt;
          setDeltas((d) => ({
            key: c.keyText,
            loads: [...(d.key === c.keyText ? d.loads.filter((l) => l.at > base) : []), { at, rows: p.items }],
          }));
          mark(fresh.map((r) => r.id));
          if (fresh.length > 0) ctx.current.onFresh?.(fresh);
          const returned = new Set(p.items.map((r) => r.id));
          if ([...ids].some((id) => known.has(id) && !returned.has(id))) {
            await qc.invalidateQueries({ queryKey: c.key, exact: true });
          }
        } catch {
          // Tải phần đổi hỏng (mạng, quyền vừa đổi): lần tự tải lại 20 giây sẽ đúng lại.
        }
      }
      running = false;
    };

    const off = subscribeChanged(qc, (e) => {
      if (e.topic !== CONVERSATION_TOPIC) return;
      pending.add(e.entity.id);
      if (!running) void run();
    });
    return () => {
      stopped = true;
      off();
      for (const t of timers) clearTimeout(t);
    };
  }, [qc]);

  const reload = useCallback(() => {
    void list.refetch();
  }, [list]);

  return {
    rows,
    flash,
    isPending: list.isPending,
    isError: list.isError,
    error: list.error,
    hasNextPage: list.hasNextPage,
    isFetchingNextPage: list.isFetchingNextPage,
    fetchNextPage: () => {
      void list.fetchNextPage();
    },
    reload,
    /** Số lần tải phần đổi đang giữ trong bộ nhớ (test kiểm không tăng mãi). */
    heldDeltas: deltas.loads.length,
  };
}
