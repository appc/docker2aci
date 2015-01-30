// Package docker2aci implements a simple library for converting docker images to
// App Container Images (ACIs).
package docker2aci

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/appc/spec/aci"
	"github.com/appc/spec/schema"
	"github.com/appc/spec/schema/types"
	"github.com/docker/docker/pkg/parsers"
	"github.com/docker/docker/registry"
	"github.com/docker/docker/runconfig"
)

type DockerImageData struct {
	ID            string            `json:"id"`
	Parent        string            `json:"parent,omitempty"`
	Comment       string            `json:"comment,omitempty"`
	Created       time.Time         `json:"created"`
	Container     string            `json:"container,omitempty"`
	DockerVersion string            `json:"docker_version,omitempty"`
	Author        string            `json:"author,omitempty"`
	Config        *runconfig.Config `json:"config,omitempty"`
	Architecture  string            `json:"architecture,omitempty"`
	OS            string            `json:"os,omitempty"`
	Checksum      string            `json:"checksum"`
}

type RepoData struct {
	Tokens    []string
	Endpoints []string
	Cookie    []string
}

type DockerURL struct {
	IndexURL  string
	ImageName string
	Tag       string
}

type SquashAccumulator struct {
	*tar.Writer
	Manifests []schema.ImageManifest
	Filelist  []string
}

const (
	defaultIndex  = "index.docker.io"
	defaultTag    = "latest"
	schemaVersion = "0.1.1"
)

// Convert generates ACI images from docker registry URLs.
// It takes as input a dockerURL of the form:
//
// 	{docker registry URL}/{image name}:{tag}
//
// It then gets all the layers of the requested image and converts each of
// them to ACI.
// If the squash flag is true, it squashes all the layers in one file and
// places this file in outputDir; if it is false, it places every layer in its
// own ACI in outputDir.
// It returns the list of generated ACI paths.
func Convert(dockerURL string, squash bool, outputDir string) ([]string, error) {
	parsedURL, err := parseDockerURL(dockerURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing docker url: %v\n", err)
	}

	repoData, err := getRepoData(parsedURL.IndexURL, parsedURL.ImageName)
	if err != nil {
		return nil, fmt.Errorf("error getting repository data: %v\n", err)
	}

	// TODO(iaguis) check more endpoints
	appImageID, err := getImageIDFromTag(repoData.Endpoints[0], parsedURL.ImageName, parsedURL.Tag, repoData)
	if err != nil {
		return nil, fmt.Errorf("error getting ImageID from tag %s: %v\n", parsedURL.Tag, err)
	}

	ancestry, err := getAncestry(appImageID, repoData.Endpoints[0], repoData)
	if err != nil {
		return nil, fmt.Errorf("error getting ancestry: %v\n", err)
	}

	layersOutputDir := outputDir
	if squash {
		layersOutputDir, err = ioutil.TempDir("", "docker2aci-")
		if err != nil {
			return nil, fmt.Errorf("error creating dir: %v", err)
		}
		defer os.RemoveAll(layersOutputDir)
	}

	var aciLayerPaths []string
	for _, layerID := range ancestry {
		aciPath, err := buildACI(layerID, repoData, parsedURL, layersOutputDir)
		if err != nil {
			return nil, fmt.Errorf("error building layer: %v\n", err)
		}

		aciLayerPaths = append(aciLayerPaths, aciPath)
	}

	if squash {
		squashedFilename := strings.Replace(parsedURL.ImageName, "/", "-", -1)
		if parsedURL.Tag != "" {
			squashedFilename += "-" + parsedURL.Tag
		}
		squashedFilename += ".aci"
		squashedImagePath := path.Join(outputDir, squashedFilename)

		if err := squashLayers(aciLayerPaths, squashedImagePath); err != nil {
			return nil, fmt.Errorf("error squashing image: %v\n", err)
		}
		aciLayerPaths = []string{squashedImagePath}
	}

	return aciLayerPaths, nil
}

func parseDockerURL(arg string) (*DockerURL, error) {
	taglessRemote, tag := parsers.ParseRepositoryTag(arg)
	if tag == "" {
		tag = defaultTag
	}
	repoInfo, err := registry.ParseRepositoryInfo(taglessRemote)
	if err != nil {
		return nil, err
	}
	indexURL := defaultIndex
	if !repoInfo.Index.Official {
		indexURL = repoInfo.Index.Name
	}

	return &DockerURL{
		IndexURL:  indexURL,
		ImageName: repoInfo.RemoteName,
		Tag:       tag,
	}, nil
}

