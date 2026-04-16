/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PlantId } from './PlantId';
import type { SpecialRequestCount } from './SpecialRequestCount';
import type { VendorFulfillmentPlantPartitionOrderRow } from './VendorFulfillmentPlantPartitionOrderRow';
export type VendorFulfillmentPlantPartitionRow = {
    orders: Array<VendorFulfillmentPlantPartitionOrderRow>;
    plantId: PlantId;
    specialRequestCounts: Array<SpecialRequestCount>;
    totalOrders: number;
    totalPortions: number;
};

