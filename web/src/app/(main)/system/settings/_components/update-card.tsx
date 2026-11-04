"use client";

import * as React from "react";

import {
  CheckCircle2Icon,
  DownloadIcon,
  ExternalLinkIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { api, sseUrl } from "@/lib/api";
import type { UpdateCheck, UpdateProgress } from "@/lib/types";

/** The longest time to wait for the new version to come online. An upgrade goes through three process starts (stage → swap → new version),
 *  each taking seconds; three minutes is enough to cover a slow disk and a Docker container rebuild. */
const RESTART_TIMEOUT_MS = 180_000;

function humanSize(n?: number): string {
  if (!n || n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export function UpdateCard() {
  const t = useTranslations("systemSettingsUpdateCard");
  const [info, setInfo] = React.useState<UpdateCheck | null>(null);
  const [checking, setChecking] = React.useState(true);
  const [progress, setProgress] = React.useState<UpdateProgress | null>(null);
  // Kept separate from progress: once staging completes the process is gone and the SSE drops, at which point we must switch to polling /api/health.
  const [restarting, setRestarting] = React.useState(false);
  const [busy, setBusy] = React.useState(false);

  // quiet also decides whether to bypass the backend cache: the automatic check on entering the page uses the cache (the top bar just queried),
  // while the user manually clicking "Check for updates" forces going back to the origin, otherwise a just-released version would only show once the cache expires.
  const check = React.useCallback((quiet = false) => {
    setChecking(true);
    api
      .checkUpdate(!quiet)
      .then((r) => {
        setInfo(r);
        if (!quiet) {
          if (r.error) toast.error(t("checkFailed", { message: r.error }));
          else if (r.has_update) toast.success(t("foundNew", { version: r.latest ?? "" }));
          else if (r.comparable) toast.success(t("alreadyLatest"));
        }
      })
      .catch((e) => {
        if (!quiet) toast.error(t("checkFailed", { message: (e as Error).message }));
      })
      .finally(() => setChecking(false));
  }, [t]);

  React.useEffect(() => {
    check(true);
  }, [check]);

  // Poll /api/health until the version number changes.
  //
  // The criterion must be "the version changed" rather than "can connect": during the swap the old version briefly comes back up once
  // (that one only swaps artex.new into place and then exits right away), so looking at connectivity alone would misjudge it as success.
  const waitForNewVersion = React.useCallback(async (fromVersion: string) => {
    setRestarting(true);
    const deadline = Date.now() + RESTART_TIMEOUT_MS;
    while (Date.now() < deadline) {
      await sleep(2000);
      try {
        const r = await fetch("/api/health", { cache: "no-store" });
        if (r.ok) {
          const j = (await r.json()) as { version?: string };
          if (j.version && j.version !== fromVersion) {
            toast.success(t("updatedTo", { version: j.version }));
            await sleep(800);
            window.location.reload();
            return;
          }
        }
      } catch {
        // Being unable to connect during the restart window is expected; keep polling.
      }
    }
    setRestarting(false);
    toast.error(t("restartTimeout"));
  }, [t]);

  // Subscribe to update progress. The SSE does not go through Next's /api rewrite (that layer buffers, and events cannot get through).
  const openStream = React.useCallback(
    (fromVersion: string) => {
      const es = new EventSource(sseUrl("/api/update/stream"));
      es.onmessage = (ev) => {
        let p: UpdateProgress;
        try {
          p = JSON.parse(ev.data) as UpdateProgress;
        } catch {
          return;
        }
        setProgress(p);
        if (p.phase === "failed") {
          es.close();
          setBusy(false);
          toast.error(t("updateFailed", { message: p.error || p.message }));
          return;
        }
        if (p.phase === "staged") {
          es.close();
          void waitForNewVersion(fromVersion);
        }
      };
      es.onerror = () => {
        // The SSE necessarily drops when the process exits. If we have already entered the wait-for-restart stage, this is normal;
        // just let the /api/health polling keep deciding.
        es.close();
      };
      return es;
    },
    [waitForNewVersion, t],
  );

  const doUpdate = () => {
    if (!info) return;
    const from = info.current;
    const ok = window.confirm(
      t("confirmUpdate", { version: info.latest ?? "" }) + (info.mode === "docker" ? t("confirmDockerNote") : ""),
    );
    if (!ok) return;

    setBusy(true);
    setProgress({ phase: "downloading", percent: 0, message: t("preparing") });
    const es = openStream(from);
    api.applyUpdate().catch((e) => {
      es.close();
      setBusy(false);
      setProgress(null);
      toast.error(t("startFailed", { message: (e as Error).message }));
    });
  };

  const doRollback = () => {
    if (!info) return;
    if (!window.confirm(t("rollbackConfirm"))) return;
    const from = info.current;
    setBusy(true);
    api
      .rollbackUpdate()
      .then(() => {
        toast.success(t("rolledBack"));
        void waitForNewVersion(from);
      })
      .catch((e) => {
        setBusy(false);
        toast.error(t("rollbackFailed", { message: (e as Error).message }));
      });
  };

  const phase = progress?.phase;
  const showProgress = busy || restarting;
  // Only the download stage yields a real percentage (computed from Content-Length). Verify/extract/wait-for-restart are all
  // stages of unknowable duration; the progress bar is filled and given a pulse animation to mean "busy, but cannot say how long it will take".
  const downloading = !restarting && phase === "downloading";
  const pct = downloading ? Math.max(progress?.percent ?? 0, 0) : 100;

  return (
    // The settings page is a multi-column masonry layout; the card is responsible for its own row spacing and forbids breaking across columns (see the comment in page.tsx).
    <Card className="mb-4 break-inside-avoid md:mb-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <DownloadIcon className="size-4" />
          {t("title")}
        </CardTitle>
        <CardDescription>{t("description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="text-muted-foreground">{t("currentVersion")}</span>
          <Badge variant="secondary" className="font-mono">
            {info?.current ?? "…"}
          </Badge>
          {info && (
            <>
              <Badge variant="outline" className="font-mono">
                {info.os}/{info.arch}
              </Badge>
              <Badge variant="outline">{info.mode === "docker" ? t("modeDocker") : t("modeStandalone")}</Badge>
            </>
          )}
          {info?.latest && (
            <>
              <span className="text-muted-foreground">{t("latestVersion")}</span>
              <Badge variant={info.has_update ? "default" : "secondary"} className="font-mono">
                {info.latest}
              </Badge>
            </>
          )}
          {info?.html_url && (
            <a
              href={info.html_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:underline"
            >
              {t("releaseNotes")} <ExternalLinkIcon className="size-3" />
            </a>
          )}
        </div>

        {info?.boot_notice && (
          <p className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-700 dark:text-amber-400">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {info.boot_notice}
          </p>
        )}

        {info?.error && (
          <p className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {t("unreachable", { error: info.error ?? "" })}
          </p>
        )}

        {info && !info.comparable && info.reason && <p className="text-xs text-muted-foreground">{info.reason}</p>}

        {info?.has_update && info.asset_available === false && (
          <p className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {t("noAsset", { latest: info.latest ?? "", os: info.os, arch: info.arch, asset: info.asset ?? "" })}
          </p>
        )}

        {info?.has_update && info.asset_available !== false && (
          <p className="text-xs text-muted-foreground">
            {t.rich("willDownload", {
              asset: info.asset ?? "",
              size: info.size ? t("sizeSuffix", { size: humanSize(info.size) }) : "",
              mono: (chunks) => <span className="font-mono">{chunks}</span>,
            })}
          </p>
        )}

        {info && !info.has_update && info.comparable && !info.error && (
          <p className="flex items-center gap-2 text-xs text-muted-foreground">
            <CheckCircle2Icon className="size-3.5 text-emerald-600" />
            {t("upToDate")}
          </p>
        )}

        {info?.mode === "docker" && info.has_update && (
          <p className="text-xs text-muted-foreground">
            {t.rich("dockerNote", {
              mono: (chunks) => <span className="font-mono">{chunks}</span>,
            })}
          </p>
        )}

        {showProgress && (
          <div className="space-y-1.5">
            <Progress value={pct} className={downloading ? undefined : "animate-pulse"} />
            <p className="text-xs text-muted-foreground">
              {restarting ? t("restarting") : progress?.message}
            </p>
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => check(false)} disabled={checking || busy || restarting}>
            <RefreshCwIcon className={checking ? "size-4 animate-spin" : "size-4"} />
            {t("checkUpdate")}
          </Button>
          <Button
            size="sm"
            onClick={doUpdate}
            disabled={busy || restarting || !info?.has_update || info?.asset_available === false}
          >
            <DownloadIcon className="size-4" />
            {info?.has_update ? t("updateTo", { version: info.latest ?? "" }) : t("updateNow")}
          </Button>
          {info?.has_backup && (
            <Button variant="ghost" size="sm" onClick={doRollback} disabled={busy || restarting}>
              <RotateCcwIcon className="size-4" />
              {t("rollbackButton")}
            </Button>
          )}
        </div>

        <p className="text-xs text-muted-foreground">
          {t.rich("startupNote", {
            mono: (chunks) => <span className="font-mono">{chunks}</span>,
          })}
        </p>
      </CardContent>
    </Card>
  );
}
