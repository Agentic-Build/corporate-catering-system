import type { PageLoad } from "./$types";
import type { BreadcrumbItem } from "$lib/platform/navigation";

export const load: PageLoad = ({ params }) => {
  const breadcrumbs: BreadcrumbItem[] = [
    { label: "今日", href: "/employee" },
    { label: "訂單", href: "/employee/orders" },
    { label: params.orderId, href: null }
  ];
  return { breadcrumbs, orderId: params.orderId };
};
