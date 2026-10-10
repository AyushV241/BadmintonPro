"use client";

// Whether this browser shows admin mode. A UI preference only: the server
// decides who may call /api/admin (see requireAdmin in the backend).

import { useSyncExternalStore } from "react";

const KEY = "bp-admin-mode";
const CHANGE_EVENT = "bp:admin-mode";

function read(): boolean {
  try {
    return window.localStorage.getItem(KEY) === "1";
  } catch {
    return false; // storage blocked (private mode, settings)
  }
}

function subscribe(onChange: () => void) {
  window.addEventListener("storage", onChange); // other tabs
  window.addEventListener(CHANGE_EVENT, onChange); // this tab
  return () => {
    window.removeEventListener("storage", onChange);
    window.removeEventListener(CHANGE_EVENT, onChange);
  };
}

export function setAdminMode(on: boolean) {
  try {
    if (on) window.localStorage.setItem(KEY, "1");
    else window.localStorage.removeItem(KEY);
  } catch {
    // Not remembered, but still switched for this page view below.
  }
  window.dispatchEvent(new Event(CHANGE_EVENT));
}

export function useAdminMode(): boolean {
  return useSyncExternalStore(subscribe, read, () => false);
}
