/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuHealthTag } from './MenuHealthTag';
import type { MenuType } from './MenuType';
import type { Money } from './Money';
export type MenuListItem = {
    cutoffDate: string;
    deliveryDate: string;
    description: string;
    earliestDeliveryDate: string;
    healthTags: Array<MenuHealthTag>;
    imageUrl?: string;
    latestDeliveryDate: string;
    menuItemId: string;
    menuType: MenuType;
    modifyCancelCutoffMinuteOfDay: number;
    name: string;
    preorderOpen: boolean;
    preorderOpenDaysAhead: number;
    price: Money;
    remainingQuantity: number;
    vendorId: string;
};

