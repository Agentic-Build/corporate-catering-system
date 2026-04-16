/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from '../models/ActorId';
import type { AdminVendorReviewRequest } from '../models/AdminVendorReviewRequest';
import type { AuditAction } from '../models/AuditAction';
import type { AuditEntityType } from '../models/AuditEntityType';
import type { AuditInvestigationResponse } from '../models/AuditInvestigationResponse';
import type { AuditResponsibilityResponse } from '../models/AuditResponsibilityResponse';
import type { AuditRetentionPurgeRequest } from '../models/AuditRetentionPurgeRequest';
import type { AuditRetentionPurgeResponse } from '../models/AuditRetentionPurgeResponse';
import type { PlantId } from '../models/PlantId';
import type { SortOrder } from '../models/SortOrder';
import type { TaipeiBusinessDateTime } from '../models/TaipeiBusinessDateTime';
import type { VendorCategory } from '../models/VendorCategory';
import type { VendorComplianceDocumentTemplate } from '../models/VendorComplianceDocumentTemplate';
import type { VendorComplianceDocumentTemplatePage } from '../models/VendorComplianceDocumentTemplatePage';
import type { VendorComplianceDocumentTemplateUpsertRequest } from '../models/VendorComplianceDocumentTemplateUpsertRequest';
import type { VendorComplianceLifecycleExecutionRequest } from '../models/VendorComplianceLifecycleExecutionRequest';
import type { VendorComplianceLifecycleExecutionResult } from '../models/VendorComplianceLifecycleExecutionResult';
import type { VendorEnrollment } from '../models/VendorEnrollment';
import type { VendorEnrollmentPage } from '../models/VendorEnrollmentPage';
import type { VendorPlantDeliveryMapping } from '../models/VendorPlantDeliveryMapping';
import type { VendorPlantDeliveryMappingPage } from '../models/VendorPlantDeliveryMappingPage';
import type { VendorPlantDeliveryMappingUpsertRequest } from '../models/VendorPlantDeliveryMappingUpsertRequest';
import type { VendorSortField } from '../models/VendorSortField';
import type { VendorStatus } from '../models/VendorStatus';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class AdminService {
    /**
     * Query immutable audit evidence for investigations
     * @param actorId
     * @param action
     * @param entityType
     * @param entityId
     * @param occurredFromEpochDay
     * @param occurredToEpochDay
     * @param correlationId
     * @returns AuditInvestigationResponse Immutable audit evidence matching investigation filters
     * @throws ApiError
     */
    public static queryAuditInvestigations(
        actorId?: ActorId,
        action?: AuditAction,
        entityType?: AuditEntityType,
        entityId?: string,
        occurredFromEpochDay?: number,
        occurredToEpochDay?: number,
        correlationId?: string,
    ): CancelablePromise<AuditInvestigationResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/audit/investigations',
            query: {
                'actorId': actorId,
                'action': action,
                'entityType': entityType,
                'entityId': entityId,
                'occurredFromEpochDay': occurredFromEpochDay,
                'occurredToEpochDay': occurredToEpochDay,
                'correlationId': correlationId,
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
     * Attribute investigation responsibility by actor identity
     * @param actorId
     * @param action
     * @param entityType
     * @param entityId
     * @param occurredFromEpochDay
     * @param occurredToEpochDay
     * @param correlationId
     * @returns AuditResponsibilityResponse Investigation responsibility attribution grouped by actor
     * @throws ApiError
     */
    public static queryAuditResponsibilities(
        actorId?: ActorId,
        action?: AuditAction,
        entityType?: AuditEntityType,
        entityId?: string,
        occurredFromEpochDay?: number,
        occurredToEpochDay?: number,
        correlationId?: string,
    ): CancelablePromise<AuditResponsibilityResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/audit/responsibilities',
            query: {
                'actorId': actorId,
                'action': action,
                'entityType': entityType,
                'entityId': entityId,
                'occurredFromEpochDay': occurredFromEpochDay,
                'occurredToEpochDay': occurredToEpochDay,
                'correlationId': correlationId,
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
     * Execute audit evidence retention purge by policy
     * @param requestBody
     * @returns AuditRetentionPurgeResponse Audit evidence retention purge result
     * @throws ApiError
     */
    public static purgeAuditEvidence(
        requestBody: AuditRetentionPurgeRequest,
    ): CancelablePromise<AuditRetentionPurgeResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/audit/retention-purge',
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
     * List vendor plant delivery mappings and their audit history
     * @param vendorId
     * @param plantId
     * @param activeAt Evaluate mappings active at this fixed Asia/Taipei business timestamp.
     * @param page
     * @param pageSize
     * @returns VendorPlantDeliveryMappingPage Paginated vendor plant delivery mappings
     * @throws ApiError
     */
    public static listVendorPlantDeliveryMappings(
        vendorId?: string,
        plantId?: PlantId,
        activeAt?: TaipeiBusinessDateTime,
        page: number = 1,
        pageSize: number = 20,
    ): CancelablePromise<VendorPlantDeliveryMappingPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/vendor-plant-delivery-mappings',
            query: {
                'vendorId': vendorId,
                'plantId': plantId,
                'activeAt': activeAt,
                'page': page,
                'pageSize': pageSize,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
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
     * Delete a vendor plant delivery mapping
     * @param vendorId
     * @param mappingId
     * @returns void
     * @throws ApiError
     */
    public static deleteVendorPlantDeliveryMapping(
        vendorId: string,
        mappingId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}',
            path: {
                'vendorId': vendorId,
                'mappingId': mappingId,
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
     * Create or update a vendor plant delivery mapping
     * @param vendorId
     * @param mappingId
     * @param requestBody
     * @returns VendorPlantDeliveryMapping Vendor plant delivery mapping upserted
     * @throws ApiError
     */
    public static upsertVendorPlantDeliveryMapping(
        vendorId: string,
        mappingId: string,
        requestBody: VendorPlantDeliveryMappingUpsertRequest,
    ): CancelablePromise<VendorPlantDeliveryMapping> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}',
            path: {
                'vendorId': vendorId,
                'mappingId': mappingId,
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
