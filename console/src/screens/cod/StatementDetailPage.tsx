// Chi tiết bảng kê /cod/statements/:id (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html trạng thái C, D;
// HV31; TI02, TI05, TI06, TI07). Đầu bảng kê và tổng theo nhóm (một lần đọc limit 1), năm ô nhóm bấm để lọc bảng
// (?nhom=), dòng của nhóm đang xem theo trang (con trỏ là số dòng), thanh Chốt. Báo hiệu cod_statement.changed của
// bảng kê này đọc lại; sau 409 statement_changed màn giữ nguyên bản đang xem (số không đổi) tới khi bấm Tải lại.
import { useInfiniteQuery, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import {
  getCodStatement,
  type CodMatchStatus,
  type CodStatementConfirmed,
  type CodStatementDetail,
  type CodStatementLine,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { detailKey, entityMeta, usePerms } from "./codData";
import {
  COD_TOPIC,
  GROUPS,
  PAGE,
  TEXT_CHANGED,
  accountTitle,
  defaultGroup,
  feeMoney,
  formatMoney,
  groupOf,
  isWorking,
  parseErrorHelp,
  parseErrorText,
  periodText,
  signedMoney,
  timeDay,
  totalOf,
  visibleGroups,
} from "./codRules";
import { ConfirmBar, ConfirmedBox } from "./ConfirmBar";
import { StatusCell } from "./StatementsPage";
import "./cod.css";

const isGroup = (v: string | null): v is CodMatchStatus => GROUPS.some((g) => g.status === v);

interface Frozen {
  head: CodStatementDetail;
  lines: InfiniteData<CodStatementDetail> | undefined;
  at: number;
}

function LineRow({ l }: { l: CodStatementLine }) {
  const diff = l.diffAmount;
  return (
    <tr className={l.matchStatus === "amount_mismatch" ? "is-err" : undefined}>
      <td className="lead">
        <span className="who-c">
          <span className="num bold">{l.trackingCode}</span>
          <span className="xs muted">{`Dòng ${String(l.lineNo)}`}</span>
        </span>
      </td>
      <td data-label="Đơn">
        {l.orderId === undefined ? (
          <span className="muted">—</span>
        ) : (
          <Link className="cell-a num" to={`/don/${l.orderId}`}>
            {l.orderCode ?? "Đơn"}
          </Link>
        )}
      </td>
      <td data-label="Hãng thu" className="r">
        <span className="money">{formatMoney(l.codAmount)}</span>
      </td>
      <td data-label="CRM chờ" className="r">
        {l.expectedAmount === undefined ? (
          <span className="muted">—</span>
        ) : (
          <span className="money">{formatMoney(l.expectedAmount)}</span>
        )}
      </td>
      <td data-label="Lệch" className="r">
        {diff === undefined ? (
          <span className="muted">—</span>
        ) : (
          <span className={diff < 0 ? "money err-text" : "money"}>{signedMoney(diff)}</span>
        )}
      </td>
      <td data-label="Phí hãng" className="r">
        <span className="money">{feeMoney(l.feeAmount + l.otherFee)}</span>
      </td>
    </tr>
  );
}

function LinesTable({ group, lines, count }: { group: CodMatchStatus; lines: CodStatementLine[]; count: number }) {
  if (lines.length === 0) return <p className="small muted">Nhóm này không có dòng nào.</p>;
  if (group === "external") {
    return (
      <div className="tbl-frame">
        <table className="tbl">
          <thead>
            <tr>
              <th>{`Ngoài hệ thống (${String(lines.length)} trong ${String(count)} dòng)`}</th>
              <th className="r">Hãng thu</th>
              <th>Ghi chú</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l) => (
              <tr key={l.id} className="is-muted">
                <td className="lead num">{l.trackingCode}</td>
                <td data-label="Hãng thu" className="r">
                  <span className="money">{formatMoney(l.codAmount)}</span>
                </td>
                <td data-label="Ghi chú">
                  <span className="small">Không có vận đơn này trong CRM (đơn Nhanh cũ) · không tính vào số liệu</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
  return (
    <div className="tbl-frame">
      <table className="tbl">
        <thead>
          <tr>
            <th>Dòng · mã vận đơn</th>
            <th>Đơn</th>
            <th className="r">Hãng thu</th>
            <th className="r">CRM chờ</th>
            <th className="r">Lệch</th>
            <th className="r">Phí hãng</th>
          </tr>
        </thead>
        <tbody>
          {lines.map((l) => (
            <LineRow key={l.id} l={l} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function StatementDetailPage() {
  const id = useParams().id ?? "";
  const qc = useQueryClient();
  const perms = usePerms();
  const [params, setParams] = useSearchParams();
  const [frozen, setFrozen] = useState<Frozen | null>(null);
  const [confirmed, setConfirmed] = useState<CodStatementConfirmed | null>(null);
  // Bản đang xem lúc bấm Chốt: 409 thì màn giữ đúng bản này (báo hiệu có thể đã đọc lại trước khi 409 về).
  const atSubmit = useRef<Frozen | null>(null);

  const head = useQuery({
    queryKey: detailKey(id, "head"),
    meta: entityMeta(id),
    queryFn: () => unwrap(getCodStatement({ path: { id }, query: { limit: 1 } })),
  });
  const shownHead = frozen?.head ?? head.data;
  const totals = shownHead?.totals ?? [];
  const picked = params.get("nhom");
  const group: CodMatchStatus = isGroup(picked) ? picked : defaultGroup(totals);
  const lines = useInfiniteQuery({
    queryKey: detailKey(id, group),
    meta: entityMeta(id),
    enabled: head.data !== undefined && !isWorking(head.data.statement),
    initialPageParam: "",
    getNextPageParam: (last: CodStatementDetail) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(
        getCodStatement({
          path: { id },
          query: { matchStatus: group, limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) },
        }),
      ),
  });

  if (shownHead === undefined) {
    return (
      <div className="cod">
        {head.isError ? (
          <div className="box box-err" role="alert">
            {errorMessage(head.error)}
          </div>
        ) : (
          <p className="muted">Đang tải…</p>
        )}
      </div>
    );
  }

  const st = shownHead.statement;
  const shownLines = (frozen === null ? lines.data : frozen.lines)?.pages.flatMap((p) => p.lines) ?? [];
  const period = periodText(st);
  const working = isWorking(st);
  const canConfirm = perms.can("cash.statements.confirm") && st.status === "matched" && confirmed === null;
  const reload = () => {
    setFrozen(null);
    void qc.invalidateQueries({ queryKey: [COD_TOPIC, "detail", id] });
  };

  return (
    <div className="cod">
      <div className="page-h">
        <div>
          <p className="crumb">
            Tiền / <Link to="/cod/statements">Đối soát COD</Link>
          </p>
          <h1>Bảng kê COD</h1>
        </div>
      </div>
      <div className="card stack">
        <div className="row-between">
          <div>
            <h2>{period === "—" ? accountTitle(st) : `${accountTitle(st)} · ${period}`}</h2>
            <p className="small muted">
              {`${String(st.lineCount)} dòng`}
              {st.declaredTotal !== undefined && (
                <>
                  {" · tổng khai trên tệp "}
                  <span className="money">{formatMoney(st.declaredTotal)}</span>
                </>
              )}
              {` · nhập ${timeDay(st.createdAt).replace(" · ", " ")}${
                st.importedBy === undefined ? "" : ` bởi ${st.importedBy.name}`
              }`}
            </p>
          </div>
          <StatusCell st={st} />
        </div>
        {st.status === "parse_failed" && (
          <div className="box box-err small" role="alert">
            <b>{`Tệp lỗi: ${parseErrorText(st.parseError)}.`}</b> {parseErrorHelp(st.parseError)}
          </div>
        )}
        {!working && st.status !== "parse_failed" && (
          <>
            <div className="grps">
              {visibleGroups(totals).map((g) => {
                const t = totalOf(totals, g.status);
                return (
                  <Link
                    key={g.status}
                    className={g.tone === undefined ? "grp" : `grp is-${g.tone}`}
                    to={`?nhom=${g.status}`}
                    aria-current={g.status === group ? "true" : undefined}
                    aria-disabled={frozen === null ? undefined : "true"}
                    onClick={(e) => {
                      e.preventDefault();
                      // Đang giữ bản cũ sau 409: chỉ có dòng của nhóm đang xem, không đổi nhóm tới khi Tải lại.
                      if (frozen === null) setParams({ nhom: g.status }, { replace: true });
                    }}
                  >
                    <span className="small">{`${g.label} · ${String(t.count)} dòng`}</span>
                    <span className="money">{formatMoney(t.codAmount)}</span>
                    {g.note !== undefined && <span className="xs muted">{g.note}</span>}
                  </Link>
                );
              })}
            </div>
            {lines.isError && frozen === null ? (
              <div className="box box-err" role="alert">
                {errorMessage(lines.error)}
              </div>
            ) : lines.isPending && frozen === null ? (
              <p className="muted">Đang tải…</p>
            ) : (
              <>
                <LinesTable group={group} lines={shownLines} count={totalOf(totals, group).count} />
                <div className="more">
                  <span>
                    {`Nhóm ${groupOf(group).label} ${String(totalOf(totals, group).count)} dòng · hiện ${String(shownLines.length)}`}
                  </span>
                  {frozen === null && lines.hasNextPage && (
                    <button
                      type="button"
                      className="btn btn-line"
                      disabled={lines.isFetchingNextPage}
                      onClick={() => {
                        void lines.fetchNextPage();
                      }}
                    >
                      Tải thêm
                    </button>
                  )}
                  <span>Lệch tiền tính bằng hãng thu − CRM chờ</span>
                </div>
              </>
            )}
          </>
        )}
        {frozen !== null && (
          <div className="box box-err" role="alert">
            <b>{TEXT_CHANGED}</b>{" "}
            <span className="small">
              {`Chưa ghi gì từ lần bấm của bạn. Số trên màn là bản bạn đang xem lúc ${
                timeDay(new Date(frozen.at).toISOString()).split(" · ")[0] ?? ""
              }.`}
            </span>
            <div className="row" style={{ marginTop: 8 }}>
              <button type="button" className="btn btn-line" onClick={reload}>
                Tải lại
              </button>
            </div>
          </div>
        )}
        {canConfirm && (
          <ConfirmBar
            statement={st}
            totals={totals}
            frozen={frozen !== null}
            onSubmit={() => {
              atSubmit.current = { head: shownHead, lines: lines.data, at: head.dataUpdatedAt };
            }}
            onConflict={() => {
              setFrozen(atSubmit.current);
            }}
            onConfirmed={(c) => {
              setConfirmed(c);
              void qc.invalidateQueries({ queryKey: [COD_TOPIC] });
            }}
          />
        )}
        {confirmed !== null && <ConfirmedBox result={confirmed} />}
      </div>
    </div>
  );
}
