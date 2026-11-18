// Định tuyến (React Router 8, chế độ khai báo; lựa chọn: docs/chon-cong-cu-console.md). Không dùng loader/action:
// dữ liệu do TanStack Query lấy trong từng màn. Chỉ màn có quyền mới có route; còn lại rơi vào "không tìm thấy".
// Không truyền chuỗi lấy từ dữ liệu ngoài (API, URL) vào navigate hay <Link to> mà không kiểm là đường nội bộ.
import { Link, Navigate, Route, Routes } from "react-router";
import type { Grant } from "../api";
import { allowedScreens, type Screen } from "./nav";

export const TEXT_EMPTY_HOME = "Tài khoản này chưa được giao màn hình nào. Nhờ Chủ shop cấp quyền";
export const TEXT_NOT_FOUND = "Không có trang này, hoặc bạn chưa được cấp quyền xem";

function Home({ first }: { first: Screen | undefined }) {
  if (first !== undefined) return <Navigate to={first.path} replace />;
  return <p className="muted">{TEXT_EMPTY_HOME}</p>;
}

function NotFound() {
  return (
    <div className="stack-sm">
      <p>{TEXT_NOT_FOUND}</p>
      <p>
        <Link to="/">Về trang đầu</Link>
      </p>
    </div>
  );
}

export interface AppRoutesProps {
  screens: readonly Screen[];
  grants: readonly Grant[];
}

export function AppRoutes({ screens, grants }: AppRoutesProps) {
  const allowed = allowedScreens(screens, grants);
  const first = allowed.find((s) => s.hidden !== true && !s.path.includes(":"));
  return (
    <Routes>
      <Route index element={<Home first={first} />} />
      {allowed.map((s) => (
        <Route key={s.path} path={s.path} element={s.element} />
      ))}
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}
