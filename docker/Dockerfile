# Building stage
FROM docker.m.daocloud.io/golang:1.24

WORKDIR /go/src/github.com/guyuxiang/projectc-solana-connector

COPY . /go/src/github.com/guyuxiang/projectc-solana-connector

ENV CGO_ENABLED 0
ENV GOOS linux
ENV GO111MODULE=on
ENV TIMEZONE "Asia/Shanghai"
ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV TIMEZONE "Asia/Shanghai"

RUN make src.build
RUN mv build/pkg/cmd/projectc-solana-connector/projectc-solana-connector /go/bin

# Production stage
FROM docker.m.daocloud.io/golang:1.24
WORKDIR /go/bin

LABEL maintainer="guyuxiang <gu_yuxiang@qq.com>"

# copy the go binaries from the building stage
COPY --from=0 /go/bin /go/bin

COPY etc/projectc-solana-connector.yaml /etc/projectc-solana-connector/projectc-solana-connector.yaml

EXPOSE 80

ENTRYPOINT ["./projectc-solana-connector"]
