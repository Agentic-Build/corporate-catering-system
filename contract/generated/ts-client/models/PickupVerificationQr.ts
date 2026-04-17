/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type PickupVerificationQr = {
    expiresAtEpochSecond: number;
    generatedAtEpochSecond: number;
    orderId: string;
    refreshIntervalSeconds: 30;
    secondsUntilRefresh: number;
    /**
     * Current TOTP QR payload bound to orderId and Asia/Taipei 30-second step boundary.
     */
    verificationCode: string;
};

