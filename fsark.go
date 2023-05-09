package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/otiai10/copy"
)

type config struct {
	imageRootFSPath string
}

func (c config) buildContainerInDir(path string) error {

	destRootFSPath := filepath.Join(path, "rootfs")

	uid := os.Getuid()
	gid := os.Getgid()

	spec := CreateRootlessSpec(
		[]string{"/bin/ls"},
		"/",
		destRootFSPath,
		[]string{},
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

	err = copy.Copy(c.imageRootFSPath, destRootFSPath)
	if err != nil {
		return fmt.Errorf("failed to clone rootfs: %w", err)
	}

	return nil
}

func main() {

	defaultImage := config{
		imageRootFSPath: "/tmp/mycontainer/rootfs",
	}

	dir, err := ioutil.TempDir("", "container-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	err = defaultImage.buildContainerInDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	fullarglist := []string{"/bin/runc", "run", "-b", dir, "mycontainerid"}

	cmd := &exec.Cmd{
		Path:   "/bin/runc",
		Args:   fullarglist,
		Stderr: os.Stderr,
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Failed to get output pipe for %v: %v", fullarglist, err)
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		log.Fatalf("Failed to run %v: %v", fullarglist, err)
	}

	buf := bufio.NewReader(stdout)
	count := 0
	for {
		line, _, err := buf.ReadLine()
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading process %v: %v", fullarglist, err)
			}
			break
		}

		fmt.Printf("%s\n", line)
		count += 1
	}

	err = cmd.Wait()
	if err != nil {
		log.Fatalf("Failed to wait %v: %v", fullarglist, err)
	}
}
