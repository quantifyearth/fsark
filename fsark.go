package main

import (
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

	"github.com/otiai10/copy"
)

type Config struct {
	ImageRootFSPath string `json:"rootfs"`
	MountsList []string `json:"mounts"`
	Command string `json:"command"`
}

//go:embed config.json
var config_bytes []byte

func (c Config) buildContainerInDir(path string, args []string, cwd string) error {

	destRootFSPath := filepath.Join(path, "rootfs")

	uid := os.Getuid()
	gid := os.Getgid()

	mounts := make([]BindMount, 1 + len(c.MountsList))
	mounts[0] = BindMount{
		Source: cwd,
		Destination: "/ark",
	}
	for index, path := range c.MountsList {
		mounts[index + 1] = BindMount{
			Source: path,
			Destination: path,
		}
	}

	spec := CreateRootlessSpec(
		args,
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

	err = copy.Copy(c.ImageRootFSPath, destRootFSPath)
	if err != nil {
		return fmt.Errorf("failed to clone rootfs: %w", err)
	}

	return nil
}

func main() {

	var conf Config
	err := json.Unmarshal(config_bytes, &conf)
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	dir, err := ioutil.TempDir("", "container-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	args := append([]string{conf.Command}, os.Args[1:]...)

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	err = conf.buildContainerInDir(dir, args, cwd)
	if err != nil {
		log.Fatal(err)
	}

	fullarglist := []string{"/sbin/runc", "run", "-b", dir, "mycontainerid"}

	cmd := &exec.Cmd{
		Path:   "/sbin/runc",
		Args:   fullarglist,
		Stderr: os.Stderr,
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("Failed to get input pipe: %v", err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Failed to get output pipe: %v", err)
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Fatalf("Failed to run runc: %v", err)
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
			os.Exit(proc_error.ExitCode())
		} else {
			log.Fatalf("Failed to wait: %v", err)
		}
	}
}
