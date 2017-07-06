// Copyright 2015 The appc Authors
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

// Package internal provides functions shared by different parts of docker2aci.
//
// Note: this package is an implementation detail and shouldn't be used outside
// of docker2aci.
package internal

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/appc/docker2aci/lib/common"
	"github.com/appc/docker2aci/lib/internal/tarball"
	"github.com/appc/docker2aci/lib/internal/types"
	"github.com/appc/docker2aci/lib/internal/typesV2"
	"github.com/appc/docker2aci/lib/internal/util"
	"github.com/appc/docker2aci/pkg/log"
	"github.com/appc/spec/aci"
	"github.com/appc/spec/schema"
	appctypes "github.com/appc/spec/schema/types"
	gzip "github.com/klauspost/pgzip"
	canJson "github.com/docker/go/canonical/json"
)

// Docker2ACIBackend is the interface that abstracts converting Docker layers
// to ACI from where they're stored (remote or file).
//
// GetImageInfo takes a Docker URL and returns a list of layers and the parsed
// Docker URL.
//
// BuildACI takes a Docker layer, converts it to ACI and returns its output
// path and its converted ImageManifest.
type Docker2ACIBackend interface {
	// GetImageInfo, given the url for a docker image, will return the
	// following:
	// - []string: an ordered list of all layer hashes
	// - string: a unique identifier for this image, like a hash of the manifest
	// - *common.ParsedDockerURL: a parsed docker URL
	// - error: an error if one occurred
	GetImageInfo(dockerUrl string) ([]string, string, *common.ParsedDockerURL, error)
	BuildACI(layerIDs []string, manhash string, dockerURL *common.ParsedDockerURL, outputDir string, tmpBaseDir string, compression common.Compression) ([]string, []*schema.ImageManifest, error)
}

// GenerateACI takes a Docker layer and generates an ACI from it.
func GenerateACI(layerNumber int, manhash string, layerData types.DockerImageData, dockerURL *common.ParsedDockerURL, outputDir string, layerFile *os.File, curPwl []string, compression common.Compression, debug log.Logger) (string, *schema.ImageManifest, error) {
	manifest, err := GenerateManifest(layerData, manhash, dockerURL, debug)
	if err != nil {
		return "", nil, fmt.Errorf("error generating the manifest: %v", err)
	}

	imageName := strings.Replace(dockerURL.ImageName, "/", "-", -1)
	aciPath := generateACIPath(outputDir, imageName, layerData.ID, dockerURL.Tag, layerData.OS, layerData.Architecture, layerNumber)

	manifest, err = writeACI(layerFile, *manifest, curPwl, aciPath, compression)
	if err != nil {
		return "", nil, fmt.Errorf("error writing ACI: %v", err)
	}

	if err := ValidateACI(aciPath); err != nil {
		return "", nil, fmt.Errorf("invalid ACI generated: %v", err)
	}

	return aciPath, manifest, nil
}

func GenerateACI22LowerLayer(dockerURL *common.ParsedDockerURL, layerDigest string, outputDir string, layerFile *os.File, curPwl []string, compression common.Compression) (string, *schema.ImageManifest, error) {
	formattedDigest := strings.Replace(layerDigest, ":", "-", -1)
	aciName := fmt.Sprintf("%s/%s-%s", dockerURL.IndexURL, dockerURL.ImageName, formattedDigest)
	sanitizedAciName, err := appctypes.SanitizeACIdentifier(aciName)
	if err != nil {
		return "", nil, err
	}
	manifest, err := GenerateEmptyManifest(sanitizedAciName)
	if err != nil {
		return "", nil, err
	}

	aciPath := generateACIPath(outputDir, aciName, layerDigest, dockerURL.Tag, runtime.GOOS, runtime.GOARCH, -1)
	manifest, err = writeACI(layerFile, *manifest, curPwl, aciPath, compression)
	if err != nil {
		return "", nil, err
	}

	err = ValidateACI(aciPath)
	if err != nil {
		return "", nil, fmt.Errorf("invalid ACI generated: %v", err)
	}
	return aciPath, manifest, nil
}

