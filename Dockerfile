# Base image tag, suffixed with the distribution: -alpine (default) or -trixie
ARG PHP_IMAGE_TAG=1-php8.3-alpine
FROM composer:2 AS composer

FROM dunglas/frankenphp:${PHP_IMAGE_TAG}

COPY --from=composer /usr/bin/composer /usr/bin/composer

# Space-separated extra packages to install on top of the defaults below,
# resolved from EXTRA_APK_PACKAGES (Alpine) or EXTRA_APT_PACKAGES (Trixie),
# scoped per PHP version via the <version without dot> suffix and merged with
# EXTRA_COMPOSER_PACKAGES(_<version>). See .env.example.
ARG EXTRA_SYSTEM_PACKAGES=""
ARG EXTRA_COMPOSER_PACKAGES=""
# Set to "true" to install pdftk (pulls a Java runtime, ~200 MB)
ARG INSTALL_PDFTK=""
# Alpine has no pdftk package: the standalone pdftk-java jar is used instead
ARG PDFTK_VERSION=3.3.3

# Build dependencies of the PHP extensions are installed and removed by
# install-php-extensions itself, so only runtime tools are listed here.
# hadolint ignore=DL3008,DL3018,SC2046
RUN if command -v apk >/dev/null; then \
        apk add --no-cache \
            git \
            zip \
            unzip \
            curl \
            rsvg-convert \
            ca-certificates \
            $([ "$INSTALL_PDFTK" = "true" ] && echo openjdk21-jre-headless) \
            ${EXTRA_SYSTEM_PACKAGES} \
        && if [ "$INSTALL_PDFTK" = "true" ]; then \
            curl -fsSL -o /usr/local/share/pdftk-all.jar \
                "https://gitlab.com/api/v4/projects/5024297/packages/generic/pdftk-java/v${PDFTK_VERSION}/pdftk-all.jar" \
            && printf '#!/bin/sh\nexec java -jar /usr/local/share/pdftk-all.jar "$@"\n' > /usr/local/bin/pdftk \
            && chmod +x /usr/local/bin/pdftk; \
        fi; \
    else \
        apt-get update && apt-get install -qq -y --no-install-recommends \
            git \
            zip \
            unzip \
            curl \
            librsvg2-bin \
            $([ "$INSTALL_PDFTK" = "true" ] && echo pdftk-java) \
            ${EXTRA_SYSTEM_PACKAGES} \
        && rm -rf /var/lib/apt/lists/*; \
    fi \
    && install-php-extensions \
    pdo_mysql \
    zip \
    gd \
    intl \
    pdo_pgsql \
    pgsql \
    mysqli \
    imagick \
    imap \
    bcmath \
    exif \
    gmp \
    sysvsem \
    redis \
    && mkdir /etc/letsencrypt \
    && mkdir -p /var/log/php && chmod 755 /var/log/php

ENV COMPOSER_ALLOW_SUPERUSER=1
ENV PATH="${PATH}:${XDG_CONFIG_HOME}/composer/vendor/bin"

RUN if [ -n "$EXTRA_COMPOSER_PACKAGES" ]; then \
        composer global require --no-interaction --optimize-autoloader ${EXTRA_COMPOSER_PACKAGES}; \
    fi

ARG CUSTOM_PATH=/home

RUN mkdir -p "$CUSTOM_PATH"

WORKDIR $CUSTOM_PATH

ENV CUSTOM_PATH=$CUSTOM_PATH

COPY docker/entrypoint.sh /usr/local/bin/frankenphp-entrypoint.sh
RUN chmod +x /usr/local/bin/frankenphp-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/frankenphp-entrypoint.sh"]
CMD ["--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
