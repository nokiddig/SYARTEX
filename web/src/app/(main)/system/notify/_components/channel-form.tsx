"use client";

import { CheckIcon } from "lucide-react";
import { useTranslations } from "next-intl";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { NotificationFilter } from "@/lib/types";

// asText / inputType are the value helpers inside this file (strongly tied to control rendering), not put in channel-fields.
import { type FieldDef, type FieldKind, SEVERITY_OPTIONS } from "./channel-fields";

// asText renders any config value into a string usable by an input box.
// config comes from JSON, so the value may be string / number / boolean / array / null,
// here we only care about "can it be put into a text box"; the concrete serialization is left to buildConfig.
function asText(v: unknown): string {
  if (typeof v === "string") return v;
  if (v === null || v === undefined) return "";
  return String(v);
}

// inputType maps the field type to the input's type attribute.
function inputType(kind: FieldKind): "text" | "password" | "number" {
  if (kind === "password") return "password";
  if (kind === "number") return "number";
  return "text";
}

// ConfigField renders the matching control for a field definition.
//
// The handling of masked fields is the only subtlety here: the input box does **not show** the masked value itself, and shows only a line
// "Saved" hint. That way the UI has a single rule - text in the box was typed by the user,
// an empty box is an empty value. If "__masked__:…abc123" were stuffed into the input box, the user would think it is placeholder text they have to
// delete themselves, which makes it easier to clear a credential by mistake.
export function ConfigField({
  def,
  value,
  isSecret,
  onChange,
}: {
  def: FieldDef;
  value: unknown;
  isSecret: boolean;
  onChange: (v: unknown) => void;
}) {
  const t = useTranslations("systemNotifyChannelForm");
  const tf = useTranslations("systemNotifyChannelFields");
  const id = `n-cfg-${def.key}`;
  const raw = asText(value);
  // The masked value echoed by the backend: of the form "__masked__:…abc123", with the tail being an identifiable fragment of the original value.
  const masked = isSecret && raw.startsWith("__masked__");
  const maskedTail = masked ? (raw.split("…")[1] ?? "") : "";

  if (def.kind === "switch") {
    return (
      <div className="flex items-center gap-2 text-sm">
        <Switch checked={value === true} onCheckedChange={onChange} aria-label={def.label} />
        {tf(def.label)}
        {def.help && <span className="text-muted-foreground">{t("helpWrap", { text: tf(def.help) })}</span>}
      </div>
    );
  }

  if (def.kind === "select") {
    return (
      <div className="grid gap-2">
        <Label>{tf(def.label)}</Label>
        <Select value={raw || def.options?.[0]?.value} onValueChange={onChange}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(def.options ?? []).map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {tf(o.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    );
  }

  // Controls are dispatched by field type. An if chain is used instead of nested ternaries because four kinds of controls must be told apart here,
  // and three levels of ternaries already make you stop to count parentheses.
  function control() {
    if (def.kind === "textarea" || def.kind === "kv") {
      return (
        <Textarea
          id={id}
          className="font-mono"
          placeholder={def.placeholder}
          value={masked ? "" : raw}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    }
    if (def.kind === "list") {
      return (
        <Input
          id={id}
          value={Array.isArray(value) ? (value as string[]).join(", ") : raw}
          onChange={(e) => onChange(e.target.value)}
          placeholder={def.placeholder}
        />
      );
    }
    return (
      <Input
        id={id}
        className={def.kind === "text" ? "font-mono" : ""}
        type={inputType(def.kind)}
        placeholder={def.placeholder}
        value={masked ? "" : raw}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  const hint = masked ? (
    <p className="text-muted-foreground flex items-center gap-1 text-xs">
      <CheckIcon className="size-3" />
      {maskedTail ? t("savedWithTail", { tail: maskedTail }) : t("saved")} {t("maskedHint")}
    </p>
  ) : (
    def.help && <p className="text-muted-foreground text-xs">{tf(def.help)}</p>
  );

  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{tf(def.label)}</Label>
      {control()}
      {hint}
    </div>
  );
}

// FilterSummary summarizes the filter conditions into one line, so the card shows what this channel pushes without expanding.
export function FilterSummary({ filter }: { filter: NotificationFilter }) {
  const t = useTranslations("systemNotifyChannelForm");
  const tf = useTranslations("systemNotifyChannelFields");
  const parts: string[] = [];
  if (filter.min_severity) {
    const opt = SEVERITY_OPTIONS.find((o) => o.value === filter.min_severity);
    parts.push(opt ? tf(opt.label) : filter.min_severity);
  }
  if (filter.vulnclass_include?.length) parts.push(t("includeWords", { count: filter.vulnclass_include.length }));
  if (filter.vulnclass_exclude?.length) parts.push(t("excludeWords", { count: filter.vulnclass_exclude.length }));
  if (filter.task_ids?.length) parts.push(t("taskCount", { count: filter.task_ids.length }));
  if (filter.asset_ids?.length) parts.push(t("assetCount", { count: filter.asset_ids.length }));
  if (filter.on_status_change) parts.push(t("statusChange"));
  if (parts.length === 0) {
    return <p className="text-muted-foreground text-sm">{t("allFindings")}</p>;
  }
  return <p className="text-muted-foreground text-sm">{parts.join(" · ")}</p>;
}
