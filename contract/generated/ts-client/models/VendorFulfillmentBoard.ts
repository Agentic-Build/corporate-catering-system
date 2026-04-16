/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
import type { VendorFulfillmentOrderEntry } from './VendorFulfillmentOrderEntry';
import type { VendorFulfillmentPlantEntry } from './VendorFulfillmentPlantEntry';
import type { VendorFulfillmentStatusTransitionAuditEntry } from './VendorFulfillmentStatusTransitionAuditEntry';
export type VendorFulfillmentBoard = {
    deliveryDate: string;
    generatedAt: TaipeiBusinessDateTime;
    orders: Array<VendorFulfillmentOrderEntry>;
    plants: Array<VendorFulfillmentPlantEntry>;
    statusTransitions: Array<VendorFulfillmentStatusTransitionAuditEntry>;
    timezone: 'Asia/Taipei';
};