func getRepoData(indexURL string, remote string) (*RepoData, error) {
	client := &http.Client{}
	repositoryURL := "https://" + path.Join(indexURL, "v1", "repositories", remote, "images")

	req, err := http.NewRequest("GET", repositoryURL, nil)
	if err != nil {
		return nil, err
	}

	// TODO(iaguis) add auth?
	req.Header.Set("X-Docker-Token", "true")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP code: %d, URL: %s", res.StatusCode, req.URL)
	}

	var tokens []string
	if res.Header.Get("X-Docker-Token") != "" {
		tokens = res.Header["X-Docker-Token"]
	}

	var cookies []string
	if res.Header.Get("Set-Cookie") != "" {
		cookies = res.Header["Set-Cookie"]
	}

	var endpoints []string
	if res.Header.Get("X-Docker-Endpoints") != "" {
		endpoints = makeEndpointsList(res.Header["X-Docker-Endpoints"])
	} else {
		// Assume same endpoint
		endpoints = append(endpoints, indexURL)
	}

	return &RepoData{
		Endpoints: endpoints,
		Tokens:    tokens,
		Cookie:    cookies,
	}, nil
}

func getImageIDFromTag(registry string, appName string, tag string, repoData *RepoData) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+path.Join(registry, "repositories", appName, "tags", tag), nil)
	if err != nil {
		return "", fmt.Errorf("failed to get Image ID: %s, URL: %s", err, req.URL)
	}

	setAuthToken(req, repoData.Tokens)
	setCookie(req, repoData.Cookie)
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get Image ID: %s, URL: %s", err, req.URL)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("HTTP code: %d. URL: %s", res.StatusCode, req.URL)
	}

	j, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return "", err
	}

	var imageID string

	if err := json.Unmarshal(j, &imageID); err != nil {
		return "", fmt.Errorf("error unmarshaling: %v", err)
	}

	return imageID, nil
}

func getAncestry(imgID, registry string, repoData *RepoData) ([]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+path.Join(registry, "images", imgID, "ancestry"), nil)
	if err != nil {
		return nil, err
	}

	setAuthToken(req, repoData.Tokens)
	setCookie(req, repoData.Cookie)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP code: %d. URL: %s", res.StatusCode, req.URL)
	}

	var ancestry []string

	j, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read downloaded json: %s (%s)", err, j)
	}

	if err := json.Unmarshal(j, &ancestry); err != nil {
		return nil, fmt.Errorf("error unmarshaling: %v", err)
	}

	return ancestry, nil
}

func buildACI(layerID string, repoData *RepoData, dockerURL *DockerURL, outputDir string) (string, error) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-")
	if err != nil {
		return "", fmt.Errorf("error creating dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	layerDest := filepath.Join(tmpDir, "layer")
	layerRootfs := filepath.Join(layerDest, "rootfs")
	err = os.MkdirAll(layerRootfs, 0700)
	if err != nil {
		return "", fmt.Errorf("error creating dir: %s", layerRootfs)
	}

	j, size, err := getRemoteImageJSON(layerID, repoData.Endpoints[0], repoData)
	if err != nil {
		return "", fmt.Errorf("error getting image json: %v", err)
	}

	layerData := DockerImageData{}
	if err := json.Unmarshal(j, &layerData); err != nil {
		return "", fmt.Errorf("error unmarshaling layer data: %v", err)
	}

	layer, err := getRemoteLayer(layerID, repoData.Endpoints[0], repoData, int64(size))
	if err != nil {
		return "", fmt.Errorf("error getting the remote layer: %v", err)
	}
	defer layer.Close()

	layerFile, err := ioutil.TempFile(tmpDir, "dockerlayer-")
	if err != nil {
		return "", fmt.Errorf("error creating layer: %v", err)
	}

	_, err = io.Copy(layerFile, layer)
	if err != nil {
		return "", fmt.Errorf("error getting layer: %v", err)
	}

	layerFile.Sync()

	manifest, err := generateManifest(layerData, dockerURL)
	if err != nil {
		return "", fmt.Errorf("error generating the manifest: %v", err)
	}

	imageName := strings.Replace(dockerURL.ImageName, "/", "-", -1)
	aciPath := imageName + "-" + layerID
	if dockerURL.Tag != "" {
		aciPath += "-" + dockerURL.Tag
	}
	if layerData.OS != "" {
		aciPath += "-" + layerData.OS
		if layerData.Architecture != "" {
			aciPath += "-" + layerData.Architecture
		}
	}
	aciPath += ".aci"

	aciPath = path.Join(outputDir, aciPath)

	if err := writeACI(layerFile, *manifest, aciPath); err != nil {
		return "", fmt.Errorf("error writing ACI: %v", err)
	}

	return aciPath, nil
}

func getRemoteImageJSON(imgID, registry string, repoData *RepoData) ([]byte, int, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+path.Join(registry, "images", imgID, "json"), nil)
	if err != nil {
		return nil, -1, err
	}
	setAuthToken(req, repoData.Tokens)
	setCookie(req, repoData.Cookie)
	res, err := client.Do(req)
	if err != nil {
		return nil, -1, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, -1, fmt.Errorf("HTTP code: %d, URL: %s", res.StatusCode, req.URL)
	}

	imageSize := -1

	if hdr := res.Header.Get("X-Docker-Size"); hdr != "" {
		imageSize, err = strconv.Atoi(hdr)
		if err != nil {
			return nil, -1, err
		}
	}

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to read downloaded json: %v (%s)", err, b)
	}

	return b, imageSize, nil
}

