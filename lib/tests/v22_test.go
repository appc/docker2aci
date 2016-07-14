package test

import (
	"testing"

	"archive/tar"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	docker2aci "github.com/appc/docker2aci/lib"
	d2acommon "github.com/appc/docker2aci/lib/common"
	"github.com/appc/docker2aci/lib/internal/typesV2"
)

func newDocker22Image(layers []Layer) Docker22Image {
	return Docker22Image{
		RepoTags: []string{"testimage:latest"},
		Layers:   layers,
		Config: typesV2.ImageConfig{
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
					"BAR=2",
				},
				Entrypoint: nil,
				Cmd:        nil,
				Volumes:    nil,
				WorkingDir: "/",
			},
		},
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

	_, err = fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
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
