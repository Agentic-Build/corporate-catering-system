/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { Money } from './Money';
import type { PayrollLedgerEntryKind } from './PayrollLedgerEntryKind';
import type { PayrollLedgerSourceKind } from './PayrollLedgerSourceKind';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollLedgerEntry = {
    amount: Money;
    kind: PayrollLedgerEntryKind;
    ledgerEntryId: number;
    occurredAt: TaipeiBusinessDateTime;
    sourceEventKind: PayrollLedgerSourceKind;
    sourceEventReference: string;
};

