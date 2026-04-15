/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { Role } from './Role';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
import type { VendorPlantDeliveryMapping } from './VendorPlantDeliveryMapping';
import type { VendorPlantDeliveryMappingAuditEventType } from './VendorPlantDeliveryMappingAuditEventType';
export type VendorPlantDeliveryMappingAuditEntry = {
    actorId: ActorId;
    actorRole: Role;
    eventType: VendorPlantDeliveryMappingAuditEventType;
    mapping: VendorPlantDeliveryMapping;
    occurredAt: TaipeiBusinessDateTime;
    operationId: string;
};

