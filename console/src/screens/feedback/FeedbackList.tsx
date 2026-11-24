// Màn Góp ý /khach/gop-y (kế hoạch M5 Task 461, M5-49; mockup docs/mockups/m5/feedback.html trạng thái A, C; HV22).
// Máy chủ trả mới nhất trước, lọc một trạng thái, một chủ đề, một khách (?khach=<mã khách>, từ khối Góp ý của hồ sơ).
// Số trên nhãn đếm cả shop (countFeedback, Task 486); liên kết hồ sơ ghi mã KH. API không trả lý do không gắn hồ sơ:
// màn không vẽ chữ đó (Task 461 Quyết định 3). Nội dung khách gõ hiện bằng chữ thuần (React tự thoát), không ghi log.
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Fragment, useState } from "react";
import { Link, useSearchParams } from "react-router";
import {
  countFeedback,
  listFeedback,
  type Feedback,
  type FeedbackPage,
  type FeedbackState,
  type FeedbackTopic,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { shortTime } from "../../app/time";
import { CountLabel } from "../don/CountLabel";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { usePerms } from "../don/orderData";
import { FeedbackPanel } from "./FeedbackPanel";
import { PAGE, countsKey, listKey } from "./feedbackData";
import { STATES, STATE_CHIP, STATE_TEXT, TOPICS, TOPIC_TEXT, asTopic, moodText } from "./feedbackRules";
import "../don/don.css";
import "./feedback.css";

export const TEXT_NO_PERMISSION = "Bạn không có quyền xem góp ý của khách.";

const isForbidden = (e: unknown) => e instanceof ApiError && e.status === 403;

function Sender({ f, canCustomer }: { f: Feedback; canCustomer: boolean }) {
  const who = [f.name, f.contact].filter((x) => x !== undefined && x !== "").join(" · ");
  if (who === "" && f.customerId === undefined) return <span className="muted">Không để lại</span>;
  const profile = `Hồ sơ ${f.customerCode ?? f.customerName ?? "khách"}`;
  return (
    <div className="who">
      {who !== "" && <span className="clamp2 wrap-any">{who}</span>}
      {f.customerId === undefined ? (
        <span className="small muted">Không gắn hồ sơ</span>
      ) : canCustomer ? (
        <Link className="cell-a small" to={`/khach/${f.customerId}`}>
          {profile}
        </Link>
      ) : (
        <span className="small">{profile}</span>
      )}
    </div>
  );
}

export function FeedbackList() {
  const perms = usePerms();
  const [params, setParams] = useSearchParams();
  const customerId = params.get("khach") ?? "";
  const [state, setState] = useState<FeedbackState | "">("");
  const [topic, setTopic] = useState<FeedbackTopic | "">("");
  const [open, setOpen] = useState<string | null>(null);
  const query = {
    ...(state === "" ? {} : { state }),
    ...(topic === "" ? {} : { topic }),
    ...(customerId === "" ? {} : { customerId }),
    limit: PAGE,
  };
  const list = useInfiniteQuery({
    queryKey: listKey(query),
    // "" là trang đầu (chưa có con trỏ).
    initialPageParam: "",
    getNextPageParam: (last: FeedbackPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listFeedback({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const counts = useQuery({ queryKey: countsKey, queryFn: () => unwrap(countFeedback()) });
  const filtered = state !== "" || topic !== "" || customerId !== "";
  const canManage = perms.can("customer.feedback.manage");
  const canCustomer = perms.can("customer.read");

  const clearCustomer = () => {
    setParams({}, { replace: true });
  };
  const showAll = () => {
    setState("");
    setTopic("");
    clearCustomer();
  };
  const label = (value: FeedbackState | "", name: string) => {
    const c = counts.data;
    const n = c === undefined ? undefined : value === "" ? c.total : c.states.find((x) => x.state === value)?.count;
    return (
      <CountLabel
        key={name}
        text={name}
        n={n}
        pressed={state === value}
        attention={value === "new"}
        onSelect={() => {
          setState(value);
        }}
      />
    );
  };

  return (
    <div>
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Khách hàng / Góp ý</p>
          <h1>Góp ý của khách</h1>
          <p className="sub">
            Khách gửi ở trang &quot;Góp ý cho ELIMAZ&quot; trên website. Góp ý có số điện thoại trùng một khách trong
            CRM thì gắn vào hồ sơ khách đó; không tạo khách mới.
          </p>
        </div>
      </div>

      <div className="scroll-x">
        <div className="lblbar" role="group" aria-label="Lọc theo trạng thái">
          {STATES.map((s) => label(s, STATE_TEXT[s]))}
          {label("", "Tất cả")}
        </div>
      </div>
      <div className="toolbar">
        <div className="select-wrap">
          <select
            aria-label="Chủ đề"
            value={topic}
            onChange={(e) => {
              setTopic(asTopic(e.target.value));
            }}
          >
            <option value="">Mọi chủ đề</option>
            {TOPICS.map((t) => (
              <option key={t} value={t}>
                {TOPIC_TEXT[t]}
              </option>
            ))}
          </select>
        </div>
        {customerId !== "" && (
          <span className="row">
            <span className="small">Đang xem góp ý của một khách</span>
            <button type="button" className="btn btn-quiet" onClick={clearCustomer}>
              Bỏ lọc khách
            </button>
          </span>
        )}
      </div>

      {list.isError ? (
        isForbidden(list.error) ? (
          <div className="box box-err stack-sm">
            <b>{TEXT_NO_PERMISSION}</b>
            <p>Nhờ Chủ shop cấp quyền xem góp ý cho vai trò của bạn.</p>
          </div>
        ) : (
          <div className="box box-err stack-sm">
            <b>Chưa tải được danh sách góp ý.</b>
            <p>{errorMessage(list.error)}</p>
            <div>
              <button
                type="button"
                className="btn btn-line"
                onClick={() => {
                  void list.refetch();
                }}
              >
                Thử lại
              </button>
            </div>
          </div>
        )
      ) : list.isPending ? (
        <p className="muted">Đang tải danh sách góp ý…</p>
      ) : rows.length === 0 ? (
        <div className="empty">
          <h2>{filtered ? "Chưa có góp ý nào trong mục này" : "Chưa có góp ý nào"}</h2>
          <p className="muted">
            {filtered ? "Đổi bộ lọc trạng thái hoặc chủ đề." : "Góp ý khách gửi từ website sẽ hiện ở đây."}
          </p>
          {filtered && (
            <button type="button" className="btn btn-quiet" onClick={showAll}>
              Xem tất cả góp ý
            </button>
          )}
        </div>
      ) : (
        <>
          <div className="tbl-frame">
            <table className="tbl fb">
              <thead>
                <tr>
                  {["Thời gian", "Chủ đề", "Mức", "Nội dung", "Người gửi", "Trạng thái"].map((h) => (
                    <th key={h}>{h}</th>
                  ))}
                  <th className="c-act" aria-label="Thao tác" />
                </tr>
              </thead>
              <tbody>
                {rows.map((f) => {
                  const isOpen = open === f.id;
                  return (
                    <Fragment key={f.id}>
                      <tr className={isOpen ? "is-open" : undefined}>
                        <td className="lead">
                          <span className="num small">{shortTime(f.createdAt)}</span>
                        </td>
                        <td data-label="Chủ đề">{TOPIC_TEXT[f.topic]}</td>
                        <td data-label="Mức">
                          <span className={f.mood === undefined ? "muted" : "small"}>{moodText(f.mood)}</span>
                        </td>
                        <td data-label="Nội dung">
                          <span className="clamp2 wrap-any">{f.body}</span>
                        </td>
                        <td data-label="Người gửi">
                          <Sender f={f} canCustomer={canCustomer} />
                        </td>
                        <td data-label="Trạng thái">
                          <span className={STATE_CHIP[f.state]}>{STATE_TEXT[f.state]}</span>
                        </td>
                        <td className="c-act">
                          <button
                            type="button"
                            className="btn btn-quiet"
                            aria-expanded={isOpen}
                            onClick={() => {
                              setOpen(isOpen ? null : f.id);
                            }}
                          >
                            {isOpen ? "Đóng" : "Mở"}
                          </button>
                        </td>
                      </tr>
                      {isOpen && (
                        <tr className="is-open">
                          <td className="full" colSpan={7}>
                            <FeedbackPanel f={f} canManage={canManage} />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
          <MoreBar text={`${String(rows.length)} góp ý${list.hasNextPage ? ", còn nữa" : ""}`} pages={list} />
        </>
      )}
    </div>
  );
}
