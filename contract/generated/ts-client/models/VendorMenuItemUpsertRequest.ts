/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuHealthTag } from './MenuHealthTag';
import type { MenuType } from './MenuType';
import type { Money } from './Money';
export type VendorMenuItemUpsertRequest = {
    deliveryDate: string;
    description: string;
    healthTags?: Array<MenuHealthTag>;
    imageUrl?: string;
    maxDailyQuantity: number;
    menuType: MenuType;
    /**
     * Optional vendor override minute-of-day (Asia/Taipei) for previous-day modify/cancel cutoff.
     */
    modifyCancelCutoffMinuteOfDayOverride?: number;
    name: string;
    /**
     * Optional vendor override for how many days ahead preorder stays open.
     */
    preorderOpenDaysAheadOverride?: number;
    price: Money;
};

