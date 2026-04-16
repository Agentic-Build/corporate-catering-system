/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PlantId } from './PlantId';
import type { SpecialRequestCount } from './SpecialRequestCount';
import type { VendorFulfillmentStatusCount } from './VendorFulfillmentStatusCount';
export type VendorFulfillmentPlantEntry = {
    deliveryStatusCounts: Array<VendorFulfillmentStatusCount>;
    orderCount: number;
    plantId: PlantId;
    portionCount: number;
    specialRequestCounts: Array<SpecialRequestCount>;
};

