import { useId, type ComponentType } from "react";

export type AppID = "morph" | "loopgate" | "workspace" | "todo" | "notes" | "journal" | "paint" | "settings";

/** App icons on the shell dock — same for both modes. */
export const DOCK_LAUNCHER_APPS_CLASSIC: AppID[] = ["morph", "loopgate", "workspace", "todo", "notes", "journal", "paint", "settings"];

/** Workstation: Morph bubble lives on the desktop; dock includes Morph for the full window. */
export const DOCK_LAUNCHER_APPS_WORKSTATION: AppID[] = ["morph", "loopgate", "workspace", "todo", "notes", "journal", "paint", "settings"];

export interface WinState {
  id: AppID;
  title: string;
  x: number;
  y: number;
  w: number;
  h: number;
  z: number;
  collapsed: boolean;
}

export interface ApprovalInfo {
  approvalRequestID: string;
  capability: string;
}

export interface PreviewState {
  path: string;
  content: string;
  truncated: boolean;
  loading: boolean;
  error: string;
}

export interface IconPos {
  x: number;
  y: number;
}

export interface DesktopFile {
  id: string;
  name: string;
  sandboxPath: string;
  x: number;
  y: number;
}

export interface PendingDrop {
  paths: string[];
  x: number;
  y: number;
}

export interface WallpaperTheme {
  chrome: string;
  chromeHi: string;
  chromeLo: string;
  chromeMid: string;
  menubar: string;
  titleHi: string;
  titleLo: string;
  titleStripe: string;
  titleOff: string;
  winBody: string;
  winBorder: string;
  taskbar: string;
  panel: string;
  card: string;
  divider: string;
  text1: string;
  text2: string;
  text3: string;
  textInv: string;
}

export interface Wallpaper {
  id: string;
  name: string;
  mood: string;
  bg: string;
  bgLight: string;
  glow: string;
  vignette: string;
  accent: string;
  accentSoft: string;
  theme: WallpaperTheme;
}

export interface Toast {
  id: number;
  title: string;
  message: string;
  variant: "info" | "success" | "warning";
}

export interface SecurityAlert {
  id: number;
  type: string;
  message: string;
  ts: string;
}

export type ContextMenuData =
  | { kind: "thread"; threadID: string }
  | { kind: "desktop" }
  | { kind: "icon"; iconID: string }
  | { kind: "file"; fileID: string };

export interface ContextMenuState {
  x: number;
  y: number;
  data: ContextMenuData;
}

export const WIN_DEFAULTS: Record<AppID, { title: string; x: number; y: number; w: number; h: number }> = {
  morph: { title: "Morph", x: 40, y: 20, w: 820, h: 620 },
  loopgate: { title: "Loopgate", x: 180, y: 50, w: 560, h: 500 },
  workspace: { title: "Workspace", x: 100, y: 30, w: 860, h: 620 },
  todo: { title: "Tasks", x: 520, y: 86, w: 540, h: 540 },
  notes: { title: "Notes", x: 180, y: 54, w: 760, h: 560 },
  journal: { title: "Journal", x: 140, y: 44, w: 760, h: 560 },
  paint: { title: "Paint", x: 170, y: 52, w: 900, h: 640 },
  settings: { title: "Settings", x: 200, y: 60, w: 480, h: 560 },
};

export const WINDOW_MIN_SIZES: Record<AppID, { w: number; h: number }> = {
  morph: { w: 620, h: 460 },
  loopgate: { w: 420, h: 340 },
  workspace: { w: 520, h: 360 },
  todo: { w: 440, h: 420 },
  notes: { w: 520, h: 380 },
  journal: { w: 540, h: 380 },
  paint: { w: 620, h: 420 },
  settings: { w: 420, h: 380 },
};

const WINDOW_VIEWPORT_RATIOS: Record<AppID, { w: number; h: number }> = {
  morph: { w: 0.64, h: 0.78 },
  loopgate: { w: 0.46, h: 0.62 },
  workspace: { w: 0.72, h: 0.8 },
  todo: { w: 0.5, h: 0.72 },
  notes: { w: 0.62, h: 0.76 },
  journal: { w: 0.6, h: 0.76 },
  paint: { w: 0.76, h: 0.82 },
  settings: { w: 0.42, h: 0.76 },
};

const WINDOW_VIEWPORT_PADDING = {
  left: 24,
  right: 24,
  top: 40,
  bottom: 86,
};

interface WindowViewport {
  width: number;
  height: number;
}

