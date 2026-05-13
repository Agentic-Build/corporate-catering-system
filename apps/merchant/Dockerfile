FROM node:20-alpine AS builder
WORKDIR /repo
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY apps/merchant ./apps/merchant
COPY packages ./packages
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @tbite/merchant build

FROM node:20-alpine AS runner
WORKDIR /app
COPY --from=builder /repo/apps/merchant/build ./
COPY --from=builder /repo/apps/merchant/package.json ./
ENV NODE_ENV=production
EXPOSE 3000
CMD ["node", "index.js"]
