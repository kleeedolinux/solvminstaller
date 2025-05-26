package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fatih/color"
	"github.com/schollz/progressbar/v3"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func getLatestRelease() (*Release, error) {
	resp, err := http.Get("https://api.github.com/repos/kleeedolinux/SolVM/releases")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	return &releases[0], nil
}

func getSystemAsset(release *Release) (*Asset, error) {
	var osName, arch string

	switch runtime.GOOS {
	case "windows":
		osName = "windows"
	case "darwin":
		osName = "darwin"
	case "linux":
		osName = "linux"
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}

	assetName := fmt.Sprintf("solvm-%s-%s", osName, arch)
	if osName == "windows" {
		assetName += ".exe"
	}

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return &asset, nil
		}
	}

	return nil, fmt.Errorf("no matching asset found for %s/%s", osName, arch)
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"Downloading",
	)

	_, err = io.Copy(io.MultiWriter(out, bar), resp.Body)
	return err
}

func addToPath(installDir string) error {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("powershell", "-Command", fmt.Sprintf(
			"$path = [Environment]::GetEnvironmentVariable('Path', 'User'); if (-not $path.Contains('%s')) { [Environment]::SetEnvironmentVariable('Path', $path + ';%s', 'User') }",
			installDir,
			installDir,
		))
		return cmd.Run()

	case "darwin", "linux":
		shell := os.Getenv("SHELL")
		var rcFile string
		if strings.Contains(shell, "zsh") {
			rcFile = filepath.Join(os.Getenv("HOME"), ".zshrc")
		} else {
			rcFile = filepath.Join(os.Getenv("HOME"), ".bashrc")
		}

		pathLine := fmt.Sprintf("\nexport PATH=\"%s:$PATH\"\n", installDir)
		f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := f.WriteString(pathLine); err != nil {
			return err
		}

		cmd := exec.Command("source", rcFile)
		return cmd.Run()
	}

	return nil
}

func askForConfirmation(message string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [y/N]: ", message)
		response, err := reader.ReadString('\n')
		if err != nil {
			return false
		}
		response = strings.ToLower(strings.TrimSpace(response))
		if response == "y" || response == "yes" {
			return true
		}
		if response == "n" || response == "no" || response == "" {
			return false
		}
	}
}

func checkExistingInstallation(installDir string) bool {
	if runtime.GOOS == "windows" {
		_, err := os.Stat(filepath.Join(installDir, "solvm.exe"))
		return err == nil
	}
	_, err := os.Stat(filepath.Join(installDir, "solvm"))
	return err == nil
}

func main() {
	color.Cyan("SolVM Installer")
	color.Cyan("===============")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		color.Red("Error getting home directory: %v", err)
		os.Exit(1)
	}

	installDir := filepath.Join(homeDir, ".solvm")
	if checkExistingInstallation(installDir) {
		if !askForConfirmation("SolVM is already installed. Do you want to replace it?") {
			color.Yellow("Installation cancelled")
			os.Exit(0)
		}
	}

	release, err := getLatestRelease()
	if err != nil {
		color.Red("Error getting latest release: %v", err)
		os.Exit(1)
	}

	color.Green("Latest version: %s", release.TagName)

	asset, err := getSystemAsset(release)
	if err != nil {
		color.Red("Error finding system asset: %v", err)
		os.Exit(1)
	}

	color.Yellow("Found matching asset: %s", asset.Name)

	if !askForConfirmation("Do you want to install SolVM?") {
		color.Yellow("Installation cancelled")
		os.Exit(0)
	}

	if err := os.MkdirAll(installDir, 0755); err != nil {
		color.Red("Error creating install directory: %v", err)
		os.Exit(1)
	}

	tempPath := filepath.Join(installDir, asset.Name)
	color.Yellow("Downloading to: %s", tempPath)

	if err := downloadFile(asset.BrowserDownloadURL, tempPath); err != nil {
		color.Red("Error downloading file: %v", err)
		os.Exit(1)
	}

	finalName := "solvm"
	if runtime.GOOS == "windows" {
		finalName += ".exe"
	}
	finalPath := filepath.Join(installDir, finalName)

	if err := os.Rename(tempPath, finalPath); err != nil {
		color.Red("Error renaming file: %v", err)
		os.Exit(1)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(finalPath, 0755); err != nil {
			color.Red("Error setting executable permissions: %v", err)
			os.Exit(1)
		}
	}

	if !askForConfirmation("Do you want to add SolVM to your PATH?") {
		color.Yellow("Skipping PATH configuration")
	} else {
		color.Yellow("Adding to PATH...")
		if err := addToPath(installDir); err != nil {
			color.Red("Error adding to PATH: %v", err)
			color.Yellow("Please manually add to PATH: %s", installDir)
		} else {
			color.Green("Successfully added to PATH")
		}
	}

	color.Green("Installation complete!")
	color.Green("Please restart your terminal to use SolVM")
}
