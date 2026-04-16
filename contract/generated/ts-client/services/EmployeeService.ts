/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrder } from '../models/EmployeeOrder';
import type { EmployeeOrderCreateRequest } from '../models/EmployeeOrderCreateRequest';
import type { EmployeeOrderPage } from '../models/EmployeeOrderPage';
import type { EmployeeOrderPatchRequest } from '../models/EmployeeOrderPatchRequest';
import type { EmployeeOrderPayrollLedger } from '../models/EmployeeOrderPayrollLedger';
import type { EmployeeOrderSortField } from '../models/EmployeeOrderSortField';
import type { EmployeeOrderStatus } from '../models/EmployeeOrderStatus';
import type { EmployeePayrollDisputeCreateRequest } from '../models/EmployeePayrollDisputeCreateRequest';
import type { EmployeeRushReminderPreferences } from '../models/EmployeeRushReminderPreferences';
import type { EmployeeRushReminderPreferencesUpsertRequest } from '../models/EmployeeRushReminderPreferencesUpsertRequest';
import type { MenuDiscoveryView } from '../models/MenuDiscoveryView';
import type { MenuHealthTag } from '../models/MenuHealthTag';
import type { MenuPage } from '../models/MenuPage';
import type { MenuSortField } from '../models/MenuSortField';
import type { MenuType } from '../models/MenuType';
import type { PayrollDispute } from '../models/PayrollDispute';
import type { PickupVerificationRequest } from '../models/PickupVerificationRequest';
import type { PickupVerificationResponse } from '../models/PickupVerificationResponse';
import type { PlantId } from '../models/PlantId';
import type { SortOrder } from '../models/SortOrder';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class EmployeeService {
    /**
     * List discoverable menus for multi-day preorder
     * @param plantId Target plant for scoping.
     * @param view
     * @param menuDate Anchor date for week/calendar discovery windows in Asia/Taipei.
     * @param fromDate
     * @param toDate
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @param search
     * @param menuType
     * @param healthTag
     * @param priceMinMinor
     * @param priceMaxMinor
     * @param remainingQuantity Exact inventory counter filter. Matches only items whose remaining quantity equals this value.
     * @returns MenuPage Deterministic multi-day menu discovery page
     * @throws ApiError
     */
    public static listEmployeeMenus(
        plantId: PlantId,
        view?: MenuDiscoveryView,
        menuDate?: string,
        fromDate?: string,
        toDate?: string,
        page: number = 1,
        pageSize: number = 20,
        sortBy?: MenuSortField,
        sortOrder?: SortOrder,
        search?: string,
        menuType?: MenuType,
        healthTag?: MenuHealthTag,
        priceMinMinor?: number,
        priceMaxMinor?: number,
        remainingQuantity?: number,
    ): CancelablePromise<MenuPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/employee/menus',
            query: {
                'plantId': plantId,
                'view': view,
                'menuDate': menuDate,
                'fromDate': fromDate,
                'toDate': toDate,
                'page': page,
                'pageSize': pageSize,
                'sortBy': sortBy,
                'sortOrder': sortOrder,
                'search': search,
                'menuType': menuType,
                'healthTag': healthTag,
                'priceMinMinor': priceMinMinor,
                'priceMaxMinor': priceMaxMinor,
                'remainingQuantity': remainingQuantity,
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
     * List employee orders
     * @param plantId Target plant for scoping.
     * @param fromDate
     * @param toDate
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @param status
     * @returns EmployeeOrderPage Paginated employee order history and active preorder entries
     * @throws ApiError
     */
    public static listEmployeeOrders(
        plantId: PlantId,
        fromDate?: string,
        toDate?: string,
        page: number = 1,
        pageSize: number = 20,
        sortBy?: EmployeeOrderSortField,
        sortOrder?: SortOrder,
        status?: EmployeeOrderStatus,
    ): CancelablePromise<EmployeeOrderPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/employee/orders',
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
     * Create a meal order
     * @param requestBody
     * @returns EmployeeOrder Order created
     * @throws ApiError
     */
    public static createEmployeeOrder(
        requestBody: EmployeeOrderCreateRequest,
    ): CancelablePromise<EmployeeOrder> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/employee/orders',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                409: `Request conflicts with business constraints.`,
                422: `Request is syntactically valid but violates business validation rules.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Modify an existing order before cutoff
     * @param orderId
     * @param requestBody
     * @returns EmployeeOrder Order updated
     * @throws ApiError
     */
    public static updateEmployeeOrder(
        orderId: string,
        requestBody: EmployeeOrderPatchRequest,
    ): CancelablePromise<EmployeeOrder> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/api/v1/employee/orders/{orderId}',
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
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Open a payroll dispute for an order deduction
     * @param orderId
     * @param requestBody
     * @returns PayrollDispute Payroll dispute opened with immutable trace seed
     * @throws ApiError
     */
    public static createEmployeeOrderDispute(
        orderId: string,
        requestBody: EmployeePayrollDisputeCreateRequest,
    ): CancelablePromise<PayrollDispute> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/employee/orders/{orderId}/disputes',
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
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Get immutable payroll ledger and dispute state for an order
     * @param orderId
     * @returns EmployeeOrderPayrollLedger Per-order payroll ledger, adjustments, refunds, and disputes
     * @throws ApiError
     */
    public static getEmployeeOrderPayrollLedger(
        orderId: string,
    ): CancelablePromise<EmployeeOrderPayrollLedger> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/employee/orders/{orderId}/payroll-ledger',
            path: {
                'orderId': orderId,
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
     * Verify order pickup handoff
     * @param orderId
     * @param requestBody
     * @returns PickupVerificationResponse Pickup verification accepted
     * @throws ApiError
     */
    public static verifyPickupOrder(
        orderId: string,
        requestBody: PickupVerificationRequest,
    ): CancelablePromise<PickupVerificationResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/employee/orders/{orderId}/pickup-verifications',
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
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Update employee rush reminder preferences
     * @param requestBody
     * @returns EmployeeRushReminderPreferences Employee rush reminder preferences persisted
     * @throws ApiError
     */
    public static upsertEmployeeRushReminderPreferences(
        requestBody: EmployeeRushReminderPreferencesUpsertRequest,
    ): CancelablePromise<EmployeeRushReminderPreferences> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/employee/rush-reminder-preferences',
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
}
