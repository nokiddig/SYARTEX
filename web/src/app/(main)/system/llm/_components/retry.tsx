"use client";

// Shared pieces of the LLM retry configuration: the "count + interval" of each of the five retry layers.
//
// The five layers from inside out: connect (SDK) → empty response (SDK) → same-provider safety window → round-robin circuit breaker → intent rerun.
// The first three layers follow the endpoint, so every model configuration can override the global default; the last two are process-level, with a single global copy.
//
// All inputs follow the same "leave empty = not configured" semantics, consistent with the backend db.RetryRule:
//   count      empty/0 = use the built-in default | -1 = disable this retry layer | >0 = use this count
//   interval   empty/0 = use the layer's original exponential backoff | >0 = switch to this fixed interval in milliseconds

import * as React from "react";

import { Loader2Icon, SaveIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { LLMRetryOverride, LLMRetryPolicy, LLMRetryRule } from "@/lib/types";

export const ZERO_RULE: LLMRetryRule = { attempts: 0, interval_ms: 0 };
export const ZERO_OVERRIDE: LLMRetryOverride = {
  connect: ZERO_RULE,
  empty: ZERO_RULE,
  stream: ZERO_RULE,
};
const ZERO_POLICY: LLMRetryPolicy = {
  ...ZERO_OVERRIDE,
  breaker: ZERO_RULE,
  intent: ZERO_RULE,
};

// Built-in default count of each layer (shown in the placeholder); all copy lives under the systemLlmRetry namespace.
const RETRY_LAYERS = {
  connect: { defAttempts: 3 },
  empty: { defAttempts: 2 },
  stream: { defAttempts: 2 },
  breaker: { defAttempts: 3 },
  intent: { defAttempts: 2 },
} as const;

type LayerKey = keyof typeof RETRY_LAYERS;

/** Milliseconds in human terms, used only to echo next to the input so the user does not have to count zeros. */
function humanMs(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${Number((ms / 60_000).toFixed(2))}min`;
}

/** Controlled number input: empty string ↔ 0; intermediate states ("-", "1e") stay as-is locally and do not disturb the parent. */
function NumField({
  id,
  value,
  onChange,
  placeholder,
  min,
}: {
  id: string;
  value: number;
  onChange: (n: number) => void;
  placeholder: string;
  min: number;
}) {
  const [text, setText] = React.useState(value === 0 ? "" : String(value));
  // Catch up when the parent swaps in a whole new set of values (a policy was read, the configuration was switched); typing by oneself never gets here,
  // because by then value already equals the result of parsing the local text.
  React.useEffect(() => {
    const incoming = value === 0 ? "" : String(value);
    setText((cur) => (Number(cur || 0) === value ? cur : incoming));
  }, [value]);
  return (
    <Input
      id={id}
      type="number"
      min={min}
      className="w-28 shrink-0"
      value={text}
      placeholder={placeholder}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        onChange(e.target.value.trim() === "" || !Number.isFinite(n) ? 0 : Math.trunc(n));
      }}
    />
  );
}

/** The two knobs of one retry layer. idPrefix keeps the label htmlFor intact when it appears several times on the same page. */
export function RetryRuleFields({
  layer,
  idPrefix,
  value,
  onChange,
  compact,
}: {
  layer: LayerKey;
  idPrefix: string;
  value: LLMRetryRule;
  onChange: (r: LLMRetryRule) => void;
  /** true = the compact version in the configuration drawer: omits the expanded description and keeps only the one sentence "which errors reach this layer" */
  compact?: boolean;
}) {
  const t = useTranslations("systemLlmRetry");
  const defAttempts = RETRY_LAYERS[layer].defAttempts;
  const human = humanMs(value.interval_ms);
  return (
    <div className={compact ? "grid gap-2" : "grid gap-3 rounded-lg border p-3"}>
      <div className="grid gap-0.5">
        <div className="flex flex-wrap items-baseline gap-2">
          <Label className="text-sm">{t(`layers.${layer}.title`)}</Label>
          <span className="text-muted-foreground text-xs">{t(`layers.${layer}.where`)}</span>
        </div>
        {/* Which errors reach this layer, down to the status code - if you filled in the knobs but see no effect, most likely the errors do not land in this layer at all. */}
        <p className="text-muted-foreground text-xs">
          <span className="font-medium text-foreground">{t("triggerLabel")}</span>: {t(`layers.${layer}.trigger`)}
        </p>
        {!compact && (
          <p className="text-muted-foreground text-xs">
            <span className="font-medium text-foreground">{t("skipsLabel")}</span>: {t(`layers.${layer}.skips`)}
          </p>
        )}
        {!compact && <p className="text-muted-foreground text-xs">{t(`layers.${layer}.desc`)}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-n`} className="text-muted-foreground text-xs">
            {t(`layers.${layer}.attemptsLabel`)}
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-n`}
            min={-1}
            value={value.attempts}
            placeholder={t("defaultAttempts", { count: defAttempts })}
            onChange={(n) => onChange({ ...value, attempts: n })}
          />
        </div>
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-ms`} className="text-muted-foreground text-xs">
            {t("intervalMs")}
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-ms`}
            min={0}
            value={value.interval_ms}
            placeholder={t("defaultBackoff")}
            onChange={(n) => onChange({ ...value, interval_ms: n })}
          />
          <span className="text-muted-foreground text-xs">
            {human ? t("fixedValue", { value: human }) : t(`layers.${layer}.defInterval`)}
          </span>
        </div>
      </div>
      {!compact && (
        <p className="text-muted-foreground text-xs">
          {t("leaveEmptyHint", { hint: t(`layers.${layer}.offHint`) })}
        </p>
      )}
    </div>
  );
}

/** The three override layers in the model configuration drawer (the three layers that follow the endpoint). */
export function ProfileRetryFields({
  value,
  onChange,
}: {
  value: LLMRetryOverride;
  onChange: (o: LLMRetryOverride) => void;
}) {
  const t = useTranslations("systemLlmRetry");
  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div className="grid gap-0.5">
        <Label className="text-sm">{t("overrideTitle")}</Label>
        <p className="text-muted-foreground text-xs">{t("overrideDesc")}</p>
      </div>
      {(["connect", "empty", "stream"] as const).map((k) => (
        <div key={k} className="border-t pt-3 first:border-t-0 first:pt-0">
          <RetryRuleFields
            compact
            layer={k}
            idPrefix="pf"
            value={value[k]}
            onChange={(r) => onChange({ ...value, [k]: r })}
          />
        </div>
      ))}
    </div>
  );
}

/** The "Retry & backoff" tab: the global defaults of the five layers. */
export function RetryPolicyPanel() {
  const t = useTranslations("systemLlmRetry");
  const [policy, setPolicy] = React.useState<LLMRetryPolicy>(ZERO_POLICY);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const p = await api.llmRetryPolicy();
      setPolicy({ ...ZERO_POLICY, ...p });
    } catch (e) {
      toast.error(t("loadFailed", { message: (e as Error).message }));
    } finally {
      setLoading(false);
    }
  }, [t]);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function save() {
    if (saving) return;
    setSaving(true);
    try {
      // The backend clamps out-of-range values back into the range and returns them; refresh directly with the returned values, so what you see is what is stored.
      const saved = await api.saveLLMRetryPolicy(policy);
      setPolicy({ ...ZERO_POLICY, ...saved });
      toast.success(t("saved"));
    } catch (e) {
      toast.error(t("saveFailed", { message: (e as Error).message }));
    } finally {
      setSaving(false);
    }
  }

  const set = (k: LayerKey) => (r: LLMRetryRule) => setPolicy((p) => ({ ...p, [k]: r }));

  if (loading) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-dashed p-10 text-muted-foreground text-sm">
        <Loader2Icon className="size-4 animate-spin" /> {t("policyLoading")}
      </div>
    );
  }

  return (
    <div className="grid gap-4">
      <div className="rounded-lg border bg-muted/30 p-3 text-muted-foreground text-xs leading-relaxed">
        {t.rich("intro", {
          hl: (chunks) => <span className="text-foreground">{chunks}</span>,
        })}
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {(Object.keys(RETRY_LAYERS) as LayerKey[]).map((k) => (
          <RetryRuleFields key={k} layer={k} idPrefix="gl" value={policy[k]} onChange={set(k)} />
        ))}
      </div>

      <div className="flex gap-2">
        <Button onClick={save} disabled={saving}>
          {saving ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
          {t("save")}
        </Button>
        <Button variant="outline" onClick={() => setPolicy(ZERO_POLICY)} disabled={saving}>
          {t("resetAll")}
        </Button>
      </div>
    </div>
  );
}
