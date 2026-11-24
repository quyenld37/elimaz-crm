// Khối "Góp ý" trong hồ sơ khách (kế hoạch M5 Task 461; mockup docs/mockups/m5/feedback.html trạng thái B, C): 5 góp ý
// mới nhất của cả nhóm khách (listFeedback customerId = gốc trả cả hồ sơ đã gộp). Còn trang sau thì không ghi tổng
// (Bẫy Task 84). Màn hồ sơ chỉ dựng khối khi người xem có customer.feedback.read.
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { listFeedback, type Feedback, type FeedbackPage } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { shortDate } from "../len-don/orderRules";
import { TEXT_NO_PERMISSION } from "./FeedbackList";
import { PROFILE_PAGE, customerKey } from "./feedbackData";
import { STATE_CHIP, STATE_TEXT, TOPIC_TEXT } from "./feedbackRules";
import "./feedback.css";

function heading(p: FeedbackPage | undefined): string {
  if (p === undefined) return "Góp ý";
  return p.nextCursor === undefined ? `Góp ý (${String(p.items.length)})` : `Góp ý · ${String(PROFILE_PAGE)} gần nhất`;
}

const title = (f: Feedback) =>
  f.mood === undefined ? TOPIC_TEXT[f.topic] : `${TOPIC_TEXT[f.topic]} · ${String(f.mood)}/5`;

export function CustomerFeedback({ customerId }: { customerId: string }) {
  const query = { customerId, limit: PROFILE_PAGE };
  const q = useQuery({ queryKey: customerKey(customerId), queryFn: () => unwrap(listFeedback({ query })) });
  const items = q.data?.items ?? [];
  return (
    <section className="card stack-sm">
      <div className="row-between">
        <h2>{heading(q.data)}</h2>
        <Link className="btn btn-quiet" to={`/khach/gop-y?khach=${customerId}`}>
          Xem ở Góp ý
        </Link>
      </div>
      {q.isError ? (
        <p className="box box-err">
          {q.error instanceof ApiError && q.error.status === 403 ? TEXT_NO_PERMISSION : errorMessage(q.error)}
        </p>
      ) : q.isPending ? (
        <p className="muted small">Đang tải góp ý…</p>
      ) : items.length === 0 ? (
        <p className="muted small">Khách chưa gửi góp ý nào.</p>
      ) : (
        <ol className="tl">
          {items.map((f) => (
            <li key={f.id}>
              <span className="at">{shortDate(f.createdAt)}</span>
              <div>
                <b>{title(f)}</b> <span className={STATE_CHIP[f.state]}>{STATE_TEXT[f.state]}</span>
                <p className="small clamp2 wrap-any">{f.body}</p>
              </div>
            </li>
          ))}
        </ol>
      )}
      <p className="hint">Gồm góp ý của mọi hồ sơ đã gộp vào khách này.</p>
    </section>
  );
}