func getRemoteLayer(imgID, registry string, repoData *RepoData, imgSize int64) (io.ReadCloser, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://"+path.Join(registry, "images", imgID, "layer"), nil)
	if err != nil {
		return nil, err
	}

	setAuthToken(req, repoData.Tokens)
	setCookie(req, repoData.Cookie)

	fmt.Printf("Downloading layer: %s\n", imgID)

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("HTTP code: %d. URL: %s", res.StatusCode, req.URL)
	}

	return res.Body, nil
}

func generateManifest(layerData DockerImageData, dockerURL *DockerURL) (*schema.ImageManifest, error) {
	dockerConfig := layerData.Config
	genManifest := &schema.ImageManifest{}

	appURL := dockerURL.IndexURL + "/" + dockerURL.ImageName + "-" + layerData.ID
	name, err := types.NewACName(appURL)
	if err != nil {
		return nil, err
	}
	genManifest.Name = *name

	acVersion, _ := types.NewSemVer(schemaVersion)
	genManifest.ACVersion = *acVersion

	genManifest.ACKind = types.ACKind("ImageManifest")

	var labels types.Labels
	var parentLabels types.Labels

	layer, _ := types.NewACName("layer")
	labels = append(labels, types.Label{Name: *layer, Value: layerData.ID})

	tag := dockerURL.Tag
	version, _ := types.NewACName("version")
	labels = append(labels, types.Label{Name: *version, Value: tag})

	if layerData.OS != "" {
		os, _ := types.NewACName("os")
		labels = append(labels, types.Label{Name: *os, Value: layerData.OS})
		parentLabels = append(parentLabels, types.Label{Name: *os, Value: layerData.OS})

		if layerData.Architecture != "" {
			arch, _ := types.NewACName("arch")
			parentLabels = append(parentLabels, types.Label{Name: *arch, Value: layerData.Architecture})
		}
	}

	genManifest.Labels = labels

	if dockerConfig != nil {
		var exec types.Exec
		if len(dockerConfig.Cmd) > 0 {
			exec = types.Exec(dockerConfig.Cmd)
		} else if len(dockerConfig.Entrypoint) > 0 {
			exec = types.Exec(dockerConfig.Entrypoint)
		}
		if exec != nil {
			user, group := parseDockerUser(dockerConfig.User)
			app := &types.App{Exec: exec, User: user, Group: group}
			genManifest.App = app
		}
	}

	if layerData.Parent != "" {
		var dependencies types.Dependencies
		parentAppNameString := dockerURL.IndexURL + "/" + dockerURL.ImageName + "-" + layerData.Parent

		parentAppName, err := types.NewACName(parentAppNameString)
		if err != nil {
			return nil, err
		}

		dependencies = append(dependencies, types.Dependency{App: *parentAppName, Labels: parentLabels})

		genManifest.Dependencies = dependencies
	}

	return genManifest, nil
}

