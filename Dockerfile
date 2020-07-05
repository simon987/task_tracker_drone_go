# Build
FROM golang:1.14 as go_build
WORKDIR /build/

COPY *.go go.mod ./
RUN GOOS=linux CGO_ENABLED=0 go build -a -installsuffix cgo -o tt_drone_go .

FROM ubuntu

RUN apt update && apt install -y python3 python3-pip git
RUN pip3 install --no-cache-dir bs4 sklearn git+git://github.com/simon987/hexlib.git

WORKDIR /root/
COPY --from=go_build ["/build/tt_drone_go", "/root/"]

ENTRYPOINT ["/root/tt_drone_go"]