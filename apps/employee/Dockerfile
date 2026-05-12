FROM node:20-alpine AS builder
WORKDIR /repo
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY apps/employee ./apps/employee
COPY packages ./packages
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @tbite/employee build

FROM node:20-alpine AS runner
WORKDIR /app
COPY --from=builder /repo/apps/employee/build ./
COPY --from=builder /repo/apps/employee/package.json ./
ENV NODE_ENV=production
EXPOSE 3000
CMD ["node", "index.js"]
