# docker2aci - Convert docker images to ACI

docker2aci is a small tool that talks to a Docker registry,
gets all the layers of a Docker image, creates an ACI for
each layer with its dependencies and optionally adds them to
the rocket storage.

## Build

```
$ ./build
```

## Examples

```
$ bin/docker2aci busybox
Downloading layer: 4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125
Generated ACI: busybox-4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125-latest-linux-amd64.aci
Downloading layer: ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2
Generated ACI: busybox-ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2-latest-linux-amd64.aci
Downloading layer: df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b
Generated ACI: busybox-df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b-latest-linux-amd64.aci
Downloading layer: 511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158
Generated ACI: busybox-511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158-latest.aci
```

```
$ sudo bin/docker2aci --import busybox
Downloading layer: 4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125
Generated ACI: busybox-4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125-latest-linux-amd64.aci
Downloading layer: ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2
Generated ACI: busybox-ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2-latest-linux-amd64.aci
Downloading layer: df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b
Generated ACI: busybox-df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b-latest-linux-amd64.aci
Downloading layer: 511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158
Generated ACI: busybox-511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158-latest.aci
sha512-9f441e8bf512c867b235afa2097dfc4af96a7a6d17deaf67ff251eeedfc7baff
```
