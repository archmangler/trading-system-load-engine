# syntax=docker/dockerfile:1

FROM ubuntu:bionic
WORKDIR /app
RUN apt-get update && apt-get install -y \
      python3.7 \
      python3-dev \
      python3-pip \
      build-essential \
      libssl-dev \
      libffi-dev \
      python3.7-venv
ADD ws-subscriber.py /app
ADD dummy.py /app
RUN chmod a+x /app/ws-subscriber.py
RUN chmod a+x /app/dummy.py
ADD wswrapper.sh /app
RUN chmod a+x /app/wswrapper.sh
SHELL ["/bin/bash", "-c"]
ENV PYTHONUNBUFFERED=1
ENV PYTHONUNBUFFERED 1
RUN python3.7 -m venv /app/.ws-subscriber && \
    source /app/.ws-subscriber/bin/activate && \
    pip3.7 install --upgrade pip && \
    pip3.7 install wheel && \
    pip3.7 install asyncio && \
    pip3.7 install websocket && \
    pip3.7 install websocket-client && \
    pip3.7 uninstall --yes websocket-client && \
    pip3.7 install websocket-client
CMD ["/bin/bash","wswrapper.sh"]
