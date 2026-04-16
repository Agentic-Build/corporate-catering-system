import type { AuthActor, AuthRequestContext } from "./lib/server/auth/contracts";

declare global {
  namespace App {
    interface Locals {
      auth: AuthRequestContext;
      actor: AuthActor | null;
    }
  }
}

export {};
