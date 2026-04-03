import { useCallback, useEffect, useRef, useState } from "react";

import {
  AddTask,
  CancelExecution,
  CompleteTodo,
  DecideApproval,
  DismissDeskNote,
  ExecuteDeskNoteAction,
  ListDeskNotes,
  ListThreads,
  LoadThread,
  NewThread,
  RenameThread,
  SecurityOverview,
  SendMessage,
  WorkspaceDelete,
  WorkspaceDiff,
  WorkspaceExport,
  WorkspaceImportDirectory,
  WorkspaceImportFile,
  WorkspaceImportPath,
  WorkspaceList,
  WorkspaceRestoreOriginal,
  UpdateTaskStandingGrant,
} from "../wailsjs/go/main/HavenApp";
import type {
  ConversationEvent,
  DiffLine,
  SecurityOverviewResponse,
  TaskDraft,
  ThreadSummary,
  WorkspaceListEntry,
} from "../wailsjs/go/main/HavenApp";
import type {
  ConfirmDialogState,
  ThreadRenameDialogState,
  WorkspaceEditState,
  WorkspaceNewNameState,
} from "./app/havenTypes";
import { NEW_WORKING_NOTE_PATH } from "./app/havenTypes";
import ConfirmDialog from "./components/ConfirmDialog";
import ContextMenu from "./components/ContextMenu";
import DesktopSurface from "./components/DesktopSurface";
import DiffReviewDialog, { type DiffReviewState } from "./components/DiffReviewDialog";
import DropApprovalDialog from "./components/DropApprovalDialog";
import HavenFloatingWindows from "./components/HavenFloatingWindows";
import HavenWorkstationStage from "./components/HavenWorkstationStage";
import MenuBar from "./components/MenuBar";
import MorphDesktopBubble from "./components/MorphDesktopBubble";
import SetupWizard from "./components/SetupWizard";
import TaskBar from "./components/TaskBar";
import TextInputDialog from "./components/TextInputDialog";
import ToastStack from "./components/ToastStack";
import MorphWindow from "./components/windows/MorphWindow";
import WorkspaceWindow from "./components/windows/WorkspaceWindow";
import "./App.css";
import {
  DEFAULT_WALLPAPER,
  DOCK_LAUNCHER_APPS_CLASSIC,
  DOCK_LAUNCHER_APPS_WORKSTATION,
  WALLPAPERS,
  WIN_DEFAULTS,
  applyWallpaperTheme,
  resolveWindowFrame,
  type AppID,
  type ApprovalInfo,
  type ContextMenuState,
  type DesktopFile,
  type PendingDrop,
  type PreviewState,
  type SecurityAlert,
  type Toast,
  type Wallpaper,
} from "./lib/haven";
import { useSystemStatusPoll } from "./hooks/useSystemStatusPoll";
import { useDeskNoteDrag } from "./hooks/useDeskNoteDrag";
import { useDeskNotesSync } from "./hooks/useDeskNotesSync";
import { useDesktopFileDrag } from "./hooks/useDesktopFileDrag";
import { useDesktopIconDrag } from "./hooks/useDesktopIconDrag";
import { useGlobalMenuDismiss } from "./hooks/useGlobalMenuDismiss";
import { useHavenBackendToastBridge } from "./hooks/useHavenBackendToastBridge";
import { useHavenBootSetup } from "./hooks/useHavenBootSetup";
import { useHavenClock } from "./hooks/useHavenClock";
import { useHavenMemorySync } from "./hooks/useHavenMemorySync";
import { useHavenMorphSessionEvents } from "./hooks/useHavenMorphSessionEvents";
import { useHavenPresence } from "./hooks/useHavenPresence";
import { useHavenDockEdge } from "./hooks/useHavenDockEdge";
import { useHavenShellLayout } from "./hooks/useHavenShellLayout";
import { useHavenThreadBootstrap } from "./hooks/useHavenThreadBootstrap";
import { useHavenToast } from "./hooks/useHavenToast";
import { useHavenWindowManager } from "./hooks/useHavenWindowManager";
import { useJournalWindowState } from "./hooks/useJournalWindowState";
import { useWorkingNotesWindowState } from "./hooks/useWorkingNotesWindowState";
import { useLoopgateSecurityPoll } from "./hooks/useLoopgateSecurityPoll";
import { useRemoteIconPositionsSync } from "./hooks/useRemoteIconPositionsSync";
import { useWailsFileDrop } from "./hooks/useWailsFileDrop";
import { useWorkspaceWindowEffects } from "./hooks/useWorkspaceWindowEffects";

