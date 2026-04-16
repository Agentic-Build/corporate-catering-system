/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderStatus } from './EmployeeOrderStatus';
import type { PlantId } from './PlantId';
import type { VendorFulfillmentDeliveryStatus } from './VendorFulfillmentDeliveryStatus';
import type { VendorFulfillmentOrderLineItem } from './VendorFulfillmentOrderLineItem';
export type VendorFulfillmentOrderEntry = {
    deliveryStatus: VendorFulfillmentDeliveryStatus;
    lineItems: Array<VendorFulfillmentOrderLineItem>;
    orderId: string;
    orderStatus: EmployeeOrderStatus;
    plantId: PlantId;
};

