/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ErrorCode } from './ErrorCode';
import type { ErrorDetail } from './ErrorDetail';
export type ErrorResponse = {
    code: ErrorCode;
    details?: Array<ErrorDetail>;
    message: string;
    requestId: string;
};

