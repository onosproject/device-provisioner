# SPDX-FileCopyrightText: 2022-present Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0

FROM goreleaser/goreleaser-cross:v1.18.2-v1.9.0 AS build

ENV GO111MODULE=on
WORKDIR /build

# copy only the files that are needed for the build, exclude everything else to make better use of the docker cache
COPY ./cmd /build/cmd
COPY ./pkg /build/pkg
COPY ./go.mod /build
COPY ./go.sum /build

# build the executable
#RUN go build -o build/_output/device-provisioner ./cmd/device-provisioner
RUN go run ./cmd/device-provisioner-build --config ./cmd/device-provisioner-build/build.yaml


FROM alpine:3.13
RUN apk add libc6-compat

USER nobody

COPY --from=build  /build/_output/device-provisioner /usr/local/bin/device-provisioner
COPY --from=build  /build/_output/plugins/ /var/device-provisioner/plugins/


ENTRYPOINT ["device-provisioner"]