/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PlantId } from './PlantId';
import type { VendorPlantDeliveryRuleEffect } from './VendorPlantDeliveryRuleEffect';
import type { VendorPlantDeliveryServiceWindow } from './VendorPlantDeliveryServiceWindow';
export type VendorPlantDeliveryMappingUpsertRequest = {
    effect: VendorPlantDeliveryRuleEffect;
    plantId: PlantId;
    precedence: number;
    serviceWindow: VendorPlantDeliveryServiceWindow;
};

