/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ObjectStorageUploadMetadata } from './ObjectStorageUploadMetadata';
import type { ObjectStorageUploadTarget } from './ObjectStorageUploadTarget';
export type ObjectStorageUploadPlanResponse = {
    metadata: ObjectStorageUploadMetadata;
    primary: ObjectStorageUploadTarget;
    thumbnail?: ObjectStorageUploadTarget;
};

