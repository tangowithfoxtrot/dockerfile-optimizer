package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/tangowithfoxtrot/dockerfile-optimizer/logging"
)

var log = logging.GetLogger("dockerfile-optimizer", "docker.go")
var snakeCaseRegexp = regexp.MustCompile(`^[A-Z_]+$`)
var commands []string
var cmdFullPaths []string

var shellBuiltins = []string{
	"exit", "return", "set", "unset", "export",
	"if", "then", "else", "elif", "fi",
	"gt", "lt", "ge", "le", "eq", "ne",
	"case", "esac", "for", "select", "while",
	"until", "do", "done", "in", "function",
	"time",
	"{", "}", "[[", "]]",
	"!", "|", "&", ";", "=",
}

type Image struct {
	Name       string
	Id         string
	Entrypoint []string
	Cmd        []string
}

type ImageProcessStrategy interface {
	ProcessImage(ctx context.Context, cli *client.Client) error
}

func main() {
	ctx := context.Background()
	cli, err := initDockerClient(ctx)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	images, err := processContainers(ctx, cli)
	if err != nil {
		panic(err)
	}

	entrypoint := images[0].Entrypoint
	if len(entrypoint) == 0 {
		entrypoint = images[0].Cmd
	}

	if len(entrypoint) == 0 {
		log.Fatal("No entrypoint or cmd found")
	}

	if entrypoint != nil {
		// anaylze the entrypoint or cmd
		isShellScript := func(entrypoint string) bool {
			log.Info("Entrypoint: %s", entrypoint)
			// FIX: this is not a good way to check if it's a shell script
			return entrypoint[len(entrypoint)-3:] == ".sh"
		}
		log.Info("Is shell script: %t", isShellScript(entrypoint[0]))

		isBinary := func(entrypoint string) bool {
			// FIX: this is not a good way to check if it's a binary
			// if the file extension is not .sh, then it's a binary
			return entrypoint[len(entrypoint)-3:] != ".sh"
		}
		log.Info("Is binary: %t", isBinary(entrypoint[0]))

		if isShellScript(entrypoint[0]) {
			containerID := images[0].Id     // the container id
			entrypointPath := entrypoint[0] // the path to the entrypoint file
			tmpFile, err := os.CreateTemp("", "entrypoint")
			if err != nil {
				log.Fatal("Error creating temporary file: %s", err)
			}
			defer os.Remove(tmpFile.Name()) // clean up the temporary file when done

			// TODO: execute the entrypoint/cmd in the container
			cmd := exec.Command("docker", "cp", fmt.Sprintf("%s:%s", containerID, entrypointPath), tmpFile.Name())
			log.Info("Executing command: %s", cmd.String())
			err = cmd.Run()
			if err != nil {
				log.Fatal("Error copying file: %s", err)
			}

			f, err := os.Open(tmpFile.Name())
			if err != nil {
				log.Fatal("Error opening file: %s", err)
			}
			defer f.Close()

			scanner := bufio.NewScanner(f)

			for scanner.Scan() {
				line := scanner.Text()

				// Trim leading and trailing white space
				line = strings.TrimSpace(line)

				// Skip empty lines and comments
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}

				commands := filterCommands(line)

				// Get the commands
				for _, command := range commands {
					cmdFullPath, err := getCommandFullPath(command)
					if err != nil {
						log.Error("Failed to get full path for command %s: %v", command, err)
						continue
					}

					// Check if the full path is already in the cmdFullPaths slice
					if !contains(cmdFullPaths, cmdFullPath) {
						cmdFullPaths = append(cmdFullPaths, cmdFullPath)
					}

					log.Info("Found command full path: %s", cmdFullPath)
				}
			}

			// use lddFindLib to find the libraries
			libs, err := lddFindLib(cmdFullPaths)
			if err != nil {
				log.Error("Failed to find libraries: %v", err)
			}

			log.Info("Total commands found: %d", len(commands))
			log.Info("Commands: %s", commands)
			log.Info("Command full paths: %s", cmdFullPaths)
			log.Info("Libraries: %s", libs)
		}

		if isBinary(entrypoint[0]) {
			commands = append(commands, entrypoint[0])
			log.Info("Commands: %s", commands)

			// Get the commands
			for _, command := range commands {
				cmdFullPath, err := getCommandFullPath(command)
				if err != nil {
					log.Error("Failed to get full path for command %s: %v", command, err)
					continue
				}

				// Check if the full path is already in the cmdFullPaths slice
				if !contains(cmdFullPaths, cmdFullPath) {
					cmdFullPaths = append(cmdFullPaths, cmdFullPath)
				}

				log.Info("Command full path: %s", cmdFullPath)
			}
		}
	}
}

func contains(slice []string, item string) bool {
	for _, a := range slice {
		if a == item || snakeCaseRegexp.MatchString(item) {
			return true
		}
	}
	return false
}

func filterCommands(line string) []string {
	// Regular expression to find words
	// FIX: this still matches things like `--code`, so if VS Code is in your PATH, `code` will be added to the list of commands
	// why can't Go support Perl-compatible regular expressions 😭
	re := regexp.MustCompile(`\b(-{1,2}[a-zA-Z_][a-zA-Z0-9_]*|[a-zA-Z_][a-zA-Z0-9_]*)\b`)
	matches := re.FindAllString(line, -1)

	// TODO: revisit this logic
	for _, match := range matches {
		if contains(shellBuiltins, match) || strings.HasPrefix(match, "-") {
			continue
		}

		// Check if the command exists in the PATH
		_, err := exec.LookPath(match)
		if err != nil {
			// The command does not exist in the PATH
			continue
		}

		if !contains(commands, match) {
			commands = append(commands, match)
		}
	}
	return commands
}

func getCommandFullPath(command string) (string, error) {
	// TODO: get the full path to the binary *in the container*
	cmd := exec.Command(os.Getenv("SHELL"), "-c", fmt.Sprintf("which %s", command))

	// Run the command and store its output
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// The output is a []byte, convert it to a string
	cmdFullPath := strings.TrimSpace(string(out))

	return cmdFullPath, nil
}

func lddFindLib(cmdFullPaths []string) ([]string, error) {
	libs := []string{}
	for _, cmdFullPath := range cmdFullPaths {
		cmd := exec.Command("ldd", cmdFullPath)

		// Run the command and store its output
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}

		// The output is a []byte, convert it to a string
		lddOutput := strings.TrimSpace(string(out))

		// Regular expression to find shared libraries
		re := regexp.MustCompile(`\b(/[^ ]+)\b`)
		matches := re.FindAllString(lddOutput, -1)

		// Add the libraries to the libs slice
		for _, match := range matches {
			libs = append(libs, match)
		}
	}
	return libs, nil
}

func initDockerClient(ctx context.Context) (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func processContainers(ctx context.Context, cli *client.Client) ([]*Image, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}

	var images []*Image
	for _, container := range containers { // TODO: pass a specific container name as an argument
		inspect, err := cli.ContainerInspect(ctx, container.ID)
		if err != nil {
			return nil, err
		}

		image := &Image{
			Name:       inspect.Config.Image,
			Id:         inspect.ID,
			Entrypoint: inspect.Config.Entrypoint,
			Cmd:        inspect.Config.Cmd,
		}

		images = append(images, image)
	}
	return images, nil
}
