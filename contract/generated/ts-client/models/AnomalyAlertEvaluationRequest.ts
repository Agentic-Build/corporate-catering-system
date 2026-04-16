/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
export type AnomalyAlertEvaluationRequest = {
    complaintCount?: number;
    daysUntilExpiry?: number;
    defaultOwnerActorId?: ActorId;
    observedAtEpochDay?: number;
    observedAtMinuteOfDay?: number;
    onTimeRate?: number;
    satisfactionScore?: number;
    vendorId: string;
};

