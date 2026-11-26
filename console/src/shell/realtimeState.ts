// Trạng thái lớp realtime của khung (src/shell/App.tsx) cho màn cần nói riêng về nó (Inbox, HV07 của mockup M4: băng
// "Mất kết nối, đang nối lại" có nút "Tải lại ngay"). Băng chung của khung (Banner) vẫn hiện như cũ.
import { createContext, useContext } from "react";
import type { RealtimeState } from "../realtime/client";

/** null: chưa nối lần nào (hay màn dựng ngoài khung, ví dụ test). */
export const RealtimeStateContext = createContext<RealtimeState | null>(null);

/** Trạng thái hiện tại của lớp realtime của tab. */
export function useRealtimeState(): RealtimeState | null {
  return useContext(RealtimeStateContext);
}
