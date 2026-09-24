# 构建阶段
FROM golang:1.23-bookworm AS build
WORKDIR /src

# 先复制模块定义以利用缓存（本项目无外部依赖）
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# 运行阶段：最小镜像，非 root
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/api /api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
