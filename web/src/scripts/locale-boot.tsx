/**
 * Boot script that reads the persisted locale cookie and applies it to
 * <html lang> before hydration, mirroring ThemeBootScript so there is no
 * mismatch between the attribute set here and the one LocaleStoreProvider
 * settles on after mount.
 */
import { DEFAULT_LOCALE, LOCALE_PERSISTENCE_KEY, LOCALES } from "@/i18n/locale-config";

export function LocaleBootScript() {
  const locales = JSON.stringify(LOCALES);
  const code = `
    (function () {
      try {
        var root = document.documentElement;
        var LOCALES = ${locales};
        var KEY = ${JSON.stringify(LOCALE_PERSISTENCE_KEY)};

        function readCookie(name) {
          var match = document.cookie.split("; ").find(function(c) {
            return c.startsWith(name + "=");
          });
          return match ? decodeURIComponent(match.split("=")[1]) : null;
        }

        var raw = readCookie(KEY);
        var locale = (raw && LOCALES.indexOf(raw) !== -1) ? raw : ${JSON.stringify(DEFAULT_LOCALE)};
        root.setAttribute("lang", locale);
      } catch (e) {
        console.warn("LocaleBootScript error:", e);
      }
    })();
  `;

  /* biome-ignore lint/security/noDangerouslySetInnerHtml: required for pre-hydration boot script */
  return <script dangerouslySetInnerHTML={{ __html: code }} />;
}
