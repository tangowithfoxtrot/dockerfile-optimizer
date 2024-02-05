# Dockerfile Optimizer

This is a simple tool to minimize the size of a Dockerfile by only including the binaries and libraries that are actually used by the application. It works by analyzing the output of the `ldd` command and copying the required files to a new directory. The new Dockerfile will be generated automatically.

## Usage

```bash
dockerfile-optimizer <path-to-dockerfile>
```

The input would be something like this:

```Dockerfile
FROM --platform=linux/amd64 debian:latest
ARG VER=2024.1.0
ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /usr/local/bin
RUN apt update && apt install -y curl unzip libsecret-1-0 jq
COPY entrypoint.sh .
RUN curl -LO "https://github.com/bitwarden/clients/releases/download/cli-v{$VER}/bw-linux-{$VER}.zip" && \
  && unzip *.zip && chmod +x ./bw
ENTRYPOINT [ "/usr/local/bin/entrypoint.sh" ]
```

Where the `entrypoint.sh` file is something like this:

```bash
#!/usr/bin/env bash

# to enable interactive CLI usage
if [[ $# -gt 0 ]]; then
  bw "$@"
  exit $?
fi

STATUS="$(bw status | jq -r '.status')"

if [[ -n "$MFA_CODE" ]]; then
  # shellcheck disable=SC2034
  export MFA_LOGIN="--method 0 --code $MFA_CODE"
fi

if [[ -n "$BW_CLIENTSECRET" ]]; then
  export API_LOGIN="--apikey"
fi

if [[ "$STATUS" == "unauthenticated" ]]; then
  bw config server "$SERVER_HOST_URL" && echo
  # shellcheck disable=SC2086
  bw login "$VAULT_EMAIL" "$VAULT_PASSWORD" $API_LOGIN $MFA_LOGIN && echo
fi

bw serve --hostname all --port "${SERVE_PORT:-8087}" &
BW_SERVE_PID=$!
echo "\`bw serve\` pid: $BW_SERVE_PID"

if [[ "$UNLOCK_VAULT" == "true" ]]; then
  while ! curl -sX POST -H "Content-Type: application/json" -d "{\"password\": \"$VAULT_PASSWORD\"}" "http://localhost:$SERVE_PORT/unlock" >/dev/null; do
    sleep 1
  done
  echo "Vault unlocked!"
fi

echo "Server can be reached at: http://localhost:${SERVE_PORT:-8087}/status"
sleep infinity
```

The `dockerfile-optimizer` will analyze the `entrypoint.sh` file and the output of the `ldd` command to generate a new Dockerfile with a final `app` stage built from the `scratch` image that only includes the required binaries and libraries.

The output would be:

