package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/schollz/progressbar/v3"
)

// DiskMetadata represents metadata about a VM's disk
type DiskMetadata struct {
	Size string `json:"size"`
}

// PortMetadata represents allocated ports for a VM
type PortMetadata struct {
	SSH int `json:"ssh"`
}

// ImageInfo represents information about a cached image
type ImageInfo struct {
	Filename string
	Path     string
	Size     int64
}

// getImageCacheDir returns the directory where images are cached
func getImageCacheDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".local", "share", "qemu-compose", "images")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	return cacheDir, nil
}

// getInstanceDir returns the directory for a VM instance
func getInstanceDir(vmName string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	instanceDir := filepath.Join(cwd, ".qemu-compose", vmName)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create instance directory: %w", err)
	}

	return instanceDir, nil
}

// getImageFilename extracts a filename from the image URL
func getImageFilename(imageURL string) (string, error) {
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return "", fmt.Errorf("invalid image URL: %w", err)
	}

	// Get the last part of the path
	filename := filepath.Base(parsedURL.Path)
	if filename == "" || filename == "." || filename == "/" {
		return "", fmt.Errorf("cannot extract filename from URL: %s", imageURL)
	}

	return filename, nil
}

// getBaseImagePath returns the path to a cached base image
func getBaseImagePath(imageURL string) (string, error) {
	cacheDir, err := getImageCacheDir()
	if err != nil {
		return "", err
	}

	filename, err := getImageFilename(imageURL)
	if err != nil {
		return "", err
	}

	imagePath := filepath.Join(cacheDir, filename)

	// Check if image exists
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("base image not found: %s (run 'qemu-compose pull' first)", filename)
	}

	return imagePath, nil
}

// getDiskMetadataPath returns the path to the disk metadata file
func getDiskMetadataPath(vmName string) (string, error) {
	instanceDir, err := getInstanceDir(vmName)
	if err != nil {
		return "", err
	}

	return filepath.Join(instanceDir, "disk.metadata.json"), nil
}

// loadDiskMetadata loads disk metadata from the metadata file
func loadDiskMetadata(vmName string) (*DiskMetadata, error) {
	metadataPath, err := getDiskMetadataPath(vmName)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Metadata file doesn't exist
		}
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadata DiskMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata file: %w", err)
	}

	return &metadata, nil
}

// saveDiskMetadata saves disk metadata to the metadata file
func saveDiskMetadata(vmName string, metadata *DiskMetadata) error {
	metadataPath, err := getDiskMetadataPath(vmName)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	logger.Printf("Saved disk metadata: %s", metadataPath)
	return nil
}

// resizeInstanceDisk resizes a QCOW2 disk image
func resizeInstanceDisk(instanceDiskPath string, size string) error {
	logger.Printf("Resizing instance disk: %s to %s", instanceDiskPath, size)

	cmd := exec.Command("qemu-img", "resize", instanceDiskPath, size)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to resize instance disk: %w\nOutput: %s", err, string(output))
	}

	logger.Printf("Successfully resized instance disk to: %s", size)
	return nil
}

// createInstanceDisk creates a COW overlay disk for a VM instance
func createInstanceDisk(vmName, baseImagePath string, diskConfig *Disk) (string, error) {
	logger.Printf("Creating instance disk for VM: %s", vmName)

	instanceDir, err := getInstanceDir(vmName)
	if err != nil {
		return "", err
	}

	instanceDiskPath := filepath.Join(instanceDir, "disk.qcow2")
	diskAlreadyExists := false

	// Apply default disk size if not specified
	if diskConfig == nil {
		diskConfig = &Disk{Size: "10G"}
	} else if diskConfig.Size == "" {
		diskConfig.Size = "10G"
	}

	// Check if instance disk already exists
	if _, err := os.Stat(instanceDiskPath); err == nil {
		logger.Printf("Instance disk already exists: %s", instanceDiskPath)
		diskAlreadyExists = true
	} else {
		logger.Printf("Creating COW overlay: %s -> %s", baseImagePath, instanceDiskPath)

		// Create qemu-img command to create COW overlay
		cmd := exec.Command("qemu-img", "create",
			"-f", "qcow2",
			"-F", "qcow2",
			"-b", baseImagePath,
			instanceDiskPath,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to create instance disk: %w\nOutput: %s", err, string(output))
		}

		logger.Printf("Successfully created instance disk: %s", instanceDiskPath)
	}

	// Handle disk resizing if size is specified
	if diskConfig != nil && diskConfig.Size != "" {
		if diskAlreadyExists {
			// Disk already exists, check if size changed by comparing with metadata
			logger.Printf("Disk already exists, checking metadata for size changes")

			metadata, err := loadDiskMetadata(vmName)
			if err != nil {
				logger.Printf("Warning: could not load disk metadata: %v", err)
				fmt.Printf("  ⚠ Warning: could not verify disk size (metadata error)\n")
			} else if metadata == nil {
				// No metadata file exists (disk created before metadata feature)
				logger.Printf("No metadata file found, creating one with current size")
				metadata = &DiskMetadata{Size: diskConfig.Size}
				if err := saveDiskMetadata(vmName, metadata); err != nil {
					logger.Printf("Warning: could not save disk metadata: %v", err)
				}
			} else if metadata.Size != diskConfig.Size {
				// Size has changed
				logger.Printf("Disk size mismatch: metadata=%s, requested=%s", metadata.Size, diskConfig.Size)
				fmt.Printf("  ⚠ Warning: disk.size is set to %s but instance disk was created with size %s\n", diskConfig.Size, metadata.Size)
				fmt.Printf("  ⚠ Disk size changes after first creation are not applied automatically\n")
				fmt.Printf("  ⚠ To resize, stop the VM, delete .qemu-compose/%s/, and run 'up' again\n", vmName)
			} else {
				logger.Printf("Disk size matches metadata: %s", metadata.Size)
			}
		} else {
			// New disk, apply resize and save metadata
			logger.Printf("Resizing new instance disk to: %s", diskConfig.Size)
			if err := resizeInstanceDisk(instanceDiskPath, diskConfig.Size); err != nil {
				return "", fmt.Errorf("failed to resize instance disk: %w", err)
			}
			fmt.Printf("  ✓ Disk resized to %s\n", diskConfig.Size)

			// Save metadata
			metadata := &DiskMetadata{Size: diskConfig.Size}
			if err := saveDiskMetadata(vmName, metadata); err != nil {
				logger.Printf("Warning: could not save disk metadata: %v", err)
			}
		}
	}

	return instanceDiskPath, nil
}

