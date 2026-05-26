# Ships the SQL migration files at /migrations for the chart's db-migrate
# hook Job. The Job's init container copies them into a shared volume so
# migrate/migrate can apply them. Built and tagged in lockstep with the
# platform image by .github/workflows/cd-publish-images.yml, so the chart's
# migrationsImage tag resolves to the same sha as the deployed app.
FROM alpine:3.20
COPY migrations/*.sql /migrations/
