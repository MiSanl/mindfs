import React, { useState, useEffect, useRef } from "react";
import { useI18n } from "../i18n";

type AppShellProps = {
  sidebar: React.ReactNode;
  main: React.ReactNode;
  rightSidebar?: React.ReactNode;
  footer: React.ReactNode;
  drawer?: React.ReactNode;
  leftOpen?: boolean;
  rightOpen?: boolean;
  onCloseLeft?: () => void;
  onCloseRight?: () => void;
  onOpenLeft?: () => void;
  onOpenRight?: () => void;
  sidebarsSwapped?: boolean;
};

const MOBILE_BREAKPOINT = 768;
const TABLET_BREAKPOINT = 1024;
const LEFT_SIDEBAR_WIDTH_KEY = "mindfs-left-sidebar-width";
const RIGHT_SIDEBAR_WIDTH_KEY = "mindfs-right-sidebar-width";
const MIN_LEFT_SIDEBAR_WIDTH = 180;
const MAX_LEFT_SIDEBAR_WIDTH = 480;
const MIN_RIGHT_SIDEBAR_WIDTH = 220;
const MAX_RIGHT_SIDEBAR_WIDTH = 520;

function readSidebarWidth(key: string, fallback: number, min: number, max: number): number {
  if (typeof window === "undefined") {
    return fallback;
  }
  try {
    const saved = Number(window.localStorage.getItem(key));
    return Number.isFinite(saved) ? Math.min(max, Math.max(min, saved)) : fallback;
  } catch {
    return fallback;
  }
}

function useResponsive() {
  const [isMobile, setIsMobile] = useState(false);
  const [isTablet, setIsTablet] = useState(false);
  useEffect(() => {
    const checkSize = () => {
      const width = window.innerWidth;
      setIsMobile(width < MOBILE_BREAKPOINT);
      setIsTablet(width >= MOBILE_BREAKPOINT && width < TABLET_BREAKPOINT);
    };
    checkSize();
    window.addEventListener("resize", checkSize);
    return () => window.removeEventListener("resize", checkSize);
  }, []);
  return { isMobile, isTablet };
}

const sidebarStyle: React.CSSProperties = {
  gridArea: "sidebar",
  borderRight: "1px solid var(--border-color)",
  overflow: "auto",
  background: "var(--mindfs-topbar-bg, var(--sidebar-bg))",
  display: "flex",
  flexDirection: "column",
  position: "relative",
  zIndex: 10,
  contain: "layout paint",
  minWidth: 0,
};

const mainStyle: React.CSSProperties = {
  gridArea: "main",
  overflow: "hidden",
  padding: "0",
  background: "var(--mindfs-topbar-bg, var(--mobile-overlay-bg, var(--content-bg)))",
  display: "flex",
  flexDirection: "column",
  minHeight: 0,
  position: "relative",
  zIndex: 1,
  contain: "layout paint",
  minWidth: 0,
};

const rightStyle: React.CSSProperties = {
  gridArea: "right",
  borderLeft: "1px solid var(--border-color)",
  overflow: "auto",
  background: "var(--mindfs-topbar-bg, var(--sidebar-bg))",
  display: "flex",
  flexDirection: "column",
  position: "relative",
  zIndex: 10,
  contain: "layout paint",
  minWidth: 0,
};

const footerStyle: React.CSSProperties = {
  gridArea: "footer",
  borderTop: "none",
  padding: "0",
  display: "flex",
  alignItems: "flex-end",
  justifyContent: "center",
  background: "var(--mindfs-topbar-bg, var(--mobile-overlay-bg, var(--content-bg)))",
  zIndex: 100,
  minWidth: 0,
};

