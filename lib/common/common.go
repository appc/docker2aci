package common

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/appc/docker2aci/lib/types"
	"github.com/appc/docker2aci/lib/util"
	"github.com/appc/docker2aci/tarball"
	"github.com/appc/spec/aci"
	"github.com/appc/spec/schema"
	appctypes "github.com/appc/spec/schema/types"
)

const (
	defaultTag                = "latest"
	schemaVersion             = "0.7.0"
	appcDockerV1RegistryURL   = "appc.io/docker/v1/registryurl"
	appcDockerV1Repository    = "appc.io/docker/v1/repository"
	appcDockerV1Tag           = "appc.io/docker/v1/tag"
	appcDockerV1ImageID       = "appc.io/docker/v1/imageid"
	appcDockerV1ParentImageID = "appc.io/docker/v1/parentimageid"
)

func ParseDockerURL(arg string) *types.ParsedDockerURL {
	if arg == "" {
		return nil
	}

	taglessRemote, tag := parseRepositoryTag(arg)
	if tag == "" {
		tag = defaultTag
	}
	indexURL, imageName := SplitReposName(taglessRemote)

	return &types.ParsedDockerURL{
		IndexURL:  indexURL,
		ImageName: imageName,
		Tag:       tag,
	}
}

func GenerateACI(layerData types.DockerImageData, dockerURL *types.ParsedDockerURL, outputDir string, layerFile *os.File, curPwl []string, compress bool) (string, *schema.ImageManifest, error) {
	manifest, err := GenerateManifest(layerData, dockerURL)
	if err != nil {
		return "", nil, fmt.Errorf("error generating the manifest: %v", err)
	}

	imageName := strings.Replace(dockerURL.ImageName, "/", "-", -1)
	aciPath := imageName + "-" + layerData.ID
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
	manifest, err = writeACI(layerFile, *manifest, curPwl, aciPath, compress)
	if err != nil {
		return "", nil, fmt.Errorf("error writing ACI: %v", err)
	}

	if err := ValidateACI(aciPath); err != nil {
		return "", nil, fmt.Errorf("invalid ACI generated: %v", err)
	}

	return aciPath, manifest, nil
}

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

