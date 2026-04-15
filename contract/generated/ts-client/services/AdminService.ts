/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AdminVendorReviewRequest } from '../models/AdminVendorReviewRequest';
import type { SortOrder } from '../models/SortOrder';
import type { VendorCategory } from '../models/VendorCategory';
import type { VendorComplianceDocumentTemplate } from '../models/VendorComplianceDocumentTemplate';
import type { VendorComplianceDocumentTemplatePage } from '../models/VendorComplianceDocumentTemplatePage';
import type { VendorComplianceDocumentTemplateUpsertRequest } from '../models/VendorComplianceDocumentTemplateUpsertRequest';
import type { VendorComplianceLifecycleExecutionRequest } from '../models/VendorComplianceLifecycleExecutionRequest';
import type { VendorComplianceLifecycleExecutionResult } from '../models/VendorComplianceLifecycleExecutionResult';
import type { VendorEnrollment } from '../models/VendorEnrollment';
import type { VendorEnrollmentPage } from '../models/VendorEnrollmentPage';
import type { VendorSortField } from '../models/VendorSortField';
import type { VendorStatus } from '../models/VendorStatus';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class AdminService {
    /**
     * List vendor compliance document templates by category
     * @param vendorCategory
     * @returns VendorComplianceDocumentTemplatePage Compliance document templates
     * @throws ApiError
     */
    public static listComplianceDocumentTemplates(
        vendorCategory?: VendorCategory,
    ): CancelablePromise<VendorComplianceDocumentTemplatePage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/compliance/document-templates',
            query: {
                'vendorCategory': vendorCategory,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
            },
        });
    }
    /**
     * Create or update a compliance document template for a vendor category
     * @param vendorCategory
     * @param templateId
     * @param requestBody
     * @returns VendorComplianceDocumentTemplate Template upserted
     * @throws ApiError
     */
    public static upsertComplianceDocumentTemplate(
        vendorCategory: VendorCategory,
        templateId: string,
        requestBody: VendorComplianceDocumentTemplateUpsertRequest,
    ): CancelablePromise<VendorComplianceDocumentTemplate> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/admin/compliance/document-templates/{vendorCategory}/{templateId}',
            path: {
                'vendorCategory': vendorCategory,
                'templateId': templateId,
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
     * Run automated compliance lifecycle evaluation
     * @param requestBody
     * @returns VendorComplianceLifecycleExecutionResult Lifecycle evaluation accepted
     * @throws ApiError
     */
    public static runVendorComplianceLifecycle(
        requestBody: VendorComplianceLifecycleExecutionRequest,
    ): CancelablePromise<VendorComplianceLifecycleExecutionResult> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/compliance/lifecycle/executions',
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
     * Approve, reject, or request fixes for vendor application
     * @param vendorId
     * @param requestBody
     * @returns VendorEnrollment Vendor enrollment decision accepted
     * @throws ApiError
     */
    public static reviewVendorApplication(
        vendorId: string,
        requestBody: AdminVendorReviewRequest,
    ): CancelablePromise<VendorEnrollment> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/vendors/{vendorId}/reviews',
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
