/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type AdminAnomalyAlertClosePatchRequest = {
    closureEvidenceRefs: Array<string>;
    closureNote: string;
    issueChecklist: Array<string>;
    note?: string;
    operation: 'CLOSE';
    ticketReference?: string;
};

