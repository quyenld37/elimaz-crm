// Danh sách bảng kê COD /cod/statements (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html trạng thái A;
// HV30). Mới nhất trước, tải thêm theo con trỏ. Máy chủ đọc tệp, khớp dòng chạy nền: dòng "Đang phân tích" đổi theo báo
// hiệu cod_statement.changed (khóa truy vấn theo chủ đề). Lỗi tệp hiện câu tiếng Việt và "Nhập lại".
import { useState } from "react";
import { Link } from "react-router";
import { listCodStatements, type CodStatement } from "../../api";
import { unwrap } from "../../app/errors";
import { listKey, usePerms } from "./codData";
import {
  accountTitle,
  formatMoney,
  importedText,
  isWorking,
  parseErrorHelp,
  parseErrorText,
  periodText,
  statusView,
} from "./codRules";
import { ImportDialog, type PutFile } from "./ImportDialog";
import { Paged, usePaged } from "./Paged";
import "./cod.css";

/** Ô trạng thái: chip; đang phân tích thêm thanh chạy; lỗi tệp thêm câu lỗi. */
export function StatusCell({ st }: { st: CodStatement }) {
  const v = statusView(st);
  if (v.tone === "busy") {
    return (
      <span className="st-busy">
        <span className="chip">{v.text}</span>
        <span className="bar" role="progressbar" aria-label={v.text} aria-busy="true">
          <i />
        </span>
      </span>
    );
  }
  if (v.tone === "err") {
    return (
      <span className="st-err">
        <span className="chip chip-err">{v.text}</span>
        <span className="xs err-text">{parseErrorText(st.parseError)}</span>
      </span>
    );
  }
  return <span className={v.tone === "ok" ? "chip chip-ok" : "chip chip-dark"}>{v.text}</span>;
}

interface Reimport {
  accountId: string;
  problem?: string;
}

function Action({ st, canImport, onReimport }: { st: CodStatement; canImport: boolean; onReimport: () => void }) {
  if (isWorking(st)) {
    return (
      <button type="button" className="btn btn-quiet" disabled>
        Xem
      </button>
    );
  }
  if (st.status === "parse_failed") {
    return canImport ? (
      <button type="button" className="btn btn-line" onClick={onReimport}>
        Nhập lại
      </button>
    ) : null;
  }
  const href = `/cod/statements/${st.id}`;
  return st.status === "matched" ? (
    <Link className="btn btn-dark" to={href}>
      Xem và chốt
    </Link>
  ) : (
    <Link className="btn btn-quiet" to={href}>
      Xem
    </Link>
  );
}

export function StatementsPage({ put }: { put?: PutFile }) {
  const perms = usePerms();
  const canImport = perms.can("cash.statements.import") && perms.can("files.upload");
  const [dialog, setDialog] = useState<Reimport | "new" | null>(null);
  const paged = usePaged(listKey(), (query) => unwrap(listCodStatements({ query })));

  return (
    <div className="cod">
      <div className="page-h">
        <div>
          <p className="crumb">Tiền / Đối soát COD</p>
          <h1>Đối soát COD</h1>
          <p className="sub">
            Bảng kê tiền thu hộ của hãng: nhập tệp, CRM tự khớp từng dòng theo mã vận đơn và tài khoản hãng, kế toán
            chốt.
          </p>
        </div>
        {canImport && (
          <button
            type="button"
            className="btn btn-dark"
            onClick={() => {
              setDialog("new");
            }}
          >
            Nhập bảng kê
          </button>
        )}
      </div>
      <Paged
        paged={paged}
        emptyTitle="Chưa có bảng kê nào"
        emptyText="Nhập tệp bảng kê tiền thu hộ của hãng để CRM tự khớp theo mã vận đơn."
        countText={(n) => `${String(n)} bảng kê mới nhất`}
      >
        <div className="tbl-frame">
          <table className="tbl cod-t">
            <thead>
              <tr>
                <th>Bảng kê</th>
                <th>Kỳ</th>
                <th className="r">Dòng</th>
                <th className="r">Tổng khai trên tệp</th>
                <th>Trạng thái</th>
                <th className="c-act" />
              </tr>
            </thead>
            <tbody>
              {paged.rows.map((st) => {
                const counted = !isWorking(st) && st.status !== "parse_failed";
                return (
                  <tr key={st.id} className={st.status === "parse_failed" ? "is-err" : undefined}>
                    <td className="lead">
                      <div className="who-c">
                        <b>{accountTitle(st)}</b>
                        <span className="xs muted">{importedText(st)}</span>
                      </div>
                    </td>
                    <td data-label="Kỳ">
                      <span className="small num">{periodText(st)}</span>
                    </td>
                    <td data-label="Dòng" className="r num">
                      {counted ? String(st.lineCount) : "—"}
                    </td>
                    <td data-label="Tổng khai trên tệp" className="r">
                      {st.declaredTotal === undefined ? (
                        <span className="small muted">—</span>
                      ) : (
                        <span className="money">{formatMoney(st.declaredTotal)}</span>
                      )}
                    </td>
                    <td data-label="Trạng thái">
                      <StatusCell st={st} />
                    </td>
                    <td className="c-act">
                      <Action
                        st={st}
                        canImport={canImport}
                        onReimport={() => {
                          setDialog({
                            accountId: st.carrierAccountId,
                            problem: `Tệp lỗi: ${lowerFirst(parseErrorText(st.parseError))}. ${parseErrorHelp(st.parseError)}`,
                          });
                        }}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Paged>
      {dialog !== null && (
        <ImportDialog
          {...(dialog === "new" ? {} : dialog)}
          {...(put === undefined ? {} : { put })}
          onClose={() => {
            setDialog(null);
          }}
        />
      )}
    </div>
  );
}

function lowerFirst(s: string): string {
  return s.charAt(0).toLowerCase() + s.slice(1);
}
