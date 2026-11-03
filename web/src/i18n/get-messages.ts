// For plain (non-component, non-hook) client modules that need a translated
// string outside of React render — e.g. an Error message thrown from a fetch
// wrapper. Components and hooks should use `useTranslations` from next-intl
// instead; reach for this only where a hook can't be called.
import { getClientCookie } from "@/lib/cookie.client";
import { DEFAULT_LOCALE, isLocale, LOCALE_PERSISTENCE_KEY } from "./locale-config";
import { MESSAGES } from "./messages";

export function getCurrentLocale() {
  if (typeof document === "undefined") return DEFAULT_LOCALE;
  const raw = getClientCookie(LOCALE_PERSISTENCE_KEY);
  return isLocale(raw) ? raw : DEFAULT_LOCALE;
}

export function getCurrentMessages() {
  return MESSAGES[getCurrentLocale()];
}
