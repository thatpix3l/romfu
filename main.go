package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/fatih/color"
)

// Format path to format for rclone's config
func fmtRclone(remote string, path string, options ...string) string {
	formattedStr := "\"" + remote + ":" + path
	for _, option := range options {
		formattedStr += ":" + option
	}
	formattedStr += "\""
	return formattedStr
}

// ROM path
type Rom struct {
	DirPath    string // Path to the ROM's top-level directory e.g. /path/to/game
	SubdirName string // Chosen subdirectory in the root directory used for the ROM e.g. "merged" or "base"
}

// Merged root and subdirectory e.g. /path/to/game/{merged,base}
func (r Rom) Parent() string {
	return path.Join(r.DirPath, r.SubdirName)
}

var invalidGameDirNames = []string{"rw", "titles"} // Invalid names of ROM directories in root of provided game library
var validSubdirNames = []string{"merged", "base"}  // Name of usable subdirectories for each switch game

type Command interface {
	Action()
	Subcommands() []Command
}

var CLI struct {
	Switch *struct {
		FS *struct {
			EnableWrite bool   `short:"w" help:"enable writing to output directory"`
			InputDir    string `short:"i" required:"" help:"path to directory containing subdirectories of switch games"`
			OutputDir   string `short:"o" required:"" help:"path to directory for mounting the flat filesystem"`
		} `cmd`
	} `cmd`
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// Create a random string of length "n"
func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

type Action func()

func main() {

	if err := kong.Parse(&CLI).Error; err != nil {
		log.Fatal(err)
	}

	switch {
	case CLI.Switch != nil:
		switch {
		case CLI.Switch.FS != nil:
			createFS()
		}
	}

}

func createFS() {

	gamesSrcPath := CLI.Switch.FS.InputDir
	gamesDestPath := CLI.Switch.FS.OutputDir

	// Read in given directory containing switch games
	files, err := ioutil.ReadDir(gamesSrcPath)
	if err != nil {
		log.Fatal(err)
	}

	// Path to each game directory's "merged" or "base" directory.
	// Example: /absolute/path/to/game/dir/containing/one/nsp
	// This directory path would contain only one NSP, the game itself.
	// Whether merged or not depends if the "merged" dir exists.
	roms := []Rom{}

	var detectedGames string

	// For any file/folder in the provided game library...
	for _, file := range files {

		gameDirName := file.Name()

		// If not a directory, skip
		if !file.IsDir() {
			continue
		}

		// If name of directory starts with a period (basically, if hidden), skip
		if strings.HasPrefix(gameDirName, ".") {
			continue
		}

		// If name of directory is one of the blacklisted names, skip
		isBlacklisted := func() bool {
			for _, invalidDirName := range invalidGameDirNames {
				if gameDirName == invalidDirName {
					return true
				}
			}
			return false
		}

		if isBlacklisted() {
			continue
		}

		dirPath := path.Join(gamesSrcPath, gameDirName)

		// For each valid ROM subdirectory name...
		for _, subdirName := range validSubdirNames {

			// Create a path, joining the path to the game's directory with the name of a subdir
			// Example: /path/to/game/root + romDirName = /path/to/game/root/romDirName
			romDirPath := path.Join(dirPath, subdirName)

			// If it exists, only use the one rom directory.
			if stat, err := os.Stat(romDirPath); err == nil && stat != nil && stat.IsDir() {
				detectedGames += fmt.Sprintf("\"%s\" -> \"%s\"\n", color.BlueString(gameDirName), subdirName)
				roms = append(roms, Rom{DirPath: dirPath, SubdirName: subdirName})
				break
			}

		}

	}

	// If no ROM directories were even detected, quit early
	if len(roms) == 0 {
		log.Fatal("no valid game folders found in given directory")
	}

	fmt.Println("Games:")
	fmt.Println(detectedGames)

	remoteLocal := "ROMFULOCAL"
	remoteUnion := "ROMFUUNION"

	rcloneConfig := map[string]map[string]string{
		remoteLocal: {
			"TYPE": "local",
		},
		remoteUnion: {
			"TYPE":      "union",
			"UPSTREAMS": "",
		},
	}

	// If the user wants to enable writing in the final directory, create a separate "rw" directory for all written content
	if CLI.Switch.FS.EnableWrite {
		rwDirPath := path.Join(gamesSrcPath, "rw")
		os.MkdirAll(rwDirPath, 0755)
		rcloneConfig[remoteUnion]["UPSTREAMS"] += fmtRclone(remoteLocal, rwDirPath) + " "
	}

	// If we have more than one ROM, implement a separator
	var separator string
	if len(roms) > 1 {
		separator = " "
	}

	// For each detected ROM, add it to the list of upstreams as read-only
	for _, rom := range roms {
		rcloneConfig[remoteUnion]["UPSTREAMS"] += fmtRclone(remoteLocal, rom.Parent(), "ro") + separator
	}

	// Apply rcloneConfig as environment variables
	for remoteName, remoteConfig := range rcloneConfig {
		for remoteOptionName, remoteOptionValue := range remoteConfig {
			envKey := "RCLONE_CONFIG_" + remoteName + "_" + remoteOptionName
			if err := os.Setenv(envKey, remoteOptionValue); err != nil {
				log.Fatal(err)
			}
		}
	}

	// Run rclone command
	cmd := exec.Command("rclone", "mount", remoteUnion+":", gamesDestPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Println("Rclone command output:")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

}
