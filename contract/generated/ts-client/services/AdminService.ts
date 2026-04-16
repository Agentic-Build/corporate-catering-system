/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from '../models/ActorId';
import type { AdminAnomalyAlertPatchRequest } from '../models/AdminAnomalyAlertPatchRequest';
import type { AdminPayrollDisputePatchRequest } from '../models/AdminPayrollDisputePatchRequest';
import type { AdminVendorReviewRequest } from '../models/AdminVendorReviewRequest';
import type { AnomalyAlert } from '../models/AnomalyAlert';
import type { AnomalyAlertEvaluationRequest } from '../models/AnomalyAlertEvaluationRequest';
import type { AnomalyAlertEvaluationResponse } from '../models/AnomalyAlertEvaluationResponse';
import type { AnomalyAlertListResponse } from '../models/AnomalyAlertListResponse';
import type { AnomalyAlertStatus } from '../models/AnomalyAlertStatus';
import type { AnomalyRule } from '../models/AnomalyRule';
import type { AnomalyRuleListResponse } from '../models/AnomalyRuleListResponse';
import type { AnomalyRuleUpsertRequest } from '../models/AnomalyRuleUpsertRequest';
import type { AnomalySlaStatus } from '../models/AnomalySlaStatus';
import type { AuditAction } from '../models/AuditAction';
import type { AuditEntityType } from '../models/AuditEntityType';
import type { AuditInvestigationResponse } from '../models/AuditInvestigationResponse';
import type { AuditResponsibilityResponse } from '../models/AuditResponsibilityResponse';
import type { AuditRetentionPurgeRequest } from '../models/AuditRetentionPurgeRequest';
import type { AuditRetentionPurgeResponse } from '../models/AuditRetentionPurgeResponse';
import type { ObjectStorageAccessLinkRequest } from '../models/ObjectStorageAccessLinkRequest';
import type { ObjectStorageAccessLinkResponse } from '../models/ObjectStorageAccessLinkResponse';
import type { OperationsAnalyticsDashboard } from '../models/OperationsAnalyticsDashboard';
import type { OrderRetentionPurgeRequest } from '../models/OrderRetentionPurgeRequest';
import type { OrderRetentionPurgeResponse } from '../models/OrderRetentionPurgeResponse';
import type { PayrollDeductionPage } from '../models/PayrollDeductionPage';
import type { PayrollDispute } from '../models/PayrollDispute';
import type { PayrollMonthlySettlementCloseRequest } from '../models/PayrollMonthlySettlementCloseRequest';
import type { PayrollRetentionPurgeRequest } from '../models/PayrollRetentionPurgeRequest';
import type { PayrollRetentionPurgeResponse } from '../models/PayrollRetentionPurgeResponse';
import type { PayrollSettlementCycleLockRequest } from '../models/PayrollSettlementCycleLockRequest';
import type { PayrollSettlementCycleLockResponse } from '../models/PayrollSettlementCycleLockResponse';
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
     * Get advanced operations analytics dashboard with vendor/plant/time breakdowns
     * @param fromEpochDay Inclusive start epoch day for operations analytics dashboard range.
     * @param toEpochDay Inclusive end epoch day for operations analytics dashboard range.
     * @returns OperationsAnalyticsDashboard Admin operations analytics breakdown and metric catalog
     * @throws ApiError
     */
    public static getAdminOperationsAnalyticsDashboard(
        fromEpochDay?: number,
        toEpochDay?: number,
    ): CancelablePromise<OperationsAnalyticsDashboard> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/analytics/operations-dashboard',
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
     * Query anomaly alerts with escalation and SLA state
     * @param vendorId
     * @param ownerActorId
     * @param status
     * @param escalatedOnly
     * @param slaStatus
     * @param asOfEpochDay
     * @param asOfMinuteOfDay
     * @returns AnomalyAlertListResponse Anomaly alerts that satisfy the supplied filters
     * @throws ApiError
     */
    public static listAnomalyAlerts(
        vendorId?: string,
        ownerActorId?: ActorId,
        status?: AnomalyAlertStatus,
        escalatedOnly?: boolean,
        slaStatus?: AnomalySlaStatus,
        asOfEpochDay?: number,
        asOfMinuteOfDay?: number,
    ): CancelablePromise<AnomalyAlertListResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/anomaly/alerts',
            query: {
                'vendorId': vendorId,
                'ownerActorId': ownerActorId,
                'status': status,
                'escalatedOnly': escalatedOnly,
                'slaStatus': slaStatus,
                'asOfEpochDay': asOfEpochDay,
                'asOfMinuteOfDay': asOfMinuteOfDay,
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
     * Evaluate anomaly rules and trigger tracked remediation alerts
     * @param requestBody
     * @returns AnomalyAlertEvaluationResponse Anomaly evaluation outcome with triggered alerts
     * @throws ApiError
     */
    public static evaluateAnomalyAlerts(
        requestBody: AnomalyAlertEvaluationRequest,
    ): CancelablePromise<AnomalyAlertEvaluationResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/anomaly/alerts/evaluations',
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
     * Assign owner and advance anomaly remediation lifecycle
     * @param alertId
     * @param requestBody
     * @returns AnomalyAlert Updated anomaly alert lifecycle record
     * @throws ApiError
     */
    public static updateAdminAnomalyAlert(
        alertId: string,
        requestBody: AdminAnomalyAlertPatchRequest,
    ): CancelablePromise<AnomalyAlert> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/api/v1/admin/anomaly/alerts/{alertId}',
            path: {
                'alertId': alertId,
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
     * List anomaly detection governance rules
     * @returns AnomalyRuleListResponse Configured anomaly detection rules
     * @throws ApiError
     */
    public static listAnomalyRules(): CancelablePromise<AnomalyRuleListResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/admin/anomaly/rules',
            errors: {
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Upsert anomaly detection governance rule
     * @param ruleId
     * @param requestBody
     * @returns AnomalyRule Upserted anomaly rule
     * @throws ApiError
     */
    public static upsertAnomalyRule(
        ruleId: string,
        requestBody: AnomalyRuleUpsertRequest,
    ): CancelablePromise<AnomalyRule> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/api/v1/admin/anomaly/rules/{ruleId}',
            path: {
                'ruleId': ruleId,
            },
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
     * Create a presigned object-storage access link for managed administrative artifacts
     * @param requestBody
     * @returns ObjectStorageAccessLinkResponse Presigned download link for an existing object-storage reference
     * @throws ApiError
     */
    public static createAdminObjectStorageAccessLink(
        requestBody: ObjectStorageAccessLinkRequest,
    ): CancelablePromise<ObjectStorageAccessLinkResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/object-storage/access-links',
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
     * Execute order retention purge by policy
     * @param requestBody
     * @returns OrderRetentionPurgeResponse Order retention purge result
     * @throws ApiError
     */
    public static purgeOrderData(
        requestBody: OrderRetentionPurgeRequest,
    ): CancelablePromise<OrderRetentionPurgeResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/orders/retention-purge',
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
     * Assign and resolve payroll disputes with immutable trace
     * @param disputeId
     * @param requestBody
     * @returns PayrollDispute Updated payroll dispute lifecycle record
     * @throws ApiError
     */
    public static updateAdminPayrollDispute(
        disputeId: string,
        requestBody: AdminPayrollDisputePatchRequest,
    ): CancelablePromise<PayrollDispute> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/api/v1/admin/payroll/disputes/{disputeId}',
            path: {
                'disputeId': disputeId,
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
     * Close previous Taipei monthly payroll settlement cycle and emit SFTP snapshot
     * @param requestBody
     * @returns PayrollDeductionPage Monthly payroll settlement snapshot
     * @throws ApiError
     */
    public static closePayrollMonthlySettlement(
        requestBody?: PayrollMonthlySettlementCloseRequest,
    ): CancelablePromise<PayrollDeductionPage> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/payroll/monthly-settlements/close',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
                409: `Request conflicts with business constraints.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Lock a monthly payroll settlement cycle with explicit reason
     * @param cycleKey
     * @param requestBody
     * @returns PayrollSettlementCycleLockResponse Settlement cycle lock state
     * @throws ApiError
     */
    public static lockPayrollSettlementCycle(
        cycleKey: string,
        requestBody: PayrollSettlementCycleLockRequest,
    ): CancelablePromise<PayrollSettlementCycleLockResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/payroll/monthly-settlements/{cycleKey}/lock',
            path: {
                'cycleKey': cycleKey,
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
     * Unlock a monthly payroll settlement cycle for authorized recomputation
     * @param cycleKey
     * @param requestBody
     * @returns PayrollSettlementCycleLockResponse Settlement cycle lock state
     * @throws ApiError
     */
    public static unlockPayrollSettlementCycle(
        cycleKey: string,
        requestBody: PayrollSettlementCycleLockRequest,
    ): CancelablePromise<PayrollSettlementCycleLockResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/payroll/monthly-settlements/{cycleKey}/unlock',
            path: {
                'cycleKey': cycleKey,
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
     * Execute payroll and dispute retention purge by policy
     * @param requestBody
     * @returns PayrollRetentionPurgeResponse Payroll retention purge result
     * @throws ApiError
     */
    public static purgePayrollData(
        requestBody: PayrollRetentionPurgeRequest,
    ): CancelablePromise<PayrollRetentionPurgeResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/admin/payroll/retention-purge',
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
