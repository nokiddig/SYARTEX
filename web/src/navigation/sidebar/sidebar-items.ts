import {
  Activity,
  Ban,
  BellRing,
  Bot,
  Brain,
  Bug,
  ClipboardList,
  FolderOpen,
  FolderSync,
  LayoutDashboard,
  type LucideIcon,
  MessageSquare,
  Network,
  Plug,
  Radio,
  ScrollText,
  Settings2,
  ShieldAlert,
  Sparkles,
  Target,
  Terminal,
  Wrench,
} from "lucide-react";

export type NavBadge = "new" | "soon";

export interface NavSubItem {
  id: string;
  title: string;
  url: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

interface NavItemBase {
  id: string;
  title: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

export interface NavMainLinkItem extends NavItemBase {
  url: string;
  subItems?: never;
}

export interface NavMainParentItem extends NavItemBase {
  subItems: NavSubItem[];
}

export type NavMainItem = NavMainLinkItem | NavMainParentItem;

export interface NavGroup {
  id: number;
  label?: string;
  items: NavMainItem[];
}

// `title` and `label` are i18n keys in the "sidebarItems" namespace (resolve with
// useTranslations("sidebarItems")). Item titles use the item id as the key.
export const sidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "groupFeatures",
    items: [
      { id: "dashboard", title: "dashboard", url: "/dashboard", icon: LayoutDashboard },
      { id: "chat", title: "chat", url: "/chat", icon: MessageSquare },
      { id: "tasks", title: "tasks", url: "/function/tasks", icon: Target },
      { id: "findings", title: "findings", url: "/function/findings", icon: Bug },
      { id: "traffic", title: "traffic", url: "/function/traffic", icon: Activity },
      { id: "commands", title: "commands", url: "/function/commands", icon: Terminal },
      { id: "llm-records", title: "llm-records", url: "/function/llm-records", icon: Radio },
      { id: "assets", title: "assets", url: "/function/assets", icon: Network },
      { id: "sync", title: "sync", url: "/function/sync", icon: FolderSync },
      { id: "workspace", title: "workspace", url: "/function/workspace", icon: FolderOpen },
    ],
  },
  {
    id: 2,
    label: "groupSystem",
    items: [
      { id: "llm", title: "llm", url: "/system/llm", icon: Brain },
      { id: "agents", title: "agents", url: "/system/agents", icon: Bot },
      { id: "mcp", title: "mcp", url: "/system/mcp", icon: Plug },
      { id: "skills", title: "skills", url: "/system/skills", icon: Sparkles },
      { id: "tools", title: "tools", url: "/system/tools", icon: Wrench },
      { id: "notify", title: "notify", url: "/system/notify", icon: BellRing },
      { id: "intercept", title: "intercept", url: "/system/intercept", icon: ShieldAlert },
      { id: "asset-intercept", title: "asset-intercept", url: "/system/intercept/assets", icon: Ban },
      { id: "approvals", title: "approvals", url: "/system/intercept/approvals", icon: ClipboardList },
      { id: "logs", title: "logs", url: "/system/logs", icon: ScrollText },
      { id: "settings", title: "settings", url: "/system/settings", icon: Settings2 },
    ],
  },
];
