/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuHealthTag } from './MenuHealthTag';
import type { Money } from './Money';
export type VendorMenuItemUpsertRequest = {
    deliveryDate: string;
    description: string;
    healthTags?: Array<MenuHealthTag>;
    maxDailyQuantity: number;
    name: string;
    price: Money;
};