type WindowFrameOverride = Partial<Pick<WinState, "x" | "y" | "w" | "h">>;

function currentWindowViewport(): WindowViewport {
  if (typeof window === "undefined") {
    return { width: 1280, height: 800 };
  }
  return { width: window.innerWidth, height: window.innerHeight };
}

function clamp(value: number, minValue: number, maxValue: number): number {
  if (maxValue < minValue) {
    return minValue;
  }
  return Math.min(Math.max(value, minValue), maxValue);
}

function fitWindowDimension(preferredValue: number, minimumValue: number, availableValue: number): number {
  const safeAvailableValue = Math.max(availableValue, 260);
  if (safeAvailableValue <= minimumValue) {
    return safeAvailableValue;
  }
  return Math.min(Math.max(preferredValue, minimumValue), safeAvailableValue);
}

export function resolveWindowFrame(id: AppID, override: WindowFrameOverride = {}, viewport = currentWindowViewport()) {
  const defaultFrame = WIN_DEFAULTS[id];
  const minimumFrame = WINDOW_MIN_SIZES[id];
  const viewportRatio = WINDOW_VIEWPORT_RATIOS[id];
  const availableWidth = Math.max(260, viewport.width - WINDOW_VIEWPORT_PADDING.left - WINDOW_VIEWPORT_PADDING.right);
  const availableHeight = Math.max(240, viewport.height - WINDOW_VIEWPORT_PADDING.top - WINDOW_VIEWPORT_PADDING.bottom);

  const preferredWidth = Math.max(defaultFrame.w, Math.round(availableWidth * viewportRatio.w));
  const preferredHeight = Math.max(defaultFrame.h, Math.round(availableHeight * viewportRatio.h));

  const width = fitWindowDimension(override.w ?? preferredWidth, minimumFrame.w, availableWidth);
  const height = fitWindowDimension(override.h ?? preferredHeight, minimumFrame.h, availableHeight);

  const minX = WINDOW_VIEWPORT_PADDING.left;
  const maxX = viewport.width - WINDOW_VIEWPORT_PADDING.right - width;
  const minY = WINDOW_VIEWPORT_PADDING.top;
  const maxY = viewport.height - WINDOW_VIEWPORT_PADDING.bottom - height;

  return {
    title: defaultFrame.title,
    x: clamp(override.x ?? defaultFrame.x, minX, maxX),
    y: clamp(override.y ?? defaultFrame.y, minY, maxY),
    w: width,
    h: height,
  };
}

export const BOOT_LINES = [
  "HAVEN OS v1.0",
  "Initializing Loopgate kernel...",
  "Loading workspace...",
  "Starting Morph runtime...",
  "",
  "System ready.",
];

export const WORKSPACE_ICONS: Record<string, string> = {
  projects: "\u{1F4C1}",
  imports: "\u{1F4E5}",
  shared: "\u{1F91D}",
  artifacts: "\u{1F4E6}",
  research: "\u{1F50D}",
  agents: "\u{1F916}",
};

// Light themes — chrome complements the warm/cool desktop rather than matching it.
// Dark themes — inverted text, dark chrome with complementary warm/cool undertones.

const THEME_CLASSIC_MAC: WallpaperTheme = { // Very calm beige/paper
  chrome: "#E8E4DC", chromeHi: "#F5F2EB", chromeLo: "#888480", chromeMid: "#A09C94",
  menubar: "#F0EDE6", titleHi: "#E8E4DC", titleLo: "#C8C4BC", titleStripe: "#A09C94", titleOff: "#D8D4CC",
  winBody: "#FFFFFF", winBorder: "#888480", taskbar: "#D8D4CC",
  panel: "#F2EFE9", card: "#FAF8F4", divider: "#D4D0C8",
  text1: "#1C1B19", text2: "#6B675F", text3: "#A09C94", textInv: "#FFFFFF",
};

const THEME_COOL_STONE: WallpaperTheme = { // For warm wallpapers — cool gray chrome grounds the warmth
  chrome: "#E0E2E5", chromeHi: "#EDEEF0", chromeLo: "#82858A", chromeMid: "#989BA1",
  menubar: "#EAECEE", titleHi: "#E0E2E5", titleLo: "#C2C5CA", titleStripe: "#989BA1", titleOff: "#D2D4D8",
  winBody: "#FAFBFC", winBorder: "#82858A", taskbar: "#D2D5DA",
  panel: "#EEEFF1", card: "#F8F9FA", divider: "#CED0D5",
  text1: "#1A1C1E", text2: "#5E6268", text3: "#989BA1", textInv: "#FFFFFF",
};

