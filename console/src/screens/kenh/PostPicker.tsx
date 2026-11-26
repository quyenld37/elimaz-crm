// Chọn bài của Page cho quy tắc tự động (Task 501; mockup cai-dat.html khối "Tự ẩn comment", ô "Trừ các bài"): bài đã
// chọn là nhãn có nút "×"; "+ Chọn bài" mở danh sách bài CRM đã gặp trên Page (listPagePosts, mới gặp trước, "Tải thêm
// bài"). Giá trị là mã bài trên kênh (externalId: exceptPostIds, postIds của quy tắc). Bài chưa đọc được nội dung hiện
// bằng mã.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { listPagePosts, type PagePost } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { CONVERSATION_TOPIC } from "./kenhData";

/** Tối đa bài mỗi danh sách (hợp đồng: maxItems 200). */
export const POSTS_MAX = 200;

const postsKey = (pageId: string) => queryKey(CONVERSATION_TOPIC, "page-posts", pageId);
const nameOf = (p: PagePost | undefined, ext: string) => p?.snippet ?? ext;

export interface PostPickerProps {
  pageId: string;
  /** Chữ của ô, vd "Trừ các bài". */
  label: string;
  value: readonly string[];
  onChange: (next: string[]) => void;
  disabled: boolean;
  error?: string | undefined;
}

export function PostPicker({ pageId, label, value, onChange, disabled, error }: PostPickerProps) {
  const [open, setOpen] = useState(false);
  const q = useInfiniteQuery({
    queryKey: postsKey(pageId),
    initialPageParam: "",
    getNextPageParam: (last: { nextCursor?: string }) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listPagePosts({ path: { id: pageId }, query: pageParam === "" ? {} : { cursor: pageParam } })),
    enabled: open || value.length > 0,
  });
  const posts = q.data?.pages.flatMap((p) => p.items) ?? [];
  const byExt = new Map(posts.map((p) => [p.externalId, p]));
  const full = value.length >= POSTS_MAX;
  const choices = posts.filter((p) => !value.includes(p.externalId));
  return (
    <div className="field">
      <span>{`${label} (${String(value.length)}/${String(POSTS_MAX)})`}</span>
      {value.length > 0 && (
        <div className="lbs">
          {value.map((ext) => (
            <span key={ext} className="lb lb-4">
              {nameOf(byExt.get(ext), ext)}
              {!disabled && (
                <button
                  type="button"
                  className="ibtn"
                  aria-label={`Bỏ bài ${nameOf(byExt.get(ext), ext)}`}
                  onClick={() => {
                    onChange(value.filter((x) => x !== ext));
                  }}
                >
                  ×
                </button>
              )}
            </span>
          ))}
        </div>
      )}
      {error !== undefined && <span className="field-err">{error}</span>}
      {!disabled && (
        <button
          type="button"
          className="btn btn-quiet"
          style={{ justifySelf: "start" }}
          aria-expanded={open}
          onClick={() => {
            setOpen(!open);
          }}
        >
          {open ? "Đóng danh sách bài" : "+ Chọn bài"}
        </button>
      )}
      {open && !disabled && (
        <div className="stack-sm">
          {q.isPending && <p className="small muted">Đang tải bài của Page…</p>}
          {q.isError && (
            <p className="small err-text" role="alert">
              {errorMessage(q.error)}
            </p>
          )}
          {q.isSuccess && posts.length === 0 && (
            <p className="small muted">CRM chưa gặp bài nào của Page này (bài có comment mới hiện ở đây).</p>
          )}
          {full && <p className="reason">{`Đã đủ ${String(POSTS_MAX)} bài.`}</p>}
          <ul className="post-pick" aria-label="Bài của Page">
            {choices.map((p) => (
              <li key={p.id}>
                <span className="ellip">{nameOf(p, p.externalId)}</span>
                <button
                  type="button"
                  className="btn btn-quiet"
                  disabled={full}
                  aria-label={`Chọn bài ${nameOf(p, p.externalId)}`}
                  onClick={() => {
                    onChange([...value, p.externalId]);
                  }}
                >
                  Chọn
                </button>
              </li>
            ))}
          </ul>
          {q.hasNextPage && (
            <button
              type="button"
              className="btn btn-quiet"
              style={{ justifySelf: "start" }}
              disabled={q.isFetchingNextPage}
              onClick={() => void q.fetchNextPage()}
            >
              Tải thêm bài
            </button>
          )}
        </div>
      )}
    </div>
  );
}
