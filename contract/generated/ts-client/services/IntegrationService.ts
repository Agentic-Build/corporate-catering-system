/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { PayrollDeductionPage } from '../models/PayrollDeductionPage';
import type { PayrollSortField } from '../models/PayrollSortField';
import type { SortOrder } from '../models/SortOrder';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class IntegrationService {
    /**
     * Export payroll deduction records
     * @param payPeriod
     * @param page
     * @param pageSize
     * @param sortBy
     * @param sortOrder
     * @returns PayrollDeductionPage Payroll deduction export page
     * @throws ApiError
     */
    public static exportPayrollDeductions(
        payPeriod: string,
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
                'page': page,
                'pageSize': pageSize,
                'sortBy': sortBy,
                'sortOrder': sortOrder,
            },
            errors: {
                400: `Request payload or query is invalid.`,
                401: `Authentication token is missing or invalid.`,
                403: `Authenticated actor is not authorized to perform this operation.`,
            },
        });
    }
}
