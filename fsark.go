package main

import (
	"archive/tar"
	"encoding/json"
	_ "embed"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type Wrapper struct {
	ImageName string `json:"image"`
	MountsList []string `json:"mounts"`
	Command string `json:"command"`
}

type Image struct {
	ImageRootFSPath string `json:"rootfs"`
	Tags []string `json:"tags,omitempty"`
}

type Config struct {
	Images map[string]Image `json:"images"`
	Commands map[string]Wrapper `json:"commands"`
}

const configPath = "/var/ark/config.json"

func unpackImage(imgPath string, rootfsPath string) error {
	file, err := os.Open(imgPath)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	tarReader := tar.NewReader(file)

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

		targetPath := filepath.Join(rootfsPath, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(targetPath); err != nil {
				if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
					return fmt.Errorf("failed to create dir %v: %w", targetPath, err)
				}
			}

		case tar.TypeReg:
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %v: %w", targetPath, err)
			}
			if _, err := io.Copy(f, tarReader); err != nil {
				return fmt.Errorf("failed to copy data for %v: %w", targetPath, err)
			}
			f.Close()

		case tar.TypeSymlink:
			err = os.Symlink(header.Linkname, targetPath)
			if err != nil {
				return fmt.Errorf("failed to create symlink %v %v: %w", header.Linkname, targetPath, err)
			}

		case tar.TypeLink:
			sourcePath := filepath.Join(rootfsPath, header.Linkname)
			err = os.Link(sourcePath, targetPath)
			if err != nil {
				return fmt.Errorf("failed to create link %v %v: %w", header.Linkname, targetPath, err)
			}

		default:
			fmt.Printf("Skipping %v of type %v\n", targetPath, header.Typeflag)
		}
	}

	return nil
}

func (c Image) buildContainerInDir(path string, args []string, cwd string, mountsList []string) error {

	destRootFSPath := filepath.Join(path, "rootfs")

	uid := os.Getuid()
	gid := os.Getgid()

	mounts := make([]BindMount, 1 + len(mountsList))
	mounts[0] = BindMount{
		Source: cwd,
		Destination: "/ark",
	}
	for index, path := range mountsList {
		mounts[index + 1] = BindMount{
			Source: path,
			Destination: path,
		}
	}

	env := []string {
		fmt.Sprintf("USER=%s", os.Getenv("USER")),
	}

	spec := CreateRootlessSpec(
		args,
		env,
		"/ark",
		destRootFSPath,
		mounts,
		uid,
		gid,
	)

	configPath := filepath.Join(path, "config.json")

	content, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to encode json spec: %w", err)
	}
	err = ioutil.WriteFile(configPath, content, 0644)
	if err != nil {
		return fmt.Errorf("failed to write spec file: %w", err)
	}

	err = os.MkdirAll(destRootFSPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create rootfs directory: %w", err)
	}
	err = unpackImage(c.ImageRootFSPath, destRootFSPath)
	if err != nil {
		return fmt.Errorf("failed to clone rootfs: %w", err)
	}

	return nil
}

func main() {
	// If you os.Exit immediately, defers don't happen :(
	retcode := 0
	defer func(code *int){
		os.Exit(*code)
	}(&retcode)

	runcPath, err := exec.LookPath("runc")
	if err != nil {
		retcode = 1
		log.Printf("Failed to find runc on path")
		return
	}

	configFile, err := os.Open(configPath)
	if err != nil {
		retcode = 1
		log.Printf("Failed to open %v: %v", configPath, err)
		return
	}

	var conf Config
	err = json.NewDecoder(configFile).Decode(&conf)
	if err != nil {
		retcode = 1
		log.Printf("Failed to parse config: %v", err)
		return
	}

	// Find the matching name
	_, exeName := filepath.Split(os.Args[0])
	commandConfig, ok := conf.Commands[exeName]
	if !ok {
		retcode = 1
		log.Printf("Configuration has no match for command %v, only:\n", exeName)
		for key, _ := range conf.Commands {
			log.Printf("\t* %v\n", key)
		}
		return
	}

	imageConfig, ok := conf.Images[commandConfig.ImageName]
	if !ok {
		retcode = 1
		log.Printf("Configuration has no match for image %v, only:\n", commandConfig.ImageName)
		for key, _ := range conf.Images {
			log.Printf("\t* %v\n", key)
		}
		return
	}

	dir, err := ioutil.TempDir("", "container-*")
	if err != nil {
		retcode = 1
		log.Printf("Failed to create temporary directory: %v", err)
		return
	}
	defer os.RemoveAll(dir)

	args := append([]string{commandConfig.Command}, os.Args[1:]...)

	cwd, err := os.Getwd()
	if err != nil {
		retcode = 1
		log.Printf("Failed to get current directory: %v", err)
		return
	}
	err = imageConfig.buildContainerInDir(dir, args, cwd, commandConfig.MountsList)
	if err != nil {
		retcode = 1
		log.Printf("Failed to create container: %v", err)
		return
	}

	_, id := filepath.Split(dir)
	fullarglist := []string{runcPath, "run", "-b", dir, id}

	cmd := &exec.Cmd{
		Path:   runcPath,
		Args:   fullarglist,
		Stderr: os.Stderr,
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		retcode = 1
		log.Printf("Failed to get input pipe: %v", err)
		return
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		retcode = 1
		log.Printf("Failed to get output pipe: %v", err)
		return
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		retcode = 1
		log.Printf("Failed to run runc: %v", err)
		return
	}

	// Read from child, echo locally
    var wgout sync.WaitGroup
	wgout.Add(1)
	go func() {
		buffer := make([]byte, 1024)
		for {
			count, err := stdout.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading from process: %v", err)
				}
				break
			}
			if count > 0 {
				towrite := buffer[:count]
				written_count, err := os.Stdout.Write(towrite)
				if err != nil {
					log.Printf("Failed to write to stdout: %v", err)
					break
				}
				if written_count != count {
					log.Fatalf("Michael should write better code: %v written, %v in buffer", written_count, count)
				}
			}
		}
		wgout.Done()
	}()

	// Read from stdin, write to child. We don't have a wait group for this,
	// just currently letting main exit and this will be reaped.
	go func() {
		buffer := make([]byte, 1024)
		for {
			count, err := os.Stdin.Read(buffer)
			if err != nil {
				log.Printf("Failed to read from stdin: %v", err)
				break
			}
			if count > 0 {
				towrite := buffer[0:count]
				written_count, err := stdin.Write(towrite)
				if err != nil {
					if err != io.EOF {
						log.Printf("Error writing to process: %v", err)
					}
					break
				}
				if written_count != count {
					log.Fatalf("Michael should write better code: %v written, %v in buffer", written_count, count)
				}
			}
		}
	}()

	// wait for things to stop
    wgout.Wait()
	err = cmd.Wait()
	if err != nil {
		if proc_error, ok := err.(*exec.ExitError); ok {
			// if the child exited with an error, just pass it on
			retcode = proc_error.ExitCode()
			return
		} else {
			retcode = 1
			log.Printf("Failed to wait: %v", err)
			return
		}
	}
}
