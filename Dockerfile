FROM dunglas/frankenphp:latest-php8.2

#RUN mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"

RUN apt-get update && apt-get install -qq -y git curl libmcrypt-dev libjpeg-dev libpng-dev libfreetype6-dev libbz2-dev

RUN apt-get update && apt-get install -y \
    libzip-dev \
    libicu-dev \
    libpng-dev \
    libjpeg-dev \
    libfreetype6-dev \
    zip \
    unzip \
    git \
    libmagickwand-dev --no-install-recommends \
    libmagickcore-dev \
    libgmp-dev \
    && docker-php-ext-configure intl \
    && docker-php-ext-install intl zip \
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
    #xdebug \
    imap \
    opcache \
    bcmath \
    exif \
    gmp \
    sysvsem \
    mbstring \
    curl \
    xml \
    redis

RUN docker-php-ext-enable \
    #xdebug \
    imagick

RUN mkdir /etc/letsencrypt

ARG CUSTOM_PATH=/home

RUN mkdir -p "$CUSTOM_PATH"

WORKDIR $CUSTOM_PATH

ENV CUSTOM_PATH=$CUSTOM_PATH

