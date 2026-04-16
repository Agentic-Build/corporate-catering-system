/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { PayrollDispute } from './PayrollDispute';
import type { PayrollLedgerEntry } from './PayrollLedgerEntry';
export type EmployeeOrderPayrollLedger = {
    currency: string;
    deliveryDate: string;
    disputes: Array<PayrollDispute>;
    employeeActorId: ActorId;
    ledgerEntries: Array<PayrollLedgerEntry>;
    netAmountMinor: number;
    orderId: string;
};