func GenerateACI22TopLayer(dockerURL *common.ParsedDockerURL, manhash string, imageConfig *typesV2.ImageConfig, layerDigest string, outputDir string, layerFile *os.File, curPwl []string, compression common.Compression, lowerLayers []*schema.ImageManifest, debug log.Logger) (string, *schema.ImageManifest, error) {
	aciName := fmt.Sprintf("%s/%s-%s", dockerURL.IndexURL, dockerURL.ImageName, layerDigest)
	sanitizedAciName, err := appctypes.SanitizeACIdentifier(aciName)
	if err != nil {
		return "", nil, err
	}
	manifest, err := GenerateManifestV22(sanitizedAciName, manhash, layerDigest, dockerURL, imageConfig, lowerLayers, debug)
	if err != nil {
		return "", nil, err
	}

	aciPath := generateACIPath(outputDir, aciName, layerDigest, dockerURL.Tag, runtime.GOOS, runtime.GOARCH, -1)
	manifest, err = writeACI(layerFile, *manifest, curPwl, aciPath, compression)
	if err != nil {
		return "", nil, err
	}

	err = ValidateACI(aciPath)
	if err != nil {
		return "", nil, fmt.Errorf("invalid ACI generated: %v", err)
	}
	return aciPath, manifest, nil
}

func generateACIPath(outputDir, imageName, digest, tag, osString, arch string, layerNum int) string {
	aciPath := imageName
	if tag != "" {
		aciPath += "-" + tag
	}
	if osString != "" {
		aciPath += "-" + osString
		if arch != "" {
			aciPath += "-" + arch
		}
	}
	if layerNum != -1 {
		aciPath += "-" + strconv.Itoa(layerNum)
	}
	aciPath += ".aci"
	return path.Join(outputDir, aciPath)
}

func generateEPCmdAnnotation(dockerEP, dockerCmd []string) (string, string, error) {
	var entrypointAnnotation, cmdAnnotation string
	if len(dockerEP) > 0 {
		entry, err := json.Marshal(dockerEP)
		if err != nil {
			return "", "", err
		}
		entrypointAnnotation = string(entry)
	}
	if len(dockerCmd) > 0 {
		cmd, err := json.Marshal(dockerCmd)
		if err != nil {
			return "", "", err
		}
		cmdAnnotation = string(cmd)
	}

	return entrypointAnnotation, cmdAnnotation, nil
}

// setLabel sets the label entries associated with non-empty key
// to the single non-empty value. It replaces any existing values
// associated with key.
func setLabel(labels map[appctypes.ACIdentifier]string, key, val string) {
	if key != "" && val != "" {
		labels[*appctypes.MustACIdentifier(key)] = val
	}
}

// setOSArch translates the given OS and architecture strings into
// the compatible with application container specification and sets
// the respective label entries.
//
// Returns an error if label translation fails.
func setOSArch(labels map[appctypes.ACIdentifier]string, os, arch string) error {
	// Translate arch tuple into the appc arch tuple.
	appcOS, appcArch, err := appctypes.ToAppcOSArch(os, arch, "")
	if err != nil {
		return err
	}

	// Set translated labels.
	setLabel(labels, "os", appcOS)
	setLabel(labels, "arch", appcArch)
	return nil
}

// setAnnotation sets the annotation entries associated with non-empty
// key to the single non-empty value. It replaces any existing values
// associated with key.
func setAnnotation(annotations *appctypes.Annotations, key, val string) {
	if key != "" && val != "" {
		annotations.Set(*appctypes.MustACIdentifier(key), val)
	}
}

