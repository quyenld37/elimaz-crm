// Thanh bốn danh sách vận hành (mockup docs/mockups/m3/van-hanh.html): mỗi danh sách một đường riêng, số đếm của cả
// shop. "Giữ hàng quá hạn" không có số: hợp đồng không trả (QĐ-135-c). Chưa đọc được số → chỉ hiện tên.
import { NavLink } from "react-router";
import { useOpsCounts } from "./opsData";

export function OpsTabs() {
  const counts = useOpsCounts().data;
  const tab = (to: string, label: string, n?: number, late?: number) => (
    <NavLink key={to} to={to} className={({ isActive }) => (isActive ? "ops-tab is-on" : "ops-tab")}>
      {label}
      {n !== undefined && n > 0 && (
        <span className="ops-n num">
          {late !== undefined && late > 0 ? `${String(n)} · ${String(late)} quá hạn` : String(n)}
        </span>
      )}
    </NavLink>
  );
  return (
    <nav className="ops-tabs" aria-label="Danh sách vận hành">
      {tab("/ops/callbacks", "Cần gọi lại", counts?.callbacksOpen, counts?.callbacksOverdue)}
      {tab("/ops/stuck", "Đơn kẹt", counts?.stuck)}
      {tab("/ops/review", "Cần xem", counts?.review)}
      {tab("/ops/aged-holds", "Giữ hàng quá hạn")}
    </nav>
  );
}

/** Khung chung của trang: đường dẫn, tiêu đề, câu dưới tiêu đề, thanh tab. */
export function OpsFrame(props: { title: string; sub: string; children: React.ReactNode }) {
  return (
    <div className="ops">
      <div className="page-h">
        <div>
          <p className="crumb">{`Vận chuyển / ${props.title}`}</p>
          <h1>{props.title}</h1>
          <p className="sub">{props.sub}</p>
        </div>
      </div>
      <OpsTabs />
      {props.children}
    </div>
  );
}

/** Ba trạng thái chung của một danh sách: đang tải, lỗi (không vẽ như rỗng), rỗng. */
export function ListState(props: {
  pending: boolean;
  error: string | null;
  empty: boolean;
  emptyTitle: string;
  emptyText: string;
}) {
  if (props.pending) return <p className="muted">Đang tải…</p>;
  if (props.error !== null) {
    return (
      <div className="box box-err" role="alert">
        {props.error}
      </div>
    );
  }
  if (props.empty) {
    return (
      <div className="empty">
        <h2>{props.emptyTitle}</h2>
        <p className="small muted">{props.emptyText}</p>
      </div>
    );
  }
  return null;
}

/** Nhóm nút lọc chọn một (aria-pressed), dùng cho bộ lọc của các danh sách. */
export function Seg<K extends string>(props: {
  label: string;
  value: K;
  options: readonly { key: K; label: string }[];
  onChange: (k: K) => void;
}) {
  return (
    <div className="seg" role="group" aria-label={props.label}>
      {props.options.map((o) => (
        <button
          key={o.key}
          type="button"
          className="toggle"
          aria-pressed={props.value === o.key}
          onClick={() => {
            props.onChange(o.key);
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
