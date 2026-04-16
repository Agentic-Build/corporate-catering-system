/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { McpResourceCatalogResponse } from '../models/McpResourceCatalogResponse';
import type { McpToolCatalogResponse } from '../models/McpToolCatalogResponse';
import type { McpToolInvocationRequest } from '../models/McpToolInvocationRequest';
import type { McpToolInvocationResponse } from '../models/McpToolInvocationResponse';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class McpService {
    /**
     * List MCP resources available for granted capability domains
     * @returns McpResourceCatalogResponse MCP resource catalog scoped by granted domains
     * @throws ApiError
     */
    public static listMcpResources(): CancelablePromise<McpResourceCatalogResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/mcp/v1/resources',
            errors: {
                401: `Authentication token is missing or invalid.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * List MCP tools granted to the authenticated OAuth service account
     * @returns McpToolCatalogResponse MCP tool catalog scoped to granted tools
     * @throws ApiError
     */
    public static listMcpTools(): CancelablePromise<McpToolCatalogResponse> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/mcp/v1/tools',
            errors: {
                401: `Authentication token is missing or invalid.`,
                500: `Internal server error while processing request.`,
            },
        });
    }
    /**
     * Invoke an MCP tool with tool-level RBAC and shared-domain execution
     * @param toolName MCP tool name, for example `ordering.create_employee_order`.
     * @param requestBody
     * @returns McpToolInvocationResponse MCP tool invocation result
     * @throws ApiError
     */
    public static invokeMcpTool(
        toolName: string,
        requestBody: McpToolInvocationRequest,
    ): CancelablePromise<McpToolInvocationResponse> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/mcp/v1/tools/{toolName}/invoke',
            path: {
                'toolName': toolName,
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
}
