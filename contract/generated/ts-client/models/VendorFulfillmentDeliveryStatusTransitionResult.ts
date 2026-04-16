/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
import type { VendorFulfillmentDeliveryStatus } from './VendorFulfillmentDeliveryStatus';
export type VendorFulfillmentDeliveryStatusTransitionResult = {
    fromStatus: VendorFulfillmentDeliveryStatus;
    occurredAt: TaipeiBusinessDateTime;
    orderId: string;
    toStatus: VendorFulfillmentDeliveryStatus;
};

