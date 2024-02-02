package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type imageManifestItem struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

type configurationData struct {
	Hostname         string            `json:"Hostname"`
	DomainName       string            `json:"Domainname"`
	User             string            `json:"User"`
	Environment      []string          `json:"Env"`
	Command          []string          `json:"Cmd"`
	WorkingDirectory string            `json:"WorkingDir"`
	Labels           map[string]string `json:"Labels"`
}

type configurationTopLevel struct {
	Architecture           string            `json:"architecture"`
	Configuration          configurationData `json:"config"`
	Container              string            `json:"container"`
	ContainerConfiguration configurationData `json:"container_config"`
	Created                time.Time         `json:"created"`
	DockerVersion          string            `json:"docker_version"`
}

func (imi imageManifestItem) Digest() string {
	// The config file in an image contains the hash, but in some cases it
	// has a .json extension, and in some cases it has a "hashtype:" prefix, so
	// we need to strip those out.
	basename := path.Base(imi.Config)
	extension := path.Ext(basename)
	body := strings.TrimSuffix(basename, extension)
	parts := strings.Split(body, ":")
	switch len(parts) {
	case 1:
		return body
	case 2:
		return parts[1]
	default:
		return imi.Config
	}
}

func loadFileFromContainer(tarballPath string, filepath string, data interface{}) error {
	file, err := os.Open(tarballPath)
	if err != nil {
		return fmt.Errorf("failed to open image for config: %w", err)
	}
	defer file.Close()

	tarReader := tar.NewReader(file)
	for {
		header, err := tarReader.Next()
		switch {
		case err == io.EOF:
			return err
		case err != nil:
			return fmt.Errorf("error reading next header: %w", err)
		case header == nil:
			continue
		}

		if header.Name != filepath {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("expected manifest to be a regular file, but is %v", header.Typeflag)
		}

		return json.NewDecoder(tarReader).Decode(&data)
	}
}

func unpackRootFS(tarballPath string, rootfsPath string) error {
	imageManifest, err := loadImageManifest(tarballPath)
	if err != nil {
		if err != io.EOF {
			return err
		}
		// if the error was io.EOF, we just didn't find the manifest, so
		// assume we have a container image
		return unpackContainer(tarballPath, rootfsPath)
	}

	// if we got here we have a docker image, so unpack that
	return unpackImage(tarballPath, rootfsPath, imageManifest.Layers)
}

func getContainerConfiguration(tarballPath string) (configurationTopLevel, error) {
	imageManifest, err := loadImageManifest(tarballPath)
	if err != nil {
		return configurationTopLevel{}, err
	}

	// after unpacking, try get the configuration for the image
	var config configurationTopLevel
	err = loadFileFromContainer(tarballPath, imageManifest.Config, &config)
	return config, err
}

func loadImageManifest(tarballPath string) (imageManifestItem, error) {
	var manifest []imageManifestItem
	err := loadFileFromContainer(tarballPath, "manifest.json", &manifest)
	if err != nil {
		return imageManifestItem{}, err
	}
	if len(manifest) != 1 {
		return imageManifestItem{}, fmt.Errorf("expected one item in manifest, got %d", len(manifest))
	}
	return manifest[0], nil
}

