/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { EmployeeOrderCancelPatchRequest } from './EmployeeOrderCancelPatchRequest';
import type { EmployeeOrderReplaceLineItemsPatchRequest } from './EmployeeOrderReplaceLineItemsPatchRequest';
/**
 * Order patch command. Supports line-item replacement and cancellation under the same cutoff governance.
 */
export type EmployeeOrderPatchRequest = (EmployeeOrderReplaceLineItemsPatchRequest | EmployeeOrderCancelPatchRequest);

