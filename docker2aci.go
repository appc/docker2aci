package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/appc/spec/schema"
	"github.com/appc/spec/schema/types"
	"github.com/coreos/rocket/cas"
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

type Compression int

const (
	defaultIndex  = "index.docker.io"
	defaultTag    = "latest"
	rocketDir     = "/var/lib/rkt"
	schemaVersion = "0.1.1"
)

const (
	Uncompressed Compression = iota
	Gzip
)

var flagImport = flag.Bool("import", false, "Import ACI images to the rocket store")

func makeEndpointsList(headers []string) []string {
	var endpoints []string

	for _, ep := range headers {
		endpointsList := strings.Split(ep, ",")
		for _, endpointEl := range endpointsList {
			endpoints = append(
				endpoints,
				// TODO(iaguis) discover if httpsOrHTTP
				fmt.Sprintf("https://%s/v1/", strings.TrimSpace(endpointEl)))
		}
	}

	return endpoints
}

func GetRepoData(indexURL string, remote string) (*RepoData, error) {
	client := &http.Client{}
	repositoryURL := fmt.Sprintf("%s/%s/v1/%s/%s/images", "https:/", indexURL, "repositories", remote)

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

func GetRemoteImageJSON(imgID, registry string, repoData *RepoData) ([]byte, int, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", registry+"images/"+imgID+"/json", nil)
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

	jsonBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, -1, fmt.Errorf("Failed to read downloaded json: %v (%s)", err, jsonBytes)
	}

	return jsonBytes, imageSize, nil
}

func GetRemoteLayer(imgID, registry string, repoData *RepoData, imgSize int64) (io.ReadCloser, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", registry+"images/"+imgID+"/layer", nil)
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

func GetImageIDFromTag(registry string, appName string, tag string, repoData *RepoData) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", registry+"repositories/"+appName+"/tags/"+tag, nil)
	if err != nil {
		return "", fmt.Errorf("Failed to get Image ID: %s, URL: %s", err, req.URL)
	}

	setAuthToken(req, repoData.Tokens)
	setCookie(req, repoData.Cookie)
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Failed to get Image ID: %s, URL: %s", err, req.URL)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("HTTP code: %d. URL: %s", res.StatusCode, req.URL)
	}

	jsonString, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return "", err
	}

	var imageID string

	if err := json.Unmarshal(jsonString, &imageID); err != nil {
		return "", fmt.Errorf("Error unmarshaling: %v", err)
	}

	return imageID, nil
}

func GetAncestry(imgID, registry string, repoData *RepoData) ([]string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", registry+"images/"+imgID+"/ancestry", nil)
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

	jsonString, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read downloaded json: %s (%s)", err, jsonString)
	}

	if err := json.Unmarshal(jsonString, &ancestry); err != nil {
		return nil, fmt.Errorf("Error unmarshaling: %v", err)
	}

	return ancestry, nil
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

func GenerateManifest(layerData DockerImageData, dockerURL *DockerURL) (*schema.ImageManifest, error) {
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
			// TODO(iaguis) populate user and group
			app := &types.App{Exec: exec, User: "0", Group: "0"}
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

func parseArgument(arg string) *DockerURL {
	indexURL := defaultIndex
	tag := defaultTag

	argParts := strings.SplitN(arg, "/", 2)
	var appString string
	if len(argParts) > 1 {
		if strings.Index(argParts[0], ".") != -1 {
			indexURL = argParts[0]
			appString = argParts[1]
		} else {
			appString = strings.Join(argParts, "/")
		}
	} else {
		appString = argParts[0]
	}

	imageName := appString
	appParts := strings.Split(appString, ":")

	if len(appParts) > 1 {
		tag = appParts[len(appParts)-1]
		imageNameParts := appParts[0 : len(appParts)-1]
		imageName = strings.Join(imageNameParts, ":")
	}

	return &DockerURL{
		IndexURL:  indexURL,
		ImageName: imageName,
		Tag:       tag,
	}
}

func DetectCompression(source []byte) Compression {
	for compression, m := range map[Compression][]byte{
		Gzip:  {0x1F, 0x8B, 0x08},
	} {
		if len(source) < len(m) {
			fmt.Fprintf(os.Stderr, "Len too short")
			continue
		}
		if bytes.Compare(m, source[:len(m)]) == 0 {
			return compression
		}
	}
	return Uncompressed
}

func Decompress(layer io.Reader) (io.Reader, error) {
	bufR := bufio.NewReader(layer)
	bs, _ := bufR.Peek(10)

	compression := DetectCompression(bs)
	switch compression {
	case Gzip:
		gz, err := gzip.NewReader(bufR)
		if err != nil {
			return nil, fmt.Errorf("Error reading layer (gzip): %v", err)
		}
		return gz, nil
	case Uncompressed:
		return bufR, nil
	default:
		return nil, fmt.Errorf("Unknown layer format")
	}
}

func WriteACI(layer io.Reader, manifest []byte, output string) error {
	reader, err := Decompress(layer)
	if err != nil {
		return err
	}

	tr := tar.NewReader(reader)
	if err != nil {
		return err
	}

	aciFile, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("Error creating ACI file: %v", err)
	}
	defer aciFile.Close()

	trw := tar.NewWriter(aciFile)

	// Write manifest
	hdr := &tar.Header{
		Name: "manifest",
		Mode: 0600,
		Size: int64(len(manifest)),
	}
	if err := trw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := trw.Write(manifest); err != nil {
		return err
	}

	// Write files in rootfs/
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return fmt.Errorf("Error reading layer tar entry: %v", err)
		}
		if hdr.Name == "./" {
			continue
		}
		hdr.Name = "rootfs/"+hdr.Name
		if err := trw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("Error writing header: %v", err)
		}
		if _, err := io.Copy(trw, tr); err != nil {
			return fmt.Errorf("Error copying file into the tar out: %v", err)
		}
	}

	if err := trw.Close(); err != nil {
		return fmt.Errorf("Error closing ACI file: %v", err)
	}

	return nil
}