// GenerateManifest converts the docker manifest format to an appc
// ImageManifest.
func GenerateManifest(layerData types.DockerImageData, manhash string, dockerURL *common.ParsedDockerURL, debug log.Logger) (*schema.ImageManifest, error) {
	dockerConfig := layerData.Config
	genManifest := &schema.ImageManifest{}

	appURL := ""
	appURL = dockerURL.IndexURL + "/"
	appURL += dockerURL.ImageName + "-" + layerData.ID
	appURL, err := appctypes.SanitizeACIdentifier(appURL)
	if err != nil {
		return nil, err
	}
	name := appctypes.MustACIdentifier(appURL)
	genManifest.Name = *name

	acVersion, err := appctypes.NewSemVer(schema.AppContainerVersion.String())
	if err != nil {
		panic("invalid appc spec version")
	}
	genManifest.ACVersion = *acVersion

	genManifest.ACKind = appctypes.ACKind(schema.ImageManifestKind)

	var annotations appctypes.Annotations

	labels := make(map[appctypes.ACIdentifier]string)
	parentLabels := make(map[appctypes.ACIdentifier]string)

	setLabel(labels, "layer", layerData.ID)
	setLabel(labels, "version", dockerURL.Tag)

	setOSArch(labels, layerData.OS, layerData.Architecture)
	setOSArch(parentLabels, layerData.OS, layerData.Architecture)

	setAnnotation(&annotations, "authors", layerData.Author)
	epoch := time.Unix(0, 0)
	if !layerData.Created.Equal(epoch) {
		setAnnotation(&annotations, "created", layerData.Created.Format(time.RFC3339))
	}
	setAnnotation(&annotations, "docker-comment", layerData.Comment)
	setAnnotation(&annotations, common.AppcDockerOriginalName, dockerURL.OriginalName)
	setAnnotation(&annotations, common.AppcDockerRegistryURL, dockerURL.IndexURL)
	setAnnotation(&annotations, common.AppcDockerRepository, dockerURL.ImageName)
	setAnnotation(&annotations, common.AppcDockerImageID, layerData.ID)
	setAnnotation(&annotations, common.AppcDockerParentImageID, layerData.Parent)
	setAnnotation(&annotations, common.AppcDockerManifestHash, manhash)

	if dockerConfig != nil {
		exec := getExecCommand(dockerConfig.Entrypoint, dockerConfig.Cmd)
		user, group := parseDockerUser(dockerConfig.User)
		var env appctypes.Environment
		for _, v := range dockerConfig.Env {
			parts := strings.SplitN(v, "=", 2)
                        if len(parts) == 2 {
				env.Set(parts[0], parts[1])
                        }
		}
		app := &appctypes.App{
			Exec:             exec,
			User:             user,
			Group:            group,
			Environment:      env,
			WorkingDirectory: dockerConfig.WorkingDir,
		}

		app.UserLabels = dockerConfig.Labels

		app.MountPoints, err = convertVolumesToMPs(dockerConfig.Volumes)
		if err != nil {
			return nil, err
		}

		app.Ports, err = convertPorts(dockerConfig.ExposedPorts, dockerConfig.PortSpecs, debug)
		if err != nil {
			return nil, err
		}

		ep, cmd, err := generateEPCmdAnnotation(dockerConfig.Entrypoint, dockerConfig.Cmd)
		if err != nil {
			return nil, err
		}
		if len(ep) > 0 {
			setAnnotation(&annotations, common.AppcDockerEntrypoint, ep)
		}
		if len(cmd) > 0 {
			setAnnotation(&annotations, common.AppcDockerCmd, cmd)
		}

		genManifest.App = app
	}

	if layerData.Parent != "" {
		indexPrefix := ""
		// omit docker hub index URL in app name
		indexPrefix = dockerURL.IndexURL + "/"
		parentImageNameString := indexPrefix + dockerURL.ImageName + "-" + layerData.Parent
		parentImageNameString, err := appctypes.SanitizeACIdentifier(parentImageNameString)
		if err != nil {
			return nil, err
		}
		parentImageName := appctypes.MustACIdentifier(parentImageNameString)

		plbl, err := appctypes.LabelsFromMap(labels)
		if err != nil {
			return nil, err
		}

		genManifest.Dependencies = append(genManifest.Dependencies, appctypes.Dependency{ImageName: *parentImageName, Labels: plbl})

		setAnnotation(&annotations, common.AppcDockerTag, dockerURL.Tag)
	}

	genManifest.Labels, err = appctypes.LabelsFromMap(labels)
	if err != nil {
		return nil, err
	}
	genManifest.Annotations = annotations

	return genManifest, nil
}

func GenerateEmptyManifest(name string) (*schema.ImageManifest, error) {
	acid, err := appctypes.NewACIdentifier(name)
	if err != nil {
		return nil, err
	}

	labelsMap := make(map[appctypes.ACIdentifier]string)
	err = setOSArch(labelsMap, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}

	labels, err := appctypes.LabelsFromMap(labelsMap)
	if err != nil {
		return nil, err
	}

	return &schema.ImageManifest{
		ACKind:    schema.ImageManifestKind,
		ACVersion: schema.AppContainerVersion,
		Name:      *acid,
		Labels:    labels,
	}, nil
}

