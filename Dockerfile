# ====== stage 1: build frontend ======
FROM node:26-alpine AS web
WORKDIR /src
COPY web/package*.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npx vite build

# ====== stage 2: build go binary ======
FROM golang:1.27-alpine AS go
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download || GOPROXY=https://proxy.golang.org,direct go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/qiansi ./cmd/server

# ====== stage 3: final image ======
FROM gcr.io/distroless/static
COPY --from=go /out/qiansi /bin/qiansi
COPY --from=web /src/dist /app/web/dist
ENV QIANSI_WEB_DIR=/app/web/dist
EXPOSE 8080
ENTRYPOINT ["/bin/qiansi"]
