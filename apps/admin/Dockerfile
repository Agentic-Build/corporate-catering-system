FROM node:24.16.0-alpine AS builder
WORKDIR /repo
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY apps/admin ./apps/admin
COPY packages ./packages
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @tbite/admin build

FROM node:24.16.0-alpine AS runner
WORKDIR /app
COPY --chown=node:node --chmod=0555 --from=builder /repo/apps/admin/build ./
COPY --chown=node:node --chmod=0555 --from=builder /repo/apps/admin/package.json ./
ENV NODE_ENV=production
USER node
EXPOSE 3000
CMD ["node", "index.js"]
