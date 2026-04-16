/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { ActorId } from './ActorId';
export type AdminAnomalyAlertAssignOwnerPatchRequest = {
    note?: string;
    operation: 'ASSIGN_OWNER';
    ownerActorId: ActorId;
};

