import { createHmac } from "node:crypto";

import type { AuthRequestContext } from "./contracts";

const CORPORATE_ROLE = {
  employee: "EMPLOYEE",
  admin: "COMMITTEE_ADMIN"
} as const;

const VENDOR_ROLE = "VENDOR_OPERATOR";

interface JwtConfig {
  issuer: string;
  audience: string;
  secret: Buffer;
}

interface JwtClaims {
  iss: string;
  aud: string;
  sub: string;
  exp: number;
  iat: number;
  nbf: number;
  role: string;
  allPlants: boolean;
  plantIds: string[];
  vendorIds: string[];
}

export function buildApiBearerToken(auth: AuthRequestContext): string | null {
  const session = auth.session;
  if (!session) {
    return null;
  }

  const nowEpochSecond = Math.floor(Date.now() / 1000);
  const sessionExpiryEpochSecond = Math.floor(session.expiresAtEpochMs / 1000);
  const expiresAtEpochSecond = Math.min(sessionExpiryEpochSecond, nowEpochSecond + 5 * 60);
  if (expiresAtEpochSecond <= nowEpochSecond) {
    return null;
  }

  if (session.actor.role === "vendor") {
    const config = loadJwtConfig(
      "VENDOR_MFA_JWT_ISSUER",
      "VENDOR_MFA_JWT_AUDIENCE",
      "VENDOR_MFA_JWT_HS256_SECRET_BASE64"
    );
    const claims: JwtClaims = {
      iss: config.issuer,
      aud: config.audience,
      sub: session.actor.id,
      exp: expiresAtEpochSecond,
      iat: nowEpochSecond,
      nbf: nowEpochSecond,
      role: VENDOR_ROLE,
      allPlants: false,
      plantIds: [...session.actor.scope.plantIds],
      vendorIds: [...session.actor.scope.vendorIds]
    };
    return signHs256Jwt(claims, config.secret);
  }

  const config = loadJwtConfig(
    "CORPORATE_SSO_JWT_ISSUER",
    "CORPORATE_SSO_JWT_AUDIENCE",
    "CORPORATE_SSO_JWT_HS256_SECRET_BASE64"
  );
  const claims: JwtClaims = {
    iss: config.issuer,
    aud: config.audience,
    sub: session.actor.id,
    exp: expiresAtEpochSecond,
    iat: nowEpochSecond,
    nbf: nowEpochSecond,
    role: CORPORATE_ROLE[session.actor.role],
    allPlants: session.actor.role === "admin",
    plantIds: session.actor.role === "admin" ? [] : [...session.actor.scope.plantIds],
    vendorIds: []
  };
  return signHs256Jwt(claims, config.secret);
}

function loadJwtConfig(issuerEnv: string, audienceEnv: string, secretEnv: string): JwtConfig {
  const issuer = readRequiredEnv(issuerEnv);
  const audience = readRequiredEnv(audienceEnv);
  const encodedSecret = readRequiredEnv(secretEnv);
  const secret = Buffer.from(encodedSecret, "base64");
  if (secret.length === 0) {
    throw new Error(`${secretEnv} must decode to a non-empty key`);
  }

  return {
    issuer,
    audience,
    secret
  };
}

function readRequiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(`${name} must be configured`);
  }
  return value;
}

function signHs256Jwt(claims: JwtClaims, secret: Buffer): string {
  const header = {
    alg: "HS256",
    typ: "JWT"
  } as const;
  const headerSegment = Buffer.from(JSON.stringify(header)).toString("base64url");
  const payloadSegment = Buffer.from(JSON.stringify(claims)).toString("base64url");
  const signingInput = `${headerSegment}.${payloadSegment}`;
  const signatureSegment = createHmac("sha256", secret)
    .update(signingInput)
    .digest("base64url");
  return `${signingInput}.${signatureSegment}`;
}
