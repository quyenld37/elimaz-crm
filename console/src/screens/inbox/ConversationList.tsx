// Danh sách hội thoại cuộn ảo theo cửa sổ (TanStack Virtual, như danh sách sản phẩm): chỉ dựng dòng đang thấy cộng 8
// dòng mỗi phía; hai khối đệm giữ chiều cao. 200 hội thoại vẫn chỉ vài chục dòng trong DOM.
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import { useLayoutEffect, useRef, useState } from "react";
import type { ConversationSummary } from "../../api";
import { ConversationRow } from "./ConversationRow";

/** Chiều cao ước tính một dòng trước khi đo thật. */
const ROW_ESTIMATE = 96;

export interface ConversationListProps {
  rows: readonly ConversationSummary[];
  /** Dòng vừa có tin mới (nền hồng 5 giây). */
  flash: ReadonlySet<string>;
  selectedId?: string;
  meId: string;
  accountName: (pageId: string) => string;
  search: string;
  now: number;
}

export function ConversationList({ rows, flash, selectedId, meId, accountName, search, now }: ConversationListProps) {
  const boxRef = useRef<HTMLDivElement>(null);
  const [margin, setMargin] = useState(0);
  useLayoutEffect(() => {
    const el = boxRef.current;
    if (el !== null) setMargin(el.getBoundingClientRect().top + window.scrollY);
  }, []);
  const v = useWindowVirtualizer({
    count: rows.length,
    estimateSize: () => ROW_ESTIMATE,
    overscan: 8,
    scrollMargin: margin,
    getItemKey: (i) => rows[i]?.id ?? i,
    useAnimationFrameWithResizeObserver: true,
    measureElement: (el) => {
      const h = el.getBoundingClientRect().height;
      return h > 0 ? h : ROW_ESTIMATE;
    },
  });
  const items = v.getVirtualItems();
  const first = items.at(0);
  const last = items.at(-1);
  const padTop = first === undefined ? 0 : first.start - margin;
  const padBottom = last === undefined ? 0 : v.getTotalSize() - (last.end - margin);
  return (
    <div className="cvlist" ref={boxRef} role="list" aria-label="Hội thoại">
      {padTop > 0 && <div aria-hidden="true" style={{ height: padTop }} />}
      {items.map((it) => {
        const r = rows[it.index];
        return (
          r !== undefined && (
            <div role="listitem" key={r.id} className="cv-li">
              <ConversationRow
                row={r}
                index={it.index}
                meId={meId}
                accountName={accountName(r.pageId)}
                search={search}
                selected={r.id === selectedId}
                fresh={flash.has(r.id)}
                now={now}
                measure={v.measureElement}
              />
            </div>
          )
        );
      })}
      {padBottom > 0 && <div aria-hidden="true" style={{ height: padBottom }} />}
    </div>
  );
}
