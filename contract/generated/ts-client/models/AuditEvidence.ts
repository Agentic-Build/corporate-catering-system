/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { AuditAction } from './AuditAction';
import type { AuditEntityType } from './AuditEntityType';
import type { AuthenticationSource } from './AuthenticationSource';
import type { Role } from './Role';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type AuditEvidence = {
    action: AuditAction;
    actorId: ActorId;
    actorRole: Role;
    authenticationSource: AuthenticationSource;
    correlationId: string;
    entityId: string;
    entityType: AuditEntityType;
    evidenceId: number;
    occurredAt: TaipeiBusinessDateTime;
    operationId: string;
    reason: string;
};

