# Stage 1: Build skopeo from source
FROM golang:1.25-alpine AS skopeo-builder
WORKDIR /src
RUN apk --no-cache add git make gcc musl-dev btrfs-progs-dev gpgme-dev linux-headers lvm2-dev
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" \
    -tags "exclude_graphdriver_btrfs exclude_graphdriver_devicemapper containers_image_openpgp" \
    -o /skopeo ./cmd/skopeo


