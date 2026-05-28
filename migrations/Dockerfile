# Ships the SQL migration files at /migrations for the chart's db-migrate
# hook Job. The Job's init container copies them into a shared volume so
# migrate/migrate can apply them. Built and tagged in lockstep with the
# platform image by .github/workflows/cd-publish-images.yml, so the chart's
# migrationsImage tag resolves to the same sha as the deployed app.
FROM busybox:1.36-musl AS sqlfiles
WORKDIR /sql
COPY migrations/*.sql ./

FROM alpine:3.20
RUN addgroup -S migrations && adduser -S migrations -G migrations
COPY --chmod=0555 --chown=migrations:migrations --from=sqlfiles /sql/ /migrations/
USER migrations
