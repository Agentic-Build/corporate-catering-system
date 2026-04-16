/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PayrollReconciliation } from './PayrollReconciliation';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollExchangeBatch = {
    batchId: string;
    cycleEndDate: string;
    cycleKey: string;
    cycleStartDate: string;
    exchangePath: 'SFTP_BATCH';
    generatedAt: TaipeiBusinessDateTime;
    hrApiSyncStatus: 'NOT_SYNCED' | 'SUCCEEDED' | 'FAILED';
    hrApiSyncedAt?: TaipeiBusinessDateTime;
    payPeriod: string;
    reconciliation: PayrollReconciliation;
    snapshotChecksum: string;
};

