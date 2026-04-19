import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = () => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "月結作業", href: "/admin/settlement" },
    { label: "結算週期", href: null }
  ];
  return { breadcrumbs };
};
