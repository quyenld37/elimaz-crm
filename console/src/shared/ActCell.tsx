// Ô thao tác cuối hàng của các bảng danh sách (cột c-act): một dãy nút hoặc liên kết kiểu btn-quiet.
import type { ReactNode } from "react";
import { Link } from "react-router";

export function ActCell({ children }: { children: ReactNode }) {
  return (
    <td className="c-act">
      <div className="row">{children}</div>
    </td>
  );
}

export function ActLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link className="btn btn-quiet" to={to}>
      {children}
    </Link>
  );
}
