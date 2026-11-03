import { createStore } from "zustand/vanilla";

import { DEFAULT_LOCALE, type Locale } from "@/i18n/locale-config";

export type LocaleState = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
};

export const createLocaleStore = (init?: Partial<LocaleState>) =>
  createStore<LocaleState>()((set) => ({
    locale: init?.locale ?? DEFAULT_LOCALE,
    setLocale: (locale) => set({ locale }),
  }));
