/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { OrderLineItemRequest } from './OrderLineItemRequest';
import type { PlantId } from './PlantId';
export type EmployeeOrderCreateRequest = {
    deliveryDate: string;
    employeeNote?: string;
    lineItems: Array<OrderLineItemRequest>;
    plantId: PlantId;
};

