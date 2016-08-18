FROM alpine:3.4
WORKDIR /app

ENV NODE_VERSION=v4.4.7 NPM_VERSION=2

RUN echo $(grep -c ^processor /proc/cpuinfo 2>/dev/null || 1) && \
    apk add --update --no-cache --virtual build-dependencies gcc g++ linux-headers libgcc libstdc++ binutils-gold && \
    apk add --update --no-cache ca-certificates openssl git curl make python && \
    curl -sSL https://nodejs.org/dist/${NODE_VERSION}/node-${NODE_VERSION}.tar.gz | tar -xz && \
    cd /app/node-${NODE_VERSION} && \
    ./configure --prefix=/usr --without-snapshot --fully-static && \
    make -j$(grep -c ^processor /proc/cpuinfo 2>/dev/null || 1) && \
    make install && \
    cd / && \
    npm install -g npm@${NPM_VERSION} && \
    apk del build-dependencies && \
    rm -rf /etc/ssl /app/node-${NODE_VERSION} /usr/include \
    /usr/share/man /tmp/* /var/cache/apk/* /root/.npm /root/.node-gyp \
    /usr/lib/node_modules/npm/man /usr/lib/node_modules/npm/doc /usr/lib/node_modules/npm/html && \
    mkdir -p /etc/ssl/certs/ && update-ca-certificates --fresh

COPY ./tasque /usr/bin
RUN chmod 777 /usr/bin/tasque
ENTRYPOINT [ "tasque" ]