const THEME_COOL_SAGE: WallpaperTheme = { // For clay/terracotta — sage-green chrome softens the red-orange
  chrome: "#DEE3DF", chromeHi: "#ECF0EC", chromeLo: "#7F8883", chromeMid: "#96A09A",
  menubar: "#E8ECE8", titleHi: "#DEE3DF", titleLo: "#BEC6C0", titleStripe: "#96A09A", titleOff: "#D0D6D0",
  winBody: "#FAFCFA", winBorder: "#7F8883", taskbar: "#D0D6D0",
  panel: "#ECEFEC", card: "#F8FAF8", divider: "#CAD0CC",
  text1: "#1B1D1B", text2: "#5F665F", text3: "#96A09A", textInv: "#FFFFFF",
};

const THEME_NEUTRAL: WallpaperTheme = { // For neutral wallpapers — clean true-gray chrome
  chrome: "#E6E6E4", chromeHi: "#F2F2F0", chromeLo: "#868684", chromeMid: "#9E9E9C",
  menubar: "#EEEEEC", titleHi: "#E6E6E4", titleLo: "#C6C6C4", titleStripe: "#9E9E9C", titleOff: "#D6D6D4",
  winBody: "#FEFEFE", winBorder: "#868684", taskbar: "#D6D6D4",
  panel: "#F0F0EE", card: "#FAFAF8", divider: "#D2D2D0",
  text1: "#1C1C1A", text2: "#686866", text3: "#9E9E9C", textInv: "#FFFFFF",
};

const THEME_COOL_LAVENDER: WallpaperTheme = { // For sunwash/solstice — lavender-gray chrome cools the amber
  chrome: "#E2E0E6", chromeHi: "#EEEDF2", chromeLo: "#84828C", chromeMid: "#9A98A4",
  menubar: "#ECEAF0", titleHi: "#E2E0E6", titleLo: "#C4C2CC", titleStripe: "#9A98A4", titleOff: "#D4D2DA",
  winBody: "#FBFAFC", winBorder: "#84828C", taskbar: "#D4D2DA",
  panel: "#EEECF2", card: "#FAF8FC", divider: "#D0CED6",
  text1: "#1C1A1E", text2: "#64626C", text3: "#9A98A4", textInv: "#FFFFFF",
};

const THEME_WARM_SAND: WallpaperTheme = { // For cool wallpapers — warm beige chrome adds warmth
  chrome: "#E8E4DC", chromeHi: "#F5F2EB", chromeLo: "#888480", chromeMid: "#A09C94",
  menubar: "#F0EDE6", titleHi: "#E8E4DC", titleLo: "#C8C4BC", titleStripe: "#A09C94", titleOff: "#D8D4CC",
  winBody: "#FFFEFA", winBorder: "#888480", taskbar: "#D8D4CC",
  panel: "#F2EFE9", card: "#FAF8F4", divider: "#D4D0C8",
  text1: "#1C1B19", text2: "#6B675F", text3: "#A09C94", textInv: "#FFFFFF",
};

const THEME_WARM_IVORY: WallpaperTheme = { // For slate/blue — creamy ivory contrasts cool blue
  chrome: "#EAE6DE", chromeHi: "#F6F2EA", chromeLo: "#8A8680", chromeMid: "#A29E96",
  menubar: "#F2EEE6", titleHi: "#EAE6DE", titleLo: "#CAC6BE", titleStripe: "#A29E96", titleOff: "#DAD6CE",
  winBody: "#FFFDF8", winBorder: "#8A8680", taskbar: "#DAD6CE",
  panel: "#F4F0EA", card: "#FCF9F4", divider: "#D6D2CA",
  text1: "#1E1C18", text2: "#6D6960", text3: "#A29E96", textInv: "#FFFFFF",
};

const THEME_WARM_ROSE: WallpaperTheme = { // For lavender — blush-gray softens the purple
  chrome: "#E8E2E4", chromeHi: "#F4EEF0", chromeLo: "#8A8486", chromeMid: "#A29C9E",
  menubar: "#F0EAEC", titleHi: "#E8E2E4", titleLo: "#C8C2C4", titleStripe: "#A29C9E", titleOff: "#D8D2D4",
  winBody: "#FEFCFC", winBorder: "#8A8486", taskbar: "#D8D2D4",
  panel: "#F2ECEE", card: "#FAF6F8", divider: "#D4CED0",
  text1: "#1E1B1C", text2: "#6A6466", text3: "#A29C9E", textInv: "#FFFFFF",
};

