import { env } from "$env/dynamic/private";
export const API_BASE_URL = env.API_BASE_URL ?? "http://localhost:8080";
