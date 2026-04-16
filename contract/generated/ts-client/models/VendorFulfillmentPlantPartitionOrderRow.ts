/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { SpecialRequestOption } from './SpecialRequestOption';
import type { VendorFulfillmentDeliveryStatus } from './VendorFulfillmentDeliveryStatus';
export type VendorFulfillmentPlantPartitionOrderRow = {
    deliveryStatus: VendorFulfillmentDeliveryStatus;
    orderId: string;
    portionCount: number;
    specialRequests: Array<SpecialRequestOption>;
};

