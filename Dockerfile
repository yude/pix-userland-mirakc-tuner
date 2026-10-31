# syntax=docker/dockerfile:1
# The image also supports a separate pixd service. Firmware stays on the host.
FROM --platform=$BUILDPLATFORM docker.io/golang:alpine AS adapter-builder
WORKDIR /src
COPY go.mod main.go ./
COPY cmd ./cmd
ARG TARGETOS TARGETARCH TARGETVARIANT
RUN if [ "$TARGETARCH" = arm ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
    go version && CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags='-s -w' -o /out/pix-mirakc-tuner . && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags='-s -w' -o /out/pixd-launch ./cmd/pixd-launch

FROM docker.io/mirakc/mirakc:alpine AS pix-ts-builder
USER root
RUN apk add --no-cache build-base cmake git libusb-dev ninja pkgconf python3
ARG PIX_USERLAND_REF=3447667bb7f93e7dd2c2e65da9cfb07ba3f34f47
# Sparse partial checkout excludes assets/ and all device material from build layers.
RUN git init -q /src/pix-userland \
    && git -C /src/pix-userland remote add origin https://github.com/Khronos31/pix-userland.git \
    && git -C /src/pix-userland config core.sparseCheckout true \
    && printf '%s\n' '/core-integration/' '/src/' '/include/' '/tests/' '/tools/core-notices.py' '/tools/firmware_bundle.py' '/firmware/manifest.json' '/pix-dt181-*' '/pix-dt295-*' '/LICENSE' '/COPYING.libusb' > /src/pix-userland/.git/info/sparse-checkout \
    && git -C /src/pix-userland fetch -q --filter=blob:none --depth 1 origin "${PIX_USERLAND_REF}" \
    && git -C /src/pix-userland checkout -q --detach FETCH_HEAD \
    && test "$(git -C /src/pix-userland rev-parse HEAD)" = "${PIX_USERLAND_REF}" \
    && test ! -e /src/pix-userland/assets \
    && test ! -e /src/pix-userland/firmware/dt181.bin \
    && test ! -e /src/pix-userland/firmware/dt295.bin
RUN cmake -S /src/pix-userland/core-integration -B /build/pix -G Ninja -DCMAKE_BUILD_TYPE=Release -DPIX_EMBEDDED_ASSETS=OFF \
    && cmake --build /build/pix --target pixd pixctl pix-ts --parallel 4 \
    && python3 /src/pix-userland/tools/core-notices.py /build/pix/_deps/dtv_userland_core-src /out/notices \
    && cp /src/pix-userland/LICENSE /out/notices/pix-userland-LICENSE \
    && cp /src/pix-userland/COPYING.libusb /out/notices/libusb-COPYING

FROM docker.io/mirakc/mirakc:alpine
USER root
RUN apk add --no-cache libstdc++ libusb
COPY --from=adapter-builder /out/pix-mirakc-tuner /usr/local/bin/pix-mirakc-tuner
COPY --from=adapter-builder /out/pixd-launch /usr/local/bin/pixd-launch
COPY --from=pix-ts-builder /build/pix/pixd /usr/local/bin/pixd
COPY --from=pix-ts-builder /build/pix/pixctl /usr/local/bin/pixctl
COPY --from=pix-ts-builder /build/pix/pix-ts /usr/local/bin/pix-ts
COPY --from=pix-ts-builder /out/notices/ /usr/share/doc/pix-userland-mirakc-tuner/licenses/
COPY LICENSE /usr/share/doc/pix-userland-mirakc-tuner/licenses/adapter-LICENSE
LABEL org.opencontainers.image.source="https://github.com/yude/pix-userland-mirakc-tuner"
ENV TZ=Asia/Tokyo
