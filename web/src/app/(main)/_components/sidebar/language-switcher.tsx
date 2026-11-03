"use client";

import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import { LOCALES } from "@/i18n/locale-config";
import { useLocaleStore, useSetLocale } from "@/stores/locale/locale-provider";

export function LanguageSwitcher() {
  const locale = useLocaleStore((s) => s.locale);
  const setLocale = useSetLocale();
  const t = useTranslations("languageSwitcher");

  const cycleLocale = () => {
    const currentIndex = LOCALES.indexOf(locale);
    const nextLocale = LOCALES[(currentIndex + 1) % LOCALES.length];
    setLocale(nextLocale);
  };

  return (
    <Button size="sm" variant="ghost" onClick={cycleLocale} aria-label={t("label", { locale: t(locale) })}>
      {t(locale)}
    </Button>
  );
}
