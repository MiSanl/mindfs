import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AgentIcon } from "./AgentIcon";
import type { AgentStatus } from "../services/agents";

type AgentModelSelectorProps = {
  agent: string;
  agents: AgentStatus[];
  model?: string;
  mode?: string;
  effort?: string;
  fastService?: "" | "on" | "off";
  onAgentChange: (agent: string) => void;
  onModelChange: (model: string) => void;
  /** Preferred: commit agent+model together so effort/fast defaults use the new agent. */
  onAgentModelChange?: (agent: string, model: string) => void;
  onModeChange?: (mode?: string) => void;
  onEffortChange?: (effort?: string) => void;
  onFastServiceChange?: (fastService?: "" | "on" | "off") => void;
  onAgentRestart?: (agent: string) => void | Promise<void>;
  compact?: boolean;
  warnUnavailable?: boolean;
  menuPlacement?: "top" | "bottom";
  maxButtonWidth?: string;
};

function parseAgentErrorMessage(error?: string): string {
  const raw = String(error || "").trim();
  if (!raw) return "";
  try {
    const parsed = JSON.parse(raw) as { message?: unknown };
    return typeof parsed.message === "string" && parsed.message.trim()
      ? parsed.message.trim()
      : raw;
  } catch {
    return raw;
  }
}

function parseAgentErrorDetails(error?: string): string[] {
  const raw = String(error || "").trim();
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw) as { data?: unknown };
    if (parsed.data === undefined) return [];
    if (Array.isArray(parsed.data)) {
      return parsed.data.map((item) => String(item)).filter(Boolean);
    }
    if (parsed.data && typeof parsed.data === "object") {
      const authMethods = (parsed.data as { authMethods?: unknown }).authMethods;
      if (Array.isArray(authMethods)) {
        return (authMethods as Array<{ name?: unknown; description?: unknown }>)
          .map((item) => {
            const name = typeof item?.name === "string" ? item.name.trim() : "";
            const description =
              typeof item?.description === "string" ? item.description.trim() : "";
            return name && description ? `${name}: ${description}` : name || description;
          })
          .filter(Boolean);
      }
      return Object.entries(parsed.data as Record<string, unknown>).map(
        ([key, value]) => `${key}: ${typeof value === "string" ? value : JSON.stringify(value)}`,
      );
    }
    return [String(parsed.data)];
  } catch {
    return [];
  }
}

/**
 * Combined Agent + Model + run-settings control for the action bar.
 * One compact trigger; menu: agents | models | mode/effort/fast.
 */
