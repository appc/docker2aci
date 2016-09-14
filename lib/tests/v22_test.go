package test

import (
	"testing"

	"archive/tar"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"time"

	docker2aci "github.com/appc/docker2aci/lib"
	d2acommon "github.com/appc/docker2aci/lib/common"
	"github.com/appc/docker2aci/lib/internal/typesV2"
	"github.com/appc/spec/aci"
	"github.com/appc/spec/schema"
	"github.com/appc/spec/schema/types"
)

var dockerConfig = typesV2.ImageConfig{
	Created:      "2016-06-02T21:43:31.291506236Z",
	Author:       "rkt developer <rkt-dev@googlegroups.com>",
	Architecture: "amd64",
	OS:           "linux",
	Config: &typesV2.ImageConfigConfig{
		User:       "",
		Memory:     12345,
		MemorySwap: 0,
		CpuShares:  9001,
		ExposedPorts: map[string]struct{}{
			"80": struct{}{},
		},
		Env: []string{
			"FOO=1",
		},
		Entrypoint: []string{
			"/bin/sh",
			"-c",
			"echo",
		},
		Cmd: []string{
			"foo",
		},
		Volumes:    nil,
		WorkingDir: "/",
	},
}

var expectedImageManifest = schema.ImageManifest{
	ACKind:    types.ACKind("ImageManifest"),
	ACVersion: schema.AppContainerVersion,
	Name:      *types.MustACIdentifier("variant"),
	Labels: []types.Label{
		types.Label{
			*types.MustACIdentifier("arch"),
			"amd64",
		},
		types.Label{
			*types.MustACIdentifier("os"),
			"linux",
		},
		types.Label{
			*types.MustACIdentifier("version"),
			"v0.1.0",
		},
	},
	App: &types.App{
		Exec: []string{
			"/bin/sh",
			"-c",
			"echo",
			"foo",
		},
		User:  "0",
		Group: "0",
		Environment: []types.EnvironmentVariable{
			{
				Name:  "FOO",
				Value: "1",
			},
		},
		WorkingDirectory: "/",
		Ports: []types.Port{
			{
				Name:            "80",
				Protocol:        "tcp",
				Port:            80,
				Count:           1,
				SocketActivated: false,
			},
		},
	},
	Annotations: []types.Annotation{
		{
			Name:  *types.MustACIdentifier("author"),
			Value: "rkt developer <rkt-dev@googlegroups.com>",
		},
		{
			Name:  *types.MustACIdentifier("created"),
			Value: "2016-06-02T21:43:31.291506236Z",
		},
		{
			Name:  *types.MustACIdentifier("appc.io/docker/registryurl"),
			Value: "variant",
		},
		{
			Name:  *types.MustACIdentifier("appc.io/docker/repository"),
			Value: "docker2aci/dockerv22test",
		},
		{
			Name:  *types.MustACIdentifier("appc.io/docker/imageid"),
			Value: "variant",
		},
		{
			Name:  *types.MustACIdentifier("appc.io/docker/entrypoint"),
			Value: "[\"/bin/sh\",\"-c\",\"echo\"]",
		},
		{
			Name:  *types.MustACIdentifier("appc.io/docker/cmd"),
			Value: "[\"foo\"]",
		},
	},
}

func newDocker22Image(layers []Layer) Docker22Image {
	return Docker22Image{
		RepoTags: []string{"testimage:latest"},
		Layers:   layers,
		Config:   dockerConfig,
	}
}

func fetchImage(imgName, outputDir string, squash bool) ([]string, error) {
	conversionTmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(conversionTmpDir)

	conf := docker2aci.RemoteConfig{
		CommonConfig: docker2aci.CommonConfig{
			Squash:      squash,
			OutputDir:   outputDir,
			TmpDir:      conversionTmpDir,
			Compression: d2acommon.GzipCompression,
		},
		Username: "",
		Password: "",
		Insecure: d2acommon.InsecureConfig{
			SkipVerify: true,
			AllowHTTP:  true,
		},
	}

	return docker2aci.ConvertRemoteRepo(imgName, conf)
}

