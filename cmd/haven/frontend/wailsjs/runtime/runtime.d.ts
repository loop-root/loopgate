export function EventsOn(
  eventName: string,
  callback: (...data: unknown[]) => void
): () => void;

export function EventsOff(eventName: string): void;

export function EventsEmit(eventName: string, ...data: unknown[]): void;

export function OnFileDrop(
  callback: (x: number, y: number, paths: string[]) => void,
  useDropTarget: boolean
): void;

export function OnFileDropOff(): void;
