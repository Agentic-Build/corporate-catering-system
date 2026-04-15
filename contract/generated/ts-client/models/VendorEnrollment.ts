/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { VendorCategory } from './VendorCategory';
import type { VendorComplianceSummary } from './VendorComplianceSummary';
import type { VendorReviewHistoryEntry } from './VendorReviewHistoryEntry';
import type { VendorStatus } from './VendorStatus';
export type VendorEnrollment = {
    compliance: VendorComplianceSummary;
    displayName: string;
    reviewHistory: Array<VendorReviewHistoryEntry>;
    status: VendorStatus;
    updatedAt: string;
    vendorCategory: VendorCategory;
    vendorId: string;
};

