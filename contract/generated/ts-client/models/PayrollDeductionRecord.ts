/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PayrollDeductionStatus } from './PayrollDeductionStatus';
import type { PayrollDisputeStatus } from './PayrollDisputeStatus';
export type PayrollDeductionRecord = {
    /**
     * AES-GCM encrypted serialized money payload envelope (`v1:nonce:ciphertext`).
     */
    amountCiphertext: string;
    deliveryDate: string;
    disputeStatus?: PayrollDisputeStatus;
    /**
     * AES-GCM encrypted employee actor identifier envelope (`v1:nonce:ciphertext`) for payroll privacy controls.
     */
    employeeActorCiphertext: string;
    /**
     * AES-GCM encrypted order identifier envelope (`v1:nonce:ciphertext`) for payroll privacy controls.
     */
    orderIdCiphertext: string;
    payPeriod: string;
    sourceEntryIds: Array<number>;
    status: PayrollDeductionStatus;
};

