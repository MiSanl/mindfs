import React, { useEffect, useMemo, useRef, useState } from "react";
import type { AgentStatus } from "../services/agents";

type AgentSettingsSelectorProps = {
  agent?: AgentStatus | null;
  model?: string;
  mode?: string;
  effort?: string;
  fastService?: "" | "on" | "off";
  onModeChange?: (mode?: string) => void;
  onEffortChange?: (effort?: string) => void;
  onFastServiceChange?: (fastService?: "" | "on" | "off") => void;
  compact?: boolean;
  menuPlacement?: "top" | "bottom";
  maxButtonWidth?: string;
};

/**
 * Single compact control for agent run settings: mode / effort / fast.
 * Replaces the three separate ActionBar buttons.
 */
export function AgentSettingsSelector({
  agent,
  model = "",
  mode = "",
  effort = "",
  fastService = "",
  onModeChange,
  onEffortChange,
  onFastServiceChange,
  compact = false,
  menuPlacement = "top",
  maxButtonWidth = "min(36vw, 148px)",
}: AgentSettingsSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const modes = agent?.modes ?? [];
  const displayedMode = mode || agent?.current_mode_id || "";
  const currentMode = modes.find((item) => item.id === displayedMode);
  const modeLabel = currentMode?.name || currentMode?.id || displayedMode || "";

  const models = agent?.models ?? [];
  const selectedModel = useMemo(() => {
    const fallback = agent?.default_model_id || agent?.current_model_id || "";
    const target = model || fallback;
    return models.find((item) => item.id === target) ?? null;
  }, [agent, model, models]);
  const modelEfforts = selectedModel?.efforts ?? [];
  const efforts = modelEfforts.length > 0 ? modelEfforts : agent?.efforts ?? [];
  const supportsEffort = efforts.length > 0 && !!selectedModel?.supportEffort;
  const displayedEffort =
    effort || selectedModel?.default_effort || agent?.default_effort || "";

  const supportsFastService = !!agent?.supports_fast_service;
  const fastModeEnabled = (fastService || agent?.default_fast_service || "") === "on";

  const hasAny =
    modes.length > 0 || supportsEffort || supportsFastService;

  const summaryParts: string[] = [];
  if (modeLabel) summaryParts.push(modeLabel);
  if (supportsEffort && displayedEffort) summaryParts.push(displayedEffort);
  if (supportsFastService && fastModeEnabled) summaryParts.push("Fast");
  const summary =
    summaryParts.length > 0
      ? summaryParts.join(" · ")
      : hasAny
        ? "设置"
        : "";

  useEffect(() => {
    if (!isOpen) return;
    const handlePointerOutside = (event: PointerEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener("pointerdown", handlePointerOutside);
    return () => document.removeEventListener("pointerdown", handlePointerOutside);
  }, [isOpen]);

  useEffect(() => setIsOpen(false), [agent?.name, model]);

  if (!hasAny) return null;

  return (
    <div ref={dropdownRef} style={{ position: "relative", minWidth: 0 }}>
      <button
        type="button"
        onClick={() => setIsOpen((previous) => !previous)}
        title={summary || "Agent 运行设置"}
        aria-label={`Agent 运行设置，当前为 ${summary || "默认"}`}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: "4px",
          maxWidth: maxButtonWidth,
          height: compact ? "28px" : "32px",
          padding: compact ? "0 6px" : "0 8px",
          border: "none",
          borderRadius: "10px",
          background: isOpen ? "rgba(59,130,246,0.08)" : "transparent",
          color: "var(--text-primary)",
          cursor: "pointer",
          outline: "none",
          minWidth: 0,
        }}
      >
        <SettingsGearIcon />
        <span
          style={{
            minWidth: 0,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            fontSize: "12px",
            fontWeight: 600,
            textTransform: "capitalize",
          }}
        >
          {summary}
        </span>
        <SelectorChevron expanded={isOpen} />
      </button>

      {isOpen ? (
        <div
          style={{
            position: "absolute",
            ...(menuPlacement === "bottom"
              ? { top: "calc(100% + 8px)" }
              : { bottom: "calc(100% + 8px)" }),
            right: 0,
            width: "min(280px, calc(100vw - 16px))",
            maxHeight: "380px",
            overflowY: "auto",
            padding: "8px 0",
            border: "1px solid var(--menu-border)",
            borderRadius: "12px",
            background: "var(--menu-bg)",
            boxShadow: "0 8px 32px rgba(0,0,0,0.15)",
            zIndex: 1000,
          }}
        >
          {modes.length > 0 ? (
            <Section title="Mode">
              {modes.map((item) => (
                <OptionButton
                  key={item.id}
                  selected={item.id === displayedMode}
                  title={item.description || item.id}
                  onClick={() => {
                    onModeChange?.(item.id);
                  }}
                >
                  <span style={{ fontSize: "13px", fontWeight: 600 }}>{item.name || item.id}</span>
                  {item.description ? (
                    <span style={descriptionStyle}>{item.description}</span>
                  ) : null}
                </OptionButton>
              ))}
            </Section>
          ) : null}

          {supportsEffort ? (
            <Section title="Effort" withTopBorder={modes.length > 0}>
              {efforts.map((item) => (
                <OptionButton
                  key={item}
                  selected={item.toLowerCase() === String(displayedEffort).toLowerCase()}
                  onClick={() => {
                    onEffortChange?.(item);
                  }}
                >
                  <span
                    style={{
                      fontSize: "13px",
                      fontWeight: 600,
                      textTransform: "capitalize",
                    }}
                  >
                    {item}
                  </span>
                </OptionButton>
              ))}
            </Section>
          ) : null}

          {supportsFastService ? (
            <Section title="Fast" withTopBorder={modes.length > 0 || supportsEffort}>
              <OptionButton
                selected={fastModeEnabled}
                onClick={() => onFastServiceChange?.("on")}
              >
                <span style={{ fontSize: "13px", fontWeight: 600 }}>On</span>
              </OptionButton>
              <OptionButton
                selected={!fastModeEnabled}
                onClick={() => onFastServiceChange?.("off")}
              >
                <span style={{ fontSize: "13px", fontWeight: 600 }}>Off</span>
              </OptionButton>
            </Section>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function Section({
  title,
  withTopBorder = false,
  children,
}: {
  title: string;
  withTopBorder?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      style={{
        borderTop: withTopBorder ? "1px solid var(--menu-divider)" : "none",
        paddingTop: withTopBorder ? 4 : 0,
      }}
    >
      <div
        style={{
          padding: "6px 12px",
          fontSize: "11px",
          fontWeight: 700,
          color: "var(--text-secondary)",
          textTransform: "uppercase",
        }}
      >
        {title}
      </div>
      {children}
    </div>
  );
}

function OptionButton({
  selected,
  title,
  onClick,
  children,
}: {
  selected: boolean;
  title?: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: "2px",
        width: "100%",
        minWidth: 0,
        padding: "10px 12px",
        border: "none",
        background: selected ? "rgba(59,130,246,0.08)" : "transparent",
        color: selected ? "#3b82f6" : "var(--text-primary)",
        textAlign: "left",
        cursor: "pointer",
      }}
    >
      {children}
    </button>
  );
}

const descriptionStyle: React.CSSProperties = {
  fontSize: "11px",
  color: "var(--text-secondary)",
  whiteSpace: "normal",
  overflowWrap: "anywhere",
};

function SelectorChevron({ expanded }: { expanded: boolean }) {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
      style={{
        flexShrink: 0,
        color: expanded ? "#3b82f6" : "var(--text-secondary)",
        transform: expanded ? "rotate(180deg)" : "none",
        transition: "transform 0.16s ease",
      }}
    >
      <path
        d="m2.5 4 3.5 4 3.5-4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function SettingsGearIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      style={{ flexShrink: 0, opacity: 0.85 }}
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}
