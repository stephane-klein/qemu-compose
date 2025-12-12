package main

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// detectOSFromImage attempts to detect the OS type from the image URL
func detectOSFromImage(imageURL string) string {
    lowerURL := strings.ToLower(imageURL)
    
    if strings.Contains(lowerURL, "fedora") {
        return "fedora"
    } else if strings.Contains(lowerURL, "ubuntu") {
        return "ubuntu"
    } else if strings.Contains(lowerURL, "debian") {
        return "debian"
    } else if strings.Contains(lowerURL, "centos") {
        return "centos"
    } else if strings.Contains(lowerURL, "rhel") {
        return "rhel"
    }
    
    // Default to ubuntu if we can't detect
    return "ubuntu"
}

// getDefaultUserForOS returns the default username for a given OS
func getDefaultUserForOS(osType string) string {
    switch osType {
    case "fedora":
        return "fedora"
    case "ubuntu":
        return "ubuntu"
    case "debian":
        return "debian"
    case "centos":
        return "centos"
    case "rhel":
        return "cloud-user"
    default:
        return "ubuntu"
    }
}

// getProjectSSHPublicKey returns the project SSH public key, generating it if needed
func getProjectSSHPublicKey() (string, error) {
    cwd, err := os.Getwd()
    if err != nil {
        return "", fmt.Errorf("failed to get current directory: %w", err)
    }
    
    sshDir := filepath.Join(cwd, ".qemu-compose", "ssh")
    privateKeyPath := filepath.Join(sshDir, "id_ed25519")
    publicKeyPath := filepath.Join(sshDir, "id_ed25519.pub")
    
    // Check if keys already exist
    if _, err := os.Stat(publicKeyPath); err == nil {
        // Keys exist, read public key
        data, err := os.ReadFile(publicKeyPath)
        if err != nil {
            return "", fmt.Errorf("failed to read SSH public key: %w", err)
        }
        logger.Printf("Using existing project SSH key: %s", publicKeyPath)
        return strings.TrimSpace(string(data)), nil
    }
    
    // Keys don't exist, generate them
    logger.Printf("Generating new SSH key pair in: %s", sshDir)
    
    if err := os.MkdirAll(sshDir, 0700); err != nil {
        return "", fmt.Errorf("failed to create SSH directory: %w", err)
    }
    
    // Generate ED25519 key pair
    cmd := exec.Command("ssh-keygen",
        "-t", "ed25519",
        "-f", privateKeyPath,
        "-N", "", // No passphrase
        "-C", "qemu-compose",
    )
    
    output, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("failed to generate SSH key: %w\nOutput: %s", err, string(output))
    }
    
    // Set correct permissions
    if err := os.Chmod(privateKeyPath, 0600); err != nil {
        return "", fmt.Errorf("failed to set private key permissions: %w", err)
    }
    
    // Read the generated public key
    data, err := os.ReadFile(publicKeyPath)
    if err != nil {
        return "", fmt.Errorf("failed to read generated SSH public key: %w", err)
    }
    
    logger.Printf("Generated new SSH key pair: %s", publicKeyPath)
    fmt.Printf("  ✓ Generated SSH key pair in .qemu-compose/ssh/\n")
    
    return strings.TrimSpace(string(data)), nil
}

// generateCloudInitISO creates a cloud-init NoCloud ISO with user-data and meta-data
func generateCloudInitISO(vmName string, imageURL string, networkCount int) (string, error) {
    return generateCloudInitISOWithVolumes(vmName, imageURL, nil, nil)
}

