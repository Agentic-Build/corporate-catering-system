import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = ({ params }) => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "總覽", href: "/admin" },
    { label: "異常告警", href: "/admin/anomalies" },
    { label: params.alertId, href: null }
  ];
  return { breadcrumbs, alertId: params.alertId };
};
