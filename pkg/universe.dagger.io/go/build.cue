package go

import (
	"dagger.io/dagger"
)

// Build a go binary
#Build: {
	// Source code
	source: dagger.#FS

	// Target package to build
	package: *"." | string

	// Target architecture
	arch: *"amd64" | string

	// Target OS
	os: *"linux" | string

	// Build tags to use for building
	tags: *"" | string

	// LDFLAGS to use for linking
	ldflags: *"" | string

	env: [string]: string

	container: #Container & {
		"source": source
		"env":    env
		command: {
			args: [package]
			flags: {
				build:      true
				"-v":       true
				"-tags":    tags
				"-ldflags": ldflags
				"-o":       "/output/"
			}
		}
	}

	_binary: dagger.#Subdir & {
		input: container.output.rootfs
		path:  "/output"
	}

	binary: _binary.output
}