const THEME_DARK_WARM: WallpaperTheme = { // Phosphor & Cocoa
  chrome: "#2A2421", chromeHi: "#38312E", chromeLo: "#4A423E", chromeMid: "#3C3532",
  menubar: "#241F1D", titleHi: "#38312E", titleLo: "#1E1A18", titleStripe: "#4A423E", titleOff: "#2C2623",
  winBody: "#1C1816", winBorder: "#4A423E", taskbar: "#241F1D",
  panel: "#221D1B", card: "#1A1614", divider: "#3C3532",
  text1: "#FFB53E", text2: "#B0A898", text3: "#706860", textInv: "#1C1B19",
};

const THEME_DARK_COOL: WallpaperTheme = { // Phosphor Green & Slate
  chrome: "#222624", chromeHi: "#2E3431", chromeLo: "#424A45", chromeMid: "#363C38",
  menubar: "#1D201E", titleHi: "#2E3431", titleLo: "#161817", titleStripe: "#424A45", titleOff: "#242826",
  winBody: "#161817", winBorder: "#424A45", taskbar: "#1D201E",
  panel: "#1A1D1C", card: "#131514", divider: "#363C38",
  text1: "#4E8C5E", text2: "#A0A8B4", text3: "#647080", textInv: "#1C1B19",
};

const THEME_DARK_SAGE: WallpaperTheme = { // For rosewood — cool sage-green dark
  chrome: "#2A3028", chromeHi: "#363E34", chromeLo: "#505A4E", chromeMid: "#404A3E",
  menubar: "#2E362C", titleHi: "#363E34", titleLo: "#222A20", titleStripe: "#424C40", titleOff: "#303828",
  winBody: "#1E261C", winBorder: "#4A544A", taskbar: "#283024",
  panel: "#262E22", card: "#20281E", divider: "#383E34",
  text1: "#ECF0EA", text2: "#A4ACA0", text3: "#667060", textInv: "#1C1B19",
};

const THEME_DARK_AMBER: WallpaperTheme = { // For aurora/moonpool — warm amber-brown dark
  chrome: "#342C22", chromeHi: "#423A30", chromeLo: "#5E5644", chromeMid: "#4C4438",
  menubar: "#3A3026", titleHi: "#423A30", titleLo: "#2C2418", titleStripe: "#4E4638", titleOff: "#383022",
  winBody: "#282018", winBorder: "#545040", taskbar: "#302822",
  panel: "#2E2820", card: "#26201A", divider: "#403A2E",
  text1: "#F0ECE4", text2: "#B4AC9A", text3: "#746A58", textInv: "#1C1B19",
};

const THEME_DARK_OLIVE: WallpaperTheme = { // For orchid — warm olive dark complements purple
  chrome: "#302E24", chromeHi: "#3E3C32", chromeLo: "#58564C", chromeMid: "#484640",
  menubar: "#343228", titleHi: "#3E3C32", titleLo: "#28261C", titleStripe: "#4A4840", titleOff: "#343028",
  winBody: "#222018", winBorder: "#504E44", taskbar: "#2C2A22",
  panel: "#2A2820", card: "#24221A", divider: "#3C3A30",
  text1: "#EEEEE6", text2: "#B0AE9E", text3: "#706E60", textInv: "#1C1B19",
};

export const WALLPAPERS: Wallpaper[] = [
  { id: "system_desk", name: "System Desk", mood: "A clean beige desk, 1984.", bg: "#E8E4DC", bgLight: "#F5F2EB", glow: "rgba(255,255,255,0.2)", vignette: "rgba(0,0,0,0.04)", accent: "rgba(200,190,170,0.1)", accentSoft: "rgba(255,255,255,0.4)", theme: THEME_CLASSIC_MAC },
  { id: "orchid", name: "Orchid Hour", mood: "A saturated lilac room with a soft pink afterglow.", bg: "#735D8E", bgLight: "#C8B3DB", glow: "rgba(240,215,255,0.32)", vignette: "rgba(56,36,80,0.28)", accent: "rgba(205,113,176,0.36)", accentSoft: "rgba(248,229,255,0.28)", theme: THEME_DARK_OLIVE },
  { id: "midnight", name: "Midnight Harbor", mood: "Inky water, city light, and a quieter room.", bg: "#334154", bgLight: "#53647A", glow: "rgba(145,180,230,0.26)", vignette: "rgba(6,11,20,0.42)", accent: "rgba(66,123,176,0.36)", accentSoft: "rgba(151,194,228,0.22)", theme: THEME_DARK_WARM },
];

