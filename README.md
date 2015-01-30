# docker2aci - Convert docker images to ACI

docker2aci is a small library that talks to a Docker registry, gets all the
layers of a Docker image and squashes them into an ACI image.
Optionally, it can generate one ACI for each layer setting the correct
dependencies.

## Examples

```
$ ./docker2aci busybox
Downloading layer: 4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125
Downloading layer: ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2
Downloading layer: df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b
Downloading layer: 511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158

Generated ACI(s):
library-busybox-latest.aci
```

```
$ sudo ./docker2aci --import busybox
Downloading layer: 4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125
Downloading layer: ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2
Downloading layer: df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b
Downloading layer: 511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158

Generated ACI(s):
library-busybox-latest.aci

Imported images into the store in /var/lib/rkt
App image ID: sha512-d422754bc8636964de1dbad0e3df0b4de53623e777fd73f772fc02c9dfe010fa
```

```
$ ./docker2aci --nosquash quay.io/coreos/etcd:latest
Downloading layer: c5f34efc44466ec7abb9a68af20d2f876ea691095747e7e5a62e890cdedadcdc
Downloading layer: 78d63abf03b980919deaac3454a80496559da893948f427868492fa8a0d717ab
Downloading layer: 185eec9979eb1288f1412ec997860d3c865ac6a9e5c71487d9876bc0ec7bbdfe
Downloading layer: 8423185475fe5bb0c86dc98ba2816ca9cc29cbf3ec5f3ec091963854746ee131
Downloading layer: 3c79dd31bf84b2fb7c55354f5069964a72bb6ae0c1263331c0f83ce4c32a4b6a

Generated ACI(s):
coreos-etcd-c5f34efc44466ec7abb9a68af20d2f876ea691095747e7e5a62e890cdedadcdc-latest-linux-amd64.aci
coreos-etcd-78d63abf03b980919deaac3454a80496559da893948f427868492fa8a0d717ab-latest-linux-amd64.aci
coreos-etcd-185eec9979eb1288f1412ec997860d3c865ac6a9e5c71487d9876bc0ec7bbdfe-latest-linux-amd64.aci
coreos-etcd-8423185475fe5bb0c86dc98ba2816ca9cc29cbf3ec5f3ec091963854746ee131-latest-linux-amd64.aci
coreos-etcd-3c79dd31bf84b2fb7c55354f5069964a72bb6ae0c1263331c0f83ce4c32a4b6a-latest-linux-amd64.aci
```
