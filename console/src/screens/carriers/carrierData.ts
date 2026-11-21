// Dữ liệu dùng chung của màn Kết nối vận chuyển (Task 132): danh sách tài khoản (đọc hết các trang; shop chỉ có vài
// tài khoản), kho, lệnh kiểm đang chờ. Khóa truy vấn bắt đầu bằng chủ đề fulfillment.carrier_account nên báo hiệu của
// máy chủ (tạo, sửa, kết quả kiểm) tải lại danh sách và trang sửa.
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { checkCarrierAccount, listCarrierAccounts, listWarehouses, type CarrierAccount } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { CARRIER_ACCOUNT_TOPIC } from "./carrierRules";

export const accountsKey = queryKey(CARRIER_ACCOUNT_TOPIC, "list");
const PAGE = 100;
/** Trần số trang đọc (100 × 20 = 2.000 tài khoản), chặn vòng lặp khi máy chủ trả con trỏ lỗi. */
const MAX_PAGES = 20;

async function allAccounts(): Promise<CarrierAccount[]> {
  const out: CarrierAccount[] = [];
  let cursor: string | undefined;
  for (let i = 0; i < MAX_PAGES; i++) {
    const page = await unwrap(
      listCarrierAccounts({ query: { limit: PAGE, ...(cursor === undefined ? {} : { cursor }) } }),
    );
    out.push(...page.items);
    cursor = page.nextCursor;
    if (cursor === undefined || cursor === "") break;
  }
  return out;
}

/** Mọi tài khoản hãng của shop (máy chủ không bao giờ trả khóa; webhookUrl chỉ cho người có quyền manage). */
export function useAccounts() {
  return useQuery({ queryKey: accountsKey, queryFn: allAccounts });
}

export function useWarehouses() {
  const q = useQuery({ queryKey: queryKey("inventory.warehouses"), queryFn: () => unwrap(listWarehouses()) });
  const items = q.data ?? [];
  return { ...q, items, nameOf: (id: string) => items.find((w) => w.id === id)?.name ?? "" };
}

// Lệnh kiểm đang chờ: mã tài khoản → lastCheckAt lúc gửi lệnh ("" khi chưa kiểm lần nào). Kết quả kiểm về bằng báo
// hiệu (lastCheckAt đổi) thì dòng hết "Đang kiểm tra". Giữ trong bộ nhớ của QueryClient (dùng chung cho danh sách,
// hộp Thêm và trang sửa), không vào localStorage.
const pendingKey = ["fulfillment.carrier_account_checks"];
type Pending = Readonly<Record<string, string>>;

export function markChecking(qc: QueryClient, a: Pick<CarrierAccount, "id" | "lastCheckAt">) {
  qc.setQueryData<Pending>(pendingKey, (old) => ({ ...old, [a.id]: a.lastCheckAt ?? "" }));
}

export function usePendingChecks(): Pending {
  const q = useQuery<Pending>({
    queryKey: pendingKey,
    queryFn: () => ({}),
    staleTime: Number.POSITIVE_INFINITY,
    initialData: {},
  });
  return q.data;
}

/** Gửi lệnh kiểm kết nối cho tài khoản (khóa chống trùng do bên gọi giữ) rồi đánh dấu đang kiểm. */
export function useSendCheck() {
  const qc = useQueryClient();
  return useCallback(
    async (key: string, a: Pick<CarrierAccount, "id" | "lastCheckAt">) => {
      const r = await unwrap(checkCarrierAccount({ path: { id: a.id }, headers: { "Idempotency-Key": key } }));
      markChecking(qc, a);
      void qc.invalidateQueries({ queryKey: accountsKey });
      return r;
    },
    [qc],
  );
}
