FROM golang:1.26.5-bookworm AS backend
WORKDIR /src
COPY server/go.mod server/go.sum ./
ARG GOPROXY=https://proxy.golang.org,direct
RUN GOPROXY="$GOPROXY" go mod download
COPY server/ ./
ARG APP_VERSION=0.10.0
RUN GOMEMLIMIT=384MiB GOGC=30 CGO_ENABLED=1 go build -p=1 -ldflags "-X smallgo/server/version.Version=$APP_VERSION -X smallgo/server/version.AppName=landlord" -o /landlord .

FROM debian:bookworm-slim
COPY --from=backend /landlord /app/landlord
ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security
RUN sed -i "s|http://deb.debian.org/debian-security|$DEBIAN_SECURITY_MIRROR|g; s|http://deb.debian.org/debian|$DEBIAN_MIRROR|g" /etc/apt/sources.list.d/debian.sources && apt-get -o Acquire::Retries=2 -o Acquire::http::Timeout=30 update && apt-get -o Acquire::Retries=2 -o Acquire::http::Timeout=30 install -y --no-install-recommends ca-certificates tzdata chromium fonts-noto-cjk && rm -rf /var/lib/apt/lists/* && useradd --uid 10001 --create-home app
WORKDIR /app
COPY web/dist/ /app/web/
COPY deploy/chromium-container.sh /usr/local/bin/landlord-chromium
RUN chmod 755 /usr/local/bin/landlord-chromium && mkdir /app/data && chown app:app /app/data
ENV LANDLORD_PDF_BROWSER=/usr/local/bin/landlord-chromium TZ=Asia/Hong_Kong
USER 10001:10001
EXPOSE 8910
ENTRYPOINT ["/app/landlord"]
CMD ["-port","8910","-data-dir","/app/data","-web-dir","/app/web","-disable-stats","-rate-limit","240"]
