/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderStatus } from '../models/EmployeeOrderStatus';
import type { PlantId } from '../models/PlantId';
import type { SortOrder } from '../models/SortOrder';
import type { VendorMenuItem } from '../models/VendorMenuItem';
import type { VendorMenuItemUpsertRequest } from '../models/VendorMenuItemUpsertRequest';
import type { VendorOrderPage } from '../models/VendorOrderPage';
import type { VendorOrderSortField } from '../models/VendorOrderSortField';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class VendorService {
    /**
     * Create or update a vendor menu item
     * @param menuItemId
     * @param requestBody
     * @returns VendorMenuItem Menu item upserted
     * @throws ApiError
     */
    public static upsertVendorMenuItem(
        menuItemId: string,
        requestBody: VendorMenuItemUpsertRequest,
    ): CancelablePromise<VendorMenuItem> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/vendor/menu-items/{menuItemId}',
            path: {
                'menuItemId': menuItemId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                422: `Request is syntactically valid but violates business validation rules.`,
            },
        });
    }
    /**
     * List vendor order board entries
     * @param plantId Target plant for scoping.
     * @param fromDate
     * @param toDate
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @param status
     * @returns VendorOrderPage Paginated vendor order board
     * @throws ApiError
     */
    public static listVendorOrders(
        plantId: PlantId,
        fromDate?: string,
        toDate?: string,
        page: number = 1,
        pageSize: number = 20,
        sortBy?: VendorOrderSortField,
        sortOrder?: SortOrder,
        status?: EmployeeOrderStatus,
    ): CancelablePromise<VendorOrderPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/vendor/orders',
            query: {
                'plantId': plantId,
                'fromDate': fromDate,
                'toDate': toDate,
                'page': page,
                'pageSize': pageSize,
                'sortBy': sortBy,
                'sortOrder': sortOrder,
                'status': status,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
            },
        });
    }
}
