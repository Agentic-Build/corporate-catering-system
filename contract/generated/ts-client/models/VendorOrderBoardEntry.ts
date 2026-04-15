/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderStatus } from './EmployeeOrderStatus';
import type { OrderLineItem } from './OrderLineItem';
import type { PlantId } from './PlantId';
export type VendorOrderBoardEntry = {
    deliveryDate: string;
    lineItems: Array<OrderLineItem>;
    orderId: string;
    plantId: PlantId;
    status: EmployeeOrderStatus;
};

