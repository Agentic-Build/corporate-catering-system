/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderStatus } from './EmployeeOrderStatus';
import type { OrderTimelineEventType } from './OrderTimelineEventType';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type OrderTimelineEvent = {
    eventType: OrderTimelineEventType;
    occurredAt: TaipeiBusinessDateTime;
    status: EmployeeOrderStatus;
};

