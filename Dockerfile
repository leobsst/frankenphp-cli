ARG PHP_IMAGE_TAG=latest-php8.3
FROM dunglas/frankenphp:${PHP_IMAGE_TAG}

# hadolint ignore=DL3008
RUN apt-get update && apt-get install -qq -y --no-install-recommends \
    git \
    zip \
    unzip \
    curl \
    libmcrypt-dev \
    libjpeg-dev \
    libpng-dev \
    libfreetype6-dev \
    libbz2-dev \
    libzip-dev \
    libicu-dev \
    libmagickwand-dev \
    libmagickcore-dev \
    libgmp-dev \
    && docker-php-ext-configure intl \
    && docker-php-ext-install intl zip \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN install-php-extensions \
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
    mbstring \
    curl \
    xml \
    redis

RUN docker-php-ext-enable imagick \
    && mkdir /etc/letsencrypt \
    && mkdir -p /var/log/php && chmod 755 /var/log/php

ARG CUSTOM_PATH=/home

RUN mkdir -p "$CUSTOM_PATH"

WORKDIR $CUSTOM_PATH

ENV CUSTOM_PATH=$CUSTOM_PATH
