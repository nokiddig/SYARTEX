"use client";

import * as React from "react";

import { useTranslations } from "next-intl";

import { Badge } from "@/components/ui/badge";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import type { ParsedCompanyScopeText } from "@/lib/company-scope";
import type { CompanyScopeKind } from "@/lib/types";

export function ScopeTextEditor({
  id,
  value,
  onValueChange,
  parsed,
  label,
  description,
}: {
  id: string;
  value: string;
  onValueChange: (value: string) => void;
  parsed: ParsedCompanyScopeText;
  label?: string;
  description?: string;
}) {
  const t = useTranslations("scopeTextEditor");
  const resolvedLabel = label ?? t("defaultLabel");
  const resolvedDescription = description ?? t("defaultDescription");
  const SCOPE_KIND_LABELS: Record<CompanyScopeKind, string> = {
    domain: t("kindDomain"),
    ip: t("kindIp"),
    cidr: t("kindCidr"),
    icp: t("kindIcp"),
    keyword: t("kindKeyword"),
  };
  const counts = React.useMemo(() => {
    const result = new Map<CompanyScopeKind, number>();
    for (const rule of parsed.rules) result.set(rule.kind, (result.get(rule.kind) ?? 0) + 1);
    return result;
  }, [parsed.rules]);

  return (
    <Field data-invalid={parsed.errors.length > 0}>
      <FieldLabel htmlFor={id}>{resolvedLabel}</FieldLabel>
      <FieldDescription>{resolvedDescription}</FieldDescription>
      <Textarea
        id={id}
        rows={8}
        value={value}
        aria-invalid={parsed.errors.length > 0}
        placeholder={t("placeholder")}
        className="min-h-36 resize-y font-mono text-sm"
        onChange={(event) => onValueChange(event.target.value)}
      />
      {parsed.rules.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 text-muted-foreground text-xs">
          <span>{t("recognizedCount", { count: parsed.rules.length })}</span>
          {Object.entries(SCOPE_KIND_LABELS).map(([kind, kindLabel]) => {
            const count = counts.get(kind as CompanyScopeKind) ?? 0;
            return count > 0 ? (
              <Badge key={kind} variant="secondary" className="font-mono tabular-nums">
                {kindLabel} {count}
              </Badge>
            ) : null;
          })}
        </div>
      )}
      {parsed.errors.length > 0 && (
        <FieldError>
          {parsed.errors.slice(0, 5).map((item) => (
            <span key={`${item.line}-${item.error}`} className="block">
              {t("lineError", { line: item.line, error: item.error })}
            </span>
          ))}
          {parsed.errors.length > 5 && (
            <span className="block">{t("moreErrors", { count: parsed.errors.length - 5 })}</span>
          )}
        </FieldError>
      )}
    </Field>
  );
}
