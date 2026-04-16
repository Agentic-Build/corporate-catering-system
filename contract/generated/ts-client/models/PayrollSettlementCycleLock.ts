/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { PayrollSettlementCycleLockState } from './PayrollSettlementCycleLockState';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollSettlementCycleLock = {
    actorId: ActorId;
    batchId: string;
    changedAt: TaipeiBusinessDateTime;
    cycleKey: string;
    lockState: PayrollSettlementCycleLockState;
    payPeriod: string;
    reason: string;
    snapshotChecksum: string;
};

