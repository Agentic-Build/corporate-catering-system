/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { PlantId } from './PlantId';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
import type { VendorPlantDeliveryRuleEffect } from './VendorPlantDeliveryRuleEffect';
import type { VendorPlantDeliveryServiceWindow } from './VendorPlantDeliveryServiceWindow';
export type VendorPlantDeliveryMapping = {
    effect: VendorPlantDeliveryRuleEffect;
    mappingId: string;
    plantId: PlantId;
    precedence: number;
    serviceWindow: VendorPlantDeliveryServiceWindow;
    updatedAt: TaipeiBusinessDateTime;
    updatedByActorId: ActorId;
    vendorId: string;
};

