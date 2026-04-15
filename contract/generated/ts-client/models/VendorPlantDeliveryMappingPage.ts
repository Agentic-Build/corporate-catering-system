/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PageMeta } from './PageMeta';
import type { VendorPlantDeliveryMapping } from './VendorPlantDeliveryMapping';
import type { VendorPlantDeliveryMappingAuditEntry } from './VendorPlantDeliveryMappingAuditEntry';
export type VendorPlantDeliveryMappingPage = {
    auditTrail: Array<VendorPlantDeliveryMappingAuditEntry>;
    items: Array<VendorPlantDeliveryMapping>;
    page: PageMeta;
};

