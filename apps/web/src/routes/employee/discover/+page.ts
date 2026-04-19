import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = () => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "今日", href: "/employee" },
    { label: "菜單", href: null }
  ];
  return { breadcrumbs };
};