export function AgentModelSelector({
  agent,
  agents,
  model = "",
  mode = "",
  effort = "",
  fastService = "",
  onAgentChange,
  onModelChange,
  onAgentModelChange,
  onModeChange,
  onEffortChange,
  onFastServiceChange,
  onAgentRestart,
  compact = false,
  warnUnavailable = false,
  menuPlacement = "top",
  maxButtonWidth = "min(42vw, 200px)",
}: AgentModelSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [errorAgent, setErrorAgent] = useState<string | null>(null);
  const [restartingAgent, setRestartingAgent] = useState<string | null>(null);
  const [hoverAgent, setHoverAgent] = useState<string>("");
  const dropdownRef = useRef<HTMLDivElement>(null);

  const selectedAgent = useMemo(
    () => agents.find((item) => item.name === agent) ?? null,
    [agents, agent],
  );
  const previewAgentName = hoverAgent || agent;
  const previewAgent = useMemo(
    () => agents.find((item) => item.name === previewAgentName) ?? selectedAgent,
    [agents, previewAgentName, selectedAgent],
  );
  const models = previewAgent?.models ?? [];
  const selectedModel = useMemo(() => {
    const fallback = selectedAgent?.default_model_id || selectedAgent?.current_model_id || "";
    const target = model || fallback;
    return (selectedAgent?.models ?? []).find((item) => item.id === target) ?? null;
  }, [selectedAgent, model]);
  const modelLabel = selectedModel?.name || selectedModel?.id || model || "模型";

  // Run settings are always for the *committed* agent+model (not hover preview).
  const modes = selectedAgent?.modes ?? [];
  const displayedMode = mode || selectedAgent?.current_mode_id || "";
  const currentMode = modes.find((item) => item.id === displayedMode);
  const modeLabel = currentMode?.name || currentMode?.id || displayedMode || "";
  const modelEfforts = selectedModel?.efforts ?? [];
  const efforts = modelEfforts.length > 0 ? modelEfforts : selectedAgent?.efforts ?? [];
  const supportsEffort = efforts.length > 0 && !!selectedModel?.supportEffort;
  const displayedEffort =
    effort || selectedModel?.default_effort || selectedAgent?.default_effort || "";
  const supportsFastService = !!selectedAgent?.supports_fast_service;
  const fastModeEnabled =
    (fastService || selectedAgent?.default_fast_service || "") === "on";
  const hasRunSettings =
    modes.length > 0 || supportsEffort || supportsFastService;

  const summaryParts = [modelLabel];
  if (modeLabel) summaryParts.push(modeLabel);
  if (supportsEffort && displayedEffort) summaryParts.push(displayedEffort);
  if (supportsFastService && fastModeEnabled) summaryParts.push("Fast");
  const summary = summaryParts.filter(Boolean).join(" · ");

  const errorAgentStatus = useMemo(
    () => agents.find((item) => item.name === errorAgent) ?? null,
    [agents, errorAgent],
  );

  useEffect(() => {
    if (!isOpen) return;
    const handlePointerOutside = (event: PointerEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
        setErrorAgent(null);
        setHoverAgent("");
      }
    };
    document.addEventListener("pointerdown", handlePointerOutside);
    return () => document.removeEventListener("pointerdown", handlePointerOutside);
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) {
      setHoverAgent("");
      setErrorAgent(null);
    }
  }, [isOpen]);

  const handleAgentSelect = useCallback(
    (nextAgent: string) => {
      setHoverAgent(nextAgent);
      setErrorAgent(null);
      const target = agents.find((item) => item.name === nextAgent);
      const catalog = target?.models ?? [];
      if (catalog.length === 0) {
        const fallback =
          target?.default_model_id || target?.current_model_id || model || "";
        if (onAgentModelChange) {
          onAgentModelChange(nextAgent, fallback);
        } else {
          if (nextAgent !== agent) onAgentChange(nextAgent);
          if (fallback) onModelChange(fallback);
        }
        // Keep menu open so user can still set mode/effort after switch when available.
        setHoverAgent("");
      }
    },
    [agent, agents, model, onAgentChange, onAgentModelChange, onModelChange],
  );

  const handleModelSelect = useCallback(
    (nextModel: string) => {
      const targetAgent = hoverAgent || agent;
      if (onAgentModelChange) {
        onAgentModelChange(targetAgent, nextModel);
      } else {
        if (targetAgent && targetAgent !== agent) {
          onAgentChange(targetAgent);
        }
        onModelChange(nextModel);
      }
      setErrorAgent(null);
      setHoverAgent("");
      // Keep open if run settings exist for the new agent so user can adjust effort.
    },
    [agent, hoverAgent, onAgentChange, onAgentModelChange, onModelChange],
  );

  const handleAgentRestart = useCallback(
    async (targetAgent: string) => {
      if (!onAgentRestart || restartingAgent) return;
      setRestartingAgent(targetAgent);
      try {
        await onAgentRestart(targetAgent);
      } finally {
        setRestartingAgent((current) => (current === targetAgent ? null : current));
      }
    },
    [onAgentRestart, restartingAgent],
  );

  return (
    <div ref={dropdownRef} style={{ position: "relative", minWidth: 0 }}>
      <style>{`
        @keyframes agent-refresh-spin {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }
      `}</style>
      <button
        type="button"
        onClick={() => {
          setIsOpen((previous) => !previous);
          setErrorAgent(null);
        }}
        title={
          warnUnavailable
            ? `当前会话的 Agent（${agent}）不可用`
            : `${agent || "Agent"} · ${summary}`
        }
        aria-label={`选择 Agent、模型与运行设置，当前为 ${agent || "未选择"} / ${summary}`}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: "5px",
          maxWidth: maxButtonWidth,
          height: compact ? "28px" : "32px",
          padding: compact ? "0 6px 0 5px" : "0 8px",
          border: "none",
          borderRadius: "10px",
          background: isOpen ? "rgba(59,130,246,0.08)" : "transparent",
          color: "var(--text-primary)",
          cursor: "pointer",
          outline: "none",
          position: "relative",
          minWidth: 0,
        }}
      >
        <AgentIcon agentName={agent} style={{ width: "16px", height: "16px", flexShrink: 0 }} />
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
        {warnUnavailable ? (
          <span
            style={{
              position: "absolute",
              top: "1px",
              right: "1px",
              minWidth: "11px",
              height: "11px",
              borderRadius: "50%",
              background: "#d97706",
              color: "#fff",
              fontSize: "9px",
              lineHeight: "11px",
              fontWeight: 700,
              textAlign: "center",
            }}
          >
            !
          </span>
        ) : null}
      </button>

      {isOpen ? (
        <div
          style={{
            position: "absolute",
            ...(menuPlacement === "bottom"
              ? { top: "calc(100% + 8px)" }
              : { bottom: "calc(100% + 8px)" }),
            right: 0,
            display: "flex",
            maxWidth: "calc(100vw - 16px)",
            maxHeight: "400px",
            padding: "0",
            border: "1px solid var(--menu-border)",
            borderRadius: "12px",
            background: "var(--menu-bg)",
            boxShadow: "0 8px 32px rgba(0,0,0,0.15)",
            zIndex: 1000,
            overflow: "hidden",
          }}
        >
          <div style={{ width: "min(36vw, 136px)", maxHeight: "400px", overflowY: "auto", padding: "8px 0" }}>
            <div style={sectionHeaderStyle}>Agent</div>
            {agents.map((item) => {
              const selected = item.name === agent;
              const preview = item.name === previewAgentName;
              const hasError = !item.available && !!item.error;
              return (
                <div
                  key={item.name}
                  onMouseEnter={() => setHoverAgent(item.name)}
                  style={{
                    display: "grid",
                    gridTemplateColumns: "20px minmax(0, 1fr) 20px",
                    alignItems: "center",
                    gap: "6px",
                    padding: "9px 12px",
                    background: preview
                      ? "rgba(59, 130, 246, 0.10)"
                      : selected
                        ? "rgba(59, 130, 246, 0.06)"
                        : "transparent",
                  }}
                >
                  <button
                    type="button"
                    onClick={() => handleAgentSelect(item.name)}
                    style={{ display: "contents", cursor: "pointer" }}
                  >
                    <AgentIcon agentName={item.name} style={{ width: "16px", height: "16px" }} />
                    <span
                      style={{
                        minWidth: 0,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        color: selected || preview ? "#3b82f6" : "var(--text-primary)",
                        fontSize: "13px",
                        fontWeight: selected ? 600 : 400,
                        textAlign: "left",
                      }}
                    >
                      {item.name}
                    </span>
                  </button>
                  {hasError ? (
                    <button
                      type="button"
                      aria-label={`查看 ${item.name} 错误信息`}
                      onClick={() => setErrorAgent((current) => (current === item.name ? null : item.name))}
                      style={{
                        width: "20px",
                        height: "20px",
                        padding: 0,
                        border: "1px solid var(--menu-border)",
                        borderRadius: "50%",
                        background: errorAgent === item.name ? "rgba(217,119,6,0.12)" : "transparent",
                        color: "#d97706",
                        cursor: "pointer",
                        fontWeight: 700,
                      }}
                    >
                      ?
                    </button>
                  ) : (
                    <span />
                  )}
                </div>
              );
            })}
          </div>

          <div
            style={{
              width: "min(48vw, 200px)",
              maxHeight: "400px",
              overflowY: "auto",
              padding: "8px 0",
              borderLeft: "1px solid var(--menu-divider)",
              boxSizing: "border-box",
            }}
          >
            <div style={sectionHeaderStyle}>
              {previewAgent?.name ? `Model · ${previewAgent.name}` : "Model"}
            </div>
            {models.length === 0 ? (
              <div style={{ padding: "10px 12px", fontSize: "12px", color: "var(--text-secondary)" }}>
                {previewAgent?.available === false
                  ? "Agent 不可用"
                  : previewAgentName && previewAgentName !== agent
                    ? "暂无模型列表 · 点击左侧 Agent 名称即可切换"
                    : "暂无模型"}
              </div>
            ) : (
              models.map((item, index) => {
                const isSelected =
                  item.id === selectedModel?.id && previewAgentName === agent;
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => handleModelSelect(item.id)}
                    title={item.description || item.id}
                    style={{
                      display: "flex",
                      flexDirection: "column",
                      alignItems: "flex-start",
                      gap: "2px",
                      width: "100%",
                      minWidth: 0,
                      padding: "10px 12px",
                      border: "none",
                      borderTop: index > 0 ? "1px solid var(--menu-divider)" : "none",
                      background: isSelected ? "rgba(59,130,246,0.08)" : "transparent",
                      color: isSelected ? "#3b82f6" : "var(--text-primary)",
                      textAlign: "left",
                      cursor: "pointer",
                      opacity: item.hidden ? 0.66 : 1,
                    }}
                  >
                    <span style={{ fontSize: "13px", fontWeight: 600 }}>{item.name || item.id}</span>
                    {item.description ? (
                      <span style={descriptionStyle}>{item.description}</span>
                    ) : null}
                  </button>
                );
              })
            )}
          </div>

          {hasRunSettings ? (
            <div
              style={{
                width: "min(40vw, 168px)",
                maxHeight: "400px",
                overflowY: "auto",
                padding: "8px 0",
                borderLeft: "1px solid var(--menu-divider)",
                boxSizing: "border-box",
              }}
            >
              <div style={sectionHeaderStyle}>
                {selectedAgent?.name ? `设置 · ${selectedAgent.name}` : "设置"}
              </div>
              {modes.length > 0 ? (
                <SettingsGroup title="Mode">
                  {modes.map((item) => (
                    <SettingsOption
                      key={item.id}
                      selected={item.id === displayedMode}
                      title={item.description || item.id}
                      onClick={() => {
                        onModeChange?.(item.id);
                        setIsOpen(false);
                      }}
                    >
                      <span style={{ fontSize: "13px", fontWeight: 600 }}>{item.name || item.id}</span>
                    </SettingsOption>
                  ))}
                </SettingsGroup>
              ) : null}
              {supportsEffort ? (
                <SettingsGroup title="Effort" withTopBorder={modes.length > 0}>
                  {efforts.map((item) => (
                    <SettingsOption
                      key={item}
                      selected={item.toLowerCase() === String(displayedEffort).toLowerCase()}
                      onClick={() => {
                        onEffortChange?.(item);
                        setIsOpen(false);
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
                    </SettingsOption>
                  ))}
                </SettingsGroup>
              ) : null}
              {supportsFastService ? (
                <SettingsGroup
                  title="Fast"
                  withTopBorder={modes.length > 0 || supportsEffort}
                >
                  <SettingsOption
                    selected={fastModeEnabled}
                    onClick={() => {
                      onFastServiceChange?.("on");
                      setIsOpen(false);
                    }}
                  >
                    <span style={{ fontSize: "13px", fontWeight: 600 }}>On</span>
                  </SettingsOption>
                  <SettingsOption
                    selected={!fastModeEnabled}
                    onClick={() => {
                      onFastServiceChange?.("off");
                      setIsOpen(false);
                    }}
                  >
                    <span style={{ fontSize: "13px", fontWeight: 600 }}>Off</span>
                  </SettingsOption>
                </SettingsGroup>
              ) : null}
            </div>
          ) : null}

          {errorAgentStatus && parseAgentErrorMessage(errorAgentStatus.error) ? (
            <div
              style={{
                width: "min(44vw, 220px)",
                padding: "12px",
                borderLeft: "1px solid var(--menu-divider)",
                overflowY: "auto",
                boxSizing: "border-box",
              }}
            >
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "8px" }}>
                <span style={{ fontSize: "11px", fontWeight: 700, color: "#d97706" }}>错误信息</span>
                {onAgentRestart ? (
                  <button
                    type="button"
                    title="重启 Agent"
                    disabled={restartingAgent === errorAgentStatus.name}
                    onClick={() => void handleAgentRestart(errorAgentStatus.name)}
                    style={{ border: "none", background: "transparent", color: "#d97706", cursor: "pointer" }}
                  >
                    <span
                      aria-hidden="true"
                      style={{
                        display: "inline-block",
                        animation:
                          restartingAgent === errorAgentStatus.name
                            ? "agent-refresh-spin 0.9s linear infinite"
                            : undefined,
                      }}
                    >
                      ↻
                    </span>
                  </button>
                ) : null}
              </div>
              <div
                style={{
                  marginTop: "8px",
                  fontSize: "12px",
                  lineHeight: 1.5,
                  color: "var(--text-primary)",
                  overflowWrap: "anywhere",
                }}
              >
                {parseAgentErrorMessage(errorAgentStatus.error)}
              </div>
              {parseAgentErrorDetails(errorAgentStatus.error).map((detail) => (
                <div
                  key={detail}
                  style={{
                    marginTop: "8px",
                    fontSize: "11px",
                    lineHeight: 1.5,
                    color: "var(--text-secondary)",
                    overflowWrap: "anywhere",
                  }}
                >
                  {detail}
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

const sectionHeaderStyle: React.CSSProperties = {
  padding: "6px 12px",
  fontSize: "11px",
  fontWeight: 700,
  color: "var(--text-secondary)",
  textTransform: "uppercase",
};

const descriptionStyle: React.CSSProperties = {
  fontSize: "11px",
  color: "var(--text-secondary)",
  whiteSpace: "normal",
  overflowWrap: "anywhere",
};

function SettingsGroup({
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
          padding: "4px 12px",
          fontSize: "10px",
          fontWeight: 700,
          color: "var(--text-secondary)",
          textTransform: "uppercase",
          opacity: 0.85,
        }}
      >
        {title}
      </div>
      {children}
    </div>
  );
}

function SettingsOption({
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
        padding: "8px 12px",
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
      <path d="m2.5 4 3.5 4 3.5-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
