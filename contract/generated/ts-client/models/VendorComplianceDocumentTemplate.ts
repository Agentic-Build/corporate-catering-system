/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { VendorCategory } from './VendorCategory';
export type VendorComplianceDocumentTemplate = {
    displayName: string;
    maxValidityDays: number;
    reminderDaysBeforeExpiry: Array<number>;
    required: boolean;
    suspensionGraceDays: number;
    templateId: string;
    updatedAt?: string;
    vendorCategory: VendorCategory;
};

