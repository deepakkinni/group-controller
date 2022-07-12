FROM gcr.io/distroless/static:latest
LABEL maintainers="Kubernetes Authors"
LABEL description="VolumeGroup Controller"
ARG binary=./bin/volumegroup-controller

COPY ${binary} volumegroup-controller
ENTRYPOINT ["/volumegroup-controller"]
