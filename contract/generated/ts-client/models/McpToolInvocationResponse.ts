/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { McpCapabilityDomain } from './McpCapabilityDomain';
import type { McpToolRisk } from './McpToolRisk';
export type McpToolInvocationResponse = {
    capabilityDomain: McpCapabilityDomain;
    /**
     * Tool-specific JSON response payload.
     */
    result: any;
    risk: McpToolRisk;
    toolName: string;
};

