// Danh sách comment của phiên live (mockup docs/mockups/m4/live.html trạng thái A; HV40, HV41). Danh sách ảo
// (@tanstack/react-virtual, khung cuộn riêng): chỉ dựng dòng đang thấy cộng vài dòng mỗi phía. Đang ở cuối thì comment mới
// tự hiện (followOnAppend); người dùng cuộn lên xem thì danh sách đứng yên và nút "↓ N comment mới" đưa xuống cuối, N
// giảm dần khi cuộn xuống (unseenCount). Nội dung comment hiện dạng chữ thuần.
import { useVirtualizer } from "@tanstack/react-virtual";
import { useRef, useState } from "react";
import type { LiveComment } from "../../api";
import { clockTime, unseenCount } from "./liveRules";
import type { LiveFilters } from "./useLiveComments";

/** Chiều cao ước tính một dòng (px), đo lại sau khi vẽ. */
const ROW_ESTIMATE = 64;
/** Cách cuối danh sách bao nhiêu px vẫn coi là "đang ở cuối". */
const END_SLACK = 48;

export interface LiveCommentListProps {
  items: readonly LiveComment[];
  status: "pending" | "error" | "success";
  filters: LiveFilters;
  onFilters: (f: LiveFilters) => void;
  selectedId: string | null;
  onSelect: (c: LiveComment) => void;
}

function Row({ c, current, onSelect }: { c: LiveComment; current: boolean; onSelect: (c: LiveComment) => void }) {
  const done = c.claim !== undefined || c.privateReply.state !== "none";
  return (
    <button
      type="button"
      className={done ? "lc is-done" : "lc"}
      aria-current={current ? "true" : undefined}
      onClick={() => {
        onSelect(c);
      }}
    >
      <span className="avatar" aria-hidden="true">
        {c.customer.name.trim().charAt(0).toUpperCase()}
      </span>
      <span className="lc-n ellip">{c.customer.name}</span>
      <span className="lc-t">{clockTime(c.sentAt)}</span>
      <span className="lc-x">{c.text}</span>
      <span className="lc-m">
        {c.hasPhone && <span className="chip">Có SĐT</span>}
        {c.isHidden && <span className="chip">Đã ẩn trên Page</span>}
        {c.claim !== undefined ? (
          <span className="chip chip-ok">{`Đã lên đơn ${c.claim.orderCode} · ${c.claim.by.name}`}</span>
        ) : (
          c.customerHasOpenDraft && <span className="chip chip-line">Khách đã có đơn nháp</span>
        )}
        {c.privateReply.state !== "none" && <span className="chip chip-line">Đã nhắn riêng</span>}
        {current && <span className="xs muted">Đang mở khung chốt</span>}
      </span>
    </button>
  );
}

export function LiveCommentList({ items, status, filters, onFilters, selectedId, onSelect }: LiveCommentListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  // Số comment lúc người dùng rời cuối danh sách; null = đang ở cuối (comment mới tự hiện).
  const [away, setAway] = useState<number | null>(null);
  // eslint-disable-next-line react-hooks/incompatible-library -- useVirtualizer đổi kết quả theo lần cuộn; thành phần này không cần React Compiler ghi nhớ
  const v = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_ESTIMATE,
    getItemKey: (i) => items[i]?.messageId ?? i,
    overscan: 6,
    followOnAppend: away === null,
    // Cùng ngưỡng "đang ở cuối" với nút comment mới: trong khoảng này comment mới tự hiện, ngoài khoảng này nút hiện.
    scrollEndThreshold: END_SLACK,
    useAnimationFrameWithResizeObserver: true,
    measureElement: (el) => {
      const h = el.getBoundingClientRect().height;
      return h > 0 ? h : ROW_ESTIMATE;
    },
  });
  const rows = v.getVirtualItems();
  const top = v.scrollOffset ?? 0;
  const viewH = v.scrollRect?.height ?? 0;
  const lastVisible = rows.reduce((m, r) => (r.start < top + viewH ? Math.max(m, r.index) : m), -1);
  const unseen = away === null ? 0 : unseenCount(items.length, away, lastVisible);

  const onScroll = () => {
    const el = scrollRef.current;
    if (el === null) return;
    const atEnd = el.scrollTop + (v.scrollRect?.height ?? el.clientHeight) >= v.getTotalSize() - END_SLACK;
    if (atEnd) setAway(null);
    else if (away === null) setAway(items.length);
  };
  const toEnd = () => {
    setAway(null);
    v.scrollToEnd();
  };

  return (
    <section className="lcpane" aria-label="Comment của phiên">
      <div className="pane-h">
        <button
          type="button"
          className="toggle"
          aria-pressed={filters.phoneOnly}
          onClick={() => {
            onFilters({ ...filters, phoneOnly: !filters.phoneOnly });
          }}
        >
          Chỉ comment có SĐT
        </button>
        <button
          type="button"
          className="toggle"
          aria-pressed={filters.unhandledOnly}
          onClick={() => {
            onFilters({ ...filters, unhandledOnly: !filters.unhandledOnly });
          }}
        >
          Chưa xử lý
        </button>
        <span className="grow" />
        <span className="small muted num">{`${items.length.toLocaleString("vi-VN")} comment`}</span>
      </div>
      {unseen > 0 && (
        <div className="newbar">
          <button type="button" className="btn btn-line" onClick={toEnd}>
            {`↓ ${unseen.toLocaleString("vi-VN")} comment mới`}
          </button>
        </div>
      )}
      {status === "error" && <p className="box box-err small lc-note">Chưa tải được comment. Đang thử lại.</p>}
      {status === "pending" && <p className="small muted lc-note">Đang tải comment…</p>}
      {status === "success" && items.length === 0 && (
        <p className="small muted lc-note">
          {filters.phoneOnly || filters.unhandledOnly ? "Không có comment khớp bộ lọc." : "Chưa có comment nào."}
        </p>
      )}
      <div className="lclist" ref={scrollRef} onScroll={onScroll} data-testid="lclist">
        <div className="lc-body" style={{ height: v.getTotalSize() }}>
          {rows.map((r) => {
            const c = items[r.index];
            if (c === undefined) return null;
            return (
              <div
                key={r.key}
                className="lc-slot"
                data-index={r.index}
                ref={v.measureElement}
                style={{ transform: `translateY(${String(r.start)}px)` }}
              >
                <Row c={c} current={c.messageId === selectedId} onSelect={onSelect} />
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
