# 「牵丝」运行时镜像：二进制与前端产物均由 CI 预编译后拼装。
#
# 编译期依赖（Go 工具链、node_modules）全部留在 CI，不进镜像，因此这里没有任何
# 构建步骤 —— bin/qiansi 不存在时 COPY 直接红，而不是构建成功、运行到一半才发现缺文件。
# 前端不打进二进制，运行时由 QIANSI_WEB_DIR 指向 /app/web/dist。
#
# 本地要构建镜像，先跑 make build 产出 bin/qiansi 与 dist/。
#
# 容器内布局：
#   /usr/local/bin/qiansi   服务二进制
#   /app/web/dist           前端静态产物（运行时读取）
#   /data                   唯一数据卷：SQLite 数据库、上传文件、备份
#
# 基镜 ghcr.io/felix2yu/base:alpine-3.24 已内置 ca-certificates、tzdata（牵丝的日期
# 口径基于本机时区，缺了会静默退化成 UTC）、su-exec 与 PUID/PGID 降权入口。
FROM ghcr.io/felix2yu/base:alpine-3.24

WORKDIR /app

COPY --chmod=755 bin/qiansi /usr/local/bin/qiansi
COPY dist /app/web/dist

ENV TZ=Asia/Shanghai \
    QIANSI_ADDR=:8080 \
    QIANSI_DATA_DIR=/data \
    QIANSI_WEB_DIR=/app/web/dist \
    APP_DATA_DIR=/data

VOLUME ["/data"]

EXPOSE 8080

# 探活走 /api/v1/health：该端点免鉴权，配了访问口令也探得通
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/v1/health >/dev/null || exit 1

CMD ["/usr/local/bin/qiansi"]
