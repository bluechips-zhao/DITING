"use client";
import { type ReactNode, useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronRight, LayoutDashboard, Target, Bug, Network, MessageSquare } from "lucide-react";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useCurrentUser } from "@/hooks/use-current-user";
import { api } from "@/lib/api";
import { sidebarItems } from "@/navigation/sidebar/sidebar-items";
import { AccountSwitcher } from "./sidebar/account-switcher";
import { LayoutControls } from "./sidebar/layout-controls";
import { SearchDialog } from "./sidebar/search-dialog";
import { ThemeSwitcher } from "./sidebar/theme-switcher";
import { UpdateBadge } from "./update-badge";
const destinations = [
  { url: "/dashboard", label: "总览", icon: LayoutDashboard },
  { url: "/function/tasks", label: "任务", icon: Target },
  { url: "/function/findings", label: "发现", icon: Bug },
  { url: "/function/assets", label: "资产", icon: Network },
  { url: "/chat", label: "对话", icon: MessageSquare },
];
export function MainContent({ children }: { children: ReactNode }) {
  const currentUser = useCurrentUser(),
    path = usePathname().replace(/\/+$/, "");
  const [version, setVersion] = useState("");
  useEffect(() => {
    api
      .health()
      .then((h) => setVersion((h.version ?? "").replace(/^v(?=\d)/, "")))
      .catch(() => setVersion(""));
  }, []);
  const entries = sidebarItems.flatMap((g) => g.items.flatMap((i) => i.subItems ?? [i]));
  const current = entries
    .filter((i) => "url" in i && (path === i.url || path.startsWith(i.url + "/")))
    .sort((a, b) => ("url" in b ? b.url.length : 0) - ("url" in a ? a.url.length : 0))[0];
  const fullBleed = path.startsWith("/function/tasks/") || path === "/chat";
  return (
    <>
      <header className="workspace-header sticky top-0 z-40 flex shrink-0 items-center gap-3 px-4 lg:px-7">
        <div className="flex min-w-0 items-center gap-3">
          <SidebarTrigger aria-label="展开或收起导航" />
          <div className="workspace-path min-w-0">
            <span className="hidden sm:inline">工作台</span>
            <ChevronRight className="hidden size-3 text-muted-foreground sm:block" />
            <strong className="truncate font-medium">{current?.title ?? "探索任务"}</strong>
          </div>
        </div>
        <nav aria-label="常用页面" className="workspace-quicknav mx-auto hidden xl:flex">
          {destinations.map(({ url, label, icon: Icon }) => (
            <Link key={url} href={url} aria-current={path === url ? "page" : undefined}>
              <Icon className="size-3.5" />
              {label}
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex shrink-0 items-center gap-1">
          <div className="hidden sm:block">
            <SearchDialog />
          </div>
          <span className="hidden text-[10px] text-muted-foreground 2xl:inline">{version ? `v${version}` : ""}</span>
          <UpdateBadge />
          <LayoutControls />
          <ThemeSwitcher />
          <div className="hidden lg:block">
            <AccountSwitcher users={currentUser ? [currentUser] : []} />
          </div>
        </div>
      </header>
      <div
        id="workspace-main"
        className={
          fullBleed
            ? "task-workspace min-h-0 min-w-0 flex-1"
            : "workspace-content min-h-0 min-w-0 flex-1 overflow-x-hidden has-data-[content-padding=false]:p-0"
        }
      >
        {children}
      </div>
    </>
  );
}
