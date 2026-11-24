// Danh sách bộ phối (mockup docs/mockups/m5/web-looks.html trạng thái A): tìm, đếm x/6 trên trang chủ, mỗi dòng có ảnh
// nhỏ, tên, các món, hiện/ẩn kèm lý do, ô "Trang chủ" (bộ thứ 7 khóa), lên/xuống, chép liên kết. Màn tải cả danh sách
// (≤ 500) một lần vì lưu là lưu cả danh sách.
import type { Look, LookStatus } from "../../api";
import { MAX_HOME, canTurnOnHome, hiddenLine, homeCount, lookProblem, matchLook } from "./lookRules";

export interface LookListProps {
  looks: readonly Look[];
  statuses: ReadonlyMap<string, LookStatus>;
  names: ReadonlyMap<string, string>;
  thumbs: ReadonlyMap<string, string>;
  selected: string | null;
  query: string;
  locked: boolean;
  /** Bộ đã lưu (chép được liên kết). */
  saved: ReadonlySet<string>;
  onQuery: (q: string) => void;
  onSelect: (id: string) => void;
  onHome: (id: string) => void;
  onMove: (i: number, dir: -1 | 1) => void;
  onCopy: (id: string) => void;
}

const TEXT_HOME_FULL = 'Trang chủ hiện tối đa 6 bộ phối. Tắt "Trang chủ" ở một bộ khác trước.';

function StatusLine({
  look,
  status,
  names,
}: {
  look: Look;
  status: LookStatus | undefined;
  names: LookListProps["names"];
}) {
  const problem = lookProblem(look);
  if (problem !== null) return <span className="why-hide">Chưa đủ để hiện: {problem}</span>;
  if (status === undefined) return <span className="small muted">Đang tính trạng thái…</span>;
  const line = hiddenLine(status, status.productId === undefined ? undefined : names.get(status.productId));
  return line.warn ? <span className="why-hide">{line.text}</span> : <span className="small muted">{line.text}</span>;
}

export function LookList(p: LookListProps) {
  const home = homeCount(p.looks);
  const shown = p.looks.filter((l) => p.statuses.get(l.id)?.shown === true).length;
  const searching = p.query.trim() !== "";
  const homeFull = home >= MAX_HOME && p.looks.some((l) => !l.onHome);
  return (
    <section className="card stack bl" aria-label="Bộ phối">
      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          placeholder="Tìm bộ phối theo tên dịp hoặc sản phẩm"
          aria-label="Tìm bộ phối"
          value={p.query}
          onChange={(e) => {
            p.onQuery(e.target.value);
          }}
        />
        <span className="small">
          {p.looks.length} bộ · {home}/{MAX_HOME} bật hiện trên trang chủ · {shown} đang hiện
        </span>
      </div>
      {homeFull && <p className="reason">{TEXT_HOME_FULL}</p>}
      {searching && <p className="small muted">Xóa ô tìm để đổi thứ tự.</p>}
      <ol className="olist" aria-label="Danh sách bộ phối">
        {p.looks.map((l, i) => {
          if (!matchLook(l, p.query, p.names)) return null;
          const n = String(i + 1);
          const thumb = p.thumbs.get(l.photoFileId);
          const items = l.items.map((it) => p.names.get(it.productId) ?? "Sản phẩm").join(", ");
          const homeOff = p.locked || !canTurnOnHome(p.looks, l);
          return (
            <li key={l.id} className={`oitem${l.id === p.selected ? " is-pick" : ""}${p.locked ? " is-off" : ""}`}>
              <span className="ord">{i + 1}</span>
              <span className="thumb">{thumb !== undefined && <img src={thumb} alt="" />}</span>
              <button
                type="button"
                className="grow wlk-pick"
                aria-current={l.id === p.selected}
                onClick={() => {
                  p.onSelect(l.id);
                }}
              >
                <b className="wrap-any">{l.name.trim() === "" ? "Bộ mới" : l.name}</b>
                <br />
                <span className="small muted">{items === "" ? "Chưa có sản phẩm" : items} · </span>
                <StatusLine look={l} status={p.statuses.get(l.id)} names={p.names} />
              </button>
              <label className={`sw${homeOff ? " is-off" : ""}`}>
                <input
                  type="checkbox"
                  aria-label={`Trang chủ: bộ ${n}`}
                  checked={l.onHome}
                  disabled={homeOff}
                  title={homeOff && !p.locked ? TEXT_HOME_FULL : undefined}
                  onChange={() => {
                    p.onHome(l.id);
                  }}
                />{" "}
                Trang chủ
              </label>
              <div className="row">
                <button
                  type="button"
                  className="ibtn"
                  aria-label={`Đưa bộ ${n} lên`}
                  disabled={p.locked || searching || i === 0}
                  onClick={() => {
                    p.onMove(i, -1);
                  }}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="ibtn"
                  aria-label={`Đưa bộ ${n} xuống`}
                  disabled={p.locked || searching || i === p.looks.length - 1}
                  onClick={() => {
                    p.onMove(i, 1);
                  }}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="btn btn-quiet"
                  aria-label={`Chép liên kết bộ ${n}`}
                  title={p.saved.has(l.id) ? undefined : "Lưu bộ phối trước khi chép liên kết"}
                  disabled={!p.saved.has(l.id)}
                  onClick={() => {
                    p.onCopy(l.id);
                  }}
                >
                  Chép liên kết
                </button>
              </div>
            </li>
          );
        })}
      </ol>
    </section>
  );
}
