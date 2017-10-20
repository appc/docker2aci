// Copyright 2017 The appc Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	_ "crypto/sha256"
	"reflect"
	"testing"
)

func TestMediaTypeSet(t *testing.T) {
	tests := []struct {
		ms                    MediaTypeSet
		expectedManifestTypes []string
		expectedConfigTypes   []string
		expectedLayerTypes    []string
	}{
		{
			MediaTypeSet{MediaTypeOptionDockerV21},
			[]string{MediaTypeDockerV21Manifest, MediaTypeDockerV21SignedManifest},
			[]string{},
			[]string{},
		},
		{
			MediaTypeSet{MediaTypeOptionDockerV22},
			[]string{MediaTypeDockerV22Manifest},
			[]string{MediaTypeDockerV22Config},
			[]string{MediaTypeDockerV22RootFS},
		},
		{
			MediaTypeSet{MediaTypeOptionOCIV1Pre},
			[]string{MediaTypeOCIV1Manifest},
			[]string{MediaTypeOCIV1Config},
			[]string{MediaTypeOCIV1Layer},
		},
		{
			MediaTypeSet{},
			[]string{MediaTypeDockerV21Manifest, MediaTypeDockerV21SignedManifest, MediaTypeDockerV22Manifest, MediaTypeOCIV1Manifest},
			[]string{MediaTypeDockerV22Config, MediaTypeOCIV1Config},
			[]string{MediaTypeDockerV22RootFS, MediaTypeOCIV1Layer},
		},
		{
			MediaTypeSet{MediaTypeOptionDockerV21, MediaTypeOptionDockerV22, MediaTypeOptionOCIV1Pre},
			[]string{MediaTypeDockerV21Manifest, MediaTypeDockerV21SignedManifest, MediaTypeDockerV22Manifest, MediaTypeOCIV1Manifest},
			[]string{MediaTypeDockerV22Config, MediaTypeOCIV1Config},
			[]string{MediaTypeDockerV22RootFS, MediaTypeOCIV1Layer},
		},
		{
			MediaTypeSet{MediaTypeOptionDockerV21, MediaTypeOptionOCIV1Pre},
			[]string{MediaTypeDockerV21Manifest, MediaTypeDockerV21SignedManifest, MediaTypeOCIV1Manifest},
			[]string{MediaTypeOCIV1Config},
			[]string{MediaTypeOCIV1Layer},
		},
	}

	for _, test := range tests {
		if !isEqual(test.expectedManifestTypes, test.ms.ManifestMediaTypes()) {
			t.Errorf("expected manifest media types didn't match what was returned:\n%v\n%v", test.expectedManifestTypes, test.ms.ManifestMediaTypes())
		}
		if !isEqual(test.expectedConfigTypes, test.ms.ConfigMediaTypes()) {
			t.Errorf("expected config media types didn't match what was returned:\n%v\n%v", test.expectedConfigTypes, test.ms.ConfigMediaTypes())
		}
		if !isEqual(test.expectedLayerTypes, test.ms.LayerMediaTypes()) {
			t.Errorf("expected layer media types didn't match what was returned:\n%v\n%v", test.expectedLayerTypes, test.ms.LayerMediaTypes())
		}
	}
}

func TestRegistryOptionSet(t *testing.T) {
	tests := []struct {
		rs       RegistryOptionSet
		allowsV1 bool
		allowsV2 bool
	}{
		{
			RegistryOptionSet{RegistryOptionV1}, true, false,
		},
		{
			RegistryOptionSet{RegistryOptionV2}, false, true,
		},
		{
			RegistryOptionSet{RegistryOptionV1, RegistryOptionV2}, true, true,
		},
		{
			RegistryOptionSet{}, true, true,
		},
	}
	for _, test := range tests {
		if test.allowsV1 != test.rs.AllowsV1() {
			t.Errorf("doesn't allow V1 when it should")
		}
		if test.allowsV2 != test.rs.AllowsV2() {
			t.Errorf("doesn't allow V1 when it should")
		}
	}
}

func isEqual(val1, val2 []string) bool {
	if len(val1) != len(val2) {
		return false
	}
loop1:
	for _, thing1 := range val1 {
		for _, thing2 := range val2 {
			if thing1 == thing2 {
				continue loop1
			}
		}
		return false
	}
	return true
}

func TestParseDockerURL(t *testing.T) {
	tests := []struct {
		input    string
		expected *ParsedDockerURL
	}{
		{
			"busybox",
			&ParsedDockerURL{
				OriginalName: "busybox",
				IndexURL:     "registry-1.docker.io",
				ImageName:    "library/busybox",
				Tag:          "latest",
				Digest:       "",
			},
		},
		{
			"library/busybox",
			&ParsedDockerURL{
				OriginalName: "library/busybox",
				IndexURL:     "registry-1.docker.io",
				ImageName:    "library/busybox",
				Tag:          "latest",
				Digest:       "",
			},
		},
		{
			"docker.io/library/busybox:1",
			&ParsedDockerURL{
				OriginalName: "docker.io/library/busybox:1",
				IndexURL:     "registry-1.docker.io",
				ImageName:    "library/busybox",
				Tag:          "1",
				Digest:       "",
			},
		},
		{
			"docker.io/library/busybox",
			&ParsedDockerURL{
				OriginalName: "docker.io/library/busybox",
				IndexURL:     "registry-1.docker.io",
				ImageName:    "library/busybox",
				Tag:          "latest",
				Digest:       "",
			},
		},
		{
			"gcr.io/google-samples/node-hello:1.0",
			&ParsedDockerURL{
				OriginalName: "gcr.io/google-samples/node-hello:1.0",
				IndexURL:     "gcr.io",
				ImageName:    "google-samples/node-hello",
				Tag:          "1.0",
				Digest:       "",
			},
		},
		{
			"alpine@sha256:ea0d1389812f43e474c50155ec4914e1b48792d420820c15cab28c0794034950",
			&ParsedDockerURL{
				OriginalName: "alpine@sha256:ea0d1389812f43e474c50155ec4914e1b48792d420820c15cab28c0794034950",
				IndexURL:     "registry-1.docker.io",
				ImageName:    "library/alpine",
				Tag:          "",
				Digest:       "sha256:ea0d1389812f43e474c50155ec4914e1b48792d420820c15cab28c0794034950",
			},
		},
	}
	for _, test := range tests {
		parsed, err := ParseDockerURL(test.input)
		if err != nil && test.expected != nil {
			t.Errorf("error when parsing %q: %v\nexpected: %+v", test.input, err, test.expected)
		} else if err == nil && test.expected == nil {
			t.Errorf("expected %q to result in error\n", test.input)
		} else if !reflect.DeepEqual(test.expected, parsed) {
			t.Errorf("expected and parsed `&ParsedDockerURL{}` differ:\nexpected: %+v\nparsed:   %+v\n", test.expected, parsed)
		}
	}
}
