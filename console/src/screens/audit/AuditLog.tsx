// Màn Nhật ký thao tác /nhat-ky-thao-tac (kế hoạch M5 Task 462, M5-50; mockup docs/mockups/m5/audit.html trạng thái
// A, B; HV24, HV25). Máy chủ (listAuditLogs, Task 457) trả mới nhất trước, 50 dòng mỗi trang, chỉ tên trường đổi (không
// giá trị, không IP). Lọc một hành động, một người làm, khoảng ngày VN. API không có danh sách người làm cho quyền
// audit.read: ô "Người làm" gồm những người đã hiện trong các trang đã tải (tệp task, Quyết định thay chủ shop). Còn
// nextCursor thì ghi "còn nữa", không coi là hết.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { listAuditLogs, type AuditLog, type AuditLogPage } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { ACTION_OPTIONS, actionText, actorText, dayRange, entityText, fieldsText } from "./auditRules";
import "./audit.css";

export const TEXT_NO_PERMISSION = "Bạn không có quyền xem nhật ký thao tác.";

const isForbidden = (e: unknown) => e instanceof ApiError && e.status === 403;

/** Nhãn của một người làm trong ô lọc: nhân viên theo tên, loại khác kèm loại ("Khóa: Website ELIMAZ"). */
function actorOption(r: AuditLog): string {
  const a = actorText(r);
  if (r.actorType === "staff") return a.name;
  if (r.actorType === "service") return `Khóa: ${a.name}`;
  return `${a.kind}: ${a.name}`;
}

/** Thêm người làm (có mã) của rows vào known; trả đúng known khi không có ai mới (không vẽ lại vô hạn). */
function mergeActors(known: ReadonlyMap<string, string>, rows: readonly AuditLog[]): ReadonlyMap<string, string> {
  const fresh = rows.filter((r) => r.actorId !== undefined && r.actorId !== "" && !known.has(r.actorId));
  if (fresh.length === 0) return known;
  const next = new Map(known);
  for (const r of fresh) if (r.actorId !== undefined) next.set(r.actorId, actorOption(r));
  return next;
}

function Row({ r }: { r: AuditLog }) {
  const who = actorText(r);
  const ent = entityText(r);
  return (
    <tr>
      <td className="lead">
        <span className="num small">{shortTime(r.createdAt).replace(" ", " · ")}</span>
      </td>
      <td data-label="Người làm">
        <div className="who">
          <span className="wrap-any">{who.name}</span>
          <span className={r.actorType === "staff" ? "chip" : "chip chip-line"}>{who.kind}</span>
        </div>
      </td>
      <td data-label="Thao tác">
        <b className="wrap-any">{actionText(r.action)}</b>
      </td>
      <td data-label="Mục">
        <div className="who">
          <span className="wrap-any">{ent.label}</span>
          {ent.id !== undefined && <span className="small muted num wrap-any">{ent.id}</span>}
        </div>
      </td>
      <td data-label="Trường đổi">
        <span className={r.changedFields.length === 0 ? "small muted" : "small wrap-any"}>
          {fieldsText(r.changedFields)}
        </span>
      </td>
    </tr>
  );
}

