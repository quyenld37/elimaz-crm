// Chấm số tệp mới ở mục menu "Tệp xuất" (Task 241, M6-18; hành vi (b)): số lần chạy xong kể từ lần cuối người dùng mở
// trang Tệp xuất. Mốc "đã xem" nằm ở localStorage của trình duyệt (chỉ tiện từng người; không có thì coi như vừa xem,
// không đếm tệp cũ). Chỉ hỏi những họ lần chạy người xem có quyền.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useSyncExternalStore } from "react";
import { NavCount } from "../../shell/NavCount";
import { FAMILIES, RUNS_REFRESH_MS, runsKey, type FamilyDef } from "./runsData";
import { useReportAccess } from "./reportAccess";

export const SEEN_KEY = "crm.exports.seenAt";
const SEEN_EVENT = "crm:exports-seen";

function readSeen(): string | null {
  try {
    return localStorage.getItem(SEEN_KEY);
  } catch {
    return null;
  }
}

/** Ghi mốc đã xem (ISO) và báo cho chấm số cập nhật. */
export function markSeen(now: Date): void {
  try {
    localStorage.setItem(SEEN_KEY, now.toISOString());
  } catch {
    // Không có kho trình duyệt: chấm số chỉ không nhớ được, không ảnh hưởng gì khác.
  }
  window.dispatchEvent(new Event(SEEN_EVENT));
}

function subscribe(cb: () => void): () => void {
  window.addEventListener(SEEN_EVENT, cb);
  window.addEventListener("storage", cb);
  return () => {
    window.removeEventListener(SEEN_EVENT, cb);
    window.removeEventListener("storage", cb);
  };
}

function useFamilyReady(def: FamilyDef, enabled: boolean): readonly string[] {
  const q = useInfiniteQuery({
    queryKey: runsKey(def.family, "nav"),
    initialPageParam: "",
    getNextPageParam: () => undefined,
    queryFn: () => def.list(""),
    enabled,
    refetchInterval: RUNS_REFRESH_MS,
  });
  return (q.data?.pages[0]?.items ?? [])
    .filter((r) => r.status === "ready" && r.finishedAt !== undefined)
    .map((r) => r.finishedAt ?? "");
}

export function ExportsNavCount() {
  const access = useReportAccess();
  const [sales, inventory, customers] = FAMILIES;
  const seen = useSyncExternalStore(subscribe, readSeen, () => null);
  useEffect(() => {
    // Lần đầu (chưa có mốc): coi như vừa xem, không đếm tệp cũ.
    if (seen === null && access.known) markSeen(new Date());
  }, [seen, access.known]);
  const a = useFamilyReady(sales, access.can(sales.permission));
  const b = useFamilyReady(inventory, access.can(inventory.permission));
  const c = useFamilyReady(customers, access.can(customers.permission));
  if (seen === null) return null;
  const since = Date.parse(seen);
  const n = [...a, ...b, ...c].filter((t) => Date.parse(t) > since).length;
  return <NavCount n={n} />;
}