export function AppShell({
  sidebar,
  main,
  rightSidebar,
  footer,
  drawer,
  leftOpen = true,
  rightOpen = true,
  onCloseLeft,
  onCloseRight,
  onOpenLeft,
  onOpenRight,
  sidebarsSwapped = false,
}: AppShellProps) {
  const { t } = useI18n();
  const { isMobile, isTablet } = useResponsive();
  const [sidebarWidth, setSidebarWidth] = useState(() =>
    readSidebarWidth(LEFT_SIDEBAR_WIDTH_KEY, 260, MIN_LEFT_SIDEBAR_WIDTH, MAX_LEFT_SIDEBAR_WIDTH),
  );
  const [rightWidth, setRightWidth] = useState(() =>
    readSidebarWidth(RIGHT_SIDEBAR_WIDTH_KEY, 280, MIN_RIGHT_SIDEBAR_WIDTH, MAX_RIGHT_SIDEBAR_WIDTH),
  );
  const draggedRef = useRef(false);
  const shellRef = useRef<HTMLDivElement>(null);
  const resizeGuideRef = useRef<HTMLDivElement>(null);
  const resizeShadeRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    try {
      window.localStorage.setItem(LEFT_SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    } catch {}
  }, [sidebarWidth]);

  useEffect(() => {
    try {
      window.localStorage.setItem(RIGHT_SIDEBAR_WIDTH_KEY, String(rightWidth));
    } catch {}
  }, [rightWidth]);

  const logicalLeftWidth = isMobile ? 0 : (isTablet ? Math.min(sidebarWidth, 240) : sidebarWidth);
  const logicalRightWidth = isMobile || !rightSidebar ? 0 : (isTablet ? Math.min(rightWidth, 300) : rightWidth);
  const mobileHeight = "var(--mindfs-viewport-height, 100dvh)";
  const physicalLeftOpen = sidebarsSwapped ? rightOpen : leftOpen;
  const physicalRightOpen = sidebarsSwapped ? leftOpen : rightOpen;
  const physicalLeftWidth = sidebarsSwapped ? logicalRightWidth : logicalLeftWidth;
  const physicalRightWidth = sidebarsSwapped ? logicalLeftWidth : logicalRightWidth;
  const physicalLeftContent = sidebarsSwapped ? rightSidebar : sidebar;
  const physicalRightContent = sidebarsSwapped ? sidebar : rightSidebar;
  const physicalLeftClose = sidebarsSwapped ? onCloseRight : onCloseLeft;
  const physicalLeftOpenHandler = sidebarsSwapped ? onOpenRight : onOpenLeft;
  const physicalRightClose = sidebarsSwapped ? onCloseLeft : onCloseRight;
  const physicalRightOpenHandler = sidebarsSwapped ? onOpenLeft : onOpenRight;
  const physicalLeftLabel = sidebarsSwapped ? t("sidebar.session") : t("sidebar.file");
  const physicalRightLabel = sidebarsSwapped ? t("sidebar.file") : t("sidebar.session");

  const startResize = (side: "left" | "right", event: React.PointerEvent<HTMLButtonElement>) => {
    if (isMobile || (side === "left" ? !physicalLeftOpen : !physicalRightOpen)) {
      return;
    }
    event.preventDefault();
    draggedRef.current = false;
    const rail = event.currentTarget;
    rail.classList.add("is-preview");
    const startX = event.clientX;
    const startWidth = side === "left" ? physicalLeftWidth : physicalRightWidth;
    const isSidebar = sidebarsSwapped ? side === "right" : side === "left";
    const minWidth = isSidebar ? MIN_LEFT_SIDEBAR_WIDTH : MIN_RIGHT_SIDEBAR_WIDTH;
    const maxWidth = isSidebar
      ? (isTablet ? Math.min(MAX_LEFT_SIDEBAR_WIDTH, 240) : MAX_LEFT_SIDEBAR_WIDTH)
      : (isTablet ? Math.min(MAX_RIGHT_SIDEBAR_WIDTH, 300) : MAX_RIGHT_SIDEBAR_WIDTH);
    let nextWidth = startWidth;
    let animationFrame = 0;
    const showPreview = () => {
      const guide = resizeGuideRef.current;
      const shade = resizeShadeRef.current;
      if (!guide || !shade) {
        return;
      }
      const previewStart = Math.min(startWidth, nextWidth);
      const previewWidth = Math.abs(nextWidth - startWidth);
      guide.style.setProperty(side, `${nextWidth}px`);
      guide.style.removeProperty(side === "left" ? "right" : "left");
      guide.style.setProperty("opacity", "1");
      shade.style.setProperty(side, `${previewStart}px`);
      shade.style.removeProperty(side === "left" ? "right" : "left");
      shade.style.setProperty("width", `${previewWidth}px`);
      shade.style.setProperty("opacity", previewWidth > 0 ? "1" : "0");
    };
    const updateWidth = (width: number) => {
      nextWidth = Math.min(maxWidth, Math.max(minWidth, width));
      if (!animationFrame) {
        animationFrame = window.requestAnimationFrame(() => {
          showPreview();
          animationFrame = 0;
        });
      }
    };
    const onPointerMove = (moveEvent: PointerEvent) => {
      const delta = side === "left" ? moveEvent.clientX - startX : startX - moveEvent.clientX;
      if (Math.abs(delta) > 3) {
        draggedRef.current = true;
      }
      updateWidth(startWidth + delta);
    };
    const onPointerUp = () => {
      if (animationFrame) {
        window.cancelAnimationFrame(animationFrame);
        animationFrame = 0;
      }
      shellRef.current?.style.setProperty("transition", "none");
      if (isSidebar) {
        setSidebarWidth(nextWidth);
      } else {
        setRightWidth(nextWidth);
      }
      window.requestAnimationFrame(() => {
        rail.classList.remove("is-preview");
        resizeGuideRef.current?.style.setProperty("opacity", "0");
        resizeShadeRef.current?.style.setProperty("opacity", "0");
        shellRef.current?.style.removeProperty("transition");
      });
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
    };
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp, { once: true });
    window.addEventListener("pointercancel", onPointerUp, { once: true });
  };

  const shellStyle: React.CSSProperties & {
    "--mindfs-actionbar-bottom-padding"?: string;
  } = {
    display: isMobile ? "flex" : "grid",
    flexDirection: isMobile ? "column" : undefined,
    gridTemplateColumns: isMobile ? undefined : `${physicalLeftOpen ? `${physicalLeftWidth}px` : "0px"} 1fr ${physicalRightOpen ? `${physicalRightWidth}px` : "0px"}`,
    gridTemplateRows: isMobile ? undefined : "1fr auto",
    gridTemplateAreas: isMobile ? undefined : `"sidebar main right" "sidebar footer right"`,
    minHeight: isMobile ? mobileHeight : "100vh",
    height: isMobile ? mobileHeight : "100dvh",
    background: isMobile
      ? "var(--mindfs-topbar-bg, var(--mindfs-system-bar-bg, var(--mobile-overlay-bg, var(--content-bg))))"
      : "var(--bg-gradient-composite, var(--bg-gradient-start, #f3f4f6))",
    color: "var(--text-primary)",
    position: "relative",
    width: isMobile ? "100%" : undefined,
    maxWidth: isMobile ? "100%" : undefined,
    paddingTop: isMobile ? "var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px))" : undefined,
    overflow: "hidden",
    isolation: "isolate",
    boxSizing: "border-box",
    transition: "grid-template-columns 0.3s cubic-bezier(0.4, 0, 0.2, 1)",
    "--mindfs-actionbar-bottom-padding": "calc(var(--mindfs-safe-area-bottom) + 12px)",
  };

  const mobileSidebarStyle = (side: 'left' | 'right'): React.CSSProperties => ({
    position: "fixed",
    top: "var(--mindfs-safe-area-top, env(safe-area-inset-top, 0px))",
    bottom: 0,
    [side]: 0,
    width: "75vw",
    zIndex: 2000,
    background: "var(--mindfs-topbar-bg, var(--mobile-sidebar-bg, var(--sidebar-bg)))",
    boxShadow: side === 'left' ? "4px 0 24px rgba(0,0,0,0.15)" : "-4px 0 24px rgba(0,0,0,0.15)",
    transition: "transform 0.22s cubic-bezier(0.2, 0.8, 0.2, 1)",
    display: "flex",
    flexDirection: "column",
    overflow: "hidden",
    borderTopRightRadius: side === 'left' ? "14px" : undefined,
    borderBottomRightRadius: side === 'left' ? "14px" : undefined,
    borderTopLeftRadius: side === 'right' ? "14px" : undefined,
    borderBottomLeftRadius: side === 'right' ? "14px" : undefined,
    willChange: "transform",
    backfaceVisibility: "hidden",
    transform: "translateX(0) translateZ(0)",
  });

  const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    background: "rgba(0,0,0,0.3)",
    zIndex: 1500,
    opacity: (isMobile && (leftOpen || rightOpen)) ? 1 : 0,
    pointerEvents: (isMobile && (leftOpen || rightOpen)) ? "auto" : "none",
    transition: "opacity 0.18s ease",
    willChange: "opacity",
    backfaceVisibility: "hidden",
    transform: "translateZ(0)",
  };

  const mobileFooterStyle: React.CSSProperties = {
    ...footerStyle,
    flexShrink: 0,
  };

  return (
    <div ref={shellRef} className="mindfs-app-shell" style={shellStyle}>
      {isMobile && <div style={overlayStyle} onClick={() => { onCloseLeft?.(); onCloseRight?.(); }} />}

      {(!isMobile || physicalLeftOpen) && physicalLeftContent ? (
        <aside
          style={
            isMobile
              ? mobileSidebarStyle('left')
              : {
                  ...sidebarStyle,
                  overflow: physicalLeftOpen ? "auto" : "hidden",
                  pointerEvents: physicalLeftOpen ? "auto" : "none",
                }
          }
        >
          {physicalLeftContent}
        </aside>
      ) : null}

      <main
        style={
          isMobile
            ? {
                ...mainStyle,
                flex: 1,
                minHeight: 0,
                minWidth: 0,
              }
            : mainStyle
        }
      >
        {main}
        {/* 将抽屉层放入主视图内部，确保绝对定位时能精准对齐主视图宽度 */}
        {drawer}
      </main>

      {(!isMobile || physicalRightOpen) && physicalRightContent ? (
        <aside
          style={
            isMobile
              ? mobileSidebarStyle('right')
              : {
                  ...rightStyle,
                  overflow: physicalRightOpen ? "auto" : "hidden",
                  pointerEvents: physicalRightOpen ? "auto" : "none",
                }
          }
        >
          {physicalRightContent}
        </aside>
      ) : null}

      {!isMobile ? (
        <>
          <div ref={resizeShadeRef} className="mindfs-sidebar-resize-shade" aria-hidden="true" />
          <div ref={resizeGuideRef} className="mindfs-sidebar-resize-guide" aria-hidden="true" />
          <button
            type="button"
            className={`mindfs-sidebar-resize-rail mindfs-sidebar-resize-rail--left${physicalLeftOpen ? " is-open" : " is-closed"}`}
            onPointerDown={(event) => startResize("left", event)}
            onClick={() => {
              if (draggedRef.current) {
                draggedRef.current = false;
                return;
              }
              (physicalLeftOpen ? physicalLeftClose : physicalLeftOpenHandler)?.();
            }}
            aria-label={physicalLeftOpen ? t("sidebar.collapse", { label: physicalLeftLabel }) : t("sidebar.expand", { label: physicalLeftLabel })}
            title={physicalLeftOpen ? t("sidebar.collapse", { label: physicalLeftLabel }) : t("sidebar.expand", { label: physicalLeftLabel })}
            style={{
              left: physicalLeftOpen ? `${physicalLeftWidth - 6}px` : 0,
              cursor: physicalLeftOpen ? "w-resize" : "e-resize",
            }}
          />
          {physicalRightContent ? (
            <button
              type="button"
              className={`mindfs-sidebar-resize-rail mindfs-sidebar-resize-rail--right${physicalRightOpen ? " is-open" : " is-closed"}`}
              onPointerDown={(event) => startResize("right", event)}
              onClick={() => {
                if (draggedRef.current) {
                  draggedRef.current = false;
                  return;
                }
                (physicalRightOpen ? physicalRightClose : physicalRightOpenHandler)?.();
              }}
              aria-label={physicalRightOpen ? t("sidebar.collapse", { label: physicalRightLabel }) : t("sidebar.expand", { label: physicalRightLabel })}
              title={physicalRightOpen ? t("sidebar.collapse", { label: physicalRightLabel }) : t("sidebar.expand", { label: physicalRightLabel })}
              style={{
                right: physicalRightOpen ? `${physicalRightWidth - 6}px` : 0,
                cursor: physicalRightOpen ? "e-resize" : "w-resize",
              }}
            />
          ) : null}
        </>
      ) : null}

      <footer
        style={
          isMobile
            ? mobileFooterStyle
            : footerStyle
        }
      >
        {footer}
      </footer>
    </div>
  );
}