export const DEFAULT_WALLPAPER = WALLPAPERS[0];

export function applyWallpaperTheme(wallpaper: Wallpaper): void {
  const root = document.documentElement;
  const t = wallpaper.theme;
  root.style.setProperty("--chrome", t.chrome);
  root.style.setProperty("--chrome-hi", t.chromeHi);
  root.style.setProperty("--chrome-lo", t.chromeLo);
  root.style.setProperty("--chrome-mid", t.chromeMid);
  root.style.setProperty("--menubar", t.menubar);
  root.style.setProperty("--title-hi", t.titleHi);
  root.style.setProperty("--title-lo", t.titleLo);
  root.style.setProperty("--title-stripe", t.titleStripe);
  root.style.setProperty("--title-off", t.titleOff);
  root.style.setProperty("--win-body", t.winBody);
  root.style.setProperty("--win-border", t.winBorder);
  root.style.setProperty("--taskbar", t.taskbar);
  root.style.setProperty("--panel", t.panel);
  root.style.setProperty("--card", t.card);
  root.style.setProperty("--divider", t.divider);
  root.style.setProperty("--text-1", t.text1);
  root.style.setProperty("--text-2", t.text2);
  root.style.setProperty("--text-3", t.text3);
  root.style.setProperty("--text-inv", t.textInv);
}

export function wallpaperBackgroundImage(wallpaper: Wallpaper): string {
  return [
    "linear-gradient(180deg, rgba(255,255,255,0.16) 0%, rgba(255,255,255,0) 24%)",
    `radial-gradient(circle at 14% 18%, ${wallpaper.accentSoft} 0%, transparent 20%)`,
    `radial-gradient(circle at 84% 14%, ${wallpaper.glow} 0%, transparent 28%)`,
    "radial-gradient(circle at 26% 78%, rgba(255,255,255,0.1) 0%, transparent 18%)",
    `radial-gradient(ellipse at 56% 78%, ${wallpaper.accent} 0%, transparent 44%)`,
    "linear-gradient(126deg, transparent 0%, transparent 42%, rgba(255,255,255,0.08) 52%, transparent 64%)",
    `radial-gradient(ellipse at 50% 48%, transparent 34%, ${wallpaper.vignette} 100%)`,
    `linear-gradient(155deg, ${wallpaper.bgLight} 0%, ${wallpaper.bg} 62%, ${wallpaper.bg} 100%)`,
  ].join(", ");
}

export function wallpaperPreviewImage(wallpaper: Wallpaper): string {
  return [
    "linear-gradient(180deg, rgba(255,255,255,0.18) 0%, rgba(255,255,255,0) 28%)",
    `radial-gradient(circle at 18% 18%, ${wallpaper.accentSoft} 0%, transparent 22%)`,
    `radial-gradient(circle at 84% 18%, ${wallpaper.glow} 0%, transparent 26%)`,
    "radial-gradient(circle at 26% 76%, rgba(255,255,255,0.1) 0%, transparent 18%)",
    `radial-gradient(ellipse at 56% 84%, ${wallpaper.accent} 0%, transparent 46%)`,
    "linear-gradient(128deg, transparent 0%, transparent 38%, rgba(255,255,255,0.08) 50%, transparent 60%)",
    `radial-gradient(ellipse at 50% 50%, transparent 32%, ${wallpaper.vignette} 100%)`,
    `linear-gradient(160deg, ${wallpaper.bgLight} 0%, ${wallpaper.bg} 65%, ${wallpaper.bg} 100%)`,
  ].join(", ");
}

const PREVIEWABLE_EXT = new Set([
  "txt", "md", "json", "yaml", "yml", "toml", "csv", "log",
  "go", "js", "ts", "tsx", "jsx", "py", "rs", "c", "h", "cpp",
  "html", "css", "xml", "sh", "bash", "zsh", "conf", "cfg",
  "sql", "graphql", "proto", "makefile", "dockerfile",
]);

