/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AnomalyAlertSeverity } from './AnomalyAlertSeverity';
import type { AnomalyRuleKind } from './AnomalyRuleKind';
import type { AnomalyThresholdComparator } from './AnomalyThresholdComparator';
export type AnomalyRule = {
    description: string;
    displayName: string;
    enabled: boolean;
    evaluationWindowDays: number;
    governanceIssueId: string;
    kind: AnomalyRuleKind;
    ruleId: string;
    severity: AnomalyAlertSeverity;
    slaMinutes: number;
    thresholdComparator: AnomalyThresholdComparator;
    thresholdValue: number;
};

