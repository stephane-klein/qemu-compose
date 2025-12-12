package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// VolumeMetadata represents metadata about a named volume
type VolumeMetadata struct {
	Name     string `json:"name"`
	Size     string `json:"size"`
	DiskPath string `json:"disk_path"`
	Created  string `json:"created"`
}

// getVolumesDir returns the directory where named volumes are stored
func getVolumesDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	volumesDir := filepath.Join(cwd, ".qemu-compose", "volumes")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(volumesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create volumes directory: %w", err)
	}

	return volumesDir, nil
}

// getVolumeMetadataPath returns the path to the volumes metadata file
func getVolumeMetadataPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	qemuComposeDir := filepath.Join(cwd, ".qemu-compose")
	if err := os.MkdirAll(qemuComposeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .qemu-compose directory: %w", err)
	}

	return filepath.Join(qemuComposeDir, "volumes.json"), nil
}

// loadVolumeMetadata loads volume metadata from disk
func loadVolumeMetadata() (map[string]VolumeMetadata, error) {
	metadataPath, err := getVolumeMetadataPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]VolumeMetadata), nil
		}
		return nil, fmt.Errorf("failed to read volume metadata: %w", err)
	}

	var metadata map[string]VolumeMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse volume metadata: %w", err)
	}

	return metadata, nil
}

// saveVolumeMetadata saves volume metadata to disk
func saveVolumeMetadata(metadata map[string]VolumeMetadata) error {
	metadataPath, err := getVolumeMetadataPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal volume metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write volume metadata: %w", err)
	}

	return nil
}

// isBindMount checks if a volume source is a bind mount (contains path separator)
func isBindMount(source string) bool {
	return strings.Contains(source, "/") || strings.Contains(source, "\\") || strings.HasPrefix(source, ".")
}

// resolveBindMountPath resolves a bind mount path to an absolute path
// Relative paths are resolved relative to the compose file location
func resolveBindMountPath(hostPath string, composeFilePath string) (string, error) {
	// If path is already absolute, return it
	if filepath.IsAbs(hostPath) {
		// Check if path exists
		if _, err := os.Stat(hostPath); err != nil {
			return "", fmt.Errorf("bind mount path does not exist: %s", hostPath)
		}
		return hostPath, nil
	}

	// Resolve relative path relative to the compose file directory
	composeDir := filepath.Dir(composeFilePath)
	absPath := filepath.Join(composeDir, hostPath)

	// Check if path exists
	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("bind mount path does not exist: %s (resolved to: %s)", hostPath, absPath)
	}

	return absPath, nil
}

// createNamedVolume creates a new named volume with the specified size
func createNamedVolume(volumeName string, size string) error {
	logger.Printf("Creating named volume: %s (size: %s)", volumeName, size)

	// Load existing metadata
	metadata, err := loadVolumeMetadata()
	if err != nil {
		return err
	}

	// Check if volume already exists
	if _, exists := metadata[volumeName]; exists {
		logger.Printf("Volume already exists: %s", volumeName)
		return nil
	}

	// Get volumes directory
	volumesDir, err := getVolumesDir()
	if err != nil {
		return err
	}

	// Create volume directory
	volumeDir := filepath.Join(volumesDir, volumeName)
	if err := os.MkdirAll(volumeDir, 0755); err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	// Create qcow2 disk image
	diskPath := filepath.Join(volumeDir, "volume.qcow2")
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", diskPath, size)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create volume disk: %w\nOutput: %s", err, string(output))
	}

	logger.Printf("Created volume disk: %s", diskPath)

	// Format the volume with ext4
	// We need to use qemu-nbd to mount the qcow2 image and format it
	if err := formatVolumeDisk(diskPath); err != nil {
		return fmt.Errorf("failed to format volume: %w", err)
	}

	// Save metadata
	metadata[volumeName] = VolumeMetadata{
		Name:     volumeName,
		Size:     size,
		DiskPath: diskPath,
		Created:  fmt.Sprintf("%d", os.Getpid()), // Simple timestamp placeholder
	}

	if err := saveVolumeMetadata(metadata); err != nil {
		return err
	}

	logger.Printf("Successfully created volume: %s", volumeName)
	return nil
}

