FROM marhoy/balena-wpe:2.32-raspberrypi3

ENV UDEV=on

COPY udev-rules/ /etc/udev/rules.d/

COPY wpe-init /wpe-init
RUN chmod +x /wpe-init

COPY wait-for-it.sh /wait-for-it.sh
RUN chmod +x /wait-for-it.sh

CMD [ "/wpe-init" ]

ENV TOHORA_VERSION=0.3.2
RUN wget -O tohora.tgz \
    "https://github.com/mozz100/tohora/releases/download/v"$TOHORA_VERSION"/tohora_"$TOHORA_VERSION"_Linux_armv5.tar.gz" \
    && tar xzf tohora.tgz \
    && rm tohora.tgz