export function approvalTitle(capability: string): string {
  if (capability.startsWith("fs_read")) return "Morph wants to read a file";
  if (capability.startsWith("fs_write")) return "Morph wants to save a file";
  if (capability.startsWith("fs_list")) return "Morph wants to browse the workspace";
  if (capability.startsWith("memory.remember")) return "Morph wants to remember something";
  if (capability.startsWith("todo.add")) return "Morph wants to add a task";
  if (capability.startsWith("todo.complete")) return "Morph wants to mark a task as done";
  if (capability.startsWith("todo.list")) return "Morph wants to review the task board";
  if (capability.startsWith("notes.read")) return "Morph wants to read a note";
  if (capability.startsWith("notes.write")) return "Morph wants to save a note";
  if (capability.startsWith("notes.list")) return "Morph wants to review its notes";
  if (capability.startsWith("browser_fetch")) return "Morph wants to visit a website";
  if (capability.startsWith("browser_search")) return "Morph wants to search the web";
  if (capability.startsWith("paint.") || capability.startsWith("paint_")) return "Morph wants to use Paint";
  if (capability.startsWith("shell_exec")) return "Morph wants to run a command";
  if (capability.startsWith("run_command")) return "Morph wants to run a command";
  if (capability.startsWith("spawn_helper")) return "Morph wants to start a helper worker";
  return `Morph wants to use ${capability}`;
}

export function approvalDescription(capability: string): string {
  if (capability.startsWith("fs_read")) return "This reads a file inside Haven's workspace. Your real files won't be touched.";
  if (capability.startsWith("fs_write")) return "This creates or modifies a file inside Haven's workspace. Your real files won't be touched.";
  if (capability.startsWith("fs_list")) return "This lists files inside Haven's workspace.";
  if (capability.startsWith("memory.remember")) return "This stores a short durable memory fact inside Loopgate's continuity system.";
  if (capability.startsWith("todo.add")) return "This adds a task to Haven's operating memory so it stays visible across sessions.";
  if (capability.startsWith("todo.complete")) return "This marks a task as done inside Haven's continuity system.";
  if (capability.startsWith("todo.list")) return "This reads Haven's current open tasks and active goals.";
  if (capability.startsWith("notes.read")) return "This opens a private working note inside Haven.";
  if (capability.startsWith("notes.write")) return "This creates or updates a private working note inside Haven.";
  if (capability.startsWith("notes.list")) return "This reviews Morph's private working notes.";
  if (capability.startsWith("browser_fetch")) return "This fetches content from the internet through Loopgate's proxy.";
  if (capability.startsWith("browser_search")) return "This searches the web through Loopgate's proxy.";
  if (capability.startsWith("paint.") || capability.startsWith("paint_")) return "This uses the Paint app to create or modify artwork.";
  if (capability.startsWith("shell_exec")) return "This runs a shell command inside Haven's workspace. Output is captured and returned to Morph.";
  if (capability.startsWith("run_command")) return "This runs a command on your system. Review carefully.";
  if (capability.startsWith("spawn_helper")) return "This creates a temporary worker with limited permissions.";
  return "This action is mediated by Loopgate's security policy.";
}

export function formatTime(ts: string): string {
  if (!ts) return "";
  try {
    return new Date(ts).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  } catch {
    return ts;
  }
}