// GenerateManifestV22, given a large set of information (documented a couple
// lines down), will produce an image manifest compliant with the Dockver V2.2
// image spec
func GenerateManifestV22(
	name string, // The name of this image
	manhash string, // The hash of this image's manifest
	imageDigest string, // The digest of the image
	dockerURL *common.ParsedDockerURL, // The parsed docker URL
	config *typesV2.ImageConfig, // The image config
	lowerLayers []*schema.ImageManifest, // A list of manifests for the lower layers
	debug log.Logger, // The debug logger, for logging debug information
) (*schema.ImageManifest, error) {
	manifest, err := GenerateEmptyManifest(name)
	if err != nil {
		return nil, err
	}

	labels := manifest.Labels.ToMap()
	annotations := manifest.Annotations

	setLabel(labels, "version", dockerURL.Tag)
	setOSArch(labels, config.OS, config.Architecture)

	setAnnotation(&annotations, "author", config.Author)
	setAnnotation(&annotations, "created", config.Created)

	setAnnotation(&annotations, common.AppcDockerOriginalName, dockerURL.OriginalName)
	setAnnotation(&annotations, common.AppcDockerRegistryURL, dockerURL.IndexURL)
	setAnnotation(&annotations, common.AppcDockerRepository, dockerURL.ImageName)
	setAnnotation(&annotations, common.AppcDockerImageID, imageDigest)
	setAnnotation(&annotations, "created", config.Created)
	setAnnotation(&annotations, common.AppcDockerManifestHash, manhash)

	if config.Config != nil {
		innerCfg := config.Config
		exec := getExecCommand(innerCfg.Entrypoint, innerCfg.Cmd)
		user, group := parseDockerUser(innerCfg.User)
		var env appctypes.Environment
		for _, v := range innerCfg.Env {
			parts := strings.SplitN(v, "=", 2)
                        if len(parts) == 2 {
				env.Set(parts[0], parts[1])
                        }
		}
		manifest.App = &appctypes.App{
			Exec:             exec,
			User:             user,
			Group:            group,
			Environment:      env,
			WorkingDirectory: innerCfg.WorkingDir,
		}
		manifest.App.MountPoints, err = convertVolumesToMPs(innerCfg.Volumes)
		if err != nil {
			return nil, err
		}
		manifest.App.Ports, err = convertPorts(innerCfg.ExposedPorts, nil, debug)
		if err != nil {
			return nil, err
		}

		ep, cmd, err := generateEPCmdAnnotation(innerCfg.Entrypoint, innerCfg.Cmd)
		if err != nil {
			return nil, err
		}
		if len(ep) > 0 {
			setAnnotation(&annotations, common.AppcDockerEntrypoint, ep)
		}
		if len(cmd) > 0 {
			setAnnotation(&annotations, common.AppcDockerCmd, cmd)
		}
	}

	for _, lowerLayer := range lowerLayers {
		manifest.Dependencies = append(manifest.Dependencies, appctypes.Dependency{
			ImageName: lowerLayer.Name,
			Labels:    lowerLayer.Labels,
		})
	}

	manifest.Labels, err = appctypes.LabelsFromMap(labels)
	if err != nil {
		return nil, err
	}
	manifest.Annotations = annotations
	return manifest, nil
}

// ValidateACI checks whether the ACI in aciPath is valid.
func ValidateACI(aciPath string) error {
	aciFile, err := os.Open(aciPath)
	if err != nil {
		return err
	}
	defer aciFile.Close()

	tr, err := aci.NewCompressedTarReader(aciFile)
	if err != nil {
		return err
	}
	defer tr.Close()

	if err := aci.ValidateArchive(tr.Reader); err != nil {
		return err
	}

	return nil
}

type appcPortSorter []appctypes.Port

func (s appcPortSorter) Len() int {
	return len(s)
}

func (s appcPortSorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s appcPortSorter) Less(i, j int) bool {
	return s[i].Name.String() < s[j].Name.String()
}

func convertPorts(dockerExposedPorts map[string]struct{}, dockerPortSpecs []string, debug log.Logger) ([]appctypes.Port, error) {
	ports := []appctypes.Port{}

	for ep := range dockerExposedPorts {
		appcPort, err := parseDockerPort(ep)
		if err != nil {
			return nil, err
		}
		ports = append(ports, *appcPort)
	}

	if dockerExposedPorts == nil && dockerPortSpecs != nil {
		debug.Println("warning: docker image uses deprecated PortSpecs field")
		for _, ep := range dockerPortSpecs {
			appcPort, err := parseDockerPort(ep)
			if err != nil {
				return nil, err
			}
			ports = append(ports, *appcPort)
		}
	}

	sort.Sort(appcPortSorter(ports))

	return ports, nil
}