export default function App() {
  const { booting, bootLines, bootFading, needsSetup, setNeedsSetup } = useHavenBootSetup();
  const { shellLayout, setShellLayout } = useHavenShellLayout();
  const { dockEdge, setDockEdge } = useHavenDockEdge();
  const { toasts, setToasts, pushToast } = useHavenToast();
  const {
    windows,
    setWindows,
    focusedWin,
    setFocusedWin,
    nextZ,
    openWindow,
    closeWindow,
    focusWindow,
    collapseWindow,
    dragWindow,
    resizeWindow,
  } = useHavenWindowManager();
  const { iconPositions, setIconPositions, iconDragOffset, setPendingIconDrag } = useDesktopIconDrag();
  const { desktopFiles, setDesktopFiles, fileDragOffset, setPendingFileDrag } = useDesktopFileDrag();
  const clock = useHavenClock();
  const { systemStatus } = useSystemStatusPoll(booting);
  const presence = useHavenPresence(booting);
  const { memoryStatus, memoryLoaded } = useHavenMemorySync(booting);
  const { deskNotes, setDeskNotes } = useDeskNotesSync(booting);
  const { deskNotePositions, noteDragOffset, setPendingNoteDrag } = useDeskNoteDrag();
  const {
    journalEntries,
    activeJournalPath,
    setActiveJournalPath,
    activeJournalEntry,
    journalLoading,
    journalEntryLoading,
    refreshJournalEntries,
    loadJournalEntry,
  } = useJournalWindowState({ journalWindowOpen: Boolean(windows.journal), pushToast });
  const {
    workingNotes,
    activeWorkingNotePath,
    setActiveWorkingNotePath,
    activeWorkingNote,
    setActiveWorkingNote,
    workingNotesLoading,
    workingNoteLoading,
    workingNoteSaving,
    refreshWorkingNotes,
    loadWorkingNote,
    handleSaveWorkingNote,
  } = useWorkingNotesWindowState({ notesWindowOpen: Boolean(windows.notes), pushToast });
  const [selectedIcon, setSelectedIcon] = useState<string | null>(null);

  const [openMenu, setOpenMenu] = useState<string | null>(null);

  const [wallpaper, setWallpaperRaw] = useState<Wallpaper>(() => {
    try {
      const saved = localStorage.getItem("haven-wallpaper");
      if (saved) {
        const match = WALLPAPERS.find((candidate) => candidate.id === saved);
        if (match) {
          applyWallpaperTheme(match);
          return match;
        }
      }
    } catch {
      // Ignore invalid local storage state.
    }
    applyWallpaperTheme(DEFAULT_WALLPAPER);
    return DEFAULT_WALLPAPER;
  });

  const setWallpaper = useCallback((next: Wallpaper) => {
    applyWallpaperTheme(next);
    setWallpaperRaw(next);
  }, []);

  const [executionState, setExecutionState] = useState("idle");
  const [securityData, setSecurityData] = useState<SecurityOverviewResponse | null>(null);
  const [securityAlerts, setSecurityAlerts] = useState<SecurityAlert[]>([]);
  const nextAlertID = useRef(0);
  const [dropActive, setDropActive] = useState(false);
  const [executingDeskNoteIDs, setExecutingDeskNoteIDs] = useState<Record<string, boolean>>({});

  const [threads, setThreads] = useState<ThreadSummary[]>([]);
  const [threadsLoaded, setThreadsLoaded] = useState(false);
  const [activeThreadID, setActiveThreadID] = useState<string | null>(null);
  const [messages, setMessages] = useState<ConversationEvent[]>([]);
  const [pendingApproval, setPendingApproval] = useState<ApprovalInfo | null>(null);
  const [input, setInput] = useState("");
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const activeThreadRef = useRef(activeThreadID);
  const startupLayoutApplied = useRef(false);
  const [renameThreadDialog, setRenameThreadDialog] = useState<ThreadRenameDialogState | null>(null);
  const [renameThreadBusy, setRenameThreadBusy] = useState(false);

  const [wsPath, setWsPath] = useState("");
  const [wsEntries, setWsEntries] = useState<WorkspaceListEntry[]>([]);
  const [wsLoading, setWsLoading] = useState(false);
  const [wsError, setWsError] = useState("");
  const [preview, setPreview] = useState<PreviewState | null>(null);
  const [wsEditing, setWsEditing] = useState<WorkspaceEditState | null>(null);
  const [wsRenaming, setWsRenaming] = useState<string | null>(null);
  const [wsRenameValue, setWsRenameValue] = useState("");
  const [wsNewName, setWsNewName] = useState<WorkspaceNewNameState | null>(null);
  const [wsNewNameValue, setWsNewNameValue] = useState("");
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);
  const [reviewDialog, setReviewDialog] = useState<DiffReviewState | null>(null);
  const [reviewActionBusy, setReviewActionBusy] = useState<"export" | "discard" | null>(null);

  const [pendingDrop, setPendingDrop] = useState<PendingDrop | null>(null);

  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);

  const selectThread = useCallback(async (threadID: string) => {
    setActiveThreadID(threadID);
    try {
      localStorage.setItem("haven-active-thread-id", threadID);
    } catch {
      // Ignore local storage write failures.
    }
    setPendingApproval(null);
    setExecutionState("idle");
    try {
      setMessages(await LoadThread(threadID) || []);
    } catch {
      setMessages([]);
    }
  }, []);

  const createThread = useCallback(async () => {
    const thread = await NewThread();
    setThreads((previous) => [thread, ...previous]);
    setActiveThreadID(thread.thread_id);
    try {
      localStorage.setItem("haven-active-thread-id", thread.thread_id);
    } catch {
      // Ignore local storage write failures.
    }
    setMessages([]);
    setExecutionState("idle");
    setPendingApproval(null);
  }, []);

  const handleAddTask = useCallback(async (request: TaskDraft) => {
    const response = await AddTask(request);
    if (response.error) {
      pushToast("Tasks", response.error, "warning");
      return response.error;
    }
    pushToast("Tasks", request.scheduled_for_utc ? "Scheduled on the task board." : "Added to the task board.", "success");
    return null;
  }, [pushToast]);

  const handleCompleteTodo = useCallback(async (itemID: string) => {
    const response = await CompleteTodo(itemID);
    if (response.error) {
      pushToast("Tasks", response.error, "warning");
      return response.error;
    }
    pushToast("Tasks", "Marked as done.", "success");
    return null;
  }, [pushToast]);

  const reloadSecurityOverview = useCallback(() => {
    SecurityOverview().then(setSecurityData).catch(() => {});
  }, []);

  const handleUpdateTaskStandingGrant = useCallback(async (className: string, granted: boolean) => {
    const response = await UpdateTaskStandingGrant(className, granted);
    if (response.error) {
      pushToast("Security", response.error, "warning");
      return response.error;
    }
    setSecurityData(response);
    pushToast(
      "Security",
      granted ? "Always-allowed Haven task updated." : "Standing Haven task approval revoked.",
      "success",
    );
    return null;
  }, [pushToast]);

  useLoopgateSecurityPoll(Boolean(windows.loopgate), reloadSecurityOverview);
  useRemoteIconPositionsSync(booting, setIconPositions);
  useHavenBackendToastBridge(booting, pushToast);
  useGlobalMenuDismiss(setContextMenu, setOpenMenu);
  useWailsFileDrop(setDropActive, setPendingDrop);
  useHavenMorphSessionEvents({
    activeThreadRef,
    setMessages,
    setExecutionState,
    setPendingApproval,
    setThreads,
    setSecurityAlerts,
    setWindows,
    nextAlertID,
    nextZ,
  });

  useEffect(() => {
    activeThreadRef.current = activeThreadID;
  }, [activeThreadID]);

  useHavenThreadBootstrap({ selectThread, setThreads, setThreadsLoaded });

  useEffect(() => {
    if (startupLayoutApplied.current) return;
    if (booting || needsSetup !== false || !threadsLoaded || !memoryLoaded) return;

    const continuityCount = (memoryStatus?.unresolved_items?.length || 0) + (memoryStatus?.active_goals?.length || 0);
    if (continuityCount > 0) {
      openWindow("todo");
    }
    if (shellLayout === "classic") {
      openWindow("morph");
    }

    if (threads.length === 0) {
      void createThread();
    }
    startupLayoutApplied.current = true;
  }, [booting, needsSetup, threadsLoaded, memoryLoaded, memoryStatus, threads.length, openWindow, shellLayout, createThread]);

  useEffect(() => {
    if (shellLayout !== "workstation") return;
    closeWindow("morph");
    closeWindow("workspace");
  }, [shellLayout, closeWindow]);

  useEffect(() => {
    if (shellLayout !== "classic") return;
    openWindow("morph");
    openWindow("workspace");
  }, [shellLayout, openWindow]);

  const loadWorkspace = useCallback(async (path: string) => {
    setWsLoading(true);
    setWsError("");
    setPreview(null);
    try {
      const response = await WorkspaceList(path);
      setWsPath(response.path || "");
      setWsEntries(response.entries || []);
    } catch (errorValue) {
      setWsError(String(errorValue));
      setWsEntries([]);
    } finally {
      setWsLoading(false);
    }
  }, []);

  useWorkspaceWindowEffects({
    workspaceOpen: Boolean(windows.workspace),
    wsPath,
    wsEntriesLength: wsEntries.length,
    wsLoading,
    loadWorkspace,
  });

  const handleSend = async () => {
    const text = input.trim();
    if (!text || !activeThreadID) return;
    setMessages((previous) => [...previous, {
      v: "",
      ts: new Date().toISOString(),
      thread_id: activeThreadID,
      type: "user_message",
      data: { text },
    } as ConversationEvent]);
    setInput("");
    setExecutionState("running");
    const response = await SendMessage(activeThreadID, text);
    if (!response.accepted) {
      setExecutionState("idle");
      setMessages((previous) => [...previous, {
        v: "",
        ts: new Date().toISOString(),
        thread_id: activeThreadID,
        type: "assistant_message",
        data: { text: `Error: ${response.reason}` },
      } as ConversationEvent]);
    }
  };

  const handleApproval = async (approved: boolean) => {
    if (!activeThreadID || !pendingApproval) return;
    await DecideApproval(activeThreadID, pendingApproval.approvalRequestID, approved);
    setPendingApproval(null);
  };

  const handleCancel = async () => {
    if (!activeThreadID) return;
    try {
      await CancelExecution(activeThreadID);
    } catch {
      // Preserve current best-effort cancel behavior.
    }
  };

  const handleMenuAction = (action: string) => {
    setOpenMenu(null);
    if (action === "layout-workstation") {
      setShellLayout("workstation");
      return;
    }
    if (action === "layout-classic") {
      setShellLayout("classic");
      return;
    }
    if (action === "dock-edge-bottom") {
      setDockEdge("bottom");
      return;
    }
    if (action === "dock-edge-left") {
      setDockEdge("left");
      return;
    }
    if (action === "dock-edge-right") {
      setDockEdge("right");
      return;
    }
    if (action === "new-thread") {
      if (shellLayout === "classic") openWindow("morph");
      void createThread();
      return;
    }
    if (action === "import-file") {
      openWindow("workspace");
      WorkspaceImportFile().then(() => {
        void loadWorkspace(wsPath);
      });
      return;
    }
    if (action === "import-folder") {
      openWindow("workspace");
      WorkspaceImportDirectory().then(() => {
        void loadWorkspace(wsPath);
      });
      return;
    }
    if (action === "open-morph") {
      if (shellLayout === "classic") openWindow("morph");
      return;
    }
    if (action === "open-loopgate") return openWindow("loopgate");
    if (action === "open-workspace") return openWindow("workspace");
    if (action === "open-todo") return openWindow("todo");
    if (action === "open-notes") return openWindow("notes");
    if (action === "open-journal") return openWindow("journal");
    if (action === "open-paint") return openWindow("paint");
    if (action === "open-settings") return openWindow("settings");
    if (action === "arrange") {
      const ids = Object.keys(windows) as AppID[];
      const updated = { ...windows };
      ids.forEach((id, index) => {
        const defaults = WIN_DEFAULTS[id];
        const frame = resolveWindowFrame(id, {
          x: defaults.x + index * 20,
          y: defaults.y + index * 20,
          w: updated[id].w,
          h: updated[id].h,
        });
        updated[id] = { ...updated[id], x: frame.x, y: frame.y, w: frame.w, h: frame.h };
      });
      setWindows(updated);
    }
  };

  const handleDropApprove = async () => {
    if (!pendingDrop) return;
    const { paths, x, y } = pendingDrop;
    setPendingDrop(null);
    const newFiles: DesktopFile[] = [];
    for (let index = 0; index < paths.length; index++) {
      try {
        const response = await WorkspaceImportPath(paths[index]);
        if (response.imported && response.name) {
          newFiles.push({
            id: `df-${Date.now()}-${index}`,
            name: response.name,
            sandboxPath: response.path || `imports/${response.name}`,
            x: x + index * 20,
            y: y + index * 20,
          });
        }
      } catch {
        // Skip failed imports and keep the successful ones.
      }
    }

    if (newFiles.length > 0) {
      setDesktopFiles((previous) => {
        const updated = [...previous, ...newFiles];
        try {
          localStorage.setItem("haven-desktop-files", JSON.stringify(updated));
        } catch {
          // Ignore local storage write failures.
        }
        return updated;
      });
      openWindow("workspace");
      void loadWorkspace(wsPath);
    }
  };

  const handleRenameThread = async (threadID: string, title: string) => {
    await RenameThread(threadID, title);
    const updatedThreads = await ListThreads();
    setThreads(updatedThreads || []);
  };

  const submitRenameThread = async () => {
    if (!renameThreadDialog || !renameThreadDialog.value.trim()) return;
    setRenameThreadBusy(true);
    try {
      await handleRenameThread(renameThreadDialog.threadID, renameThreadDialog.value.trim());
      setRenameThreadDialog(null);
    } catch (errorValue) {
      pushToast("Rename failed", String(errorValue), "warning");
    } finally {
      setRenameThreadBusy(false);
    }
  };

  const handleDismissDeskNote = async (noteID: string) => {
    const response = await DismissDeskNote(noteID);
    if (response.success) {
      setDeskNotes((previous) => previous.filter((note) => note.id !== noteID));
    }
  };

  const handleExecuteDeskNote = async (noteID: string) => {
    setExecutingDeskNoteIDs((previous) => ({ ...previous, [noteID]: true }));
    try {
      const response = await ExecuteDeskNoteAction(noteID);
      if (!response.success) {
        pushToast("Morph", response.error || "That note could not be started.", "warning");
        return;
      }

      if (response.thread_id) {
        if (shellLayout === "classic") openWindow("morph");
        await selectThread(response.thread_id);
        const updatedThreads = await ListThreads();
        setThreads(updatedThreads || []);
      }

      const refreshedNotes = await ListDeskNotes().catch(() => null);
      if (refreshedNotes) {
        setDeskNotes(refreshedNotes);
      }
      pushToast("Morph", "Started from the desk note.", "success");
    } finally {
      setExecutingDeskNoteIDs((previous) => {
        const next = { ...previous };
        delete next[noteID];
        return next;
      });
    }
  };

  const exportWorkspacePath = useCallback(async (path: string) => {
    try {
      const response = await WorkspaceExport(path);
      if (response.error) {
        pushToast("Export failed", response.error, "warning");
        return false;
      }
      if (response.exported) {
        pushToast("Export complete", `${path.split("/").pop() || path} was exported to your computer.`, "success");
        return true;
      }
      return false;
    } catch (errorValue) {
      pushToast("Export failed", String(errorValue), "warning");
      return false;
    }
  }, [pushToast]);

  const openReviewDialog = async (path: string) => {
    setReviewDialog({
      path,
      lines: [] as DiffLine[],
      hasChanges: false,
      loading: true,
      error: "",
    });
    try {
      const response = await WorkspaceDiff(path);
      setReviewDialog({
        path: response.path || path,
        lines: response.lines || [],
        hasChanges: response.has_changes,
        loading: false,
        error: response.error || "",
      });
    } catch (errorValue) {
      setReviewDialog({
        path,
        lines: [],
        hasChanges: false,
        loading: false,
        error: String(errorValue),
      });
    }
  };

  const handleExportReview = async () => {
    if (!reviewDialog) return;
    setReviewActionBusy("export");
    try {
      const exported = await exportWorkspacePath(reviewDialog.path);
      if (exported) setReviewDialog(null);
    } finally {
      setReviewActionBusy(null);
    }
  };

  const handleRequestCloseEditor = () => {
    if (!wsEditing) return;
    if (!wsEditing.dirty) {
      setWsEditing(null);
      return;
    }
    setConfirmDialog({ kind: "discard_editor_changes", path: wsEditing.path });
  };

  const handleConfirmDialog = async () => {
    if (!confirmDialog) return;
    setConfirmBusy(true);
    try {
      switch (confirmDialog.kind) {
        case "discard_editor_changes":
          setWsEditing(null);
          break;
        case "delete_workspace_entry": {
          const response = await WorkspaceDelete(confirmDialog.path);
          if (response.error) {
            setWsError(response.error);
            break;
          }
          if (preview?.path === confirmDialog.path) setPreview(null);
          if (wsEditing?.path === confirmDialog.path) setWsEditing(null);
          if (reviewDialog?.path === confirmDialog.path) setReviewDialog(null);
          await loadWorkspace(wsPath);
          pushToast("Deleted", `${confirmDialog.entryName} was removed from Haven.`, "success");
          break;
        }
        case "discard_review_changes": {
          const response = await WorkspaceRestoreOriginal(confirmDialog.path);
          if (response.error) {
            pushToast("Discard failed", response.error, "warning");
            break;
          }
          if (preview?.path === confirmDialog.path) setPreview(null);
          if (wsEditing?.path === confirmDialog.path) setWsEditing(null);
          setReviewDialog(null);
          await loadWorkspace(wsPath);
          pushToast(
            "Changes discarded",
            `Restored the original version of ${confirmDialog.path.split("/").pop() || confirmDialog.path}.`,
            "success",
          );
          break;
        }
      }
    } catch (errorValue) {
      pushToast("Action failed", String(errorValue), "warning");
    } finally {
      setConfirmBusy(false);
      setConfirmDialog(null);
    }
  };

  const openDesktopFileInWorkspace = (fileID: string) => {
    const desktopFile = desktopFiles.find((file) => file.id === fileID);
    if (!desktopFile) return;
    openWindow("workspace");
    const dir = desktopFile.sandboxPath.includes("/") ? desktopFile.sandboxPath.substring(0, desktopFile.sandboxPath.lastIndexOf("/")) : "";
    void loadWorkspace(dir);
  };

  const handleDockLaunch = useCallback((appID: AppID) => {
    openWindow(appID);
  }, [openWindow]);

  const removeDesktopFile = (fileID: string) => {
    setDesktopFiles((previous) => {
      const updated = previous.filter((file) => file.id !== fileID);
      try {
        localStorage.setItem("haven-desktop-files", JSON.stringify(updated));
      } catch {
        // Ignore local storage write failures.
      }
      return updated;
    });
  };

  const exportDesktopFile = (fileID: string) => {
    const desktopFile = desktopFiles.find((file) => file.id === fileID);
    if (!desktopFile) return;
    void exportWorkspacePath(desktopFile.sandboxPath);
  };

  const isRunning = executionState === "running" || executionState === "waiting_for_approval";
  const pendingApprovalCount = securityData?.pending_approvals?.length || 0;
  const continuityCount = (memoryStatus?.unresolved_items?.length || 0) + (memoryStatus?.active_goals?.length || 0);
  const openWinList = Object.values(windows).sort((left, right) => left.z - right.z);

  const morphWindowProps = {
    threads,
    activeThreadID,
    messages,
    pendingApproval,
    input,
    isRunning,
    executionState,
    presence,
    memoryStatus,
    systemStatus,
    messagesEndRef,
    onCreateThread: createThread,
    onSelectThread: selectThread,
    onOpenThreadContextMenu: (x: number, y: number, threadID: string) => setContextMenu({ x, y, data: { kind: "thread", threadID } }),
    onHandleApproval: handleApproval,
    onCancel: handleCancel,
    onInputChange: setInput,
    onSend: handleSend,
  };

  const workspaceWindowProps = {
    wsPath,
    wsEntries,
    wsLoading,
    wsError,
    preview,
    wsEditing,
    wsRenaming,
    wsRenameValue,
    wsNewName,
    wsNewNameValue,
    onLoadWorkspace: loadWorkspace,
    onSetWsError: setWsError,
    onClearWsError: () => setWsError(""),
    onSetPreview: setPreview,
    onSetWsEditing: setWsEditing,
    onSetWsRenaming: setWsRenaming,
    onSetWsRenameValue: setWsRenameValue,
    onSetWsNewName: setWsNewName,
    onSetWsNewNameValue: setWsNewNameValue,
    onRequestCloseEditor: handleRequestCloseEditor,
    onRequestDeleteEntry: (path: string, entryName: string) => setConfirmDialog({ kind: "delete_workspace_entry", path, entryName }),
    onRequestReviewPath: openReviewDialog,
    onExportPath: (path: string) => { void exportWorkspacePath(path); },
  };

  const sharedFloatingWindowsProps = {
    openWinList,
    focusedWin,
    onFocusWindow: focusWindow,
    onCloseWindow: closeWindow,
    onCollapseWindow: collapseWindow,
    onDragWindow: dragWindow,
    onResizeWindow: resizeWindow,
    morphProps: morphWindowProps,
    loopgateProps: {
      securityAlerts,
      securityData,
      onClearSecurityAlerts: () => setSecurityAlerts([]),
      onUpdateTaskStandingGrant: handleUpdateTaskStandingGrant,
    },
    workspaceProps: workspaceWindowProps,
    settingsProps: {
      wallpaper,
      wallpapers: WALLPAPERS,
      onWallpaperChange: (nextWallpaper: Wallpaper) => {
        setWallpaper(nextWallpaper);
        try {
          localStorage.setItem("haven-wallpaper", nextWallpaper.id);
        } catch {
          // Ignore local storage write failures.
        }
      },
    },
    todoProps: {
      memoryStatus,
      onAddTask: handleAddTask,
      onCompleteTodo: handleCompleteTodo,
      standingTaskGrants: securityData?.standing_task_grants ?? [],
    },
    notesProps: {
      notes: workingNotes,
      selectedPath: activeWorkingNotePath === NEW_WORKING_NOTE_PATH ? null : activeWorkingNotePath,
      activeNote: activeWorkingNote,
      loadingList: workingNotesLoading,
      loadingNote: workingNoteLoading,
      savingNote: workingNoteSaving,
      onRefresh: () => { void refreshWorkingNotes(activeWorkingNotePath); },
      onSelectNote: (path: string) => {
        setActiveWorkingNotePath(path);
        void loadWorkingNote(path);
      },
      onCreateNote: () => {
        setActiveWorkingNotePath(NEW_WORKING_NOTE_PATH);
        setActiveWorkingNote({ path: "", title: "", content: "" });
      },
      onSaveNote: handleSaveWorkingNote,
    },
    journalProps: {
      entries: journalEntries,
      selectedPath: activeJournalPath,
      activeEntry: activeJournalEntry,
      loadingList: journalLoading,
      loadingEntry: journalEntryLoading,
      onRefresh: () => { void refreshJournalEntries(activeJournalPath); },
      onSelectEntry: (path: string) => {
        setActiveJournalPath(path);
        void loadJournalEntry(path);
      },
    },
    paintOnToast: pushToast,
  };

  if (booting) {
    return (
      <div className={`boot-screen ${bootFading ? "fading" : ""}`}>
        <div className="boot-scanlines" />
        <div className="boot-terminal">
          {bootLines.map((line, index) => <div key={index} className="boot-line">{line || "\u00A0"}</div>)}
          <span className="boot-cursor">_</span>
        </div>
      </div>
    );
  }

  if (needsSetup === true) {
    return <SetupWizard onComplete={(nextWallpaperID) => {
      const nextWallpaper = WALLPAPERS.find((candidate) => candidate.id === nextWallpaperID);
      if (nextWallpaper) {
        setWallpaper(nextWallpaper);
      }
      setNeedsSetup(false);
    }} />;
  }

  const dockLauncherAppIDs = shellLayout === "workstation" ? DOCK_LAUNCHER_APPS_WORKSTATION : DOCK_LAUNCHER_APPS_CLASSIC;

  const shellStage = shellLayout === "workstation" ? (
    <HavenWorkstationStage
      wallpaper={wallpaper}
      selectedIcon={selectedIcon}
      deskNotes={deskNotes}
      deskNotePositions={deskNotePositions}
      desktopFiles={desktopFiles}
      executingDeskNoteIDs={executingDeskNoteIDs}
      onDesktopClick={() => {
        setSelectedIcon(null);
        setFocusedWin(null);
      }}
      onDesktopContextMenu={(x, y) => setContextMenu({ x, y, data: { kind: "desktop" } })}
      onSelectDesktopFile={setSelectedIcon}
      onOpenDesktopFile={openDesktopFileInWorkspace}
      onOpenDesktopFileContextMenu={(x, y, fileID) => setContextMenu({ x, y, data: { kind: "file", fileID } })}
      onStartDesktopFileDrag={(event, file) => {
        fileDragOffset.current = { x: event.clientX - file.x, y: event.clientY - file.y };
        setPendingFileDrag({ id: file.id, startX: event.clientX, startY: event.clientY });
        setSelectedIcon(file.id);
      }}
      onStartDeskNoteDrag={(event, id) => {
        const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
        noteDragOffset.current = { x: event.clientX - rect.left, y: event.clientY - rect.top };
        setPendingNoteDrag({ id, startX: event.clientX, startY: event.clientY });
      }}
      onExecuteDeskNote={handleExecuteDeskNote}
      onDismissDeskNote={handleDismissDeskNote}
      morphBubble={(
        <MorphDesktopBubble
          presenceState={presence.state}
          presenceStatusText={presence.status_text}
          messages={messages}
          pendingApproval={pendingApproval}
          input={input}
          isRunning={isRunning}
          executionState={executionState}
          onHandleApproval={handleApproval}
          onCancel={handleCancel}
          onInputChange={setInput}
          onSend={handleSend}
        />
      )}
      floatingLayer={(
        <HavenFloatingWindows
          {...sharedFloatingWindowsProps}
        />
      )}
    />
  ) : (
    <DesktopSurface
      wallpaper={wallpaper}
      selectedIcon={selectedIcon}
      presenceState={presence.state}
      presenceStatusText={presence.status_text}
      presenceDetailText={presence.detail_text || ""}
      deskNotes={deskNotes}
      deskNotePositions={deskNotePositions}
      desktopFiles={desktopFiles}
      executingDeskNoteIDs={executingDeskNoteIDs}
      onDesktopClick={() => {
        setSelectedIcon(null);
        setFocusedWin(null);
      }}
      onDesktopContextMenu={(x, y) => setContextMenu({ x, y, data: { kind: "desktop" } })}
      onExecuteDeskNote={handleExecuteDeskNote}
      onDismissDeskNote={handleDismissDeskNote}
      onSelectDesktopFile={setSelectedIcon}
      onOpenDesktopFile={openDesktopFileInWorkspace}
      onOpenDesktopFileContextMenu={(x, y, fileID) => setContextMenu({ x, y, data: { kind: "file", fileID } })}
      onStartDesktopFileDrag={(event, file) => {
        fileDragOffset.current = { x: event.clientX - file.x, y: event.clientY - file.y };
        setPendingFileDrag({ id: file.id, startX: event.clientX, startY: event.clientY });
        setSelectedIcon(file.id);
      }}
      onStartDeskNoteDrag={(event, id) => {
        const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
        noteDragOffset.current = { x: event.clientX - rect.left, y: event.clientY - rect.top };
        setPendingNoteDrag({ id, startX: event.clientX, startY: event.clientY });
      }}
    >
      <HavenFloatingWindows {...sharedFloatingWindowsProps} />
    </DesktopSurface>
  );

  const taskBar = (
    <TaskBar
      dockEdge={dockEdge}
      launcherAppIDs={dockLauncherAppIDs}
      windows={windows}
      focusedWin={focusedWin}
      executionState={executionState}
      pendingApprovalCount={pendingApprovalCount}
      continuityCount={continuityCount}
      systemStatus={systemStatus}
      clock={clock}
      onDockLaunch={handleDockLaunch}
    />
  );

  return (
    <div
      className={`haven-shell haven-shell--dock-${dockEdge}`}
      style={{ "--wails-drop-target": "drop" } as React.CSSProperties}
      onDragOver={() => setDropActive(true)}
      onDragLeave={() => setDropActive(false)}
      onContextMenu={(event) => event.preventDefault()}
    >
      <MenuBar
        openMenu={openMenu}
        executionState={executionState}
        presence={presence}
        systemStatus={systemStatus}
        memoryStatus={memoryStatus}
        clock={clock}
        shellLayout={shellLayout}
        dockEdge={dockEdge}
        onToggleMenu={(label) => setOpenMenu(openMenu === label ? null : label)}
        onAction={handleMenuAction}
      />

      <div className="haven-shell-main">
        {dockEdge === "left" ? taskBar : null}
        <div className="haven-shell-stage">{shellStage}</div>
        {dockEdge === "right" ? taskBar : null}
        {dockEdge === "bottom" ? taskBar : null}
      </div>

      <ContextMenu
        contextMenu={contextMenu}
        threads={threads}
        onClose={() => setContextMenu(null)}
        onRequestRenameThread={(threadID, currentTitle) => setRenameThreadDialog({ threadID, value: currentTitle })}
        onDesktopAction={handleMenuAction}
        onOpenIcon={(iconID) => openWindow(iconID as AppID)}
        onOpenDesktopFile={openDesktopFileInWorkspace}
        onRemoveDesktopFile={removeDesktopFile}
        onExportDesktopFile={exportDesktopFile}
      />

      <DropApprovalDialog pendingDrop={pendingDrop} onAllow={handleDropApprove} onDeny={() => setPendingDrop(null)} />

      {dropActive && !pendingDrop && (
        <div className="drop-overlay">
          <div className="drop-overlay-text">Drop files to import into Haven</div>
        </div>
      )}

      <ToastStack toasts={toasts} onDismiss={(id) => setToasts((previous) => previous.filter((toast) => toast.id !== id))} />

      {renameThreadDialog && (
        <TextInputDialog
          title="Rename Thread"
          description="Give this conversation a clearer name so it is easier to come back to later."
          value={renameThreadDialog.value}
          placeholder="Thread title"
          confirmLabel="Rename"
          busy={renameThreadBusy}
          onChange={(value) => setRenameThreadDialog((previous) => previous ? { ...previous, value } : previous)}
          onConfirm={submitRenameThread}
          onCancel={() => setRenameThreadDialog(null)}
        />
      )}

      {confirmDialog && (
        <ConfirmDialog
          title={
            confirmDialog.kind === "discard_editor_changes" ? "Discard Unsaved Changes?" :
            confirmDialog.kind === "delete_workspace_entry" ? `Delete ${confirmDialog.entryName}?` :
            "Discard Haven Changes?"
          }
          description={
            confirmDialog.kind === "discard_editor_changes" ? "Your unsaved edits will be lost." :
            confirmDialog.kind === "delete_workspace_entry" ? "This removes the file from Haven's workspace." :
            "This restores the imported file to its original version inside Haven."
          }
          detail={confirmDialog.path}
          confirmLabel={
            confirmDialog.kind === "discard_editor_changes" ? "Discard" :
            confirmDialog.kind === "delete_workspace_entry" ? "Delete" :
            "Discard Changes"
          }
          destructive
          busy={confirmBusy}
          onConfirm={handleConfirmDialog}
          onCancel={() => setConfirmDialog(null)}
        />
      )}

      <DiffReviewDialog
        reviewState={reviewDialog}
        busyAction={reviewActionBusy}
        onClose={() => setReviewDialog(null)}
        onExport={handleExportReview}
        onDiscard={() => {
          if (!reviewDialog) return;
          setConfirmDialog({ kind: "discard_review_changes", path: reviewDialog.path });
        }}
      />
    </div>
  );
}
