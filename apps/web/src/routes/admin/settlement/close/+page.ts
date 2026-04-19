import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = () => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "月結作業", href: "/admin/settlement" },
    { label: "執行關帳", href: null }
  ];
  return { breadcrumbs };
};
