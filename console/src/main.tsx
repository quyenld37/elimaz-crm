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
