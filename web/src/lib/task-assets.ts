import { getCurrentMessages } from "@/i18n/get-messages";
import type { NewAssetType } from "@/lib/types";

const ASSET_TYPE_LABELS: Record<NewAssetType, string> = {
  app: "App",
  endpoint: "Endpoint",
  ip: "IP",
  root_domain: "Root domain",
  service: "Service",
  subdomain: "Subdomain",
};

const TASK_ASSET_SOURCE_LABELS: Record<string, string> = {
  agent: "Agent discovered",
  anchor: "Blackboard anchor",
  api: "Asset API",
  company: "Company association",
  legacy: "Legacy association",
  manual: "Manually added",
  system: "System association",
  task: "Task initialization",
};

export function taskAssetTypeLabel(type: NewAssetType): string {
  const translated = (getCurrentMessages().libTaskAssets as Record<string, string>)[`type.${type}`];
  return translated ?? ASSET_TYPE_LABELS[type];
}

export function taskAssetSourceLabel(source: string): string {
  const translated = (getCurrentMessages().libTaskAssets as Record<string, string>)[`source.${source}`];
  return translated ?? TASK_ASSET_SOURCE_LABELS[source] ?? source;
}
