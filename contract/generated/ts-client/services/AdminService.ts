/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AdminVendorApprovalRequest } from '../models/AdminVendorApprovalRequest';
import type { SortOrder } from '../models/SortOrder';
import type { VendorEnrollment } from '../models/VendorEnrollment';
import type { VendorEnrollmentPage } from '../models/VendorEnrollmentPage';
import type { VendorSortField } from '../models/VendorSortField';
import type { VendorStatus } from '../models/VendorStatus';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class AdminService {
    /**
     * List vendor enrollments
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @param status
     * @returns VendorEnrollmentPage Paginated vendor enrollments
     * @throws ApiError
     */
    public static listAdminVendors(
        page: number = 1,
        pageSize: number = 20,
        sortBy?: VendorSortField,
        sortOrder?: SortOrder,
        status?: VendorStatus,
    ): CancelablePromise<VendorEnrollmentPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/vendors',
            query: {
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
     * Approve or reject vendor enrollment
     * @param vendorId
     * @param requestBody
     * @returns VendorEnrollment Vendor enrollment decision accepted
     * @throws ApiError
     */
    public static approveVendorEnrollment(
        vendorId: string,
        requestBody: AdminVendorApprovalRequest,
    ): CancelablePromise<VendorEnrollment> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/vendors/{vendorId}/approvals',
            path: {
                'vendorId': vendorId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                404: `Requested resource was not found.`,
                422: `Request is syntactically valid but violates business validation rules.`,
            },
        });
    }
}
