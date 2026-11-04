"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowUpCircleIcon } from "lucide-react";
import { useTranslations } from "next-intl";

import { api } from "@/lib/api";

/**
 * The top bar "new version" hint: queried once on full page load, and lit up next to the version
 * number when an update exists; clicking it jumps straight to the "Version and updates" card on the system settings page.
 *
 * The backend caches GitHub query results for 30 minutes, so querying on every mount here is safe
 * - the unauthenticated GitHub API allows only 60 requests/hour/IP; without that cache, opening a few
 * tabs would exhaust the quota, and then a real update could no longer be looked up.
 *
 * Query failures are always silent: the top bar is not the place for errors; the settings page's "Check for updates" shows the reason.
 */
export function UpdateBadge() {
  const t = useTranslations("updateBadge");
  const [latest, setLatest] = React.useState("");

  React.useEffect(() => {
    let alive = true;
    api
      .checkUpdate()
      .then((r) => {
        // has_update already includes the "version is comparable" check, so dev builds never show this hint.
        if (alive && r.has_update && r.latest) setLatest(r.latest.replace(/^v(?=\d)/, ""));
      })
      .catch(() => {
        // Silent: being offline / GitHub rate limiting should not pop up an error in the top bar.
      });
    return () => {
      alive = false;
    };
  }, []);

  if (!latest) return null;

  return (
    <Link
      href="/system/settings"
      title={t("title", { version: latest })}
      className="inline-flex items-center gap-1.5 rounded-full bg-primary px-2.5 py-1 font-medium text-primary-foreground text-xs transition-opacity hover:opacity-90"
    >
      {/* Pulsing dot: the top bar has many elements and plain text is easy to overlook; the animation makes it visible at a glance. */}
      <span className="relative flex size-1.5">
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-primary-foreground opacity-75" />
        <span className="relative inline-flex size-1.5 rounded-full bg-primary-foreground" />
      </span>
      <ArrowUpCircleIcon className="size-3.5" />
      <span className="hidden sm:inline">{t("label", { version: latest })}</span>
      <span className="sm:hidden">{t("labelShort")}</span>
    </Link>
  );
}
