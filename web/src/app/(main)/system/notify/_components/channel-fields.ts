// Channel field table and config-value parsing utilities.
//
// Kept separate from the page because this one is **data** rather than a view: it describes which fields each kind of channel has,
// which control each should use, and the two-way conversion between form text and config values (JSON).
// With a standalone file, adding a channel only touches this file, and the page itself need not change.
//
// Note on copy: this file is a plain module (it cannot call useTranslations), so the
// label / help / option copy here all store only a "translation key", the key pointing into the systemNotifyChannelFields
// namespace; the caller (channel-form.tsx / page.tsx) resolves it with t() into the current language.
// Display name and blurb of each channel type. Kept in the frontend because it only affects copy and the backend need not know.
export const KIND_LABEL: Record<string, string> = {
  dingtalk: "kind.dingtalk",
  feishu: "kind.feishu",
  wecom: "kind.wecom",
  webhook: "kind.webhook",
  telegram: "kind.telegram",
  email: "kind.email",
};

// Config field definitions of each channel.
//
// A frontend field table is deliberately kept here, instead of having the backend send down a schema: the backend is only responsible for
// Validate (required/format), while the UI needs layout and control types - the two are not the same concern.
// The only coupling point is secret_keys - which fields should render as password boxes is given by the backend,
// because only the channel implementation knows which values count as credentials (WeCom's whole Webhook is a credential,
// whereas for DingTalk it is only one secret among them). A missing entry here when adding a channel only leaves the form blank,
// and does not fail silently (hasFields below will flag it).
//
// label / help / options[].label are all translation keys (see the file header note).
export type FieldKind = "text" | "password" | "number" | "select" | "textarea" | "switch" | "kv" | "list";
export interface FieldDef {
  key: string;
  label: string;
  kind: FieldKind;
  placeholder?: string;
  help?: string;
  options?: { value: string; label: string }[];
}
export const CHANNEL_FIELDS: Record<string, FieldDef[]> = {
  dingtalk: [
    {
      key: "webhook",
      label: "dingtalk.webhook.label",
      kind: "text",
      placeholder: "https://oapi.dingtalk.com/robot/send?access_token=...",
    },
    {
      key: "secret",
      label: "dingtalk.secret.label",
      kind: "password",
      help: "dingtalk.secret.help",
    },
  ],
  feishu: [
    {
      key: "webhook",
      label: "feishu.webhook.label",
      kind: "text",
      placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/...",
    },
    { key: "secret", label: "feishu.secret.label", kind: "password", help: "feishu.secret.help" },
  ],
  wecom: [
    {
      key: "webhook",
      label: "wecom.webhook.label",
      kind: "text",
      placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...",
    },
  ],
  webhook: [
    { key: "url", label: "webhook.url.label", kind: "text", placeholder: "https://your-endpoint.example.com/hook" },
    {
      key: "method",
      label: "webhook.method.label",
      kind: "select",
      options: [
        { value: "POST", label: "webhook.method.options.POST" },
        { value: "PUT", label: "webhook.method.options.PUT" },
        { value: "PATCH", label: "webhook.method.options.PATCH" },
        { value: "GET", label: "webhook.method.options.GET" },
      ],
    },
    { key: "headers", label: "webhook.headers.label", kind: "kv", help: "webhook.headers.help" },
    {
      key: "body_template",
      label: "webhook.body_template.label",
      kind: "textarea",
      help: "webhook.body_template.help",
    },
  ],
  telegram: [
    { key: "bot_token", label: "telegram.bot_token.label", kind: "password", placeholder: "123456:ABC-DEF..." },
    { key: "chat_id", label: "telegram.chat_id.label", kind: "text", placeholder: "-1001234567890" },
    {
      key: "base_url",
      label: "telegram.base_url.label",
      kind: "text",
      placeholder: "https://api.telegram.org",
      help: "telegram.base_url.help",
    },
  ],
  email: [
    { key: "host", label: "email.host.label", kind: "text", placeholder: "smtp.example.com" },
    {
      key: "port",
      label: "email.port.label",
      kind: "number",
      placeholder: "587",
      help: "email.port.help",
    },
    { key: "username", label: "email.username.label", kind: "text" },
    { key: "password", label: "email.password.label", kind: "password" },
    { key: "from", label: "email.from.label", kind: "text", placeholder: "artex@example.com" },
    { key: "to", label: "email.to.label", kind: "list", help: "email.to.help" },
    { key: "tls", label: "email.tls.label", kind: "switch", help: "email.tls.help" },
  ],
};

// value is the filter value (consistent with the backend), label is a translation key.
export const SEVERITY_OPTIONS = [
  { value: "", label: "severity.any" },
  { value: "low", label: "severity.low" },
  { value: "medium", label: "severity.medium" },
  { value: "high", label: "severity.high" },
  { value: "critical", label: "severity.critical" },
];

export type ChannelForm = {
  name: string;
  kind: string;
  mode: "realtime" | "digest";
  enabled: boolean;
  ratePerMin: string;
  config: Record<string, unknown>;
  minSeverity: string;
  includeText: string;
  excludeText: string;
  taskIDsText: string;
  assetIDsText: string;
  onStatusChange: boolean;
};

export const emptyForm = (kind: string): ChannelForm => ({
  name: "",
  kind,
  mode: "realtime",
  enabled: true,
  ratePerMin: "",
  config: {},
  minSeverity: "",
  includeText: "",
  excludeText: "",
  taskIDsText: "",
  assetIDsText: "",
  onStatusChange: false,
});

// parseKV parses a "one KEY=VALUE per line" textarea.
export function parseKV(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const i = t.indexOf("=");
    if (i > 0) out[t.slice(0, i).trim()] = t.slice(i + 1).trim();
  }
  return out;
}
// parseIDs parses a list of ids separated by commas/whitespace (fullwidth commas are accepted on purpose, since users may type Chinese punctuation).
export function parseIDs(text: string): number[] {
  return text
    .split(/[\s,，]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => Number(s))
    .filter((n) => Number.isFinite(n) && n > 0);
}
// parseKeywords parses a keyword list separated by lines/commas (finding type names may contain spaces, so split by line or comma; fullwidth commas are accepted on purpose).
export function parseKeywords(text: string): string[] {
  return text
    .split(/[\n,，]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}
