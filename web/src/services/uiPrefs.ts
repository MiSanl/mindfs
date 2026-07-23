import { getStoredString, setStoredString } from "./storage";

/** UI preferences that should survive refresh (localStorage). */
export const UI_PREFS_KEYS = {
  showTurnRequest: "mindfs-show-turn-request",
  showHiddenFiles: "mindfs-show-hidden-files",
} as const;

export type UIPrefs = {
  /** Show actual request provider/model chips under user messages. Default true. */
  showTurnRequest: boolean;
  /** Show hidden files in project tree. Default false. */
  showHiddenFiles: boolean;
};

export const UI_PREFS_CHANGE_EVENT = "mindfs:ui-prefs-changed";

function readFlag(key: string, defaultValue: boolean): boolean {
  const raw = getStoredString(key);
  if (raw == null || raw === "") {
    return defaultValue;
  }
  if (raw === "1" || raw === "true") return true;
  if (raw === "0" || raw === "false") return false;
  return defaultValue;
}

function writeFlag(key: string, value: boolean): void {
  setStoredString(key, value ? "1" : "0");
}

export function getUIPrefs(): UIPrefs {
  return {
    showTurnRequest: readFlag(UI_PREFS_KEYS.showTurnRequest, true),
    showHiddenFiles: readFlag(UI_PREFS_KEYS.showHiddenFiles, false),
  };
}

export function getShowTurnRequest(): boolean {
  return getUIPrefs().showTurnRequest;
}

export function setShowTurnRequest(value: boolean): void {
  writeFlag(UI_PREFS_KEYS.showTurnRequest, value);
  emitUIPrefsChange({ showTurnRequest: value });
}

export function getShowHiddenFiles(): boolean {
  return getUIPrefs().showHiddenFiles;
}

export function setShowHiddenFiles(value: boolean): void {
  writeFlag(UI_PREFS_KEYS.showHiddenFiles, value);
  emitUIPrefsChange({ showHiddenFiles: value });
}

function emitUIPrefsChange(partial: Partial<UIPrefs>): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent<Partial<UIPrefs>>(UI_PREFS_CHANGE_EVENT, { detail: partial }),
  );
}
