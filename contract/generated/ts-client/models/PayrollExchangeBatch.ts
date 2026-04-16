/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollExchangeBatch = {
    batchId: string;
    cycleKey: string;
    exchangePath: 'SFTP_BATCH';
    generatedAt: TaipeiBusinessDateTime;
    hrApiSyncStatus: 'NOT_SYNCED' | 'SUCCEEDED' | 'FAILED';
    hrApiSyncedAt?: TaipeiBusinessDateTime;
    payPeriod: string;
    snapshotChecksum: string;
};

