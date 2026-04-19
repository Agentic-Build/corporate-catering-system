import { writable } from "svelte/store";

export type ToastTone = "success" | "error" | "info";

export interface ToastEntry {
  id: number;
  tone: ToastTone;
  message: string;
}

const TOAST_TTL_MS = 6000;
const MAX_TOASTS = 5;

let counter = 0;

function createToastStore() {
  const { subscribe, update } = writable<ToastEntry[]>([]);

  function push(tone: ToastTone, message: string): void {
    counter += 1;
    const entry: ToastEntry = { id: counter, tone, message };
    update((list) => [...list, entry].slice(-MAX_TOASTS));
    setTimeout(() => dismiss(entry.id), TOAST_TTL_MS);
  }

  function dismiss(id: number): void {
    update((list) => list.filter((t) => t.id !== id));
  }

  return {
    subscribe,
    success: (msg: string) => push("success", msg),
    error: (msg: string) => push("error", msg),
    info: (msg: string) => push("info", msg),
    dismiss
  };
}

export const toasts = createToastStore();
