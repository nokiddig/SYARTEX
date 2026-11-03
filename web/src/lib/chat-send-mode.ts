"use client";

import * as React from "react";

import { getCurrentMessages } from "@/i18n/get-messages";

import { getLocalStorageValue, setLocalStorageValue } from "@/lib/local-storage.client";

// Send / newline keys for the chat input. Pure front-end preference: stored only in localStorage, not persisted to the account
// or synced across accounts, so it must be set again in each browser. See issue #39: 0.3.2 changed Ctrl+Enter sending to Enter sending,
// and here the old key binding is restored as an option.
export type ChatSendMode = "enter" | "ctrl-enter";

export const CHAT_SEND_MODE_KEY = "artex_chat_send_mode";
export const DEFAULT_CHAT_SEND_MODE: ChatSendMode = "enter";

// `label` is resolved from the current locale on every read (getter), so the
// settings page gets the translated text without any hook.
export const CHAT_SEND_MODE_OPTIONS: { value: ChatSendMode; readonly label: string }[] = (
  ["enter", "ctrl-enter"] as const
).map((value) => ({
  value,
  get label() {
    return getCurrentMessages().libChatSendMode[value];
  },
}));

function parseMode(raw: string | null): ChatSendMode {
  return raw === "ctrl-enter" || raw === "enter" ? raw : DEFAULT_CHAT_SEND_MODE;
}

// Subscriber set for the same tab. The localStorage storage event only fires in OTHER tabs,
// so after a change in settings on this page, emit must notify the input boxes on this page; otherwise a refresh is needed.
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

// Returns a string literal; Object.is compares by value, so useSyncExternalStore does not loop.
function getSnapshot(): ChatSendMode {
  return parseMode(getLocalStorageValue(CHAT_SEND_MODE_KEY));
}

// There is no localStorage on the server, so the default is rendered first and getSnapshot corrects it after hydration.
function getServerSnapshot(): ChatSendMode {
  return DEFAULT_CHAT_SEND_MODE;
}

export function useChatSendMode(): ChatSendMode {
  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function setChatSendMode(mode: ChatSendMode) {
  setLocalStorageValue(CHAT_SEND_MODE_KEY, mode);
  for (const listener of listeners) listener();
}

// shouldSubmitOnKey decides whether a keypress should submit.
// isComposing / keyCode 229 means an IME (e.g. Chinese) is choosing a candidate; it must pass through, otherwise Enter to select a word would submit by mistake.
// enter mode only excludes Shift, matching the 0.3.2 behaviour exactly, so users who do not change the setting keep the same feel.
// ctrl-enter mode accepts both Ctrl and Cmd (macOS).
export function shouldSubmitOnKey(e: React.KeyboardEvent, mode: ChatSendMode): boolean {
  if (e.key !== "Enter") return false;
  if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return false;
  if (mode === "ctrl-enter") return e.ctrlKey || e.metaKey;
  return !e.shiftKey;
}