func parseDockerUser(dockerUser string) (string, string) {
	// if the docker user is empty assume root user and group
	if dockerUser == "" {
		return "0", "0"
	}

	dockerUserParts := strings.Split(dockerUser, ":")

	// when only the user is given, the docker spec says that the default and
	// supplementary groups of the user in /etc/passwd should be applied.
	// Assume root group for now in this case.
	if len(dockerUserParts) < 2 {
		return dockerUserParts[0], "0"
	}

	return dockerUserParts[0], dockerUserParts[1]
}

func writeACI(layer io.ReadSeeker, manifest schema.ImageManifest, output string) error {
	reader, err := aci.NewCompressedTarReader(layer)
	if err != nil {
		return err
	}

	aciFile, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("error creating ACI file: %v", err)
	}
	defer aciFile.Close()

	trw := tar.NewWriter(aciFile)

	archiveWriter := aci.NewImageWriter(manifest, trw)
	defer archiveWriter.Close()

	// Write files in rootfs/
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return fmt.Errorf("error reading layer tar entry: %v", err)
		}
		if hdr.Name == "./" {
			continue
		}
		if strings.HasPrefix(path.Base(hdr.Name), ".wh.") {
			continue
		}
		hdr.Name = "rootfs/" + hdr.Name
		if hdr.Typeflag == tar.TypeLink {
			hdr.Linkname = "rootfs/" + hdr.Linkname
		}

		err = archiveWriter.AddFile(hdr, reader)
		if err != nil {
			return fmt.Errorf("error adding file to ACI: %v", err)
		}
	}

	return nil
}

func squashLayers(layers []string, squashedImagePath string) error {
	squashedImageFile, err := os.Create(squashedImagePath)
	if err != nil {
		return err
	}
	defer squashedImageFile.Close()

	squashAcc := &SquashAccumulator{
		tar.NewWriter(squashedImageFile),
		[]schema.ImageManifest{},
		[]string{},
	}
	for _, aciPath := range layers {
		squashAcc, err = reduceACIs(squashAcc, aciPath)
		if err != nil {
			return err
		}
	}
	defer squashAcc.Close()

	finalManifest := mergeManifests(squashAcc.Manifests)

	b, err := json.Marshal(finalManifest)
	if err != nil {
		return err
	}

	// Write final manifest
	hdr := &tar.Header{
		Name: "manifest",
		Mode: 0600,
		Size: int64(len(b)),
	}
	if err := squashAcc.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := squashAcc.Write(b); err != nil {
		return err
	}

	return nil
}

func reduceACIs(squashAcc *SquashAccumulator, currentPath string) (*SquashAccumulator, error) {
	currentFile, err := os.Open(currentPath)
	if err != nil {
		return nil, err
	}
	defer currentFile.Close()

	manifestCur, err := aci.ManifestFromImage(currentFile)
	if err != nil {
		return nil, err
	}
	if _, err := currentFile.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}

	squashAcc.Manifests = append(squashAcc.Manifests, *manifestCur)

	reader, err := aci.NewCompressedTarReader(currentFile)
	if err != nil {
		return nil, err
	}
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading layer tar entry: %v", err)
		}

		if hdr.Name == "manifest" {
			continue
		}

		if !in(squashAcc.Filelist, hdr.Name) {
			squashAcc.Filelist = append(squashAcc.Filelist, hdr.Name)

			if err := squashAcc.WriteHeader(hdr); err != nil {
				return nil, fmt.Errorf("error writing header: %v", err)
			}
			if _, err := io.Copy(squashAcc, reader); err != nil {
				return nil, fmt.Errorf("error copying file into the tar out: %v", err)
			}
		}
	}

	return squashAcc, nil
}

func in(list []string, el string) bool {
	for _, x := range list {
		if el == x {
			return true
		}
	}
	return false
}

func mergeManifests(manifests []schema.ImageManifest) schema.ImageManifest {
	// FIXME(iaguis) we take last layer's manifest as the final manifest for now
	manifest := manifests[0]

	manifest.Dependencies = nil

	layerIndex := -1
	for i, l := range manifest.Labels {
		if l.Name.String() == "layer" {
			layerIndex = i
		}
	}

	if layerIndex != -1 {
		manifest.Labels = append(manifest.Labels[:layerIndex], manifest.Labels[layerIndex+1:]...)
	}

	return manifest
}

func setAuthToken(req *http.Request, token []string) {
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Token "+strings.Join(token, ","))
	}
}

func setCookie(req *http.Request, cookie []string) {
	if req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", strings.Join(cookie, ""))
	}
}
