// Các trang sau của một báo cáo phân trang bằng con trỏ (Task 241, M6-18). Trang đầu do useReport lấy (chạy nền, 503…);
// các trang tiếp theo tải ở đây và nối vào sau. Báo cáo tải lại (trang đầu đổi) thì các trang nối bị bỏ. Còn
// nextCursor thì chưa được coi là hết (bẫy Task 84).
import { useEffect, useMemo, useRef, useState } from "react";
import { errorMessage } from "../../app/errors";

export interface Paged<Row> {
  rows: readonly Row[];
  nextCursor?: string | undefined;
}

export interface MorePages<Row> {
  rows: readonly Row[];
  hasMore: boolean;
  loading: boolean;
  error: string | null;
  loadMore: () => void;
}

interface Extra<Row> {
  base: object;
  rows: Row[];
  next: string | undefined;
}

export function useMorePages<Row>(
  first: (Paged<Row> & object) | null,
  fetchPage: (cursor: string) => Promise<Paged<Row>>,
): MorePages<Row> {
  const [extra, setExtra] = useState<Extra<Row> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const latest = useRef(first);
  useEffect(() => {
    latest.current = first;
  });

  const cur = first !== null && extra !== null && extra.base === first ? extra : null;
  const next = first === null ? undefined : cur === null ? first.nextCursor : cur.next;
  const rows = useMemo(() => (first === null ? [] : [...first.rows, ...(cur?.rows ?? [])]), [first, cur]);

  const loadMore = () => {
    if (first === null || next === undefined || loading) return;
    const base = first;
    const have = cur?.rows ?? [];
    setLoading(true);
    setError(null);
    void fetchPage(next).then(
      (p) => {
        setLoading(false);
        if (latest.current !== base) return;
        setExtra({ base, rows: [...have, ...p.rows], next: p.nextCursor });
      },
      (e: unknown) => {
        setLoading(false);
        if (latest.current !== base) return;
        setError(errorMessage(e));
      },
    );
  };
  return { rows, hasMore: next !== undefined, loading, error, loadMore };
}
