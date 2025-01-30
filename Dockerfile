FROM alpine

RUN apk add go

COPY go.mod go.sum ./

RUN go mod download

COPY ./ ./

RUN CGO_ENABLED=0 go build -o jobs

FROM alpine

RUN apk add openssh

RUN mkdir keys

RUN ssh-keygen -t rsa -f keys/id_rsa -P ""

RUN ssh-keygen -t ed25519 -f keys/id_ed25519 -P ""

FROM alpine 

COPY --from=0 /jobs /jobs

COPY --from=1 /keys /tmp 

ENTRYPOINT ["/jobs"]
