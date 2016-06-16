// Package image consolidates knowledge about various container image formats
// (as opposed to image storage mechanisms, which are handled by types.ImageSource)
// and exposes all of them using an unified interface.
package image

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
	"time"

	"github.com/projectatomic/skopeo/directory"
	"github.com/projectatomic/skopeo/docker/utils"
	"github.com/projectatomic/skopeo/types"
)

var (
	validHex = regexp.MustCompile(`^([a-f0-9]{64})$`)
)

// genericImage is a general set of utilities for working with container images,
// whatever is their underlying location (i.e. dockerImageSource-independent).
// Note the existence of skopeo/docker.Image: some instances of a `types.Image`
// may not be a `genericImage` directly. However, most users of `types.Image`
// do not care, and those who care about `skopeo/docker.Image` know they do.
type genericImage struct {
	src              types.ImageSource
	cachedManifest   []byte   // Private cache for Manifest(); nil if not yet known.
	cachedSignatures [][]byte // Private cache for Signatures(); nil if not yet known.
}

// FromSource returns a types.Image implementation for source.
func FromSource(src types.ImageSource) types.Image {
	return &genericImage{src: src}
}

// IntendedDockerReference returns the full, unambiguous, Docker reference for this image, _as specified by the user_
// (not as the image itself, or its underlying storage, claims).  This can be used e.g. to determine which public keys are trusted for this image.
// May be "" if unknown.
func (i *genericImage) IntendedDockerReference() string {
	return i.src.IntendedDockerReference()
}

// Manifest is like ImageSource.GetManifest, but the result is cached; it is OK to call this however often you need.
func (i *genericImage) Manifest() ([]byte, error) {
	if i.cachedManifest == nil {
		m, _, err := i.src.GetManifest([]string{utils.DockerV2Schema1MIMEType})
		if err != nil {
			return nil, err
		}
		i.cachedManifest = m
	}
	return i.cachedManifest, nil
}

// Signatures is like ImageSource.GetSignatures, but the result is cached; it is OK to call this however often you need.
func (i *genericImage) Signatures() ([][]byte, error) {
	if i.cachedSignatures == nil {
		sigs, err := i.src.GetSignatures()
		if err != nil {
			return nil, err
		}
		i.cachedSignatures = sigs
	}
	return i.cachedSignatures, nil
}

func (i *genericImage) Inspect() (*types.ImageInspectInfo, error) {
	// TODO(runcom): unused version param for now, default to docker v2-1
	m, err := i.getSchema1Manifest()
	if err != nil {
		return nil, err
	}
	ms1, ok := m.(*manifestSchema1)
	if !ok {
		return nil, fmt.Errorf("error retrivieng manifest schema1")
	}
	v1 := &v1Image{}
	if err := json.Unmarshal([]byte(ms1.History[0].V1Compatibility), v1); err != nil {
		return nil, err
	}
	return &types.ImageInspectInfo{
		Tag:           ms1.Tag,
		DockerVersion: v1.DockerVersion,
		Created:       v1.Created,
		Labels:        v1.Config.Labels,
		Architecture:  v1.Architecture,
		Os:            v1.OS,
		Layers:        ms1.GetLayers(),
	}, nil
}

type config struct {
	Labels map[string]string
}

type v1Image struct {
	// Config is the configuration of the container received from the client
	Config *config `json:"config,omitempty"`
	// DockerVersion specifies version on which image is built
	DockerVersion string `json:"docker_version,omitempty"`
	// Created timestamp when image was created
	Created time.Time `json:"created"`
	// Architecture is the hardware that the image is build and runs on
	Architecture string `json:"architecture,omitempty"`
	// OS is the operating system used to build and run the image
	OS string `json:"os,omitempty"`
}

// TODO(runcom)
func (i *genericImage) DockerTar() ([]byte, error) {
	return nil, nil
}

// will support v1 one day...
type manifest interface {
	String() string
	GetLayers() []string
}

type manifestSchema1 struct {
	Name     string
	Tag      string
	FSLayers []struct {
		BlobSum string `json:"blobSum"`
	} `json:"fsLayers"`
	History []struct {
		V1Compatibility string `json:"v1Compatibility"`
	} `json:"history"`
	// TODO(runcom) verify the downloaded manifest
	//Signature []byte `json:"signature"`
}

func (m *manifestSchema1) GetLayers() []string {
	layers := make([]string, len(m.FSLayers))
	for i, layer := range m.FSLayers {
		layers[i] = layer.BlobSum
	}
	return layers
}

