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
    imageUrl?: string;
    maxDailyQuantity: number;
    menuItemId: string;
    modifyCancelCutoffMinuteOfDay: number;
    name: string;
    preorderOpenDaysAhead: number;
    price: Money;
    remainingQuantity: number;
    vendorId: string;
};

