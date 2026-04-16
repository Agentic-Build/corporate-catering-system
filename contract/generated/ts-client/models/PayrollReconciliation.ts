/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PayrollExceptionClass } from './PayrollExceptionClass';
export type PayrollReconciliation = {
    deductionFailedRecords: number;
    disputedRecords: number;
    employeeTerminatedRecords: number;
    lockedRecords: number;
    presentExceptionClasses: Array<PayrollExceptionClass>;
    readyRecords: number;
    refundedRecords: number;
    requiredExceptionClasses: Array<PayrollExceptionClass>;
    totalAmountMinor: number;
    totalRecords: number;
    totalSourceEntries: number;
};