func parseDockerPort(dockerPort string) (*appctypes.Port, error) {
	var portString string
	proto := "tcp"
	sp := strings.Split(dockerPort, "/")
	if len(sp) < 2 {
		portString = dockerPort
	} else {
		proto = sp[1]
		portString = sp[0]
	}

	port, err := strconv.ParseUint(portString, 10, 0)
	if err != nil {
		return nil, fmt.Errorf("error parsing port %q: %v", portString, err)
	}

	sn, err := appctypes.SanitizeACName(dockerPort)
	if err != nil {
		return nil, err
	}

	appcPort := &appctypes.Port{
		Name:     *appctypes.MustACName(sn),
		Protocol: proto,
		Port:     uint(port),
	}

	return appcPort, nil
}

type appcVolSorter []appctypes.MountPoint

func (s appcVolSorter) Len() int {
	return len(s)
}

func (s appcVolSorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s appcVolSorter) Less(i, j int) bool {
	return s[i].Name.String() < s[j].Name.String()
}

func convertVolumesToMPs(dockerVolumes map[string]struct{}) ([]appctypes.MountPoint, error) {
	mps := []appctypes.MountPoint{}
	dup := make(map[string]int)

	for p := range dockerVolumes {
		n := filepath.Join("volume", p)
		sn, err := appctypes.SanitizeACName(n)
		if err != nil {
			return nil, err
		}

		// check for duplicate names
		if i, ok := dup[sn]; ok {
			dup[sn] = i + 1
			sn = fmt.Sprintf("%s-%d", sn, i)
		} else {
			dup[sn] = 1
		}

		mp := appctypes.MountPoint{
			Name: *appctypes.MustACName(sn),
			Path: p,
		}

		mps = append(mps, mp)
	}

	sort.Sort(appcVolSorter(mps))

	return mps, nil
}

func writeACI(layer io.ReadSeeker, manifest schema.ImageManifest, curPwl []string, output string, compression common.Compression) (*schema.ImageManifest, error) {
	dir, _ := path.Split(output)
	if dir != "" {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return nil, fmt.Errorf("error creating ACI parent dir: %v", err)
		}
	}
	aciFile, err := os.Create(output)
	if err != nil {
		return nil, fmt.Errorf("error creating ACI file: %v", err)
	}
	defer aciFile.Close()

	var w io.WriteCloser = aciFile
	if compression == common.GzipCompression {
		w = gzip.NewWriter(aciFile)
		defer w.Close()
	}
	trw := tar.NewWriter(w)
	defer trw.Close()

	if err := WriteRootfsDir(trw); err != nil {
		return nil, fmt.Errorf("error writing rootfs entry: %v", err)
	}

	fileMap := make(map[string]struct{})
	var whiteouts []string
	convWalker := func(t *tarball.TarFile) error {
		name := t.Name()
		if name == "./" {
			return nil
		}
		t.Header.Name = path.Join("rootfs", name)
		absolutePath := strings.TrimPrefix(t.Header.Name, "rootfs")

		if filepath.Clean(absolutePath) == "/dev" && t.Header.Typeflag != tar.TypeDir {
			return fmt.Errorf(`invalid layer: "/dev" is not a directory`)
		}

		fileMap[absolutePath] = struct{}{}
		if strings.Contains(t.Header.Name, "/.wh.") {
			whiteouts = append(whiteouts, strings.Replace(absolutePath, ".wh.", "", 1))
			return nil
		}
		if t.Header.Typeflag == tar.TypeLink {
			t.Header.Linkname = path.Join("rootfs", t.Linkname())
		}

		if err := trw.WriteHeader(t.Header); err != nil {
			return err
		}
		if _, err := io.Copy(trw, t.TarStream); err != nil {
			return err
		}

		if !util.In(curPwl, absolutePath) {
			curPwl = append(curPwl, absolutePath)
		}

		return nil
	}
	tr, err := aci.NewCompressedTarReader(layer)
	if err == nil {
		defer tr.Close()
		// write files in rootfs/
		if err := tarball.Walk(*tr.Reader, convWalker); err != nil {
			return nil, err
		}
	} else {
		// ignore errors: empty layers in tars generated by docker save are not
		// valid tar files so we ignore errors trying to open them. Converted
		// ACIs will have the manifest and an empty rootfs directory in any
		// case.
	}
	newPwl := subtractWhiteouts(curPwl, whiteouts)

	newPwl, err = writeStdioSymlinks(trw, fileMap, newPwl)
	if err != nil {
		return nil, err
	}
	// Let's copy the newly generated PathWhitelist to avoid unintended
	// side-effects
	manifest.PathWhitelist = make([]string, len(newPwl))
	copy(manifest.PathWhitelist, newPwl)

	if err := WriteManifest(trw, manifest); err != nil {
		return nil, fmt.Errorf("error writing manifest: %v", err)
	}

	return &manifest, nil
}

