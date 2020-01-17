FROM golang:1.13.4

WORKDIR /app

COPY filebeat .

COPY filebeat.yml .

ARG network=host

USER root

ENTRYPOINT ["./filebeat"]
