// Các lần gộp của một nhóm khách (kế hoạch M2 Task M2-29; mockup docs/mockups/m2/khach-gop.html trạng thái C; HV30,
// DH14). Không có API liệt kê lần gộp: đọc các mục merge, merge_undone của dòng thời gian nhóm (getCustomerTimeline;
// summary chỉ có mã hồ sơ, không có mã KH của hồ sơ con). "Hoàn tác" gọi undoCustomerMerge; máy chủ quyết:
// merge_not_latest kèm blockingMergeId → câu báo có liên kết tới lần gộp phải hoàn tác trước; merge_window_expired →
// quá 30 ngày (Q14). Dòng đã quá 30 ngày ghi lý do thay cho nút.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { undoCustomerMerge, type Customer } from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { headers, reloadCustomers, useTimeline } from "./customerData";
import { mergeEntries, undoState, type MergeEntry } from "./customerRules";

const timeText = (iso: string) => shortTime(iso).replace(" ", " · ");

export function MergeHistory({ customer, canMerge }: { customer: Customer; canMerge: boolean }) {
  const qc = useQueryClient();
  const tl = useTimeline(customer.rootId);
  const [now] = useState(() => Date.now());
  const [failed, setFailed] = useState<{ entry: MergeEntry; error: unknown } | null>(null);
  const [done, setDone] = useState<MergeEntry | null>(null);
  const undo = useWriteAction((key, b: { mergeId: string }) =>
    unwrap(undoCustomerMerge({ path: { mergeId: b.mergeId }, headers: headers(key) })),
  );
  const entries = mergeEntries(tl.data?.pages.flatMap((p) => p.items) ?? []);

  const label = (id: string) =>
    id === customer.rootId ? customer.code : customer.memberIds.includes(id) ? "hồ sơ trong nhóm" : "hồ sơ khác";

  const run = async (e: MergeEntry) => {
    setFailed(null);
    setDone(null);
    const r = await undo.run({ mergeId: e.id });
    if (!r.ok) {
      setFailed({ entry: e, error: r.error });
      return;
    }
    setDone(e);
    reloadCustomers(qc);
  };

  const blockingId =
    failed !== null && failed.error instanceof ApiError && failed.error.code === "merge_not_latest"
      ? failed.error.details.blockingMergeId
      : undefined;
  const blocking = typeof blockingId === "string" ? entries.find((x) => x.id === blockingId) : undefined;

  return (
    <div className="card" id="lan-gop">
      <h2>Các lần gộp</h2>
      {tl.isError && <div className="box box-err">{errorMessage(tl.error)}</div>}
      {tl.isSuccess && entries.length === 0 && <p className="muted">Chưa có lần gộp nào.</p>}
      {entries.length > 0 && (
        <div className="tbl-frame" style={{ marginTop: 8 }}>
          <table className="tbl">
            <thead>
              <tr>
                <th>Lúc</th>
                <th>Gộp</th>
                <th>Người làm</th>
                <th>Hoàn tác</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => {
                const st = undoState(e, now);
                return (
                  <tr key={e.id} id={`gop-${e.id}`} className={st === "open" ? undefined : "is-muted"}>
                    <td className="lead num">{timeText(e.at)}</td>
                    <td data-label="Gộp">{`${label(e.mergedId)} vào ${label(e.survivorId)}`}</td>
                    <td data-label="Người làm">{e.actor === "" ? "—" : e.actor}</td>
                    {st === "open" ? (
                      <td data-label="Hoàn tác" className="c-act">
                        {canMerge ? (
                          <button
                            type="button"
                            className="btn btn-line"
                            disabled={undo.pending}
                            onClick={() => {
                              void run(e);
                            }}
                          >
                            Hoàn tác
                          </button>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                    ) : (
                      <td className="full">
                        <span className="reason">
                          {st === "undone"
                            ? `Đã hoàn tác lúc ${timeText(e.undoneAt ?? "")}.`
                            : "Quá 30 ngày, không hoàn tác được."}
                        </span>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {tl.hasNextPage && (
        <div className="more">
          <span>Mới nhất trước</span>
          <button
            type="button"
            className="btn btn-line"
            disabled={tl.isFetchingNextPage}
            onClick={() => {
              void tl.fetchNextPage();
            }}
          >
            Xem cũ hơn
          </button>
        </div>
      )}
      {failed !== null && (
        <div className="box box-err" role="alert" style={{ marginTop: 12 }}>
          <b>Chưa hoàn tác được.</b>{" "}
          {typeof blockingId === "string" ? (
            blocking === undefined ? (
              "Sau lần gộp này còn một lần gộp khác chạm các hồ sơ đó. Hoàn tác lần gộp đó trước."
            ) : (
              <>
                {`Sau lần gộp này còn lần gộp lúc ${timeText(blocking.at)} chạm các hồ sơ đó. Hoàn tác lần gộp ${timeText(blocking.at)} trước. `}
                <a className="cell-a" href={`#gop-${blocking.id}`}>
                  Mở lần gộp đó
                </a>
              </>
            )
          ) : (
            errorMessage(failed.error)
          )}
        </div>
      )}
      {done !== null && (
        <div className="toast" role="status" style={{ marginTop: 12 }}>
          <span className="grow">{`Đã hoàn tác lần gộp ${timeText(done.at)}: hai hồ sơ tách lại như trước.`}</span>
        </div>
      )}
    </div>
  );
}
