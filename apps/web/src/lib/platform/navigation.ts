import type { AuthRole } from "$lib/server/auth/contracts";

/**
 * Task-oriented navigation model (Blueprint v2).
 *
 * The navigation tree is the single source of truth for every route's label,
 * icon, and active trail. Each +page.ts may enrich it with dynamic breadcrumbs.
 *
 * Tree layout:
 *   role (PortalRole)
 *     └─ section nodes (primary nav: side-nav on desktop / bottom-tab on mobile)
 *          └─ optional task nodes (visible as sub-nav inside a section)
 */

export const PORTAL_ROLES = ["employee", "vendor", "admin"] as const;

export type PortalRole = (typeof PORTAL_ROLES)[number];

export interface NavNode {
  id: string;
  labelKey: string;
  descriptionKey?: string;
  href: string;
  icon?: string;
  children?: NavNode[];
}

export type RoleNavigationTree = readonly NavNode[];

export interface BreadcrumbItem {
  label: string;
  href: string | null;
}

export interface ActiveTrail {
  role: PortalRole | null;
  nodes: NavNode[];
}

export interface PortalLink {
  role: PortalRole;
  href: string;
  active: boolean;
  locked: boolean;
}

export interface PrimaryNavItem {
  id: string;
  labelKey: string;
  descriptionKey?: string;
  href: string;
  icon?: string;
  active: boolean;
}

export interface RoleAwareNavigation {
  activePortal: PortalRole | null;
  rolePortal: PortalRole | null;
  sectionPortal: PortalRole | null;
  portalLinks: PortalLink[];
  primary: PrimaryNavItem[];
  trail: NavNode[];
  activeSectionId: string | null;
}

export const NAV_TREES: Record<PortalRole, RoleNavigationTree> = {
  employee: [
    {
      id: "home",
      labelKey: "nav.employee.home",
      descriptionKey: "nav.employee.homeDesc",
      href: "/employee",
      icon: "home"
    },
    {
      id: "discover",
      labelKey: "nav.employee.discover",
      descriptionKey: "nav.employee.discoverDesc",
      href: "/employee/discover",
      icon: "search"
    },
    {
      id: "orders",
      labelKey: "nav.employee.orders",
      descriptionKey: "nav.employee.ordersDesc",
      href: "/employee/orders",
      icon: "bag"
    },
    {
      id: "wallet",
      labelKey: "nav.employee.wallet",
      descriptionKey: "nav.employee.walletDesc",
      href: "/employee/wallet",
      icon: "wallet"
    }
  ],
  vendor: [
    {
      id: "today",
      labelKey: "nav.vendor.today",
      descriptionKey: "nav.vendor.todayDesc",
      href: "/vendor",
      icon: "today"
    },
    {
      id: "menu",
      labelKey: "nav.vendor.menu",
      descriptionKey: "nav.vendor.menuDesc",
      href: "/vendor/menu",
      icon: "menu"
    },
    {
      id: "schedule",
      labelKey: "nav.vendor.schedule",
      descriptionKey: "nav.vendor.scheduleDesc",
      href: "/vendor/schedule",
      icon: "calendar"
    },
    {
      id: "batches",
      labelKey: "nav.vendor.batches",
      descriptionKey: "nav.vendor.batchesDesc",
      href: "/vendor/batches",
      icon: "print"
    },
    {
      id: "orders",
      labelKey: "nav.vendor.orders",
      descriptionKey: "nav.vendor.ordersDesc",
      href: "/vendor/orders",
      icon: "clipboard"
    },
    {
      id: "compliance",
      labelKey: "nav.vendor.compliance",
      descriptionKey: "nav.vendor.complianceDesc",
      href: "/vendor/compliance",
      icon: "shield"
    },
    {
      id: "insights",
      labelKey: "nav.vendor.insights",
      descriptionKey: "nav.vendor.insightsDesc",
      href: "/vendor/insights",
      icon: "chart"
    }
  ],
  admin: [
    {
      id: "overview",
      labelKey: "nav.admin.overview",
      descriptionKey: "nav.admin.overviewDesc",
      href: "/admin",
      icon: "inbox"
    },
    {
      id: "vendors",
      labelKey: "nav.admin.vendors",
      descriptionKey: "nav.admin.vendorsDesc",
      href: "/admin/vendors",
      icon: "building"
    },
    {
      id: "compliance",
      labelKey: "nav.admin.compliance",
      descriptionKey: "nav.admin.complianceDesc",
      href: "/admin/compliance/templates",
      icon: "shield"
    },
    {
      id: "settlement",
      labelKey: "nav.admin.settlement",
      descriptionKey: "nav.admin.settlementDesc",
      href: "/admin/settlement",
      icon: "cash"
    },
    {
      id: "anomalies",
      labelKey: "nav.admin.anomalies",
      descriptionKey: "nav.admin.anomaliesDesc",
      href: "/admin/anomalies",
      icon: "alert"
    },
    {
      id: "audit",
      labelKey: "nav.admin.audit",
      descriptionKey: "nav.admin.auditDesc",
      href: "/admin/audit",
      icon: "eye"
    },
    {
      id: "analytics",
      labelKey: "nav.admin.analytics",
      descriptionKey: "nav.admin.analyticsDesc",
      href: "/admin/analytics",
      icon: "chart"
    }
  ]
};

