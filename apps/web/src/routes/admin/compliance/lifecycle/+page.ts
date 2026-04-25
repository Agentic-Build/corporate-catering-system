import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = () => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "合規文件", href: "/admin/compliance/templates" },
    { label: "執行 lifecycle", href: null }
  ];
  return { breadcrumbs };
};