func getExecCommand(entrypoint []string, cmd []string) appctypes.Exec {
	return append(entrypoint, cmd...)
}

func parseDockerUser(dockerUser string) (string, string) {
	// if the docker user is empty assume root user and group
	if dockerUser == "" {
		return "0", "0"
	}

	dockerUserParts := strings.Split(dockerUser, ":")

	// when only the user is given, the docker spec says that the default and
	// supplementary groups of the user in /etc/passwd should be applied.
	// To avoid inspecting image content, we set gid to the same value as uid.
	if len(dockerUserParts) < 2 {
		return dockerUserParts[0], dockerUserParts[0]
	}

	return dockerUserParts[0], dockerUserParts[1]
}

func subtractWhiteouts(pathWhitelist []string, whiteouts []string) []string {
	matchPaths := []string{}
	for _, path := range pathWhitelist {
		// If one of the parent dirs of the current path matches the
		// whiteout then also this path should be removed
		curPath := path
		for curPath != "/" {
			for _, whiteout := range whiteouts {
				if curPath == whiteout {
					matchPaths = append(matchPaths, path)
				}
			}
			curPath = filepath.Dir(curPath)
		}
	}
	for _, matchPath := range matchPaths {
		idx := util.IndexOf(pathWhitelist, matchPath)
		if idx != -1 {
			pathWhitelist = append(pathWhitelist[:idx], pathWhitelist[idx+1:]...)
		}
	}

	sort.Sort(sort.StringSlice(pathWhitelist))

	return pathWhitelist
}

// WriteManifest writes a schema.ImageManifest entry on a tar.Writer.
func WriteManifest(outputWriter *tar.Writer, manifest schema.ImageManifest) error {
	b, err := canJson.Marshal(manifest)
	if err != nil {
		return err
	}

	hdr := getGenericTarHeader()
	hdr.Name = "manifest"
	hdr.Mode = 0644
	hdr.Size = int64(len(b))
	hdr.Typeflag = tar.TypeReg

	if err := outputWriter.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := outputWriter.Write(b); err != nil {
		return err
	}

	return nil
}

// WriteRootfsDir writes a "rootfs" dir entry on a tar.Writer.
func WriteRootfsDir(tarWriter *tar.Writer) error {
	hdr := getGenericTarHeader()
	hdr.Name = "rootfs"
	hdr.Mode = 0755
	hdr.Size = int64(0)
	hdr.Typeflag = tar.TypeDir

	return tarWriter.WriteHeader(hdr)
}

type symlink struct {
	linkname string
	target   string
}

// writeStdioSymlinks adds the /dev/stdin, /dev/stdout, /dev/stderr, and
// /dev/fd symlinks expected by Docker to the converted ACIs so apps can find
// them as expected
func writeStdioSymlinks(tarWriter *tar.Writer, fileMap map[string]struct{}, pwl []string) ([]string, error) {
	stdioSymlinks := []symlink{
		{"/dev/stdin", "/proc/self/fd/0"},
		// Docker makes /dev/{stdout,stderr} point to /proc/self/fd/{1,2} but
		// we point to /dev/console instead in order to support the case when
		// stdout/stderr is a Unix socket (e.g. for the journal).
		{"/dev/stdout", "/dev/console"},
		{"/dev/stderr", "/dev/console"},
		{"/dev/fd", "/proc/self/fd"},
	}

	for _, s := range stdioSymlinks {
		name := s.linkname
		target := s.target
		if _, exists := fileMap[name]; exists {
			continue
		}
		hdr := &tar.Header{
			Name:     filepath.Join("rootfs", name),
			Mode:     0777,
			Typeflag: tar.TypeSymlink,
			Linkname: target,
		}
		if err := tarWriter.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if !util.In(pwl, name) {
			pwl = append(pwl, name)
		}
	}

	return pwl, nil
}

func getGenericTarHeader() *tar.Header {
	// FIXME(iaguis) Use docker image time instead of the Unix Epoch?
	hdr := &tar.Header{
		Uid:        0,
		Gid:        0,
		ModTime:    time.Unix(0, 0),
		Uname:      "0",
		Gname:      "0",
		ChangeTime: time.Unix(0, 0),
	}

	return hdr
}
