FROM k8s.gcr.io/fluentd-elasticsearch:v2.0.4

RUN BUILD_DEPS="make gcc g++ libc6-dev ruby-dev libffi-dev" \
    && clean-install $BUILD_DEPS \
                     ca-certificates \
                     libjemalloc1 \
                     liblz4-1 \
                     ruby \
    && echo 'gem: --no-document' >> /etc/gemrc \
    && gem install fluent-plugin-google-cloud -v 0.6.19 \
    && rm -rf /tmp/* \
              /var/lib/apt/lists/* \
              /usr/lib/ruby/gems/*/cache/*.gem \
              /var/log/* \
              /var/tmp/*
