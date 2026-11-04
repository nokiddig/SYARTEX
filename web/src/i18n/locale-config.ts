export const LOCALES = ["zh", "en"] as const;

export type Locale = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_LABELS: Record<Locale, string> = {
  // intentionally Chinese: language endonym shown in the language switcher
  zh: "中文",
  en: "English",
};

export const LOCALE_PERSISTENCE_KEY = "locale";

export function isLocale(value: string | null | undefined): value is Locale {
  return !!value && (LOCALES as readonly string[]).includes(value);
}