export function formatSize(bytes: number): string {
  if (bytes === 0) return "--";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1048576).toFixed(1)} MB`;
}

export function isPreviewable(name: string): boolean {
  const extension = name.split(".").pop()?.toLowerCase() || "";
  return PREVIEWABLE_EXT.has(extension) || PREVIEWABLE_EXT.has(name.toLowerCase());
}

export function dotClass(state: string): string {
  if (state === "running" || state === "waiting_for_approval") return "dot dot-amber dot-pulse";
  if (state === "failed") return "dot dot-red";
  if (state === "cancelled") return "dot dot-idle";
  return "dot dot-green";
}

/** Task bar status next to the dot: approvals, run state, or last-run issue. */
export function taskbarStatusLabel(executionState: string, pendingApprovalCount: number): string {
  if (pendingApprovalCount > 0) {
    return `${pendingApprovalCount} approval${pendingApprovalCount > 1 ? "s" : ""}`;
  }
  if (executionState === "failed") {
    return "Needs attention";
  }
  if (executionState === "running") {
    return "Working";
  }
  if (executionState === "waiting_for_approval") {
    return "Awaiting approval";
  }
  if (executionState === "cancelled") {
    return "Cancelled";
  }
  return "No approvals pending";
}

export function morphLabel(state: string): string {
  if (state === "running") return "working";
  if (state === "waiting_for_approval") return "awaiting approval";
  return "idle";
}

export function MorphGlyph({ size = 48, className = "" }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" fill="none" className={className}>
      <rect x="10" y="14" width="44" height="32" rx="4" fill="#F4EFE6" stroke="#4A453D" strokeWidth="3" />
      <rect x="16" y="18" width="32" height="24" rx="2" fill="#1C1816" stroke="#4A453D" strokeWidth="2" />
      {/* Phosphor text/face */}
      <text x="32" y="34" fontFamily="monospace" fontSize="14" fill="#FFB53E" textAnchor="middle" fontWeight="bold">^_^</text>
      {/* Rainbow detail */}
      <rect x="42" y="14" width="6" height="4" fill="#D35F5F" />
      <rect x="44" y="14" width="2" height="4" fill="#E89F5D" />
      <rect x="46" y="14" width="2" height="4" fill="#E8C95D" />
      <rect x="48" y="14" width="2" height="4" fill="#6B9E7D" />
    </svg>
  );
}

export function HavenWordmark({ compact = false }: { compact?: boolean }) {
  const uniqueID = useId().replace(/:/g, "-");
  const strokeID = `havenWordmarkStroke-${uniqueID}`;

  return (
    <div className={`haven-wordmark ${compact ? "compact" : ""}`}>
      <svg width={compact ? "28" : "36"} height={compact ? "16" : "20"} viewBox="0 0 72 40" fill="none" className="haven-wordmark-mark" aria-hidden="true">
        <defs>
          <linearGradient id={strokeID} x1="5" y1="8" x2="67" y2="32" gradientUnits="userSpaceOnUse">
            <stop offset="0%" stopColor="#E0B16D" />
            <stop offset="58%" stopColor="#A6876A" />
            <stop offset="100%" stopColor="#8B6B4D" />
          </linearGradient>
        </defs>
        <path
          d="M8 22C8 15 13 10 19 10C26 10 30 16 36 22C42 28 46 34 53 34C59 34 64 29 64 22C64 15 59 10 53 10C46 10 42 16 36 22C30 28 26 34 19 34C13 34 8 29 8 22Z"
          stroke={`url(#${strokeID})`}
          strokeWidth="3.6"
          strokeLinecap="round"
          strokeLinejoin="round"
          fill="none"
        />
      </svg>
      <span className="haven-wordmark-text">
        <span className="haven-wordmark-name">Haven</span>
        {!compact && <span className="haven-wordmark-suffix">OS</span>}
      </span>
    </div>
  );
}

export function IconMorph() {
  return (
    <MorphGlyph size={48} />
  );
}

export function IconLoopgate() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Shield */}
      <path d="M24 6L10 14V26C10 34.5 16.5 41 24 43C31.5 41 38 34.5 38 26V14L24 6Z" fill="#F4EFE6" stroke="#4A453D" strokeWidth="2.5" strokeLinejoin="round" />
      {/* Keyhole */}
      <circle cx="24" cy="22" r="4" fill="#4A453D" />
      <path d="M22 25H26V32H22V25Z" fill="#4A453D" />
      {/* Rainbow accent stripe */}
      <rect x="18" y="11" width="3" height="3" rx="0.5" fill="#D35F5F" />
      <rect x="21" y="11" width="3" height="3" rx="0.5" fill="#E89F5D" />
      <rect x="24" y="11" width="3" height="3" rx="0.5" fill="#E8C95D" />
      <rect x="27" y="11" width="3" height="3" rx="0.5" fill="#6B9E7D" />
    </svg>
  );
}

export function IconWorkspace() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Folder back */}
      <path d="M8 16C8 14 9.8 12 12 12H19L23 16H36C38.2 16 40 17.8 40 20V36C40 38.2 38.2 40 36 40H12C9.8 40 8 38.2 8 36V16Z" fill="#E8C95D" stroke="#4A453D" strokeWidth="2.5" strokeLinejoin="round" />
      {/* Folder front face */}
      <path d="M8 22H40V36C40 38.2 38.2 40 36 40H12C9.8 40 8 38.2 8 36V22Z" fill="#F4EFE6" stroke="#4A453D" strokeWidth="2.5" strokeLinejoin="round" />
    </svg>
  );
}

export function IconTodo() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Clipboard body */}
      <rect x="10" y="10" width="28" height="34" rx="3" fill="#F4EFE6" stroke="#4A453D" strokeWidth="2.5" />
      {/* Clip */}
      <rect x="18" y="6" width="12" height="8" rx="2" fill="#8BB7CA" stroke="#4A453D" strokeWidth="2.5" />
      {/* Checkmarks and lines */}
      <path d="M16 22L19 25L23 19" stroke="#6B9E7D" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      <rect x="26" y="21" width="8" height="2" rx="1" fill="#4A453D" />
      <path d="M16 32L19 35L23 29" stroke="#6B9E7D" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
      <rect x="26" y="31" width="8" height="2" rx="1" fill="#4A453D" />
    </svg>
  );
}

