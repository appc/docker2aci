# docker2aci - Convert docker images to ACI

docker2aci is a small tool that talks to a Docker registry,
gets all the layers of a Docker image, creates an ACI for
each layer with its dependencies and adds them to the rocket
storage.

## Build

```
$ ./build
```

## Example

```
$ sudo docker2aci busybox
511136ea3c5a64f264b78b5433614aec563103b4d4702f3ba7d4d2698e22c158: Downloading layer
df7546f9f060a2268024c8a230d8639878585defcc1bc6f79d2728a13957871b: Downloading layer
ea13149945cb6b1e746bf28032f02e9b5a793523481a0a18645fc77ad53c4ea2: Downloading layer
4986bf8c15363d1c5d15512d5266f8777bfba4974ac56e3270e7760f6f0a8125: Downloading layer
sha512-2ef9b25d0b75e3d7dca8724e7d875e59285a9f0252f114f4f93c886b3e7dac20
```
