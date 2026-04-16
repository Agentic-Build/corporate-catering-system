/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PlantId } from './PlantId';
import type { SpecialRequestOption } from './SpecialRequestOption';
import type { VendorFulfillmentDeliveryStatus } from './VendorFulfillmentDeliveryStatus';
export type VendorFulfillmentLabelEntry = {
    deliveryStatus: VendorFulfillmentDeliveryStatus;
    menuItemId: string;
    orderId: string;
    plantId: PlantId;
    quantity: number;
    specialRequests: Array<SpecialRequestOption>;
};

