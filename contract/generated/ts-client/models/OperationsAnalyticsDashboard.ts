/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { OperationsAnalyticsMetricDefinition } from './OperationsAnalyticsMetricDefinition';
import type { OperationsAnalyticsPlantBreakdown } from './OperationsAnalyticsPlantBreakdown';
import type { OperationsAnalyticsTimeBreakdown } from './OperationsAnalyticsTimeBreakdown';
import type { OperationsAnalyticsVendorBreakdown } from './OperationsAnalyticsVendorBreakdown';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type OperationsAnalyticsDashboard = {
    fromEpochDay: number;
    generatedAt: TaipeiBusinessDateTime;
    metricDefinitions: Array<OperationsAnalyticsMetricDefinition>;
    metricSchemaVersion: string;
    plantBreakdown: Array<OperationsAnalyticsPlantBreakdown>;
    timeBreakdown: Array<OperationsAnalyticsTimeBreakdown>;
    toEpochDay: number;
    vendorBreakdown: Array<OperationsAnalyticsVendorBreakdown>;
};

