/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { Role } from './Role';
import type { VendorLifecycleEventType } from './VendorLifecycleEventType';
import type { VendorSuspensionReasonCode } from './VendorSuspensionReasonCode';
export type VendorLifecycleEvent = {
    actorId: ActorId;
    actorRole: Role;
    eventType: VendorLifecycleEventType;
    occurredAt: string;
    summary: string;
    suspensionReasonCode?: VendorSuspensionReasonCode;
    templateId?: string;
};