// removeInstanceDisk removes the instance disk for a VM
func removeInstanceDisk(vmName string) error {
	logger.Printf("Removing instance disk for VM: %s", vmName)

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	instanceDir := filepath.Join(cwd, ".qemu-compose", vmName)

	// Check if instance directory exists
	if _, err := os.Stat(instanceDir); os.IsNotExist(err) {
		logger.Printf("Instance directory does not exist: %s", instanceDir)
		return nil
	}

	// Remove the entire instance directory
	if err := os.RemoveAll(instanceDir); err != nil {
		return fmt.Errorf("failed to remove instance directory: %w", err)
	}

	logger.Printf("Successfully removed instance directory: %s", instanceDir)
	return nil
}

// downloadImage downloads an image from a URL with a progress bar
func downloadImage(imageURL, vmName string, force bool) error {
	logger.Printf("Starting download of image: %s for VM: %s (force=%v)", imageURL, vmName, force)

	// Get cache directory
	cacheDir, err := getImageCacheDir()
	if err != nil {
		return err
	}

	// Extract filename from URL
	filename, err := getImageFilename(imageURL)
	if err != nil {
		return err
	}

	destPath := filepath.Join(cacheDir, filename)
	logger.Printf("Destination path: %s", destPath)

	// Check if file already exists
	if _, err := os.Stat(destPath); err == nil {
		if !force {
			logger.Printf("Image already exists: %s", destPath)
			fmt.Printf("✓ %s: Image already exists\n", vmName)
			return nil
		}
		logger.Printf("Image already exists but force=true, will overwrite: %s", destPath)
	}

	// Create HTTP request
	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download image: HTTP %d", resp.StatusCode)
	}

	// Create temporary file
	tempPath := destPath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Create progress bar
	bar := progressbar.NewOptions64(
		resp.ContentLength,
		progressbar.OptionSetDescription(fmt.Sprintf("%-20s", vmName)),
		progressbar.OptionSetWriter(os.Stdout),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionThrottle(65*1000000), // 65ms
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stdout, "\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
	)

	// Download with progress
	_, err = io.Copy(io.MultiWriter(out, bar), resp.Body)
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to download image: %w", err)
	}

	// Rename temp file to final destination
	if err := os.Rename(tempPath, destPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to save image: %w", err)
	}

	logger.Printf("Successfully downloaded image to: %s", destPath)
	return nil
}

// isValidImageURL checks if a string is a valid HTTP/HTTPS URL
func isValidImageURL(image string) bool {
	return strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://")
}

// getImageChecksum calculates SHA256 checksum of a file
func getImageChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// getPortMetadataPath returns the path to the port metadata file
func getPortMetadataPath(vmName string) (string, error) {
	instanceDir, err := getInstanceDir(vmName)
	if err != nil {
		return "", err
	}
	return filepath.Join(instanceDir, "ports.json"), nil
}

// loadPortMetadata loads port metadata from file
func loadPortMetadata(vmName string) (*PortMetadata, error) {
	metadataPath, err := getPortMetadataPath(vmName)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Metadata file doesn't exist
		}
		return nil, fmt.Errorf("failed to read port metadata: %w", err)
	}

	var metadata PortMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse port metadata: %w", err)
	}

	return &metadata, nil
}

// savePortMetadata saves port metadata to file
func savePortMetadata(vmName string, metadata *PortMetadata) error {
	metadataPath, err := getPortMetadataPath(vmName)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal port metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write port metadata: %w", err)
	}

	logger.Printf("Saved port metadata: %s", metadataPath)
	return nil
}

// listImages returns a list of all cached images
func listImages() ([]ImageInfo, error) {
	cacheDir, err := getImageCacheDir()
	if err != nil {
		return nil, err
	}

	logger.Printf("Scanning image cache directory: %s", cacheDir)

	// Read directory contents
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache directory: %w", err)
	}

	images := make([]ImageInfo, 0)

	for _, entry := range entries {
		// Skip directories and hidden files
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Skip temporary files
		if strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		fullPath := filepath.Join(cacheDir, entry.Name())

		// Get file info for size
		info, err := entry.Info()
		if err != nil {
			logger.Printf("Warning: could not get info for %s: %v", entry.Name(), err)
			continue
		}

		images = append(images, ImageInfo{
			Filename: entry.Name(),
			Path:     fullPath,
			Size:     info.Size(),
		})
	}

	logger.Printf("Found %d cached images", len(images))
	return images, nil
}
