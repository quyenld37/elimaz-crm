// Thẻ bài viết đầu hội thoại comment (Task 175, thêm trường ở Task 501; mockup hoi-thoai.html trạng thái C): ảnh bài
// (đã chép về CRM, đường 5 phút xin qua getAttachmentUrl của hội thoại — ảnh bài là nội dung công khai của Page nên nạp
// ngay, khác ảnh khách gửi), đoạn bài, giờ đăng, "Mở bài" trên Facebook ở tab mới. Bài chưa đọc được: chữ "Bài", câu
// chung, không có nút.
import { useQuery } from "@tanstack/react-query";
import { getAttachmentUrl, type ChatPostRef } from "../../../api";
import { unwrap } from "../../../app/errors";
import { shortTime } from "../../../app/time";

/** Đường ảnh hết hạn sau 5 phút: lấy lại sau 4 phút. */
const IMAGE_STALE_MS = 4 * 60_000;

export function PostCard({ conversationId, post }: { conversationId: string; post: ChatPostRef }) {
  const fileId = post.imageFileId;
  const img = useQuery({
    queryKey: ["conversation", "post-image", conversationId, fileId],
    queryFn: () => unwrap(getAttachmentUrl({ path: { id: conversationId, fileId: fileId ?? "" } })),
    enabled: fileId !== undefined,
    staleTime: IMAGE_STALE_MS,
    refetchInterval: IMAGE_STALE_MS,
  });
  const sub = post.postedAt === undefined ? "Comment trên bài này" : `Bài trên Page · đăng ${shortTime(post.postedAt)}`;
  return (
    <div className="post">
      {img.isSuccess ? (
        <img className="thumb" src={img.data.url} alt="Ảnh bài" />
      ) : (
        <span className="thumb" aria-hidden="true">
          Bài
        </span>
      )}
      <div className="grow">
        <b className="ellip">{post.snippet ?? "Bài viết trên Page"}</b>
        <span className="xs muted">{sub}</span>
      </div>
      {post.permalink !== undefined && (
        <a className="btn btn-quiet" href={post.permalink} target="_blank" rel="noopener noreferrer">
          Mở bài
        </a>
      )}
    </div>
  );
}
