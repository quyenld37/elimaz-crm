// Định tuyến (React Router 8, chế độ khai báo; lựa chọn: docs/chon-cong-cu-console.md). Không dùng loader/action:
// dữ liệu do TanStack Query lấy trong từng màn. Màn có quyền có route tới nội dung; màn có thật mà người xem thiếu quyền
// hiện "Không có quyền" (mockup M1 suc-khoe.html trạng thái C, Task 53); đường không có thật rơi vào "không tìm thấy".
// Không truyền chuỗi lấy từ dữ liệu ngoài (API, URL) vào navigate hay <Link to> mà không kiểm là đường nội bộ.
import { Link, Navigate, Route, Routes } from "react-router";
import type { Grant } from "../api";
import { allowedScreens, canSee, type Screen } from "./nav";

export const TEXT_EMPTY_HOME = "Tài khoản này chưa được giao màn hình nào. Nhờ Chủ shop cấp quyền";
export const TEXT_NOT_FOUND = "Không có trang này, hoặc bạn chưa được cấp quyền xem";
export const TEXT_FORBIDDEN = "Không có quyền";

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

/** Màn có thật nhưng người xem thiếu quyền (API vẫn kiểm quyền thật; trang này chỉ nói rõ lý do). */
function Forbidden({ screen, first }: { screen: Screen; first: Screen | undefined }) {
  return (
    <div className="empty">
      <h2>{TEXT_FORBIDDEN}</h2>
      <p className="muted">Tài khoản của bạn không được xem {screen.label}. Cần thì nhờ Chủ shop cấp quyền.</p>
      {first !== undefined && (
        <Link className="btn btn-line" to={first.path}>
          Về trang {first.label}
        </Link>
      )}
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
      {screens.map((s) => (
        <Route
          key={s.path}
          path={s.path}
          element={canSee(grants, s) ? s.element : <Forbidden screen={s} first={first} />}
        />
      ))}
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}