export function getPortalBasePath(role: PortalRole): string {
  return `/${role}`;
}

export function resolvePortalFromPath(pathname: string): PortalRole | null {
  for (const role of PORTAL_ROLES) {
    const basePath = getPortalBasePath(role);
    if (pathname === basePath || pathname.startsWith(`${basePath}/`)) {
      return role;
    }
  }
  return null;
}

/**
 * Active section = the tree node whose href is the longest prefix of pathname.
 * Example: pathname "/employee/orders/abc/pickup" → section "orders".
 */
export function resolveActiveSectionId(role: PortalRole, pathname: string): string | null {
  const tree = NAV_TREES[role];
  let match: NavNode | null = null;
  let matchLen = -1;
  for (const node of tree) {
    if (pathname === node.href || pathname.startsWith(`${node.href}/`)) {
      if (node.href.length > matchLen) {
        match = node;
        matchLen = node.href.length;
      }
    }
  }
  return match ? match.id : null;
}

export function isValidSectionId(role: PortalRole, sectionId: string): boolean {
  return NAV_TREES[role].some((node) => node.id === sectionId);
}

export function buildRoleAwareNavigation(
  actorRole: AuthRole | null,
  pathname: string
): RoleAwareNavigation {
  const activePortal = resolvePortalFromPath(pathname);
  const rolePortal = isPortalRole(actorRole) ? actorRole : null;
  const sectionPortal = rolePortal ?? activePortal;

  const portalLinks: PortalLink[] = PORTAL_ROLES.map((role) => ({
    role,
    href: getPortalBasePath(role),
    active: activePortal === role,
    locked: rolePortal !== null && rolePortal !== role
  }));

  const primary: PrimaryNavItem[] = sectionPortal
    ? NAV_TREES[sectionPortal].map((node) => ({
        id: node.id,
        labelKey: node.labelKey,
        descriptionKey: node.descriptionKey,
        href: node.href,
        icon: node.icon,
        active:
          pathname === node.href ||
          pathname.startsWith(`${node.href}/`) ||
          (node.href === getPortalBasePath(sectionPortal) && pathname === node.href)
      }))
    : [];

  const activeSectionId = sectionPortal ? resolveActiveSectionId(sectionPortal, pathname) : null;
  const trail = buildActiveTrail(sectionPortal, activeSectionId);

  return {
    activePortal,
    rolePortal,
    sectionPortal,
    portalLinks,
    primary,
    trail,
    activeSectionId
  };
}

function buildActiveTrail(
  role: PortalRole | null,
  activeSectionId: string | null
): NavNode[] {
  if (!role || !activeSectionId) {
    return [];
  }
  const node = NAV_TREES[role].find((n) => n.id === activeSectionId);
  return node ? [node] : [];
}

function isPortalRole(role: AuthRole | null): role is PortalRole {
  return role === "employee" || role === "vendor" || role === "admin";
}