func TestFetchingByTagV22(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(tmpDir)
	layers := []Layer{
		Layer{
			&tar.Header{
				Name:    "thisisafile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("these are its contents"),
		},
	}
	err = GenerateDocker22(tmpDir, newDocker22Image(layers))
	if err != nil {
		t.Fatalf("%v", err)
	}
	imgName := "docker2aci/dockerv22test"
	imgRef := "v0.1.0"
	server := RunDockerRegistry(t, tmpDir, imgName, imgRef, typesV2.MediaTypeDockerV22Manifest)
	defer server.Close()

	localUrl := path.Join(strings.TrimPrefix(server.URL, "http://"), imgName) + ":" + imgRef

	outputDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(outputDir)

	acis, err := fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}

	converted := acis[0]

	f, err := os.Open(converted)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer f.Close()

	manifest, err := aci.ManifestFromImage(f)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if err := manifestEqual(manifest, &expectedImageManifest); err != nil {
		t.Fatalf("manifest doesn't match expected manifest: %v", err)
	}
}

func manifestEqual(manifest, expected *schema.ImageManifest) error {
	if manifest.ACKind != expected.ACKind {
		return fmt.Errorf("expected ACKind %q, got %q", expected.ACKind, manifest.ACKind)
	}
	if manifest.ACVersion != expected.ACVersion {
		return fmt.Errorf("expected ACVersion %q, got %q", expected.ACVersion, manifest.ACVersion)
	}
	if !reflect.DeepEqual(*manifest.App, *expected.App) {
		return fmt.Errorf("expected App %q, got %q", *expected.App, *manifest.App)
	}
	for _, label := range []string{"arch", "os", "version"} {
		if err := checkLabel(label, manifest, expected); err != nil {
			return err
		}
	}
	for _, ann := range []string{
		"author",
		"created",
		"appc.io/docker/repository",
		"appc.io/docker/entrypoint",
		"appc.io/docker/cmd",
	} {
		if err := checkAnnotation(ann, manifest, expected); err != nil {
			return err
		}
	}

	return nil
}

func checkLabel(name string, manifest, expected *schema.ImageManifest) error {
	got, ok := manifest.GetLabel(name)
	if !ok {
		return fmt.Errorf("missing %q label", name)
	}
	exp, _ := expected.GetLabel(name)
	if got != exp {
		return fmt.Errorf("expected label %q, got %q", exp, got)
	}

	return nil
}

func checkAnnotation(name string, manifest, expected *schema.ImageManifest) error {
	got, ok := manifest.GetAnnotation(name)
	if !ok {
		return fmt.Errorf("missing %q annotation", name)
	}
	exp, _ := expected.GetAnnotation(name)
	if got != exp {
		return fmt.Errorf("expected annotation %q, got %q", exp, got)
	}

	return nil
}

func TestFetchingByDigestV22(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(tmpDir)
	layers := []Layer{
		Layer{
			&tar.Header{
				Name:    "thisisafile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("these are its contents"),
		},
	}
	err = GenerateDocker22(tmpDir, newDocker22Image(layers))
	if err != nil {
		t.Fatalf("%v", err)
	}
	imgName := "docker2aci/dockerv22test"
	imgRef := "sha256:45b23dee08af5e43a7fea6c4cf9c25ccf269ee113168c19722f87876677c5cb2"
	server := RunDockerRegistry(t, tmpDir, imgName, imgRef, typesV2.MediaTypeDockerV22Manifest)
	defer server.Close()

	localUrl := path.Join(strings.TrimPrefix(server.URL, "http://"), imgName) + "@" + imgRef

	outputDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(outputDir)

	_, err = fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
}

func TestFetchingMultipleLayersV22(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(tmpDir)
	layers := []Layer{
		Layer{
			&tar.Header{
				Name:    "thisisafile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("these are its contents"),
		},
		Layer{
			&tar.Header{
				Name:    "thisisadifferentfile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("the contents of this file are different from the last!"),
		},
	}
	err = GenerateDocker22(tmpDir, newDocker22Image(layers))
	if err != nil {
		t.Fatalf("%v", err)
	}
	imgName := "docker2aci/dockerv22test"
	imgRef := "v0.1.0"
	server := RunDockerRegistry(t, tmpDir, imgName, imgRef, typesV2.MediaTypeDockerV22Manifest)
	defer server.Close()

	localUrl := path.Join(strings.TrimPrefix(server.URL, "http://"), imgName) + ":" + imgRef

	outputDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(outputDir)

	_, err = fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
}
