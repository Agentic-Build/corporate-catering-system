import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = () => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "稽核查詢", href: "/admin/audit" },
    { label: "責任歸屬", href: null }
  ];
  return { breadcrumbs };
};
