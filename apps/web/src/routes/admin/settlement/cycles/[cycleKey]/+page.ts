import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = ({ params }) => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "月結作業", href: "/admin/settlement" },
    { label: "結算週期", href: "/admin/settlement/cycles" },
    { label: params.cycleKey, href: null }
  ];
  return { breadcrumbs, cycleKey: params.cycleKey };
};
