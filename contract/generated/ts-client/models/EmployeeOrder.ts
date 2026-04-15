/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { EmployeeOrderStatus } from './EmployeeOrderStatus';
import type { Money } from './Money';
import type { OrderLineItem } from './OrderLineItem';
import type { PlantId } from './PlantId';
export type EmployeeOrder = {
    createdAt?: string;
    deliveryDate: string;
    employeeActorId: ActorId;
    lineItems: Array<OrderLineItem>;
    orderId: string;
    plantId: PlantId;
    status: EmployeeOrderStatus;
    total: Money;
};