func (m *manifestSchema1) String() string {
	return fmt.Sprintf("%s-%s", sanitize(m.Name), sanitize(m.Tag))
}

func sanitize(s string) string {
	return strings.Replace(s, "/", "-", -1)
}

func (i *genericImage) getSchema1Manifest() (manifest, error) {
	manblob, err := i.Manifest()
	if err != nil {
		return nil, err
	}
	mschema1 := &manifestSchema1{}
	if err := json.Unmarshal(manblob, mschema1); err != nil {
		return nil, err
	}
	if err := fixManifestLayers(mschema1); err != nil {
		return nil, err
	}
	// TODO(runcom): verify manifest schema 1, 2 etc
	//if len(m.FSLayers) != len(m.History) {
	//return nil, fmt.Errorf("length of history not equal to number of layers for %q", ref.String())
	//}
	//if len(m.FSLayers) == 0 {
	//return nil, fmt.Errorf("no FSLayers in manifest for %q", ref.String())
	//}
	return mschema1, nil
}

func (i *genericImage) Layers(layers ...string) error {
	m, err := i.getSchema1Manifest()
	if err != nil {
		return err
	}
	tmpDir, err := ioutil.TempDir(".", "layers-"+m.String()+"-")
	if err != nil {
		return err
	}
	dest := directory.NewDirImageDestination(tmpDir)
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := dest.PutManifest(data); err != nil {
		return err
	}
	if len(layers) == 0 {
		layers = m.GetLayers()
	}
	for _, l := range layers {
		if !strings.HasPrefix(l, "sha256:") {
			l = "sha256:" + l
		}
		if err := i.getLayer(dest, l); err != nil {
			return err
		}
	}
	return nil
}

func (i *genericImage) getLayer(dest types.ImageDestination, digest string) error {
	stream, _, err := i.src.GetBlob(digest)
	if err != nil {
		return err
	}
	defer stream.Close()
	return dest.PutBlob(digest, stream)
}

// fixManifestLayers, after validating the supplied manifest
// (to use correctly-formatted IDs, and to not have non-consecutive ID collisions in manifest.History),
// modifies manifest to only have one entry for each layer ID in manifest.History (deleting the older duplicates,
// both from manifest.History and manifest.FSLayers).
// Note that even after this succeeds, manifest.FSLayers may contain duplicate entries
// (for Dockerfile operations which change the configuration but not the filesystem).
func fixManifestLayers(manifest *manifestSchema1) error {
	type imageV1 struct {
		ID     string
		Parent string
	}
	// Per the specification, we can assume that len(manifest.FSLayers) == len(manifest.History)
	imgs := make([]*imageV1, len(manifest.FSLayers))
	for i := range manifest.FSLayers {
		img := &imageV1{}

		if err := json.Unmarshal([]byte(manifest.History[i].V1Compatibility), img); err != nil {
			return err
		}

		imgs[i] = img
		if err := validateV1ID(img.ID); err != nil {
			return err
		}
	}
	if imgs[len(imgs)-1].Parent != "" {
		return errors.New("Invalid parent ID in the base layer of the image.")
	}
	// check general duplicates to error instead of a deadlock
	idmap := make(map[string]struct{})
	var lastID string
	for _, img := range imgs {
		// skip IDs that appear after each other, we handle those later
		if _, exists := idmap[img.ID]; img.ID != lastID && exists {
			return fmt.Errorf("ID %+v appears multiple times in manifest", img.ID)
		}
		lastID = img.ID
		idmap[lastID] = struct{}{}
	}
	// backwards loop so that we keep the remaining indexes after removing items
	for i := len(imgs) - 2; i >= 0; i-- {
		if imgs[i].ID == imgs[i+1].ID { // repeated ID. remove and continue
			manifest.FSLayers = append(manifest.FSLayers[:i], manifest.FSLayers[i+1:]...)
			manifest.History = append(manifest.History[:i], manifest.History[i+1:]...)
		} else if imgs[i].Parent != imgs[i+1].ID {
			return fmt.Errorf("Invalid parent ID. Expected %v, got %v.", imgs[i+1].ID, imgs[i].Parent)
		}
	}
	return nil
}

func validateV1ID(id string) error {
	if ok := validHex.MatchString(id); !ok {
		return fmt.Errorf("image ID %q is invalid", id)
	}
	return nil
}