// formatVolumeDisk formats a volume disk with ext4 filesystem
func formatVolumeDisk(diskPath string) error {
	logger.Printf("Formatting volume disk: %s", diskPath)

	// Load nbd kernel module
	cmd := exec.Command("sudo", "modprobe", "nbd", "max_part=8")
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Printf("Warning: failed to load nbd module: %v\nOutput: %s", err, string(output))
		// Continue anyway, module might already be loaded
	}

	// Find available nbd device
	nbdDevice := "/dev/nbd0"

	// Connect qcow2 to nbd device
	cmd = exec.Command("sudo", "qemu-nbd", "--connect", nbdDevice, diskPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to connect qcow2 to nbd: %w\nOutput: %s", err, string(output))
	}

	// Ensure we disconnect on exit
	defer func() {
		cmd := exec.Command("sudo", "qemu-nbd", "--disconnect", nbdDevice)
		if output, err := cmd.CombinedOutput(); err != nil {
			logger.Printf("Warning: failed to disconnect nbd: %v\nOutput: %s", err, string(output))
		}
	}()

	// Format with ext4
	cmd = exec.Command("sudo", "mkfs.ext4", "-F", nbdDevice)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to format volume with ext4: %w\nOutput: %s", err, string(output))
	}

	logger.Printf("Successfully formatted volume with ext4")
	return nil
}

// ensureVolumeExists ensures a named volume exists, creating it if necessary
func ensureVolumeExists(volumeName string, config *ComposeConfig) error {
	// Load existing metadata
	metadata, err := loadVolumeMetadata()
	if err != nil {
		return err
	}

	// Check if volume already exists
	if _, exists := metadata[volumeName]; exists {
		logger.Printf("Volume already exists: %s", volumeName)
		return nil
	}

	// Get volume configuration from compose file
	volumeConfig, exists := config.Volumes[volumeName]
	if !exists {
		return fmt.Errorf("volume not defined in compose file: %s", volumeName)
	}

	// Determine size (default to 10G if not specified)
	size := "10G"
	if volumeConfig.Size != "" {
		size = volumeConfig.Size
	}

	// Create the volume
	return createNamedVolume(volumeName, size)
}

// getVolumeDiskPath returns the disk path for a named volume
func getVolumeDiskPath(volumeName string) (string, error) {
	metadata, err := loadVolumeMetadata()
	if err != nil {
		return "", err
	}

	volumeMeta, exists := metadata[volumeName]
	if !exists {
		return "", fmt.Errorf("volume not found: %s", volumeName)
	}

	return volumeMeta.DiskPath, nil
}

// removeNamedVolume removes a named volume and its data
func removeNamedVolume(volumeName string) error {
	logger.Printf("Removing named volume: %s", volumeName)

	// Load metadata
	metadata, err := loadVolumeMetadata()
	if err != nil {
		return err
	}

	volumeMeta, exists := metadata[volumeName]
	if !exists {
		return fmt.Errorf("volume not found: %s", volumeName)
	}

	// Remove volume directory
	volumeDir := filepath.Dir(volumeMeta.DiskPath)
	if err := os.RemoveAll(volumeDir); err != nil {
		return fmt.Errorf("failed to remove volume directory: %w", err)
	}

	// Remove from metadata
	delete(metadata, volumeName)
	if err := saveVolumeMetadata(metadata); err != nil {
		return err
	}

	logger.Printf("Successfully removed volume: %s", volumeName)
	return nil
}

// listVolumes returns a list of all named volumes
func listVolumes() (map[string]VolumeMetadata, error) {
	return loadVolumeMetadata()
}
