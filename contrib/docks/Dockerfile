FROM alpine:edge
MAINTAINER Leo <thinkabit.ukim@gmail.com>

RUN rm /etc/apk/repositories \
    && printf -- >> /etc/apk/repositories \
    'http://dl-cdn.alpinelinux.org/alpine/edge/%s\n' \
    main community testing

RUN apk add -U --no-cache \
    alpine-conf \
    alpine-sdk \
    abuild \
    sudo \
    openssh-server \
    fish
RUN apk upgrade -a
RUN setup-apkcache /var/cache/apk

RUN /usr/bin/ssh-keygen -A
RUN printf "Port 2243\nListenAddress localhost\n" >> /etc/ssh/sshd_config

USER root

CMD ["/usr/sbin/sshd", "-D"]
