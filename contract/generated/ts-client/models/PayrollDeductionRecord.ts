/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { Money } from './Money';
export type PayrollDeductionRecord = {
    amount: Money;
    deliveryDate: string;
    employeeActorId: ActorId;
    orderId: string;
    payPeriod: string;
    status: 'READY' | 'LOCKED' | 'REFUNDED';
};

