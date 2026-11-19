// Trình duyệt hiểu URL tương đối "/health/live"; Request của Node (dùng trong jsdom) thì không. Trong test jsdom, đặt
// gốc URL của client sinh bằng location.origin của jsdom. Mã chạy thật không cần dòng này.
//
// findBy* và waitFor chờ theo sự kiện; hạn của chúng chỉ là chốt chống treo, không phải tiêu chí đúng/sai. Hạn mặc định
// 1 s của Testing Library quá ngắn khi vitest chạy cả bộ trên máy tải cao (đỏ oan, Task 421), nên đặt 30 s như chốt
// chống treo của test Go; testTimeout trong vite.config.ts rộng hơn để chốt này báo lỗi rõ hơn.
import { configure } from "@testing-library/react";
import { client } from "./api/client.gen";

export const HANG_GUARD_MS = 30_000;

configure({ asyncUtilTimeout: HANG_GUARD_MS });

if (typeof window !== "undefined") client.setConfig({ baseUrl: window.location.origin });
