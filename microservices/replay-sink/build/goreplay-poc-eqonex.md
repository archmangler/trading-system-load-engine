
```
logger(logFile string, logMessage string)
```

1. https://www.youtube.com/watch?v=CxuKZcMKaW4
2. 

# Testing

- Capture:

```
goreplay  --input-raw :80 --output-stdout 
```


```
/app/gor --input-raw :80 --output-file=`hostname`.gor
```

- Replay/Redirect:

```
goreplay --input-raw :80 --output-http http://replay-service.ragnarok.svc.cluster.local/sink-orders
```

- Replay from  file:

```
./gor --input-file requests.gor --output-http=http://sink-service.ragnarok.svc.cluster.local/sink-orders
```

- Replay at same timeline:

/app # ./gor --input-file replay-sink-b7d74b857-rllrc_0.gor -verbose 3 --output-http=http://sink-service.ragnarok.svc.cluster.local/sink-orders
2022/05/05 05:53:20 [PPID 50 and PID 111] Version:1.3.0
[DEBUG][elapsed 861.046µs]: [EMITTER] input:  1 b5c60050c0a80d51f5562f09 1651724388079432123 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 15.960835ms]: [EMITTER] input:  1 476a0050c0a80b7eeec1dc7f 1651724388095248405 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 12.432201ms]: [EMITTER] input:  1 8cba0050c0a809bc7cba453a 1651724388107550647 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor

.
.
.
.

<delay>

.
.
.
.

[DEBUG][elapsed 14.981578014s]: [EMITTER] input:  1 b60e0050c0a80d510906ced6 1651724403088949406 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 6.684113ms]: [EMITTER] input:  1 47b60050c0a80b7e38c43670 1651724403095505363 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 6.34565ms]: [EMITTER] input:  1 8d080050c0a809bc479610a4 1651724403101677791 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 14.996600509s]: [EMITTER] input:  1 47f00050c0a80b7e1d714e67 1651724418098081058 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 567.161µs]: [EMITTER] input:  1 b64a0050c0a80d5114c27ab4 1651724418098537557 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 9.909668ms]: [EMITTER] input:  1 8d480050c0a809bcdc633304 1651724418108348847 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 14.992502786s]: [EMITTER] input:  1 482c0050c0a80b7ed1855162 1651724433100643979 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 7.977015ms]: [EMITTER] input:  1 b6880050c0a80d511c6c544b 1651724433108503904 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor
[DEBUG][elapsed 3.296205ms]: [EMITTER] input:  1 8d800050c0a809bc1a115455 1651724433111657515 0  from:  File input: replay-sink-b7d74b857-rllrc_0.gor

```

# Installation process:

```
apk add flex
apk add bison
wget http://www.tcpdump.org/release/libpcap-1.7.4.tar.gz && tar xzf libpcap-1.7.4.tar.gz
cd libpcap-1.7.4
apk add gcc
apk add g++
apk add make
apk search libc-dev
apk add libc-dev
apk add linux-headers
./configure && make install
export HOME=/app
mkdir $HOME/gocode
export GOPATH=$HOME/gocode
go get github.com/buger/goreplay
goreplay
```

# Ingress Entries

```
      - path: /replay-admin
        pathType: Prefix
        backend:
          service:
            name: replay-service
            port:
              number: 80
      - path: /replay-orders
        pathType: Prefix
        backend:
          service:
            name: replay-service
            port:
              number: 80
```
