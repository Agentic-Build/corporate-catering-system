FROM node:20-alpine AS builder
WORKDIR /repo
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY apps/admin ./apps/admin
COPY packages ./packages
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @tbite/admin build

FROM node:20-alpine AS runner
WORKDIR /app
COPY --from=builder /repo/apps/admin/build ./
COPY --from=builder /repo/apps/admin/package.json ./
ENV NODE_ENV=production
EXPOSE 3000
CMD ["node", "index.js"]
