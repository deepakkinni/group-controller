FROM gcr.io/distroless/static:latest
LABEL maintainers="Kubernetes Authors"
LABEL description="CSI Volume Group Sidecar"
ARG binary=./bin/volumegroup-sidecar

COPY ${binary} volumegroup-sidecar
ENTRYPOINT ["/volumegroup-sidecar"]
