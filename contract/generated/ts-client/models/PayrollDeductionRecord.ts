/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { Money } from './Money';
import type { PayrollDeductionStatus } from './PayrollDeductionStatus';
import type { PayrollDisputeStatus } from './PayrollDisputeStatus';
export type PayrollDeductionRecord = {
    amount: Money;
    deliveryDate: string;
    disputeStatus?: PayrollDisputeStatus;
    employeeActorId: ActorId;
    orderId: string;
    payPeriod: string;
    sourceEntryIds: Array<number>;
    status: PayrollDeductionStatus;
};

