# SPDX-FileCopyrightText: 2022-present Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0

FROM onosproject/golang-build:v1.3.0 as build

ENV GO111MODULE=on
WORKDIR /build

# copy only the files that are needed for the build, exclude everything else to make better use of the docker cache
COPY ./cmd /build/cmd
COPY ./pkg /build/pkg
COPY ./vendor /build/vendor
COPY ./go.mod /build
COPY ./go.sum /build

# build the executable(s)
RUN go build -mod=vendor -o build/_output/device-provisioner ./cmd/device-provisioner

FROM alpine:3.13
RUN apk add libc6-compat
RUN mkdir -p /etc/onos/device-configs && chmod 777 /etc/onos/device-configs

USER nobody

COPY --from=build /build/build/_output/device-provisioner /usr/local/bin/device-provisioner

ENTRYPOINT ["device-provisioner"]
