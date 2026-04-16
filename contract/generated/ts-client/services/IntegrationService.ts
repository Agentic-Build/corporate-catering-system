/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PayrollDeductionPage } from '../models/PayrollDeductionPage';
import type { PayrollHrApiSyncRequest } from '../models/PayrollHrApiSyncRequest';
import type { PayrollHrApiSyncResponse } from '../models/PayrollHrApiSyncResponse';
import type { PayrollSortField } from '../models/PayrollSortField';
import type { SortOrder } from '../models/SortOrder';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class IntegrationService {
    /**
     * Export payroll deduction records
     * @param payPeriod
     * @param cycleKey
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @returns PayrollDeductionPage Payroll deduction export page
     * @throws ApiError
     */
    public static exportPayrollDeductions(
        payPeriod: string,
        cycleKey: string,
        page: number = 1,
        pageSize: number = 20,
        sortBy?: PayrollSortField,
        sortOrder?: SortOrder,
    ): CancelablePromise<PayrollDeductionPage> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/api/v1/integrations/payroll/deductions',
            query: {
                'payPeriod': payPeriod,
                'cycleKey': cycleKey,
                'page': page,
                'pageSize': pageSize,
                'sortBy': sortBy,
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
     * Trigger optional HR API adjunct sync for an SFTP payroll batch
     * @param batchId
     * @param requestBody
     * @returns PayrollHrApiSyncResponse Batch HR API adjunct sync status
     * @throws ApiError
     */
    public static syncPayrollHrApiAdjunct(
        batchId: string,
        requestBody: PayrollHrApiSyncRequest,
    ): CancelablePromise<PayrollHrApiSyncResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/api/v1/integrations/payroll/sftp-batches/{batchId}/hr-api-sync',
            path: {
                'batchId': batchId,
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
