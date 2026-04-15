/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuHealthTag } from './MenuHealthTag';
import type { Money } from './Money';
import type { PlantId } from './PlantId';
export type VendorMenuItemUpsertRequest = {
    deliverablePlantIds: Array<PlantId>;
    deliveryDate: string;
    description: string;
    healthTags?: Array<MenuHealthTag>;
    maxDailyQuantity: number;
    name: string;
    price: Money;
};