export function IconJournal() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Closed book */}
      <rect x="10" y="8" width="24" height="32" rx="2" fill="#C1A78B" stroke="#4A453D" strokeWidth="2.5" />
      {/* Spine */}
      <rect x="10" y="8" width="6" height="32" rx="1" fill="#A8896C" stroke="#4A453D" strokeWidth="2.5" />
      {/* Pages */}
      <rect x="16" y="10" width="16" height="28" fill="#F4EFE6" stroke="#4A453D" strokeWidth="1.5" />
      {/* Text lines */}
      <rect x="19" y="16" width="10" height="1.5" rx="0.5" fill="#B6A48C" />
      <rect x="19" y="21" width="10" height="1.5" rx="0.5" fill="#B6A48C" />
      <rect x="19" y="26" width="7" height="1.5" rx="0.5" fill="#B6A48C" />
      {/* Bookmark ribbon */}
      <path d="M28 8V16L30 14L32 16V8" fill="#D35F5F" stroke="#4A453D" strokeWidth="1.5" />
    </svg>
  );
}

export function IconNotes() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Notepad */}
      <rect x="10" y="10" width="28" height="32" rx="2" fill="#FFF6B8" stroke="#4A453D" strokeWidth="2.5" />
      {/* Spiral holes */}
      <circle cx="14" cy="16" r="2" fill="#F4EFE6" stroke="#4A453D" strokeWidth="1.5" />
      <circle cx="14" cy="24" r="2" fill="#F4EFE6" stroke="#4A453D" strokeWidth="1.5" />
      <circle cx="14" cy="32" r="2" fill="#F4EFE6" stroke="#4A453D" strokeWidth="1.5" />
      {/* Lines */}
      <rect x="20" y="16" width="14" height="1.5" rx="0.5" fill="#C3A44E" />
      <rect x="20" y="22" width="14" height="1.5" rx="0.5" fill="#C3A44E" />
      <rect x="20" y="28" width="10" height="1.5" rx="0.5" fill="#C3A44E" />
      <rect x="20" y="34" width="8" height="1.5" rx="0.5" fill="#C3A44E" />
    </svg>
  );
}

export function IconPaint() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Palette */}
      <ellipse cx="24" cy="26" rx="16" ry="14" fill="#F4EFE6" stroke="#4A453D" strokeWidth="2.5" />
      {/* Thumb hole */}
      <ellipse cx="20" cy="32" rx="4" ry="3" fill="#E8E0D4" stroke="#4A453D" strokeWidth="2" />
      {/* Paint blobs */}
      <circle cx="18" cy="20" r="3" fill="#D35F5F" />
      <circle cx="26" cy="17" r="3" fill="#E8C95D" />
      <circle cx="33" cy="22" r="3" fill="#6B9E7D" />
      <circle cx="30" cy="30" r="2.5" fill="#8BB7CA" />
      {/* Brush */}
      <path d="M34 10L38 6" stroke="#4A453D" strokeWidth="3" strokeLinecap="round" />
      <path d="M32 12L35 9" stroke="#E89F5D" strokeWidth="4" strokeLinecap="round" />
    </svg>
  );
}

export function IconSettings() {
  return (
    <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
      {/* Gear outer */}
      <path d="M24 8L28 10L30 6H34L32 12L36 14L40 10L42 14L38 16L40 20L44 22V26L38 24L36 28L40 32L38 36L34 32L30 34L32 40H28L26 34L22 34L20 40H16L18 34L14 32L10 36L8 32L12 28L10 24L6 26V22L10 20L12 16L8 14L10 10L14 14L18 12L16 6H20L22 10L24 8Z" fill="#F4EFE6" stroke="#4A453D" strokeWidth="2" strokeLinejoin="round" />
      {/* Center circle */}
      <circle cx="24" cy="23" r="7" fill="#E8E0D4" stroke="#4A453D" strokeWidth="2.5" />
      <circle cx="24" cy="23" r="3" fill="#4A453D" />
      {/* Rainbow accent dot */}
      <circle cx="24" cy="23" r="1.5" fill="#E89F5D" />
    </svg>
  );
}

export const ICON_MAP: Partial<Record<AppID, ComponentType>> = {
  morph: IconMorph,
  loopgate: IconLoopgate,
  workspace: IconWorkspace,
  todo: IconTodo,
  notes: IconNotes,
  journal: IconJournal,
  paint: IconPaint,
  settings: IconSettings,
};
