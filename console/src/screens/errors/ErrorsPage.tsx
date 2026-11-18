// Màn Sự kiện lỗi (kế hoạch M1 Task 330; mockup đã duyệt docs/mockups/m1/su-kien-loi.html, trạng thái A, B). 10 loại
// lỗi nhiều nhất 24 giờ qua từ phần `errors` của GET /api/v1/platform/health (máy chủ gộp theo dấu vân tay, xếp nhiều
// lần nhất trước, nội dung đã che dữ liệu cá nhân). Tự tải lại mỗi 20 s (ADR-42); resync của realtime tải lại mọi
// truy vấn đang mở (app/queryClient.ts).
import { useQuery } from "@tanstack/react-query";
import { getPlatformHealth, type ErrorSummary } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { clockTime, countText, shortId, shortTime } from "./errorsRules";
import "./errors.css";

/** Chu kỳ tự tải lại (ADR-42). */
export const REFRESH_MS = 20_000;

export const TEXT_EMPTY = "Không có lỗi nào trong 24 giờ qua";
export const TEXT_NO_ACCESS = "Không có quyền";

export function ErrorsPage() {
  const health = useQuery({
    queryKey: queryKey("platform.health"),
    queryFn: () => unwrap(getPlatformHealth()),
    refetchInterval: REFRESH_MS,
  });
  const forbidden = health.error instanceof ApiError && health.error.status === 403;
  // Bị thu quyền khi đang mở (lần tải lại trả 403): không hiện tiếp dữ liệu cũ.
  const rows = forbidden ? [] : (health.data?.errors ?? []);

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Sự kiện lỗi</h1>
          <p className="sub">
            10 loại lỗi xảy ra nhiều nhất trong 24 giờ qua. Lỗi cùng chỗ gộp làm một dòng. Nội dung đã che số điện
            thoại, email.
          </p>
        </div>
        {!forbidden && (
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              void health.refetch();
            }}
          >
            Tải lại
          </button>
        )}
      </div>

      {health.isPending && <p className="muted">Đang tải…</p>}

      {forbidden && (
        <div className="empty">
          <h2>{TEXT_NO_ACCESS}</h2>
          <p className="muted">Tài khoản của bạn không được xem Sự kiện lỗi. Cần thì nhờ Chủ shop cấp quyền.</p>
        </div>
      )}
      {health.isError && !forbidden && (
        <div className="box box-err" role="alert" style={{ marginBottom: 16 }}>
          {errorMessage(health.error)}
        </div>
      )}

      {health.isSuccess && rows.length === 0 && (
        <div className="empty">
          <h2>{TEXT_EMPTY}</h2>
          <p className="muted">Lần kiểm {clockTime(health.dataUpdatedAt)}.</p>
        </div>
      )}

      {rows.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl err-tbl">
            <thead>
              <tr>
                <th>Lỗi</th>
                <th>Tiến trình</th>
                <th className="r">Số lần</th>
                <th>Lần đầu</th>
                <th>Lần cuối</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e) => (
                <ErrorRow key={e.fingerprint} e={e} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

function ErrorRow({ e }: { e: ErrorSummary }) {
  return (
    <tr>
      <td className="full">
        <b className="wrap-any">{e.name}</b>
        <span className="xs muted"> · {shortId(e.fingerprint)}</span>
        {e.message !== "" && (
          <details className="err-msg">
            <summary>
              <span className="clamp2 small">{e.message}</span>
            </summary>
            <p className="small wrap-any">{e.message}</p>
          </details>
        )}
      </td>
      <td data-label="Tiến trình">{e.role}</td>
      <td data-label="Số lần" className="r num bold">
        {countText(e.count)}
      </td>
      <td data-label="Lần đầu" className="num nowrap">
        {shortTime(e.firstSeenAt)}
      </td>
      <td data-label="Lần cuối" className="num nowrap">
        {shortTime(e.lastSeenAt)}
      </td>
    </tr>
  );
}
