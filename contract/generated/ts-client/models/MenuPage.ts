/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { MenuDiscoveryDay } from './MenuDiscoveryDay';
import type { MenuDiscoveryView } from './MenuDiscoveryView';
import type { MenuListItem } from './MenuListItem';
import type { PageMeta } from './PageMeta';
export type MenuPage = {
    days: Array<MenuDiscoveryDay>;
    fromDate: string;
    items: Array<MenuListItem>;
    page: PageMeta;
    recommendationApplied: boolean;
    recommendationRequested: boolean;
    timezone: 'Asia/Taipei';
    toDate: string;
    view: MenuDiscoveryView;
};

