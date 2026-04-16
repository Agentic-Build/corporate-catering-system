/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { AnomalyAlertSeverity } from './AnomalyAlertSeverity';
import type { AnomalyAlertStatus } from './AnomalyAlertStatus';
import type { AnomalyAlertTraceEvent } from './AnomalyAlertTraceEvent';
import type { AnomalyRuleKind } from './AnomalyRuleKind';
import type { AnomalySlaStatus } from './AnomalySlaStatus';
import type { AnomalyThresholdComparator } from './AnomalyThresholdComparator';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
export type AnomalyAlert = {
    alertId: string;
    closedAt?: TaipeiBusinessDateTime;
    closureEvidenceRefs: Array<string>;
    closureNote?: string;
    escalatedAt?: TaipeiBusinessDateTime;
    governanceIssueId: string;
    observedAt: TaipeiBusinessDateTime;
    observedValue: number;
    openedAt: TaipeiBusinessDateTime;
    ownerActorId: ActorId;
    ruleDisplayName: string;
    ruleId: string;
    ruleKind: AnomalyRuleKind;
    severity: AnomalyAlertSeverity;
    slaDueAt: TaipeiBusinessDateTime;
    slaStatus: AnomalySlaStatus;
    status: AnomalyAlertStatus;
    thresholdComparator: AnomalyThresholdComparator;
    thresholdValue: number;
    ticketReference?: string;
    trace: Array<AnomalyAlertTraceEvent>;
    updatedAt: TaipeiBusinessDateTime;
    vendorId: string;
};

