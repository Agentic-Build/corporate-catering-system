/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuHealthTag } from './MenuHealthTag';
import type { Money } from './Money';
export type VendorMenuItem = {
    deliveryDate: string;
    description: string;
    healthTags?: Array<MenuHealthTag>;
    maxDailyQuantity: number;
    menuItemId: string;
    name: string;
    price: Money;
    vendorId: string;
};