func GenerateManifest(layerData types.DockerImageData, dockerURL *types.ParsedDockerURL) (*schema.ImageManifest, error) {
	dockerConfig := layerData.Config
	genManifest := &schema.ImageManifest{}

	appURL := ""
	// omit docker hub index URL in app name
	if dockerURL.IndexURL != defaultIndex {
		appURL = dockerURL.IndexURL + "/"
	}
	appURL += dockerURL.ImageName + "-" + layerData.ID
	appURL, err := appctypes.SanitizeACIdentifier(appURL)
	if err != nil {
		return nil, err
	}
	name := appctypes.MustACIdentifier(appURL)
	genManifest.Name = *name

	acVersion, err := appctypes.NewSemVer(schemaVersion)
	if err != nil {
		panic("invalid appc spec version")
	}
	genManifest.ACVersion = *acVersion

	genManifest.ACKind = appctypes.ACKind(schema.ImageManifestKind)

	var (
		labels       appctypes.Labels
		parentLabels appctypes.Labels
		annotations  appctypes.Annotations
	)

	layer := appctypes.MustACIdentifier("layer")
	labels = append(labels, appctypes.Label{Name: *layer, Value: layerData.ID})

	tag := dockerURL.Tag
	version := appctypes.MustACIdentifier("version")
	labels = append(labels, appctypes.Label{Name: *version, Value: tag})

	if layerData.OS != "" {
		os := appctypes.MustACIdentifier("os")
		labels = append(labels, appctypes.Label{Name: *os, Value: layerData.OS})
		parentLabels = append(parentLabels, appctypes.Label{Name: *os, Value: layerData.OS})

		if layerData.Architecture != "" {
			arch := appctypes.MustACIdentifier("arch")
			labels = append(labels, appctypes.Label{Name: *arch, Value: layerData.Architecture})
			parentLabels = append(parentLabels, appctypes.Label{Name: *arch, Value: layerData.Architecture})
		}
	}

	if layerData.Author != "" {
		authorsKey := appctypes.MustACIdentifier("authors")
		annotations = append(annotations, appctypes.Annotation{Name: *authorsKey, Value: layerData.Author})
	}
	epoch := time.Unix(0, 0)
	if !layerData.Created.Equal(epoch) {
		createdKey := appctypes.MustACIdentifier("created")
		annotations = append(annotations, appctypes.Annotation{Name: *createdKey, Value: layerData.Created.Format(time.RFC3339)})
	}
	if layerData.Comment != "" {
		commentKey := appctypes.MustACIdentifier("docker-comment")
		annotations = append(annotations, appctypes.Annotation{Name: *commentKey, Value: layerData.Comment})
	}

	annotations = append(annotations, appctypes.Annotation{Name: *appctypes.MustACIdentifier(appcDockerV1RegistryURL), Value: dockerURL.IndexURL})
	annotations = append(annotations, appctypes.Annotation{Name: *appctypes.MustACIdentifier(appcDockerV1Repository), Value: dockerURL.ImageName})
	annotations = append(annotations, appctypes.Annotation{Name: *appctypes.MustACIdentifier(appcDockerV1ImageID), Value: layerData.ID})
	annotations = append(annotations, appctypes.Annotation{Name: *appctypes.MustACIdentifier(appcDockerV1ParentImageID), Value: layerData.Parent})

	genManifest.Labels = labels
	genManifest.Annotations = annotations

	if dockerConfig != nil {
		exec := getExecCommand(dockerConfig.Entrypoint, dockerConfig.Cmd)
		if exec != nil {
			user, group := parseDockerUser(dockerConfig.User)
			var env appctypes.Environment
			for _, v := range dockerConfig.Env {
				parts := strings.SplitN(v, "=", 2)
				env.Set(parts[0], parts[1])
			}
			app := &appctypes.App{
				Exec:             exec,
				User:             user,
				Group:            group,
				Environment:      env,
				WorkingDirectory: dockerConfig.WorkingDir,
			}

			app.MountPoints, err = convertVolumesToMPs(dockerConfig.Volumes)
			if err != nil {
				return nil, err
			}

			app.Ports, err = convertPorts(dockerConfig.ExposedPorts, dockerConfig.PortSpecs)
			if err != nil {
				return nil, err
			}

			genManifest.App = app
		}
	}

	if layerData.Parent != "" {
		indexPrefix := ""
		// omit docker hub index URL in app name
		if dockerURL.IndexURL != defaultIndex {
			indexPrefix = dockerURL.IndexURL + "/"
		}
		parentImageNameString := indexPrefix + dockerURL.ImageName + "-" + layerData.Parent
		parentImageNameString, err := appctypes.SanitizeACIdentifier(parentImageNameString)
		if err != nil {
			return nil, err
		}
		parentImageName := appctypes.MustACIdentifier(parentImageNameString)

		genManifest.Dependencies = append(genManifest.Dependencies, appctypes.Dependency{ImageName: *parentImageName, Labels: parentLabels})

		annotations = append(annotations, appctypes.Annotation{Name: *appctypes.MustACIdentifier(appcDockerV1Tag), Value: dockerURL.Tag})
	}

	return genManifest, nil
}

func convertPorts(dockerExposedPorts map[string]struct{}, dockerPortSpecs []string) ([]appctypes.Port, error) {
	ports := []appctypes.Port{}

	for ep := range dockerExposedPorts {
		appcPort, err := parseDockerPort(ep)
		if err != nil {
			return nil, err
		}
		ports = append(ports, *appcPort)
	}

	if dockerExposedPorts == nil && dockerPortSpecs != nil {
		util.Debug("warning: docker image uses deprecated PortSpecs field")
		for _, ep := range dockerPortSpecs {
			appcPort, err := parseDockerPort(ep)
			if err != nil {
				return nil, err
			}
			ports = append(ports, *appcPort)
		}
	}

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

	return mps, nil
}

