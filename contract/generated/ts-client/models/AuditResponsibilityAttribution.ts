/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { AuditAction } from './AuditAction';
import type { AuditEntityRef } from './AuditEntityRef';
import type { AuthenticationSource } from './AuthenticationSource';
import type { Role } from './Role';
export type AuditResponsibilityAttribution = {
    actions: Array<AuditAction>;
    actorId: ActorId;
    authenticationSource: AuthenticationSource;
    entities: Array<AuditEntityRef>;
    eventCount: number;
    role: Role;
};

