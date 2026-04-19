/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollSettlementCycleSummary = {
    batchId: string;
    cycleKey: string;
    deductionFailedRecords: number;
    disputedRecords: number;
    generatedAt: TaipeiBusinessDateTime;
    hrSyncStatus?: 'SUCCEEDED' | 'FAILED';
    lockState: 'LOCKED' | 'UNLOCKED';
    payPeriod: string;
    refundedRecords: number;
    totalRecords: number;
};