// generateCloudInitISOWithVolumes creates a cloud-init NoCloud ISO with user-data, meta-data, and volume mounts
func generateCloudInitISOWithVolumes(vmName string, imageURL string, macAddresses []string, volumeMounts []VMVolumeMount) (string, error) {
    logger.Printf("Generating cloud-init ISO for VM: %s", vmName)
    
    instanceDir, err := getInstanceDir(vmName)
    if err != nil {
        return "", err
    }
    
    // Detect OS type and get default user
    osType := detectOSFromImage(imageURL)
    defaultUser := getDefaultUserForOS(osType)
    logger.Printf("Detected OS type: %s, default user: %s", osType, defaultUser)
    
    // Create cloud-init directory
    cloudInitDir := filepath.Join(instanceDir, "cloud-init")
    if err := os.MkdirAll(cloudInitDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create cloud-init directory: %w", err)
    }
    
    // Get project SSH public key
    sshPublicKey, err := getProjectSSHPublicKey()
    if err != nil {
        logger.Printf("Warning: could not get SSH public key: %v", err)
        sshPublicKey = ""
    }
    
    sshKeysYAML := ""
    if sshPublicKey != "" {
        sshKeysYAML = fmt.Sprintf("\n    ssh_authorized_keys:\n      - %s", sshPublicKey)
    }
    
    // Build network configuration for all interfaces using MAC address matching
    // This ensures all network interfaces are brought up with DHCP
    networkConfigYAML := ""
    if len(macAddresses) > 0 {
        networkConfigYAML = "\nnetwork:\n  version: 2\n  ethernets:\n"
        for i, macAddr := range macAddresses {
            // Use generic interface names (net0, net1, etc.) with MAC matching
            ifName := fmt.Sprintf("net%d", i)
            networkConfigYAML += fmt.Sprintf("    %s:\n      match:\n        macaddress: \"%s\"\n      dhcp4: true\n      set-name: %s\n", ifName, macAddr, ifName)
            logger.Printf("Added network interface to cloud-init: %s (MAC: %s)", ifName, macAddr)
        }
    }
    
    // Build volume mount configuration
    // Named volumes appear as /dev/vdb, /dev/vdc, etc. (after /dev/vda which is the main disk)
    // Bind mounts use 9p with mount tags
    mountsYAML := ""
    bootcmdYAML := ""
    packagesYAML := ""
    
    if len(volumeMounts) > 0 {
        mountsYAML = "\nmounts:\n"
        bootcmdYAML = "\nbootcmd:\n"
        
        // Check if we need 9p support
        has9pMounts := false
        for _, mount := range volumeMounts {
            if mount.IsBindMount {
                has9pMounts = true
                break
            }
        }
        
        // Add 9pnet_virtio module if needed
        if has9pMounts {
            packagesYAML = "\npackages:\n  - 9base\n"
            bootcmdYAML += "  - modprobe 9p\n  - modprobe 9pnet_virtio\n"
        }
        
        namedVolumeIndex := 0
        bindMountIndex := 0
        
        for _, mount := range volumeMounts {
            // Create mount point directory
            bootcmdYAML += fmt.Sprintf("  - mkdir -p %s\n", mount.MountPath)
            
            if mount.IsBindMount {
                // Only add mount entry if automount is enabled
                if mount.Automount {
                    // Use 9p for bind mounts
                    mountTag := fmt.Sprintf("mount%d", bindMountIndex)
                    
                    // Build mount options
                    mountOptions := "trans=virtio,version=9p2000.L"
                    if mount.MountOptions != "" {
                        mountOptions = mount.MountOptions
                    }
                    if mount.ReadOnly {
                        mountOptions += ",ro"
                    }
                    
                    mountsYAML += fmt.Sprintf("  - [%s, %s, 9p, \"%s\", \"0\", \"0\"]\n", mountTag, mount.MountPath, mountOptions)
                    logger.Printf("Added 9p bind mount to cloud-init: %s -> %s (ro=%v, automount=%v)", mount.HostPath, mount.MountPath, mount.ReadOnly, mount.Automount)
                } else {
                    logger.Printf("Skipped auto-mount for bind mount: %s -> %s (automount=false)", mount.HostPath, mount.MountPath)
                }
                bindMountIndex++
            } else {
                // Use virtio-blk for named volumes (always auto-mounted)
                // Device name: /dev/vdb, /dev/vdc, etc.
                deviceName := fmt.Sprintf("/dev/vd%c", 'b'+namedVolumeIndex)
                namedVolumeIndex++
                
                mountOptions := "defaults"
                if mount.ReadOnly {
                    mountOptions = "ro"
                }
                
                mountsYAML += fmt.Sprintf("  - [%s, %s, ext4, \"%s\", \"0\", \"2\"]\n", deviceName, mount.MountPath, mountOptions)
                logger.Printf("Added named volume mount to cloud-init: %s -> %s (ro=%v)", deviceName, mount.MountPath, mount.ReadOnly)
            }
        }
    }
    
    // Create user-data file with detected user, SSH key, network configuration, and volume mounts
    userData := fmt.Sprintf(`#cloud-config
users:
  - name: %s
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: false%s
chpasswd:
  expire: false
  list: |
    %s:password
ssh_pwauth: true%s%s%s%s
`, defaultUser, sshKeysYAML, defaultUser, networkConfigYAML, packagesYAML, bootcmdYAML, mountsYAML)
    
    userDataPath := filepath.Join(cloudInitDir, "user-data")
    if err := os.WriteFile(userDataPath, []byte(userData), 0644); err != nil {
        return "", fmt.Errorf("failed to write user-data: %w", err)
    }
    
    // Create meta-data file
    metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", vmName, vmName)
    metaDataPath := filepath.Join(cloudInitDir, "meta-data")
    if err := os.WriteFile(metaDataPath, []byte(metaData), 0644); err != nil {
        return "", fmt.Errorf("failed to write meta-data: %w", err)
    }
    
    // Create network-config file if we have network configuration
    if networkConfigYAML != "" {
        networkConfigPath := filepath.Join(cloudInitDir, "network-config")
        // Extract just the network config part (remove the leading newline)
        networkConfigContent := strings.TrimPrefix(networkConfigYAML, "\n")
        if err := os.WriteFile(networkConfigPath, []byte(networkConfigContent), 0644); err != nil {
            return "", fmt.Errorf("failed to write network-config: %w", err)
        }
        logger.Printf("Created network-config with %d interface(s)", len(macAddresses))
    }
    
    // Create ISO using genisoimage or mkisofs
    isoPath := filepath.Join(instanceDir, "cloud-init.iso")
    
    // Build file list for ISO
    isoFiles := []string{userDataPath, metaDataPath}
    if networkConfigYAML != "" {
        networkConfigPath := filepath.Join(cloudInitDir, "network-config")
        isoFiles = append(isoFiles, networkConfigPath)
    }
    
    // Try genisoimage first, then mkisofs
    var cmd *exec.Cmd
    if _, err := exec.LookPath("genisoimage"); err == nil {
        args := []string{
            "-output", isoPath,
            "-volid", "cidata",
            "-joliet",
            "-rock",
        }
        args = append(args, isoFiles...)
        cmd = exec.Command("genisoimage", args...)
    } else if _, err := exec.LookPath("mkisofs"); err == nil {
        args := []string{
            "-output", isoPath,
            "-volid", "cidata",
            "-joliet",
            "-rock",
        }
        args = append(args, isoFiles...)
        cmd = exec.Command("mkisofs", args...)
    } else {
        return "", fmt.Errorf("neither genisoimage nor mkisofs found (install genisoimage package)")
    }
    
    output, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("failed to create cloud-init ISO: %w\nOutput: %s", err, string(output))
    }
    
    logger.Printf("Created cloud-init ISO: %s", isoPath)
    return isoPath, nil
}
