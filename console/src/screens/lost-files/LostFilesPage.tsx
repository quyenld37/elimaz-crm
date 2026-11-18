// Màn Hệ thống — Tệp bị mất (kế hoạch M1 Task 331, ca TP12; mockup đã duyệt docs/mockups/m1/tep-bi-mat.html, trạng
// thái A, B, C). Danh sách tệp lost (listLostFiles) có mã mẫu và nút mở mẫu để tải lại ảnh; "Kiểm lại toàn bộ tệp"
// gọi verifyFiles (useWriteAction: khóa chống trùng, gửi lại cùng khóa). API chỉ trả jobId, không có API hỏi việc đã
// xong: sau khi máy chủ nhận, màn hiện "Đang kiểm" tới khi rời trang; danh sách tự tải lại theo báo hiệu files.file và
// lưới an toàn 20 s của khung (docs/tasks/task-331.md, Quyết định 4).
import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listLostFiles, verifyFiles, type LostFile } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { TOPICS, queryKey } from "../../app/queryClient";
import { lostAtText, purposeLabel } from "./lostFilesRules";
import "./lost-files.css";

export const PAGE_SIZE = 50;
export const TEXT_VERIFY = "Kiểm lại toàn bộ tệp";

export function LostFilesPage() {
  const [checking, setChecking] = useState(false);
  const verify = useWriteAction((key: string) => unwrap(verifyFiles({ headers: { "Idempotency-Key": key } })));

  const lost = useInfiniteQuery({
    queryKey: queryKey(TOPICS.file, "lost"),
    // "" là trang đầu (không gửi cursor).
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      unwrap(listLostFiles({ query: { limit: PAGE_SIZE, ...(pageParam !== "" && { cursor: pageParam }) } })),
    getNextPageParam: (last) => last.nextCursor,
    placeholderData: keepPreviousData,
  });
  const rows = lost.data?.pages.flatMap((p) => p.items) ?? [];

  const start = async () => {
    const r = await verify.run(null);
    if (r.ok) setChecking(true);
  };

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Tệp bị mất</h1>
          <p className="sub">
            Tệp có trong CRM nhưng kho tệp không còn (ví dụ sau khi dựng lại kho tệp từ bản sao). Ảnh mất hiện ô trống ở
            mẫu; tải lại ảnh là hết mất.
          </p>
        </div>
        {!checking && (
          <button type="button" className="btn btn-dark" disabled={verify.pending} onClick={() => void start()}>
            {TEXT_VERIFY}
          </button>
        )}
      </div>

      {verify.message !== null && (
        <div className="box box-err lf-gap" role="alert">
          {verify.message}
        </div>
      )}
      {checking && (
        <div className="card lf-gap">
          <div className="row-between">
            <b>Đang kiểm toàn bộ tệp…</b>
            <button type="button" className="btn" disabled>
              {TEXT_VERIFY}
            </button>
          </div>
          <p className="small muted lf-note">
            Chạy nền, vài phút với vài nghìn tệp. Có thể rời trang; danh sách tự cập nhật khi xong.
          </p>
        </div>
      )}

      {lost.isPending && <p className="muted">Đang tải…</p>}
      {lost.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(lost.error)}
        </div>
      )}
      {lost.isSuccess && rows.length === 0 && (
        <div className="empty">
          <h2>Không có tệp nào bị mất</h2>
          <p className="muted">Bấm "{TEXT_VERIFY}" để kiểm ngay; nên kiểm mỗi lần dựng lại kho tệp.</p>
        </div>
      )}

      {rows.length > 0 && (
        <>
          <div className="tbl-frame">
            <table className="tbl lf">
              <thead>
                <tr>
                  <th>Tệp</th>
                  <th>Thuộc về</th>
                  <th>Phát hiện</th>
                  <th>
                    <span className="sr">Việc cần làm</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((f) => (
                  <LostRow key={f.fileId} f={f} />
                ))}
              </tbody>
            </table>
          </div>
          {lost.hasNextPage && (
            <div className="more">
              <span>{`Đang hiện ${String(rows.length)} tệp đầu.`}</span>
              <button
                type="button"
                className="btn btn-quiet"
                disabled={lost.isFetchingNextPage}
                onClick={() => void lost.fetchNextPage()}
              >
                {`Tải thêm ${String(PAGE_SIZE)}`}
              </button>
            </div>
          )}
        </>
      )}
    </>
  );
}

function LostRow({ f }: { f: LostFile }) {
  const href = f.productId === undefined ? null : `/san-pham/${f.productId}`;
  return (
    <tr>
      <td className="lead">
        <b>{purposeLabel(f.purpose)}</b>
      </td>
      <td data-label="Thuộc về">
        {href === null ? (
          "—"
        ) : (
          <Link className="cell-a" to={href}>
            {f.productCode ?? f.productId}
          </Link>
        )}
      </td>
      <td data-label="Phát hiện" className="num nowrap">
        {lostAtText(f.lostAt)}
      </td>
      <td>
        {href !== null && (
          <Link className="btn btn-line" to={href}>
            Mở mẫu để tải lại
          </Link>
        )}
      </td>
    </tr>
  );
}