func BuildACI(layerID string, repoData *RepoData, dockerURL *DockerURL) (string, error) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-")
	if err != nil {
		return "", fmt.Errorf("Error creating dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	layerDest := tmpDir + "/layer"
	layerRootfs := layerDest + "/rootfs"
	err = os.MkdirAll(layerRootfs, 0700)
	if err != nil {
		return "", fmt.Errorf("Error creating dir: %s", layerRootfs)
	}

	jsonString, size, err := GetRemoteImageJSON(layerID, repoData.Endpoints[0], repoData)
	if err != nil {
		return "", fmt.Errorf("Error getting image json: %v", err)
	}

	layerData := DockerImageData{}
	if err := json.Unmarshal(jsonString, &layerData); err != nil {
		return "", fmt.Errorf("Error unmarshaling layer data: %v", err)
	}

	layer, err := GetRemoteLayer(layerID, repoData.Endpoints[0], repoData, int64(size))
	if err != nil {
		return "", fmt.Errorf("Error getting the remote layer: %v", err)
	}
	defer layer.Close()

	manifest, err := GenerateManifest(layerData, dockerURL)
	if err != nil {
		return "", fmt.Errorf("Error generating the manifest: %v", err)
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return "", err
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

	if err := WriteACI(layer, manifestBytes, aciPath); err != nil {
		return "", fmt.Errorf("Error writing ACI: %v", err)
	}

	fmt.Printf("Generated ACI: %s\n", aciPath)

	return aciPath, nil
}

func ImportACI(aciPath string, dataStore *cas.Store) (string, error) {
		aciFile, err := os.Open(aciPath)
		if err != nil {
			return "", err
		}
		defer aciFile.Close()

		aciReader := bufio.NewReader(aciFile)
		rocketImageID, err := dataStore.WriteACI(aciReader)
		if err != nil {
			return "", err
		}

		return rocketImageID, nil
}

func runDocker2ACI(arg string, importACI bool) error {
	dockerURL := parseArgument(arg)

	repoData, err := GetRepoData(dockerURL.IndexURL, dockerURL.ImageName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting image data: %v\n", err)
		return err
	}

	// TODO(iaguis) check more endpoints
	appImageID, err := GetImageIDFromTag(repoData.Endpoints[0], dockerURL.ImageName, dockerURL.Tag, repoData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting ImageID from tag %s: %v\n", dockerURL.Tag, err)
		return err
	}

	ancestry, err := GetAncestry(appImageID, repoData.Endpoints[0], repoData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting ancestry: %v\n", err)
		return err
	}

	ds := cas.NewStore(rocketDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open store: %v\n", err)
		return err
	}

	rocketAppImageID := ""

	// From base image
	for i := range(ancestry) {
		layerID := ancestry[i]
		aciPath, err := BuildACI(layerID, repoData, dockerURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error importing image: %v\n", err)
			return err
		}

		if importACI {
			rocketLayerID, err := ImportACI(aciPath, ds)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error importing ACI to the store: %v\n", err)
				return err
			}

			if layerID == appImageID {
				rocketAppImageID = rocketLayerID + "\n"
			}
		}
	}

	fmt.Printf(rocketAppImageID)
	return nil
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) != 1 {
		fmt.Println("Usage: docker2aci [--import] [REGISTRYURL/]IMAGE_NAME[:TAG]")
		return
	}

	if err := runDocker2ACI(args[0], *flagImport); err != nil {
		os.Exit(1)
	}
}
