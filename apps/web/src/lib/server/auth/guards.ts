import type { AuthRole } from "./contracts";

export interface RoleGuard {
  prefix: string;
  allowedRoles: readonly AuthRole[];
  requiredPermissions: readonly string[];
}

export interface ScopeRequirement {
  kind: "vendorId" | "plantId";
  value: string;
}

export interface ScopeResolutionResult {
  requirements: ScopeRequirement[];
  hasMalformedEncoding: boolean;
}

const ROLE_GUARDS: readonly RoleGuard[] = [
  {
    prefix: "/employee",
    allowedRoles: ["employee"],
    requiredPermissions: ["employee:portal"]
  },
  {
    prefix: "/vendor",
    allowedRoles: ["vendor"],
    requiredPermissions: ["vendor:portal"]
  },
  {
    prefix: "/admin",
    allowedRoles: ["admin"],
    requiredPermissions: ["admin:portal"]
  },
  {
    prefix: "/portal/employee",
    allowedRoles: ["employee"],
    requiredPermissions: ["employee:portal"]
  },
  {
    prefix: "/portal/vendor",
    allowedRoles: ["vendor"],
    requiredPermissions: ["vendor:portal"]
  },
  {
    prefix: "/portal/admin",
    allowedRoles: ["admin"],
    requiredPermissions: ["admin:portal"]
  },
  {
    prefix: "/console/employee",
    allowedRoles: ["employee"],
    requiredPermissions: ["employee:portal"]
  },
  {
    prefix: "/console/vendor",
    allowedRoles: ["vendor"],
    requiredPermissions: ["vendor:portal"]
  },
  {
    prefix: "/console/admin",
    allowedRoles: ["admin"],
    requiredPermissions: ["admin:portal"]
  }
];

const SCOPE_RULES: readonly {
  kind: ScopeRequirement["kind"];
  pattern: RegExp;
  groupIndex: number;
}[] = [
  {
    kind: "vendorId",
    pattern: /^\/vendor\/vendors\/([^/]+)(?:\/|$)/,
    groupIndex: 1
  },
  {
    kind: "vendorId",
    pattern: /^\/portal\/vendor\/vendors\/([^/]+)(?:\/|$)/,
    groupIndex: 1
  },
  {
    kind: "plantId",
    pattern: /^\/employee\/plants\/([^/]+)(?:\/|$)/,
    groupIndex: 1
  },
  {
    kind: "plantId",
    pattern: /^\/portal\/employee\/plants\/([^/]+)(?:\/|$)/,
    groupIndex: 1
  }
];

export function resolveRoleGuard(pathname: string): RoleGuard | null {
  for (const guard of ROLE_GUARDS) {
    if (pathname === guard.prefix || pathname.startsWith(`${guard.prefix}/`)) {
      return guard;
    }
  }

  return null;
}

export function resolveScopeRequirements(pathname: string): ScopeResolutionResult {
  const requirements: ScopeRequirement[] = [];
  let hasMalformedEncoding = false;

  for (const rule of SCOPE_RULES) {
    const match = pathname.match(rule.pattern);
    if (!match) {
      continue;
    }

    const captured = match[rule.groupIndex];
    if (!captured) {
      continue;
    }

    try {
      requirements.push({
        kind: rule.kind,
        value: decodeURIComponent(captured)
      });
    } catch {
      hasMalformedEncoding = true;
    }
  }

  return {
    requirements,
    hasMalformedEncoding
  };
}