```Dockerfile
# syntax = docker/dockerfile:1.2
###############################################
#                 Build stage                 #
###############################################
FROM --platform=linux/amd64 debian:latest as builder
ARG VER=2024.1.0
ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /usr/local/bin
RUN apt update && apt install -y curl unzip libsecret-1-0 jq
RUN curl -LO "https://github.com/bitwarden/clients/releases/download/cli-v{$VER}/bw-linux-{$VER}.zip" \
  && unzip *.zip && chmod +x ./bw

###############################################
#                  App stage                  #
###############################################
FROM --platform=linux/amd64 scratch as app
SHELL ["/bin/bash"]

# binaries
COPY --from=builder /bin/bash /bin/bash
COPY --from=builder /usr/bin/curl /usr/bin/curl
COPY --from=builder /usr/bin/jq /usr/bin/jq
COPY --from=builder /usr/bin/sleep /usr/bin/sleep
COPY --from=builder /usr/local/bin/bw /usr/local/bin/bw

# shared libraries
COPY --from=builder /lib/x86_64-linux-gnu/libtinfo.so.6 /lib/x86_64-linux-gnu/libtinfo.so.6
COPY --from=builder /lib/x86_64-linux-gnu/libdl.so.2 /lib/x86_64-linux-gnu/libdl.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libstdc++.so.6 /lib/x86_64-linux-gnu/libstdc++.so.6
COPY --from=builder /lib/x86_64-linux-gnu/libm.so.6 /lib/x86_64-linux-gnu/libm.so.6
COPY --from=builder /lib/x86_64-linux-gnu/libgcc_s.so.1 /lib/x86_64-linux-gnu/libgcc_s.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libpthread.so.0 /lib/x86_64-linux-gnu/libpthread.so.0
COPY --from=builder /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6
COPY --from=builder /lib/x86_64-linux-gnu/libselinux.so.1 /lib/x86_64-linux-gnu/libselinux.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libpcre2-8.so.0 /lib/x86_64-linux-gnu/libpcre2-8.so.0
COPY --from=builder /lib/x86_64-linux-gnu/libonig.so.5 /lib/x86_64-linux-gnu/libonig.so.5
COPY --from=builder /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libjq.so.1 /lib/x86_64-linux-gnu/libjq.so.1

# curl shared libraries
COPY --from=builder /lib/x86_64-linux-gnu/libcurl.so.4 /lib/x86_64-linux-gnu/libcurl.so.4
COPY --from=builder /lib/x86_64-linux-gnu/libz.so.1 /lib/x86_64-linux-gnu/libz.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libnghttp2.so.14 /lib/x86_64-linux-gnu/libnghttp2.so.14
COPY --from=builder /lib/x86_64-linux-gnu/libidn2.so.0 /lib/x86_64-linux-gnu/libidn2.so.0
COPY --from=builder /lib/x86_64-linux-gnu/librtmp.so.1 /lib/x86_64-linux-gnu/librtmp.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libssh2.so.1 /lib/x86_64-linux-gnu/libssh2.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libpsl.so.5 /lib/x86_64-linux-gnu/libpsl.so.5
COPY --from=builder /lib/x86_64-linux-gnu/libssl.so.3 /lib/x86_64-linux-gnu/libssl.so.3
COPY --from=builder /lib/x86_64-linux-gnu/libcrypto.so.3 /lib/x86_64-linux-gnu/libcrypto.so.3
COPY --from=builder /lib/x86_64-linux-gnu/libgssapi_krb5.so.2 /lib/x86_64-linux-gnu/libgssapi_krb5.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libldap-2.5.so.0 /lib/x86_64-linux-gnu/libldap-2.5.so.0
COPY --from=builder /lib/x86_64-linux-gnu/liblber-2.5.so.0 /lib/x86_64-linux-gnu/liblber-2.5.so.0
COPY --from=builder /lib/x86_64-linux-gnu/libzstd.so.1 /lib/x86_64-linux-gnu/libzstd.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libbrotlidec.so.1 /lib/x86_64-linux-gnu/libbrotlidec.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libunistring.so.2 /lib/x86_64-linux-gnu/libunistring.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libgnutls.so.30 /lib/x86_64-linux-gnu/libgnutls.so.30
COPY --from=builder /lib/x86_64-linux-gnu/libhogweed.so.6 /lib/x86_64-linux-gnu/libhogweed.so.6
COPY --from=builder /lib/x86_64-linux-gnu/libnettle.so.8 /lib/x86_64-linux-gnu/libnettle.so.8
COPY --from=builder /lib/x86_64-linux-gnu/libgmp.so.10 /lib/x86_64-linux-gnu/libgmp.so.10
COPY --from=builder /lib/x86_64-linux-gnu/libkrb5.so.3 /lib/x86_64-linux-gnu/libkrb5.so.3
COPY --from=builder /lib/x86_64-linux-gnu/libk5crypto.so.3 /lib/x86_64-linux-gnu/libk5crypto.so.3
COPY --from=builder /lib/x86_64-linux-gnu/libcom_err.so.2 /lib/x86_64-linux-gnu/libcom_err.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libkrb5support.so.0 /lib/x86_64-linux-gnu/libkrb5support.so.0
COPY --from=builder /lib/x86_64-linux-gnu/libsasl2.so.2 /lib/x86_64-linux-gnu/libsasl2.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libbrotlicommon.so.1 /lib/x86_64-linux-gnu/libbrotlicommon.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libp11-kit.so.0 /lib/x86_64-linux-gnu/libp11-kit.so.0
COPY --from=builder /lib/x86_64-linux-gnu/libtasn1.so.6 /lib/x86_64-linux-gnu/libtasn1.so.6
COPY --from=builder /lib/x86_64-linux-gnu/libkeyutils.so.1 /lib/x86_64-linux-gnu/libkeyutils.so.1
COPY --from=builder /lib/x86_64-linux-gnu/libresolv.so.2 /lib/x86_64-linux-gnu/libresolv.so.2
COPY --from=builder /lib/x86_64-linux-gnu/libffi.so.8 /lib/x86_64-linux-gnu/libffi.so.8

# ca-certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# entrypoint
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT [ "/usr/local/bin/entrypoint.sh" ]
```
