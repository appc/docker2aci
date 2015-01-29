package docker2aci

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type Compression int

const (
	Uncompressed Compression = iota
	Gzip
)

func makeEndpointsList(headers []string) []string {
	var endpoints []string

	for _, ep := range headers {
		endpointsList := strings.Split(ep, ",")
		for _, endpointEl := range endpointsList {
			endpoints = append(
				endpoints,
				// TODO(iaguis) discover if httpsOrHTTP
				path.Join(strings.TrimSpace(endpointEl), "v1"))
		}
	}

	return endpoints
}

func decompress(layer io.Reader) (io.Reader, error) {
	bufR := bufio.NewReader(layer)
	bs, _ := bufR.Peek(10)

	compression := detectCompression(bs)
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

func detectCompression(source []byte) Compression {
	for compression, m := range map[Compression][]byte{
		Gzip: {0x1F, 0x8B, 0x08},
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
