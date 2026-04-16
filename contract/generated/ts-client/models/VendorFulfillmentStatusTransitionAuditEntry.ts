/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { Role } from './Role';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
import type { VendorFulfillmentDeliveryStatus } from './VendorFulfillmentDeliveryStatus';
export type VendorFulfillmentStatusTransitionAuditEntry = {
    actorId: ActorId;
    actorRole: Role;
    fromStatus: VendorFulfillmentDeliveryStatus;
    occurredAt: TaipeiBusinessDateTime;
    operationId: string;
    orderId: string;
    toStatus: VendorFulfillmentDeliveryStatus;
};

