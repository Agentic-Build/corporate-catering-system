import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = ({ params }) => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "異常告警", href: "/admin/anomalies" },
    { label: "規則", href: "/admin/anomalies/rules" },
    { label: params.ruleId, href: null }
  ];
  return { breadcrumbs, ruleId: params.ruleId };
};
