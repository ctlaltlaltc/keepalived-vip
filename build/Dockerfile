# Copyright 2015 The Kubernetes Authors. All rights reserved.
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

FROM ubuntu:latest AS keepalived-builder

ARG GOPROXY
ENV VERSION 2.1.5
ENV SHA256 c5f622a2d90db6249fe9a320b7209b89d8bc9b1933805ac1244ff3e0484b93cf

RUN apt-get update && apt-get install -y --no-install-recommends bash golang
COPY . /work
RUN cd /work && ./build/build.sh
RUN cd /work && go get -v -t -d ./... && \
  go test -v . && \
  CGO_ENABLED=0 go build -v -a -ldflags '-w' -o /kube-keepalived-vip

FROM ubuntu:latest

COPY --from=keepalived-builder /keepalived.tar.gz /
COPY --from=keepalived-builder /kube-keepalived-vip /

RUN apt-get update && apt-get install -y --no-install-recommends \
  libssl1.1 \
  libnl-3-200 \
  libnl-route-3-200 \
  libnl-genl-3-200 \
  iptables \
  libnfnetlink0 \
  libiptcdata0 \
  libipset13 \
  libipset-dev \
  libsnmp35 \
  kmod \
  ca-certificates \
  iproute2 \
  ipvsadm \
  bash \
  jq \
  curl && \
  apt-get clean && \
  rm -rf /var/lib/apt/lists/* && \
  mkdir -p /etc/keepalived && \
  ln -s /keepalived/sbin/keepalived /usr/sbin && \
  ln -s /keepalived/bin/genhash /usr/sbin && \
  tar -x -f /keepalived.tar.gz -C / && rm -f /keepalived.tar.gz

COPY keepalived.tmpl /
COPY keepalived.conf /etc/keepalived

ENTRYPOINT ["/kube-keepalived-vip"]