export function AuditLogScreen() {
  const [action, setAction] = useState("");
  const [actorId, setActorId] = useState("");
  const [fromDay, setFromDay] = useState("");
  const [toDay, setToDay] = useState("");
  const [actors, setActors] = useState<ReadonlyMap<string, string>>(new Map());
  const range = dayRange(fromDay, toDay);
  const rangeError = "error" in range ? range.error : null;
  const query = {
    ...(action === "" ? {} : { action }),
    ...(actorId === "" ? {} : { actorId }),
    ...("error" in range ? {} : range),
  };
  const list = useInfiniteQuery({
    queryKey: queryKey("identity.audit_logs", "list", query),
    enabled: rangeError === null,
    // "" là trang đầu (chưa có con trỏ).
    initialPageParam: "",
    getNextPageParam: (last: AuditLogPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listAuditLogs({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const filtered = action !== "" || actorId !== "" || fromDay !== "" || toDay !== "";
  // Gom người làm khi có trang mới (đổi state lúc vẽ theo dữ liệu trước, không dùng effect).
  const pages = list.data?.pages;
  const [seen, setSeen] = useState(pages);
  if (pages !== seen) {
    setSeen(pages);
    setActors((known) => mergeActors(known, pages?.flatMap((p) => p.items) ?? []));
  }

  const date = (label: string, value: string, set: (v: string) => void) => (
    <label className="field">
      <span className="sr">{label}</span>
      <input
        type="date"
        className="input num"
        aria-label={label}
        value={value}
        onChange={(e) => {
          set(e.target.value);
        }}
      />
    </label>
  );

  let body;
  if (rangeError !== null) {
    body = (
      <div className="box box-err" role="alert">
        {rangeError}
      </div>
    );
  } else if (list.isError) {
    body = isForbidden(list.error) ? (
      <div className="box box-err stack-sm">
        <b>{TEXT_NO_PERMISSION}</b>
        <p>Chỉ người có quyền &quot;Xem nhật ký&quot; (vai trò Chủ, Quản lý) mở được màn này.</p>
      </div>
    ) : (
      <div className="box box-err stack-sm">
        <b>Chưa tải được nhật ký thao tác.</b>
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
    );
  } else if (list.isPending) {
    body = <p className="muted">Đang tải nhật ký thao tác…</p>;
  } else if (rows.length === 0) {
    body = (
      <div className="empty">
        <h2>{filtered ? "Không có thao tác phù hợp" : "Chưa có thao tác nào"}</h2>
        <p className="muted">
          {filtered ? "Đổi hành động, người làm hoặc khoảng ngày." : "Thao tác ghi dữ liệu trong CRM sẽ hiện ở đây."}
        </p>
      </div>
    );
  } else {
    body = (
      <>
        <div className="tbl-frame">
          <table className="tbl au">
            <thead>
              <tr>
                {["Thời gian", "Người làm", "Thao tác", "Mục", "Trường đổi"].map((h) => (
                  <th key={h}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row key={r.id} r={r} />
              ))}
            </tbody>
          </table>
        </div>
        <MoreBar
          text={`${String(rows.length)} dòng${list.hasNextPage ? " · còn nữa" : ""}`}
          label="Xem thêm"
          pages={list}
        />
      </>
    );
  }

  return (
    <div>
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Quản trị / Nhật ký thao tác</p>
          <h1>Nhật ký thao tác</h1>
          <p className="sub">
            Mọi thao tác ghi dữ liệu trong CRM: ai (nhân viên, khóa dịch vụ của website hay hệ thống), làm gì, trên mục
            nào, đổi những trường nào. Nhật ký chỉ ghi tên trường, không ghi nội dung (SĐT, địa chỉ, tin nhắn, lý do…).
          </p>
        </div>
      </div>

      <div className="toolbar">
        <div className="select-wrap">
          <select
            aria-label="Hành động"
            value={action}
            onChange={(e) => {
              setAction(e.target.value);
            }}
          >
            <option value="">Mọi thao tác</option>
            {ACTION_OPTIONS.map((a) => (
              <option key={a} value={a}>
                {actionText(a)}
              </option>
            ))}
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Người làm"
            value={actorId}
            onChange={(e) => {
              setActorId(e.target.value);
            }}
          >
            <option value="">Mọi người</option>
            {[...actors].map(([id, name]) => (
              <option key={id} value={id}>
                {name}
              </option>
            ))}
          </select>
        </div>
        <div className="row">
          {date("Từ ngày", fromDay, setFromDay)}
          <span className="muted">→</span>
          {date("Đến ngày", toDay, setToDay)}
        </div>
      </div>

      {body}
      <p className="small muted">
        Nhật ký trên admin cũ không chuyển sang CRM; bản xuất CSV của admin giữ ngoài CRM. Màn này bắt đầu từ ngày CRM
        nhận việc.
      </p>
    </div>
  );
}
