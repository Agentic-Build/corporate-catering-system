/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AdminAnomalyAlertAcknowledgePatchRequest } from './AdminAnomalyAlertAcknowledgePatchRequest';
import type { AdminAnomalyAlertAssignOwnerPatchRequest } from './AdminAnomalyAlertAssignOwnerPatchRequest';
import type { AdminAnomalyAlertClosePatchRequest } from './AdminAnomalyAlertClosePatchRequest';
import type { AdminAnomalyAlertEscalatePatchRequest } from './AdminAnomalyAlertEscalatePatchRequest';
import type { AdminAnomalyAlertStartRemediationPatchRequest } from './AdminAnomalyAlertStartRemediationPatchRequest';
/**
 * Governed anomaly alert lifecycle command.
 */
export type AdminAnomalyAlertPatchRequest = (AdminAnomalyAlertAssignOwnerPatchRequest | AdminAnomalyAlertAcknowledgePatchRequest | AdminAnomalyAlertStartRemediationPatchRequest | AdminAnomalyAlertEscalatePatchRequest | AdminAnomalyAlertClosePatchRequest);

