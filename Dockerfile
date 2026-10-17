# Build stage: compile the program.
FROM golang:1.27-alpine AS build
WORKDIR /src
# This layer is cached until the dependencies change.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The SQLite driver is pure Go, so cgo is not needed and the binary is static.
RUN CGO_ENABLED=0 go build -o /task-tracker-api .

# Run stage: a small image with only the binary.
FROM alpine:3.24
COPY --from=build /task-tracker-api /usr/local/bin/task-tracker-api
ENV PORT=8080 DB_PATH=/data/tasks.db
# A named volume here (-v task-data:/data) keeps the tasks after the container is removed.
VOLUME /data
EXPOSE 8080
# Runs as root: a non-root user could not write to the root-owned /data volume.
CMD ["task-tracker-api"]
