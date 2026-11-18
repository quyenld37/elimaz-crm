// Trình duyệt hiểu URL tương đối "/health/live"; Request của Node (dùng trong jsdom) thì không. Trong test jsdom, đặt
// gốc URL của client sinh bằng location.origin của jsdom. Mã chạy thật không cần dòng này.
import { client } from "./api/client.gen";

if (typeof window !== "undefined") client.setConfig({ baseUrl: window.location.origin });
