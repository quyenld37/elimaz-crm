// Màn Vai trò — danh sách (kế hoạch M1 Task M1-56, Q35; mockup docs/mockups/m1/vai-tro.html trạng thái A). Vai trò Chủ
// và vai trò đã lưu trữ chỉ "Xem"; còn lại "Sửa".
import { Link } from "react-router";
import type { Role } from "../../api";
import { RefusalBox } from "../staff/staffData";
import { roleKind, isReadOnly } from "./roleRules";
import { useRoleData } from "./roleData";
import "../staff/staff.css";
import "./roles.css";

// Cột của bảng (mockup trạng thái A): tên cột, lớp căn phải cho cột số.
const COLUMNS: readonly (readonly [string, string | undefined])[] = [
  ["Vai trò", undefined],
  ["Loại", undefined],
  ["Số quyền", "r"],
  ["Người đang giữ", "r"],
  ["Trạng thái", undefined],
];

export function RoleList() {
  const { roles } = useRoleData();
  return (
    <>
      <div className="page-h">
        <div>
          <h1>Vai trò</h1>
          <p className="sub">
            Vai trò là một bộ quyền. Năm vai trò mẫu có sẵn; tự tạo thêm từ danh sách quyền. Phạm vi (toàn shop hay một
            kho) chọn khi gán vai trò cho người ở màn Nhân viên.
          </p>
        </div>
        <Link to="/vai-tro/moi" className="btn btn-dark">
          Tạo vai trò
        </Link>
      </div>
      {roles.isPending && <p className="muted">Đang tải…</p>}
      {roles.isError && <RefusalBox error={roles.error} message={null} />}
      {roles.data !== undefined && (
        <div className="tbl-frame">
          <table className="tbl rtbl">
            <thead>
              <tr>
                {COLUMNS.map(([label, cls]) => (
                  <th key={label} className={cls}>
                    {label}
                  </th>
                ))}
                <th className="c-act">
                  <span className="sr">Thao tác</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {roles.data.map((r) => (
                <RoleRow key={r.id} r={r} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

function RoleRow({ r }: { r: Role }) {
  const archived = r.status === "archived";
  return (
    <tr className={archived ? "is-muted" : undefined}>
      <td className="lead">
        <b className="wrap-any">{r.name}</b>
      </td>
      <td data-label="Loại">{roleKind(r)}</td>
      <td data-label="Số quyền" className="r">
        {r.permissions.length}
      </td>
      <td data-label="Người đang giữ" className="r">
        {r.holderCount}
      </td>
      <td data-label="Trạng thái">
        {archived ? (
          <span className="chip chip-line">Đã lưu trữ</span>
        ) : (
          <span className="chip chip-ok">Đang dùng</span>
        )}
      </td>
      <td className="c-act">
        <Link
          to={`/vai-tro/${r.id}`}
          className="btn btn-quiet"
          aria-label={`${isReadOnly(r) ? "Xem" : "Sửa"} ${r.name}`}
        >
          {isReadOnly(r) ? "Xem" : "Sửa"}
        </Link>
      </td>
    </tr>
  );
}
