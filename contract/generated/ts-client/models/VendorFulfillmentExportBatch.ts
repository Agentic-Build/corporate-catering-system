/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { TaipeiBusinessDateTime } from './TaipeiBusinessDateTime';
import type { VendorFulfillmentBatchArtifacts } from './VendorFulfillmentBatchArtifacts';
import type { VendorFulfillmentBoard } from './VendorFulfillmentBoard';
export type VendorFulfillmentExportBatch = {
    artifacts: VendorFulfillmentBatchArtifacts;
    batchId: string;
    board: VendorFulfillmentBoard;
    capturedAt: TaipeiBusinessDateTime;
    deliveryDate: string;
    generatedByActorId: ActorId;
    vendorId: string;
};

