## v0.11.2

v0.11.2 is a bugfix release.

- backend/repository2: don't ignore when there's an image digest ([#171](https://github.com/appc/docker2aci/pull/171)).

## v0.11.1

v0.11.1 is a bugfix release.

- Fix parallel pull synchronisation ([#167](https://github.com/appc/docker2aci/pull/167), [#168](https://github.com/appc/docker2aci/pull/168)).

## v0.11.0

This release splits the `--insecure` flag in two, `--insecure-skip-verify` to skip TLS verification, and `--insecure-allow-http` to allow unencrypted connections when fetching images. It also includes a couple of bugfixes.

- Add missing message to channel on successful layer download ([#161](https://github.com/appc/docker2aci/pull/161)).
- Fix a panic when a layer being fetched encounters an error ([#162](https://github.com/appc/docker2aci/pull/162)).
- Split `--insecure` flag in two ([#163](https://github.com/appc/docker2aci/pull/163)).

## v0.10.0

This release includes two major performance optimizations: parallel layer pull and parallel ACI compression.

- Pull layers in parallel ([#158](https://github.com/appc/docker2aci/pull/158)).
- Use a parallel compression library ([#157](https://github.com/appc/docker2aci/pull/157)).
- Fix auth token parsing to handle services with spaces in their names ([#150](https://github.com/appc/docker2aci/pull/150)).

## v0.9.3

v0.9.3 is a minor bug fix release.

- Use the default transport when doing HTTP requests ([#147](https://github.com/appc/docker2aci/pull/147)). We were using an empty transport which didn't pass on the proxy configuration.

## v0.9.2

v0.9.2 is a minor release with a bug fix and a cleanup over the previous one.

- Use upstream docker functions to parse docker URLs and parse digest ([#140](https://github.com/appc/docker2aci/pull/140)).
- Change docker entrypoint/cmd annotations to json ([#142](https://github.com/appc/docker2aci/pull/142)).

## v0.9.1

v0.9.1 is mainly a bugfix and cleanup release.

- Remove redundant dependency fetching, we're vendoring them now ([#134](https://github.com/appc/docker2aci/pull/134)).
- Export ParseDockerURL which is used by rkt ([#135](https://github.com/appc/docker2aci/pull/135)).
- Export annotations so people can use them outside docker2aci ([#135](https://github.com/appc/docker2aci/pull/135)).
- Refactor the library so internal functions are in the "internal" package ([#135](https://github.com/appc/docker2aci/pull/135)).
- Document release process and add a bump-version script ([#137](https://github.com/appc/docker2aci/pull/137)).

## v0.9.0

v0.9.0 is the initial release of docker2aci.

docker2aci converts to ACI Docker images from a remote repository or from a local file generated with "docker save".

It supports v1 and v2 Docker registries, compression, and layer squashing.
