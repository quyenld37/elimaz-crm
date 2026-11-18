// Khung màn hình: cột trái có chữ ELIMAZ CRM, vùng nội dung và băng báo. Chưa có màn hình hay mục menu nào: mọi màn
// cần mockup chủ shop đã duyệt (ADR-34, luật console 1); M1 thêm menu và định tuyến.
import { useEffect, useState } from "react";
import { createRealtimeClient, type RealtimeState } from "../realtime/client";
import { watchApiLive } from "./apiHealth";
import { Banner } from "./Banner";

export function App() {
  const [realtime, setRealtime] = useState<RealtimeState | null>(null);
  const [apiDown, setApiDown] = useState(false);

  useEffect(() => {
    const rt = createRealtimeClient({
      url: "/api/v1/stream",
      onState: setRealtime,
      // Chưa có danh sách nào đang mở để tải lại; M1 nối onResync, onChanged vào lớp lấy dữ liệu.
      onResync: () => undefined,
      onChanged: () => undefined,
    });
    const stopHealth = watchApiLive({ onChange: setApiDown });
    return () => {
      rt.close();
      stopHealth();
    };
  }, []);

  return (
    <div className="app">
      <aside className="side">
        <div className="brand">
          <span className="wordmark">ELIMAZ</span>
          <span className="xs">CRM</span>
        </div>
      </aside>
      <main className="main">
        <Banner realtime={realtime} apiDown={apiDown} />
      </main>
    </div>
  );
}
