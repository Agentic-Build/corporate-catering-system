/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { VendorComplianceDocumentRecord } from './VendorComplianceDocumentRecord';
import type { VendorComplianceRetentionPolicy } from './VendorComplianceRetentionPolicy';
import type { VendorLifecycleEvent } from './VendorLifecycleEvent';
export type VendorComplianceSummary = {
    documents: Array<VendorComplianceDocumentRecord>;
    lifecycleHistory: Array<VendorLifecycleEvent>;
    retentionPolicy: VendorComplianceRetentionPolicy;
};

