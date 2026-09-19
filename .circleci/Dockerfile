# check=skip=InvalidDefaultArgInFrom
# Copyright 2026 The Agent Substrate Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Fork (giantswarm/substrate): the image of one component of the Giant Swarm
# line, built by the architect orb's push-to-registries job for linux/amd64 and
# linux/arm64 (FORK.md, "Publishing"). It reproduces what ko builds for
# upstream: the binary of ./cmd/<component>, compiled with CGO_ENABLED=0 and
# -trimpath from the vendored modules (the VCS revision embedded from the
# checkout, as ko does), at /ko-app/<component> on the distroless base
# .ko.yaml pins, /ko-app on PATH and the binary as the ENTRYPOINT. The builder
# runs on the build host's own architecture and cross-compiles — nothing is
# emulated; the runtime stage only copies. An ENTRYPOINT in exec form cannot
# take a build argument, so the last FROM selects one of the six runtime
# stages by COMPONENT — which has no default on purpose: a build without it
# fails instead of quietly producing some component (the directive on the
# first line stops the linter from asking for one).
#
#   docker build --build-arg COMPONENT=ateapi -f .circleci/Dockerfile .

ARG COMPONENT

FROM --platform=$BUILDPLATFORM golang:1.27 AS builder
ARG COMPONENT
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -mod=vendor -o /ko-app/${COMPONENT} ./cmd/${COMPONENT}

FROM gcr.io/distroless/static-debian13:latest@sha256:f2ea2709ac8db56323cbd7d014277f32cb572d9ea124b0076f7aafe5980678fe AS runtime
ARG COMPONENT
COPY --from=builder /ko-app/${COMPONENT} /ko-app/${COMPONENT}
ENV PATH=$PATH:/ko-app

FROM runtime AS ateapi
ENTRYPOINT ["/ko-app/ateapi"]

FROM runtime AS atecontroller
ENTRYPOINT ["/ko-app/atecontroller"]

FROM runtime AS atelet
ENTRYPOINT ["/ko-app/atelet"]

FROM runtime AS atenet
ENTRYPOINT ["/ko-app/atenet"]

FROM runtime AS podcertcontroller
ENTRYPOINT ["/ko-app/podcertcontroller"]

FROM runtime AS ateom-gvisor
ENTRYPOINT ["/ko-app/ateom-gvisor"]

FROM ${COMPONENT}
