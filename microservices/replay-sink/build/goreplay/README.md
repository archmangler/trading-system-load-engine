# Building goreplay for Alpine (and perhaps others)

If needed,  goreplay can be compiled for Alpine using the following method:

(in container)

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

Or one could just get the latest binary from here:

https://github.com/buger/goreplay/releases

# References

- https://github.com/buger/goreplay
- https://github.com/buger/goreplay/wiki/Compilation
- https://github.com/buger/goreplay/releases