func expandTar(tarReader *tar.Reader, rootfsPath string, overlay bool) error {
	for {
		header, err := tarReader.Next()
		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return fmt.Errorf("error reading next header: %w", err)
		case header == nil:
			continue
		}

		potentialTargetPath := filepath.Join(rootfsPath, header.Name)
		cleanTargetPath := path.Clean(potentialTargetPath)
		cleanRoot := path.Clean(rootfsPath)
		if !strings.HasPrefix(cleanTargetPath, cleanRoot) {
			return fmt.Errorf("suspicious path in tar that escapes target directory: %v", header.Name)
		}
		targetPath := cleanTargetPath

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(targetPath); err != nil {
				if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
					return fmt.Errorf("failed to create dir %v: %w", targetPath, err)
				}
			}

		case tar.TypeReg:
			basename := path.Base(header.Name)
			if overlay && basename == ".wh..wh..opq" {
				victimDirectory := path.Dir(targetPath)
				if !strings.HasPrefix(victimDirectory, cleanRoot) {
					return fmt.Errorf("attempt to remove children not in root: %v", header.Name)
				}
				victimFiles, err := os.ReadDir(victimDirectory)
				if err != nil {
					return fmt.Errorf("failed to index childern for overlay removal %v: %w", header.Name, err)
				}
				for _, victim := range victimFiles {
					victimName := victim.Name()
					if (victimName == ".") || (victimName == "..") {
						continue
					}
					victimPath := path.Clean(path.Join(victimDirectory, victimName))
					if !strings.HasPrefix(victimPath, cleanRoot) {
						return fmt.Errorf("attempt to remove file not in root: %v", header.Name)
					}
					err = os.RemoveAll(victimPath)
					if err != nil {
						return fmt.Errorf("attempt to remove files failed %v: %w", victimName, err)
					}
				}

			} else if overlay && strings.HasPrefix(basename, ".wh.") {
				victimBasename := strings.TrimPrefix(basename, ".wh.")
				victimDirectory := path.Dir(targetPath)
				victimPath := path.Clean(path.Join(victimDirectory, victimBasename))
				if !strings.HasPrefix(victimPath, cleanRoot) {
					return fmt.Errorf("attempt to remove file not in root: %v", header.Name)
				}
				err = os.RemoveAll(victimPath)
				if err != nil {
					return fmt.Errorf("failed to remove overlay file %v: %w", header.Name, err)
				}
			} else {
				f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
				if err != nil {
					return fmt.Errorf("failed to create file %v: %w", targetPath, err)
				}
				if _, err := io.Copy(f, tarReader); err != nil {
					return fmt.Errorf("failed to copy data for %v: %w", targetPath, err)
				}
				f.Close()
			}

		case tar.TypeSymlink:
			if overlay {
				err = os.Remove(targetPath)
				if (err != nil) && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove file for symlink %v: %w", targetPath, err)
				}
			}
			err = os.Symlink(header.Linkname, targetPath)
			if err != nil {
				return fmt.Errorf("failed to create symlink %v %v (overlay=%t): %w", header.Linkname, targetPath, overlay, err)
			}

		case tar.TypeLink:
			if overlay {
				err = os.Remove(targetPath)
				if (err != nil) && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove file for symlink %v: %w", targetPath, err)
				}
			}
			sourcePath := filepath.Join(rootfsPath, header.Linkname)
			err = os.Link(sourcePath, targetPath)
			if err != nil {
				return fmt.Errorf("failed to create link %v %v: %w", header.Linkname, targetPath, err)
			}

		default:
			fmt.Printf("Skipping %v of type %v\n", targetPath, header.Typeflag)
		}
	}
}

func unpackContainer(imgPath string, rootfsPath string) error {
	file, err := os.Open(imgPath)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	tarReader := tar.NewReader(file)
	return expandTar(tarReader, rootfsPath, false)
}

func unpackImage(imgPath string, rootfsPath string, layers []string) error {
	file, err := os.Open(imgPath)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	// In my limited sample set it looks like the layers appear in order
	// in the tarball, but that's not something I'd want to bet my life on always being
	// the case, so this is thus rather pedantic
	for _, layer := range layers {
		offset, err := file.Seek(0, 0)
		if err != nil {
			return err
		}
		if offset != 0 {
			return fmt.Errorf("Attemtped to move file reader to start, ended up at %d", offset)
		}
		tarReader := tar.NewReader(file)

		for {
			header, err := tarReader.Next()
			switch {
			case err == io.EOF:
				return fmt.Errorf("failed to find layer %v in image", layer)
			case err != nil:
				return fmt.Errorf("error reading next header: %w", err)
			case header == nil:
				continue
			}

			if header.Name != layer {
				continue
			}
			if header.Typeflag != tar.TypeReg {
				return fmt.Errorf("expected layer %v to be a regular file, but is %v", layer, header.Typeflag)
			}

			extension := path.Ext(layer)
			var layerTarReader *tar.Reader
			if extension == ".gz" {
				archiveReader, err := gzip.NewReader(tarReader)
				if err != nil {
					return fmt.Errorf("failed to read achive of layer %v: %w", layer, err)
				}
				layerTarReader = tar.NewReader(archiveReader)
			} else {
				layerTarReader = tar.NewReader(tarReader)
			}
			err = expandTar(layerTarReader, rootfsPath, true)
			if err != nil {
				return fmt.Errorf("failed to expand layer %v: %w", layer, err)
			}
			break
		}
	}
	return nil
}
