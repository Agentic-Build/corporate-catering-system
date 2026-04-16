/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { PayrollDisputeStatus } from './PayrollDisputeStatus';
import type { PayrollDisputeTraceEvent } from './PayrollDisputeTraceEvent';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollDispute = {
    disputeId: string;
    employeeActorId: ActorId;
    openedAt: TaipeiBusinessDateTime;
    orderId: string;
    ownerActorId: ActorId;
    status: PayrollDisputeStatus;
    trace: Array<PayrollDisputeTraceEvent>;
    updatedAt: TaipeiBusinessDateTime;
};

