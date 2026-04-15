/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuHealthTag } from './MenuHealthTag';
import type { Money } from './Money';
export type MenuListItem = {
    cuisine?: string;
    deliveryDate: string;
    description?: string;
    healthTags: Array<MenuHealthTag>;
    menuItemId: string;
    name: string;
    price: Money;
    remainingQuantity: number;
    vendorId: string;
};

