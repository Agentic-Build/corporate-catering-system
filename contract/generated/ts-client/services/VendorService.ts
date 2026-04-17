/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderStatus } from '../models/EmployeeOrderStatus';
import type { ObjectStorageAccessLinkRequest } from '../models/ObjectStorageAccessLinkRequest';
import type { ObjectStorageAccessLinkResponse } from '../models/ObjectStorageAccessLinkResponse';
import type { ObjectStorageUploadPlanRequest } from '../models/ObjectStorageUploadPlanRequest';
import type { ObjectStorageUploadPlanResponse } from '../models/ObjectStorageUploadPlanResponse';
import type { OperationsAnalyticsDashboard } from '../models/OperationsAnalyticsDashboard';
import type { PlantId } from '../models/PlantId';
import type { SortOrder } from '../models/SortOrder';
import type { VendorFulfillmentBatchCreateRequest } from '../models/VendorFulfillmentBatchCreateRequest';
import type { VendorFulfillmentBoard } from '../models/VendorFulfillmentBoard';
import type { VendorFulfillmentDeliveryStatusTransitionRequest } from '../models/VendorFulfillmentDeliveryStatusTransitionRequest';
import type { VendorFulfillmentDeliveryStatusTransitionResult } from '../models/VendorFulfillmentDeliveryStatusTransitionResult';
import type { VendorFulfillmentExportBatch } from '../models/VendorFulfillmentExportBatch';
import type { VendorMenuItem } from '../models/VendorMenuItem';
import type { VendorMenuItemStatus } from '../models/VendorMenuItemStatus';
import type { VendorMenuItemStatusPatchRequest } from '../models/VendorMenuItemStatusPatchRequest';
import type { VendorMenuItemUpsertRequest } from '../models/VendorMenuItemUpsertRequest';
import type { VendorMenuPage } from '../models/VendorMenuPage';
import type { VendorOrderingPolicy } from '../models/VendorOrderingPolicy';
import type { VendorOrderingPolicyUpsertRequest } from '../models/VendorOrderingPolicyUpsertRequest';
import type { VendorOrderPage } from '../models/VendorOrderPage';
import type { VendorOrderSortField } from '../models/VendorOrderSortField';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class VendorService {
    /**
     * Get advanced operations analytics dashboard scoped to the authenticated vendor
     * @param fromEpochDay Inclusive start epoch day for operations analytics dashboard range.
     * @param toEpochDay Inclusive end epoch day for operations analytics dashboard range.
     * @returns OperationsAnalyticsDashboard Vendor-scoped operations analytics breakdown and metric catalog
     * @throws ApiError
     */
    public static getVendorOperationsAnalyticsDashboard(
        fromEpochDay?: number,
        toEpochDay?: number,
    ): CancelablePromise<OperationsAnalyticsDashboard> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/vendor/analytics/operations-dashboard',
            query: {
                'fromEpochDay': fromEpochDay,
                'toEpochDay': toEpochDay,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                404: `Requested resource was not found.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
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
     * List vendor menu items with delivery window and status filters
     * @param fromDate
     * @param toDate
     * @param status
     * @param page
     * @param pageSize
     * @param sortOrder
     * @returns VendorMenuPage Paginated vendor menu inventory page
     * @throws ApiError
     */
    public static listVendorMenuItems(
        fromDate?: string,
        toDate?: string,
        status?: VendorMenuItemStatus,
        page: number = 1,
        pageSize: number = 20,
        sortOrder?: SortOrder,
    ): CancelablePromise<VendorMenuPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/vendor/menu-items',
            query: {
                'fromDate': fromDate,
                'toDate': toDate,
                'status': status,
                'page': page,
                'pageSize': pageSize,
                'sortOrder': sortOrder,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                500: `Internal server error while processing request.`,
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
     * Update vendor menu item listing status
     * @param menuItemId
     * @param requestBody
     * @returns VendorMenuItem Menu item status updated
     * @throws ApiError
     */
    public static updateVendorMenuItemStatus(
        menuItemId: string,
        requestBody: VendorMenuItemStatusPatchRequest,
    ): CancelablePromise<VendorMenuItem> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/api/v1/vendor/menu-items/{menuItemId}/status',
            path: {
                'menuItemId': menuItemId,
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
    /**
     * Create a presigned object-storage access link for vendor-managed artifacts
     * @param requestBody
     * @returns ObjectStorageAccessLinkResponse Presigned download link for an existing object-storage reference
     * @throws ApiError
     */
    public static createVendorObjectStorageAccessLink(
        requestBody: ObjectStorageAccessLinkRequest,
    ): CancelablePromise<ObjectStorageAccessLinkResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/vendor/object-storage/access-links',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Create a presigned object-storage upload plan for vendor artifacts
     * @param requestBody
     * @returns ObjectStorageUploadPlanResponse Presigned upload plan with metadata and optional thumbnail target
     * @throws ApiError
     */
    public static createVendorObjectStorageUploadPlan(
        requestBody: ObjectStorageUploadPlanRequest,
    ): CancelablePromise<ObjectStorageUploadPlanResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/vendor/object-storage/upload-plans',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Get effective vendor order-window policy
     * @returns VendorOrderingPolicy Effective ordering policy
     * @throws ApiError
     */
    public static getVendorOrderingPolicy(): CancelablePromise<VendorOrderingPolicy> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/vendor/ordering-policy',
            errors: {
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Update vendor order-window overrides
     * @param requestBody
     * @returns VendorOrderingPolicy Ordering policy updated
     * @throws ApiError
     */
    public static upsertVendorOrderingPolicy(
        requestBody: VendorOrderingPolicyUpsertRequest,
    ): CancelablePromise<VendorOrderingPolicy> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/vendor/ordering-policy',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                422: `Request is syntactically valid but violates business validation rules.`,
                500: `Internal server error while processing request.`,
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
                500: `Internal server error while processing request.`,
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
