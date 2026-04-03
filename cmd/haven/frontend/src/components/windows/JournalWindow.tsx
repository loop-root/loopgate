import type {
  JournalEntryResponse,
  JournalEntrySummary,
} from "../../../wailsjs/go/main/HavenApp";

interface JournalWindowProps {
  entries: JournalEntrySummary[];
  selectedPath: string | null;
  activeEntry: JournalEntryResponse | null;
  loadingList: boolean;
  loadingEntry: boolean;
  onRefresh: () => void;
  onSelectEntry: (path: string) => void;
}

function isJournalTimeHeader(line: string): boolean {
  return line.startsWith("--- ") && line.endsWith(" ---");
}

export default function JournalWindow({
  entries,
  selectedPath,
  activeEntry,
  loadingList,
  loadingEntry,
  onRefresh,
  onSelectEntry,
}: JournalWindowProps) {
  return (
    <div className="journal-app">
      <div className="app-hero journal-hero">
        <div className="app-kicker">Journal</div>
        <div className="app-title">Morph's quieter private reflections live here.</div>
        <div className="app-subtitle">
          A paper-like reading room for the traces Morph leaves behind when Haven has been lived in for a while.
        </div>
        <div className="app-summary-pills">
          <span className="app-summary-pill">{entries.length} day{entries.length === 1 ? "" : "s"}</span>
          {activeEntry && (
            <span className="app-summary-pill">{activeEntry.entry_count} reflection{activeEntry.entry_count === 1 ? "" : "s"}</span>
          )}
          {selectedPath && <span className="app-summary-pill">selected</span>}
        </div>
      </div>

      <div className="journal-sidebar">
        <div className="journal-sidebar-header">
          <div>
            <div className="journal-sidebar-title">Journal</div>
            <div className="journal-sidebar-subtitle">Morph's private reflections</div>
          </div>
          <button className="ws-tb-btn" onClick={onRefresh} title="Refresh">↻</button>
        </div>

        <div className="journal-entry-list">
          {loadingList ? (
            <div className="journal-empty">Loading journal...</div>
          ) : entries.length === 0 ? (
            <div className="journal-empty">
              <div className="journal-empty-title">Nothing here yet</div>
              <div className="journal-empty-copy">Leave Haven open for a while and Morph will start leaving quiet traces behind.</div>
            </div>
          ) : (
            entries.map((entry) => (
              <div
                key={entry.path}
                className={`journal-list-entry ${selectedPath === entry.path ? "active" : ""}`}
                onClick={() => onSelectEntry(entry.path)}
              >
                <div className="journal-list-title">{entry.title}</div>
                <div className="journal-list-meta">
                  {entry.entry_count} {entry.entry_count === 1 ? "entry" : "entries"}
                </div>
                <div className="journal-list-preview">{entry.preview}</div>
              </div>
            ))
          )}
        </div>
      </div>

      <div className="journal-main">
        {loadingEntry ? (
          <div className="journal-reader-empty">Opening journal entry...</div>
        ) : activeEntry ? (
          <>
            <div className="journal-reader-header">
              <div className="journal-reader-title">{activeEntry.title}</div>
              <div className="journal-reader-meta">
                {activeEntry.entry_count} {activeEntry.entry_count === 1 ? "reflection" : "reflections"}
              </div>
            </div>
            <div className="journal-reader-body">
              {activeEntry.content.split("\n").map((line, index) => (
                <div
                  key={`${index}-${line}`}
                  className={`journal-reader-line ${isJournalTimeHeader(line.trim()) ? "journal-reader-line-header" : ""}`}
                >
                  {line || "\u00A0"}
                </div>
              ))}
            </div>
          </>
        ) : (
          <div className="journal-reader-empty">Select a journal day to read what Morph left behind.</div>
        )}
      </div>
    </div>
  );
}
