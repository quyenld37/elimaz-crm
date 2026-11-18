// Gốc của màn hình: lớp lấy dữ liệu (TanStack Query), định tuyến (React Router), khung điều hướng và băng báo.
// Một createRealtimeClient cho cả tab; báo hiệu changed tải lại đúng truy vấn đang mở của chủ đề, resync tải lại mọi
// truy vấn đang mở (src/app/queryClient.ts). Khung vẫn tải và hiện băng báo khi API tạm ngừng (ADR-34).
import { QueryClientProvider, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { BrowserRouter } from "react-router";
import { getMe } from "../api";
import { unwrap } from "../app/errors";
import type { Screen } from "../app/nav";
import { createQueryClient, onRealtimeChanged, onRealtimeResync, queryKey } from "../app/queryClient";
import { AppRoutes } from "../app/router";
import { SCREENS } from "../app/screens";
import { createRealtimeClient, type RealtimeState } from "../realtime/client";
import { watchApiLive } from "./apiHealth";
import { Banner } from "./Banner";
import { Layout } from "./Layout";

export interface AppProps {
  /** Thay QueryClient (test). */
  queryClient?: QueryClient;
  /** Thay danh sách màn (test). */
  screens?: readonly Screen[];
}

export function App({ queryClient, screens = SCREENS }: AppProps) {
  const [qc] = useState(() => queryClient ?? createQueryClient());
  return (
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <Shell screens={screens} />
      </BrowserRouter>
    </QueryClientProvider>
  );
}

function Shell({ screens }: { screens: readonly Screen[] }) {
  const qc = useQueryClient();
  const [realtime, setRealtime] = useState<RealtimeState | null>(null);
  const [apiDown, setApiDown] = useState(false);
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });

  useEffect(() => {
    const rt = createRealtimeClient({
      url: "/api/v1/stream",
      onState: setRealtime,
      onResync: () => {
        onRealtimeResync(qc);
      },
      onChanged: (e) => {
        onRealtimeChanged(qc, e);
      },
    });
    const stopHealth = watchApiLive({ onChange: setApiDown });
    return () => {
      rt.close();
      stopHealth();
    };
  }, [qc]);

  const grants = me.data?.grants ?? [];
  return (
    <Layout
      screens={screens}
      grants={grants}
      email={me.data?.email ?? ""}
      banner={<Banner realtime={realtime} apiDown={apiDown} />}
    >
      {/* Chưa đọc được /api/v1/me (đang tải, API tạm ngừng, phiên hết hạn): không định tuyến với quyền rỗng, vì sẽ báo
          nhầm "chưa được giao màn hình" hay "không tìm thấy"; băng báo nói lý do. */}
      {me.data !== undefined && <AppRoutes screens={screens} grants={grants} />}
    </Layout>
  );
}
