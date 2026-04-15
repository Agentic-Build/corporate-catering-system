/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
import type { VendorReviewDecision } from './VendorReviewDecision';
export type VendorReviewHistoryEntry = {
    comment: string;
    decidedAt: string;
    decidedByActorId: ActorId;
    decision: VendorReviewDecision;
};

