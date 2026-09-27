"use client";

// Ties a sheet's open state to the URL hash (`/players#filters`), so that:
// - the device/browser Back button closes the sheet instead of leaving the page;
// - a URL with the hash opens straight into the sheet (shareable, survives refresh).
//
// Opening pushes a history entry tagged with MARK. Closing pops that entry if we
// pushed it; if the user arrived with the hash already in the URL there is
// nothing of ours to pop, so the hash is replaced away instead.

import { useSyncExternalStore } from "react";

const MARK = "__uiBottomSheet";
const CHANGE_EVENT = "ui:bottomsheet";

function subscribe(onChange: () => void) {
  window.addEventListener("popstate", onChange);
  window.addEventListener("hashchange", onChange);
  window.addEventListener(CHANGE_EVENT, onChange);
  return () => {
    window.removeEventListener("popstate", onChange);
    window.removeEventListener("hashchange", onChange);
    window.removeEventListener(CHANGE_EVENT, onChange);
  };
}

const currentHash = () => decodeURIComponent(window.location.hash.slice(1));

export function openBottomSheet(hash: string) {
  if (currentHash() === hash) return;
  window.history.pushState({ [MARK]: hash }, "", `#${encodeURIComponent(hash)}`);
  window.dispatchEvent(new Event(CHANGE_EVENT));
}

export function closeBottomSheet(hash: string) {
  if (currentHash() !== hash) return;
  if (window.history.state?.[MARK] === hash) {
    window.history.back(); // fires popstate
  } else {
    window.history.replaceState(null, "", window.location.pathname + window.location.search);
    window.dispatchEvent(new Event(CHANGE_EVENT));
  }
}

/** Open/close the `BottomSheet` with this `hash` from anywhere on the page. */
export function useBottomSheet(hash: string) {
  const active = useSyncExternalStore(subscribe, currentHash, () => "");
  return {
    isOpen: active === hash,
    open: () => openBottomSheet(hash),
    close: () => closeBottomSheet(hash),
  };
}
