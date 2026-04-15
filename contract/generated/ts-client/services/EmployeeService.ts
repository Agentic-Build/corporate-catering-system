/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrder } from '../models/EmployeeOrder';
import type { EmployeeOrderCreateRequest } from '../models/EmployeeOrderCreateRequest';
import type { EmployeeOrderPatchRequest } from '../models/EmployeeOrderPatchRequest';
import type { MenuHealthTag } from '../models/MenuHealthTag';
import type { MenuPage } from '../models/MenuPage';
import type { MenuSortField } from '../models/MenuSortField';
import type { PickupVerificationRequest } from '../models/PickupVerificationRequest';
import type { PickupVerificationResponse } from '../models/PickupVerificationResponse';
import type { PlantId } from '../models/PlantId';
import type { SortOrder } from '../models/SortOrder';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class EmployeeService {
    /**
     * List available menus
     * @param plantId Target plant for scoping.
     * @param menuDate
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @param cuisine
     * @param healthTag
     * @returns MenuPage Paginated menu list
     * @throws ApiError
     */
    public static listEmployeeMenus(
        plantId: PlantId,
        menuDate: string,
        page: number = 1,
        pageSize: number = 20,
        sortBy?: MenuSortField,
        sortOrder?: SortOrder,
        cuisine?: string,
        healthTag?: MenuHealthTag,
    ): CancelablePromise<MenuPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/employee/menus',
            query: {
                'plantId': plantId,
                'menuDate': menuDate,
                'page': page,
                'pageSize': pageSize,
                'sortBy': sortBy,
                'sortOrder': sortOrder,
                'cuisine': cuisine,
                'healthTag': healthTag,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
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
                500: `Internal server error while processing request.`,
            },
        });
    }
}
