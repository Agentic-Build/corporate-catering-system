/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { AnomalyAlertStatus } from './AnomalyAlertStatus';
import type { AnomalyAlertTraceEventType } from './AnomalyAlertTraceEventType';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type AnomalyAlertTraceEvent = {
    actorId: ActorId;
    eventType: AnomalyAlertTraceEventType;
    note?: string;
    occurredAt: TaipeiBusinessDateTime;
    status: AnomalyAlertStatus;
};

