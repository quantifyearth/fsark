package main

import (
	"fmt"
	"os"
	"path"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func getImagePathForName(imageName string) (string, error) {
	_, err := os.Stat(imageName)
	if err == nil {
		return imageName, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("problem accessing image %s: %w", imageName, err)
	}

	containerCachePath, ok := os.LookupEnv("SHARK_CONTAINER_CACHE")
	if !ok {
		containerCachePath = path.Join(os.Getenv("HOME"), ".shark")
	}
	err = os.MkdirAll(containerCachePath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("could not find container cache %v: %w", containerCachePath, err)
	}

	ref, err := name.ParseReference(imageName, name.Insecure)
	if err != nil {
		return "", err
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		panic(err)
	}

	hash, err := img.Digest()
	if err != nil {
		panic(err)
	}

	imageMap := map[string]v1.Image{}
	imageMap[imageName] = img

	path := path.Join(containerCachePath, fmt.Sprintf("%s.tar", hash.Hex))

	_, err = os.Stat(path)
	if err == nil {
		return path, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	err = crane.MultiSave(imageMap, path)
	if err != nil {
		panic(fmt.Errorf("saving tarball %s: %w", path, err))
	}

	return path, err
}
