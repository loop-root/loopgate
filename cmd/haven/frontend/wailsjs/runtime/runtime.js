// Wails runtime stubs — wraps the injected window.runtime object.

export function EventsOn(eventName, callback) {
  return window.runtime.EventsOn(eventName, callback);
}

export function EventsOff(eventName) {
  window.runtime.EventsOff(eventName);
}

export function EventsEmit(eventName, ...data) {
  window.runtime.EventsEmit(eventName, ...data);
}

export function OnFileDrop(callback, useDropTarget) {
  return window.runtime.OnFileDrop(callback, useDropTarget);
}

export function OnFileDropOff() {
  return window.runtime.OnFileDropOff();
}
