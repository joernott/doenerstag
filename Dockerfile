# doenerstag, as one static binary in an empty image.
#
# Two stages. The first builds the frontend and then the Go binary with
# -tags embedstatic, so static/ travels inside the executable. The second is
# scratch: the binary, the CA bundle and a passwd entry, and nothing else.
#
# That is possible precisely because the application fetches nothing at runtime
# — no CDN, no template directory, no asset path to mount. See
# docs/10_operations.md.
#
# There is no shell in the result. `docker exec ... sh` will not work, which is
# the point; run another container from the same image with a different command.

# --- build ------------------------------------------------------------------

FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend

WORKDIR /src/frontend
# The lockfile first, so a change to the sources does not re-resolve the whole
# dependency tree on every build.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
COPY static/ /src/static/
RUN npm run build


FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

# Set by buildx for each architecture being built. The Go toolchain runs
# natively on the build platform and cross-compiles, which is far faster than
# emulating the target.
ARG TARGETOS
ARG TARGETARCH

# Stamped in by the caller so the image reports the same version as every other
# artefact of the release.
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The frontend built above, replacing whatever was committed. Building it here
# rather than trusting the checked-in copy is what keeps the image honest.
COPY --from=frontend /src/static/ ./static/

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -tags embedstatic \
    -ldflags "-s -w \
      -X github.com/joernott/doenerstag/internal/version.version=${VERSION} \
      -X github.com/joernott/doenerstag/internal/version.commit=${COMMIT} \
      -X github.com/joernott/doenerstag/internal/version.buildDate=${BUILD_DATE}" \
    -o /out/doenerstag ./cmd/doenerstag

# scratch has no /etc/passwd, and a numeric USER without one leaves the process
# with no name. One line is enough and costs nothing.
RUN printf 'doenerstag:x:65532:65532:doenerstag:/config:/sbin/nologin\n' > /out/passwd \
 && printf 'doenerstag:x:65532:\n' > /out/group


# --- runtime ----------------------------------------------------------------

FROM scratch

# Only for outbound TLS the application might do; it makes no outbound calls
# today, and the bundle is here so that adding one later is not a mystery.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/passwd /etc/passwd
COPY --from=build /out/group /etc/group
COPY --from=build /out/doenerstag /doenerstag

USER 65532:65532
WORKDIR /config
EXPOSE 8443
VOLUME ["/config"]

# `--version` rather than a request against the port: scratch has neither curl
# nor a shell, and the binary answering at all is what this is asking.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/doenerstag", "--version"]

ENTRYPOINT ["/doenerstag"]
CMD ["server"]
