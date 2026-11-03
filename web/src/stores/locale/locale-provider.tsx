"use client";

import { createContext, useContext, useEffect, useState } from "react";

import { NextIntlClientProvider } from "next-intl";
import { type StoreApi, useStore } from "zustand";

import { getClientCookie, setClientCookie } from "@/lib/cookie.client";
import { isLocale, type Locale, LOCALE_PERSISTENCE_KEY } from "@/i18n/locale-config";
import { MESSAGES } from "@/i18n/messages";

import { createLocaleStore, type LocaleState } from "./locale-store";

const LocaleStoreContext = createContext<StoreApi<LocaleState> | null>(null);

export function LocaleStoreProvider({ children, locale }: { children: React.ReactNode; locale: Locale }) {
  const [store] = useState<StoreApi<LocaleState>>(() => createLocaleStore({ locale }));

  useEffect(() => {
    const cookieValue = getClientCookie(LOCALE_PERSISTENCE_KEY);
    if (isLocale(cookieValue) && cookieValue !== store.getState().locale) {
      store.setState({ locale: cookieValue });
    }
  }, [store]);

  const currentLocale = useStore(store, (s) => s.locale);

  useEffect(() => {
    document.documentElement.lang = currentLocale;
    document.title = MESSAGES[currentLocale].appMeta.title;
  }, [currentLocale]);

  return (
    <LocaleStoreContext.Provider value={store}>
      <NextIntlClientProvider locale={currentLocale} messages={MESSAGES[currentLocale]}>
        {children}
      </NextIntlClientProvider>
    </LocaleStoreContext.Provider>
  );
}

export function useLocaleStore<T>(selector: (state: LocaleState) => T): T {
  const store = useContext(LocaleStoreContext);
  if (!store) {
    throw new Error("useLocaleStore must be used within LocaleStoreProvider");
  }
  return useStore(store, selector);
}

export function useSetLocale() {
  const setLocale = useLocaleStore((s) => s.setLocale);
  return (locale: Locale) => {
    setLocale(locale);
    setClientCookie(LOCALE_PERSISTENCE_KEY, locale);
  };
}
