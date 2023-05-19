A wrapper that lets you run tasks in containers using runc. It will by default run a specific command in a container with the current working directory mounted in it.

## Setup

### 1: Get your container image

As per runc, you need to create your container images using docker:

```
$ docker create python:buster
$ docker export [ID FROM ABOVE] > container.tar
```

### 2: Configuration file

You then need a config file that maps container images to commands, for example:

```
{
	"images": {
		"pythonbuster": {
			"rootfs": "/path/to/container.tar",
			"tags": ["python3", "python", "buster"]
		}
	},
	"commands": {
		"mypython3": {
			"image": "pythonbuster",
			"mounts": [
				"/scratch"
			],
			"command": "python3"
		}
	}
}
```

This is currently looked for in `/var/ark/config.json`.

### Install with symlinks

The go build process will create `fsark` as a binary. You then symlink to this with the name of the command you want it to use from the config file when run:

```
$ go build
$ sudo cp fsark /usr/local/bin
$ sudo ln -s /usr/local/bin/fsark /usr/local/bin/mypython3
```