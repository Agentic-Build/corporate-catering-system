FROM node:24.16.0-alpine AS builder
WORKDIR /repo
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY apps/employee ./apps/employee
COPY packages ./packages
RUN pnpm install --frozen-lockfile --ignore-scripts
RUN pnpm --filter @tbite/employee build

FROM node:24.16.0-alpine AS runner
WORKDIR /app
COPY --chown=root:root --chmod=0444 --from=builder /repo/apps/employee/build ./
COPY --chown=root:root --chmod=0444 --from=builder /repo/apps/employee/package.json ./
RUN chmod -R a+X /app
ENV NODE_ENV=production
USER node
EXPOSE 3000
CMD ["node", "index.js"]
