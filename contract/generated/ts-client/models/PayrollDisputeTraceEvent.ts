/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { PayrollDisputeStatus } from './PayrollDisputeStatus';
import type { PayrollDisputeTraceEventType } from './PayrollDisputeTraceEventType';
import type { PayrollLedgerSourceKind } from './PayrollLedgerSourceKind';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type PayrollDisputeTraceEvent = {
    actorId: ActorId;
    eventType: PayrollDisputeTraceEventType;
    note?: string;
    occurredAt: TaipeiBusinessDateTime;
    ownerActorId: ActorId;
    refundLedgerEntryId?: number;
    sourceEventKind: PayrollLedgerSourceKind;
    sourceEventReference: string;
    status: PayrollDisputeStatus;
};

