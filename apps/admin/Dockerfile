FROM node:24.16.0-alpine AS builder
WORKDIR /repo
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY apps/admin ./apps/admin
COPY packages ./packages
RUN pnpm install --frozen-lockfile --ignore-scripts
RUN pnpm --filter @tbite/admin build

FROM node:24.16.0-alpine AS runner
WORKDIR /app
COPY --chown=root:root --chmod=0444 --from=builder /repo/apps/admin/build ./
COPY --chown=root:root --chmod=0444 --from=builder /repo/apps/admin/package.json ./
RUN chmod -R a+X /app
ENV NODE_ENV=production
USER node
EXPOSE 3000
CMD ["node", "index.js"]
