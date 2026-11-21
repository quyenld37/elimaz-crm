// Khung điều hướng theo mockup M1 đã duyệt (docs/mockups/m1/khung.js, m1.css): từ 1000 px có cột menu trái 240 px;
// điện thoại, máy tính bảng có thanh trên 56 px (☰, tên màn, chữ ELIMAZ, nút tài khoản) và menu trượt từ trái.
// Menu mở: phần nội dung bị inert, Escape hay bấm nền mờ thì đóng và trả focus về ☰; bấm một mục thì đi và đóng.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router";
import type { Grant } from "../api";
import { currentScreen, isCurrent, navSections, type Screen } from "../app/nav";

export interface LayoutProps {
  screens: readonly Screen[];
  grants: readonly Grant[];
  /** Email người đang đăng nhập; rỗng khi chưa tải xong /api/v1/me. */
  email: string;
  /** Băng báo (Banner) đặt trên đầu nội dung. */
  banner: ReactNode;
  children: ReactNode;
}

export function Layout({ screens, grants, email, banner, children }: LayoutProps) {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(false);
  const menuBtn = useRef<HTMLButtonElement>(null);
  const navRef = useRef<HTMLElement>(null);
  const sections = navSections(screens, grants);
  const title = currentScreen(screens, pathname)?.label ?? "";
  const initial = email.charAt(0).toUpperCase();

  const close = useCallback((restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) menuBtn.current?.focus();
  }, []);

  useEffect(() => {
    if (!open) return;
    const nav = navRef.current;
    (nav?.querySelector<HTMLElement>("a") ?? nav)?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close(true);
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
    };
  }, [open, close]);

  return (
    <div className={open ? "app nav-open" : "app"}>
      <aside className="side" id="side-nav">
        <div className="brand">
          <span className="wordmark">ELIMAZ</span>
          <span className="xs">CRM</span>
        </div>
        <nav aria-label="Menu chính" ref={navRef} tabIndex={-1}>
          {sections.map((s) => (
            <div className="nav-g" role="group" aria-label={s.group} key={s.group}>
              <p aria-hidden="true">{s.group}</p>
              {s.items.map((item) => (
                <Link
                  key={item.path}
                  className="nav-a"
                  to={item.path}
                  aria-current={isCurrent(item, pathname) ? "page" : undefined}
                  onClick={() => {
                    close(false);
                  }}
                >
                  <span className="grow">{item.label}</span>
                  {item.badge}
                </Link>
              ))}
            </div>
          ))}
        </nav>
        {email !== "" && (
          <div className="side-user">
            <b className="wrap-any">{email}</b>
          </div>
        )}
      </aside>
      {open && (
        <button
          type="button"
          className="scrim"
          aria-label="Đóng menu"
          onClick={() => {
            close(true);
          }}
        />
      )}
      <header className="top">
        <button
          type="button"
          ref={menuBtn}
          className="ibtn"
          aria-label="Mở menu"
          aria-expanded={open}
          aria-controls="side-nav"
          onClick={() => {
            setOpen(true);
          }}
        >
          ☰
        </button>
        <span className="t ellip">{title}</span>
        <span className="wordmark xs">ELIMAZ</span>
        {email !== "" && (
          <button
            type="button"
            className="ibtn"
            aria-label={`Tài khoản ${email}`}
            onClick={() => {
              setOpen(true);
            }}
          >
            {initial}
          </button>
        )}
      </header>
      <main className="main" inert={open}>
        {banner}
        {children}
      </main>
    </div>
  );
}
