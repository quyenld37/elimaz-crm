// Nhịp tim "đang xem / đang gõ / đang lên đơn" (Task 175; máy chủ Task 163: hạn 30 giây, CH10). Gửi ngay khi mở hội
// thoại hay khi trạng thái đổi, rồi mỗi 10 giây tới khi đóng. Mỗi nhịp một khóa chống trùng mới (Q9: nhịp sau là một
// yêu cầu mới, không phải gửi lại nhịp cũ), nên không qua useWriteAction (vốn giữ khóa khi lỗi chưa rõ kết quả). Nhịp
// lỗi thì bỏ qua: nhịp sau 10 giây gửi lại, máy chủ cho trạng thái hết hạn sau 30 giây.
import { useEffect, useRef } from "react";
import { setPresence, type PresenceState } from "../../../api";
import { unwrap } from "../../../app/errors";
import { idem } from "./messageWrites";

export const HEARTBEAT_MS = 10_000;

export interface HeartbeatOptions {
  /** Gửi một nhịp. Mặc định setPresence với khóa mới. */
  send?: (conversationId: string, state: PresenceState) => Promise<unknown>;
}

const defaultSend = (id: string, state: PresenceState) =>
  unwrap(setPresence({ path: { id }, body: { state }, headers: idem(crypto.randomUUID()) }));

export function usePresenceHeartbeat(conversationId: string, state: PresenceState, opts: HeartbeatOptions = {}): void {
  const sendRef = useRef(opts.send ?? defaultSend);
  useEffect(() => {
    sendRef.current = opts.send ?? defaultSend;
  });
  useEffect(() => {
    const beat = () => {
      sendRef.current(conversationId, state).catch(() => undefined);
    };
    beat();
    const t = setInterval(beat, HEARTBEAT_MS);
    return () => {
      clearInterval(t);
    };
  }, [conversationId, state]);
}
