// Điểm vào của màn hình. Phông chữ tự chứa trong bản dựng (CSP default-src 'self' chặn Google Fonts, docs/RUNBOOK.md):
// Mulish 400/600/700 (tiêu đề, menu; bộ latin + vietnamese) và Playfair Display 500 (chữ ELIMAZ, chỉ latin), như mockup M1.
import "@fontsource/mulish/latin-400.css";
import "@fontsource/mulish/latin-600.css";
import "@fontsource/mulish/latin-700.css";
import "@fontsource/mulish/vietnamese-400.css";
import "@fontsource/mulish/vietnamese-600.css";
import "@fontsource/mulish/vietnamese-700.css";
import "@fontsource/playfair-display/latin-500.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./shell/App";
import "./styles.css";

const root = document.getElementById("root");
if (root === null) throw new Error("index.html thiếu #root");
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
