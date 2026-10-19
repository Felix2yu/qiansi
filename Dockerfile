# 「牵丝」运行时镜像：二进制与前端产物均由 CI 预编译后拼装。
#
# 编译期依赖（Go 工具链、node_modules）全部留在 CI，不进镜像，因此这里没有任何
# 构建步骤 —— bin/qiansi 不存在时 COPY 直接红，而不是构建成功、运行到一半才发现缺文件。
# 前端不打进二进制，运行时由 QIANSI_WEB_DIR 指向 /app/web/dist。
#
# 本地要构建镜像，先跑 make build 产出 bin/qiansi 与 dist/。
#
# 容器内布局（全 fleet 统一）：
#   /app/qiansi             服务二进制
#   /app/web/dist           前端静态产物（运行时读取）
#   /app/data               唯一数据卷：SQLite 数据库、uploads/ 上传文件、backups/ 归档
# /app 不在 PATH 上，因此外部备份工具的 BACKUP_SQLITE_CMD 必须写绝对路径
# `/app/qiansi backup`，裸名在 docker exec 里会 command not found。
#
# 基镜 ghcr.io/felix2yu/base:alpine-3.24 已内置 ca-certificates、tzdata（牵丝的日期
# 口径基于本机时区，缺了会静默退化成 UTC）、su-exec 与 PUID/PGID 降权入口。
FROM ghcr.io/felix2yu/base:alpine-3.24

WORKDIR /app

COPY --chmod=755 bin/qiansi /app/qiansi
COPY dist /app/web/dist

RUN mkdir -p /app/data

ENV TZ=Asia/Shanghai \
    QIANSI_ADDR=:8080 \
    QIANSI_DATA_DIR=/app/data \
    QIANSI_WEB_DIR=/app/web/dist \
    APP_DATA_DIR=/app/data

VOLUME ["/app/data"]

EXPOSE 8080

# 探活走 /api/v1/health：该端点免鉴权，配了访问口令也探得通
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/v1/health >/dev/null || exit 1

CMD ["/app/qiansi"]
