/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderStatus } from '../models/EmployeeOrderStatus';
import type { PlantId } from '../models/PlantId';
import type { SortOrder } from '../models/SortOrder';
import type { VendorFulfillmentBatchCreateRequest } from '../models/VendorFulfillmentBatchCreateRequest';
import type { VendorFulfillmentBoard } from '../models/VendorFulfillmentBoard';
import type { VendorFulfillmentDeliveryStatusTransitionRequest } from '../models/VendorFulfillmentDeliveryStatusTransitionRequest';
import type { VendorFulfillmentDeliveryStatusTransitionResult } from '../models/VendorFulfillmentDeliveryStatusTransitionResult';
import type { VendorFulfillmentExportBatch } from '../models/VendorFulfillmentExportBatch';
import type { VendorMenuItem } from '../models/VendorMenuItem';
import type { VendorMenuItemUpsertRequest } from '../models/VendorMenuItemUpsertRequest';
import type { VendorOrderPage } from '../models/VendorOrderPage';
import type { VendorOrderSortField } from '../models/VendorOrderSortField';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class VendorService {
    /**
     * Create immutable fulfillment export batch snapshot
     * @param requestBody
     * @returns VendorFulfillmentExportBatch Fulfillment export batch snapshot created
     * @throws ApiError
     */
    public static createVendorFulfillmentExportBatch(
        requestBody: VendorFulfillmentBatchCreateRequest,
    ): CancelablePromise<VendorFulfillmentExportBatch> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/vendor/fulfillment-batches',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                409: `Request conflicts with business constraints.`,
                422: `Request is syntactically valid but violates business validation rules.`,
            },
        });
    }
    /**
     * Read immutable fulfillment export batch snapshot
     * @param batchId
     * @returns VendorFulfillmentExportBatch Fulfillment export batch snapshot
     * @throws ApiError
     */
    public static getVendorFulfillmentExportBatch(
        batchId: string,
    ): CancelablePromise<VendorFulfillmentExportBatch> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/vendor/fulfillment-batches/{batchId}',
            path: {
                'batchId': batchId,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                404: `Requested resource was not found.`,
            },
        });
    }
    /**
     * Get real-time vendor fulfillment board with per-plant operational metrics
     * @param deliveryDate Target delivery date in Asia/Taipei for fulfillment board and export snapshots.
     * @param plantId
     * @param includeAuditTransitions When false, omits status transition audit entries from fulfillment board payload.
     * @returns VendorFulfillmentBoard Vendor fulfillment operations board
     * @throws ApiError
     */
    public static listVendorFulfillmentBoard(
        deliveryDate: string,
        plantId?: PlantId,
        includeAuditTransitions: boolean = true,
    ): CancelablePromise<VendorFulfillmentBoard> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/vendor/fulfillment-board',
            query: {
                'deliveryDate': deliveryDate,
                'plantId': plantId,
                'includeAuditTransitions': includeAuditTransitions,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
            },
        });
    }
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
    /**
     * Advance delivery execution status for a vendor order
     * @param orderId
     * @param requestBody
     * @returns VendorFulfillmentDeliveryStatusTransitionResult Delivery execution status transition accepted
     * @throws ApiError
     */
    public static advanceVendorFulfillmentDeliveryStatus(
        orderId: string,
        requestBody: VendorFulfillmentDeliveryStatusTransitionRequest,
    ): CancelablePromise<VendorFulfillmentDeliveryStatusTransitionResult> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/vendor/orders/{orderId}/delivery-status',
            path: {
                'orderId': orderId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                404: `Requested resource was not found.`,
                409: `Request conflicts with business constraints.`,
                422: `Request is syntactically valid but violates business validation rules.`,
            },
        });
    }
}
