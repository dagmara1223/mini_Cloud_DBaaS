FROM postgres:15

ENV POSTGRES_DB=cloud_metadata
ENV POSTGRES_USER=postgres
ENV POSTGRES_PASSWORD=admin

COPY schema.sql /docker-entrypoint-initdb.d/