func getIdFromName(layer, target string) string {
	h := sha512.New()
	h.Write([]byte(layer))
	h.Write([]byte{0})
	h.Write([]byte(target))
	h.Write([]byte{0})

	return fmt.Sprintf(".hidden.docker2aci.sha512-%x", h.Sum(nil))
}

func writeACI(layer io.ReadSeeker, manifest schema.ImageManifest, curPwl []string, output string, compress bool) (*schema.ImageManifest, error) {
	aciFile, err := os.Create(output)
	if err != nil {
		return nil, fmt.Errorf("error creating ACI file: %v", err)
	}
	defer aciFile.Close()

	var w io.WriteCloser = aciFile
	if compress {
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
			hash := getIdFromName(manifest.Name.String(), t.Linkname())
			t.Header.Linkname = path.Join("rootfs", hash)
		} else if t.Header.Typeflag == tar.TypeReg {
			var newHeader tar.Header = *t.Header
			linkname := path.Join("rootfs", getIdFromName(manifest.Name.String(), name))
			newHeader.Name = linkname
			if err := trw.WriteHeader(&newHeader); err != nil {
				return err
			}
			if _, err := io.Copy(trw, t.TarStream); err != nil {
				return err
			}
			t.Header.Typeflag = tar.TypeLink
			t.Header.Linkname = linkname
			t.Header.Size = 0
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

	manifest.PathWhitelist, err = writeStdioSymlinks(trw, fileMap, newPwl)
	if err != nil {
		return nil, err
	}

	if err := WriteManifest(trw, manifest); err != nil {
		return nil, fmt.Errorf("error writing manifest: %v", err)
	}

	return &manifest, nil
}

func getExecCommand(entrypoint []string, cmd []string) appctypes.Exec {
	var command []string
	if entrypoint == nil && cmd == nil {
		return nil
	}
	command = append(entrypoint, cmd...)
	// non-absolute paths are not allowed, fallback to "/bin/sh -c command"
	if len(command) > 0 && !filepath.IsAbs(command[0]) {
		command_prefix := []string{"/bin/sh", "-c"}
		quoted_command := util.Quote(command)
		command = append(command_prefix, strings.Join(quoted_command, " "))
	}
	return command
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

	return pathWhitelist
}

func WriteManifest(outputWriter *tar.Writer, manifest schema.ImageManifest) error {
	b, err := json.Marshal(manifest)
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

func WriteRootfsDir(tarWriter *tar.Writer) error {
	hdr := getGenericTarHeader()
	hdr.Name = "rootfs"
	hdr.Mode = 0755
	hdr.Size = int64(0)
	hdr.Typeflag = tar.TypeDir

	return tarWriter.WriteHeader(hdr)
}

// writeStdioSymlinks adds the /dev/stdin, /dev/stdout, /dev/stderr, and
// /dev/fd symlinks expected by Docker to the converted ACIs so apps can find
// them as expected
func writeStdioSymlinks(tarWriter *tar.Writer, fileMap map[string]struct{}, pwl []string) ([]string, error) {
	stdioSymlinks := map[string]string{
		"/dev/stdin": "/proc/self/fd/0",
		// Docker makes /dev/{stdout,stderr} point to /proc/self/fd/{1,2} but
		// we point to /dev/console instead in order to support the case when
		// stdout/stderr is a Unix socket (e.g. for the journal).
		"/dev/stdout": "/dev/console",
		"/dev/stderr": "/dev/console",
		"/dev/fd":     "/proc/self/fd",
	}

	for name, target := range stdioSymlinks {
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
