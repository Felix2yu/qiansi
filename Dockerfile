# 「牵丝」运行时镜像：二进制与前端产物均由 CI 预编译后拼装。
#
# distroless 无 shell、无包管理器 —— 因此这里不能有任何 RUN。
# 前端不打进二进制，运行时由 QIANSI_WEB_DIR 指向 /app/web/dist。

FROM gcr.io/distroless/static

COPY --chmod=755 bin/qiansi /bin/qiansi
COPY dist /app/web/dist

ENV QIANSI_WEB_DIR=/app/web/dist

EXPOSE 8080

ENTRYPOINT ["/bin/qiansi"]
