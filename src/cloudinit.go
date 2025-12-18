package main

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "text/template"
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

// CloudInitData holds all data needed for cloud-init template rendering
type CloudInitData struct {
    VMName        string
    OSUser        string
    SSHPublicKey  string
    MACAddresses  []string
    VolumeMounts  []VMVolumeMount
    Has9pMounts   bool
}

// has9pMounts checks if any volume mount is a bind mount (9p)
func has9pMounts(volumeMounts []VMVolumeMount) bool {
    for _, mount := range volumeMounts {
        if mount.IsBindMount {
            return true
        }
    }
    return false
}

// cloudInitTemplate is the Go template for cloud-init user-data
const cloudInitTemplate = `#cloud-config
# Automatically resize partitions and filesystems to fill available disk space
growpart:
  mode: auto
  devices: ['/']
  ignore_growroot_disabled: false
resizefs: true

users:
  - name: {{.OSUser}}
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: false
{{- if .SSHPublicKey}}
    ssh_authorized_keys:
      - {{.SSHPublicKey}}
{{- end}}{{/* if .SSHPublicKey */}}
chpasswd:
  expire: false
  list: |
    {{.OSUser}}:password
ssh_pwauth: true
{{- if .MACAddresses}}
network:
  version: 2
  ethernets:
{{- range $i, $mac := .MACAddresses}}
    net{{$i}}:
      match:
        macaddress: "{{$mac}}"
      dhcp4: true
      set-name: net{{$i}}
{{- end}}{{/* range .MACAddresses */}}
{{- end}}{{/* if .MACAddresses */}}
{{- if .VolumeMounts}}
{{- if .Has9pMounts}}
packages:
  - 9base
{{- end}}{{/* if .Has9pMounts */}}
bootcmd:
{{- range .VolumeMounts}}
  - mkdir -p {{.MountPath}}
{{- end}}{{/* range .VolumeMounts - bootcmd */}}
{{- if .Has9pMounts}}
  - modprobe 9p
  - modprobe 9pnet_virtio
{{- end}}{{/* if .Has9pMounts */}}
mounts:
{{- $namedIdx := 0}}{{$bindIdx := 0}}
{{- range .VolumeMounts}}
{{- if .IsBindMount}}
{{- if .Automount}}
  - [mount{{$bindIdx}}, {{.MountPath}}, 9p, "{{if .MountOptions}}{{.MountOptions}}{{else}}trans=virtio,version=9p2000.L{{if .ReadOnly}},ro{{end}}{{end}}", "0", "0"]
{{- $bindIdx = add $bindIdx 1}}
{{- end}}{{/* if .Automount */}}
{{- else}}
  - [/dev/vd{{indexToLetter $namedIdx}}, {{.MountPath}}, ext4, "{{if .ReadOnly}}ro{{else}}defaults{{end}}", "0", "2"]
{{- $namedIdx = add $namedIdx 1}}
{{- end}}{{/* if .IsBindMount */}}
{{- end}}{{/* range .VolumeMounts - mounts */}}
{{- end}}{{/* if .VolumeMounts */}}`

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
    
    // Prepare template data
    data := CloudInitData{
        VMName:       vmName,
        OSUser:       defaultUser,
        SSHPublicKey: sshPublicKey,
        MACAddresses: macAddresses,
        VolumeMounts: volumeMounts,
        Has9pMounts:  has9pMounts(volumeMounts),
    }
    
    // Create template with custom functions
    tmpl, err := template.New("cloud-init").
        Funcs(template.FuncMap{
            "indexToLetter": func(i int) string {
                return string('b' + i)
            },
            "add": func(a, b int) int {
                return a + b
            },
        }).
        Parse(cloudInitTemplate)
    
    if err != nil {
        return "", fmt.Errorf("failed to parse cloud-init template: %w", err)
    }
    
    // Execute template
    var userDataBuilder strings.Builder
    if err := tmpl.Execute(&userDataBuilder, data); err != nil {
        return "", fmt.Errorf("failed to execute cloud-init template: %w", err)
    }
    
    userData := userDataBuilder.String()
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
    if len(macAddresses) > 0 {
        networkConfigPath := filepath.Join(cloudInitDir, "network-config")
        // Build network config YAML
        var networkConfigBuilder strings.Builder
        networkConfigBuilder.WriteString("network:\n  version: 2\n  ethernets:\n")
        for i, macAddr := range macAddresses {
            ifName := fmt.Sprintf("net%d", i)
            networkConfigBuilder.WriteString(fmt.Sprintf("    %s:\n      match:\n        macaddress: \"%s\"\n      dhcp4: true\n      set-name: %s\n", ifName, macAddr, ifName))
            logger.Printf("Added network interface to cloud-init: %s (MAC: %s)", ifName, macAddr)
        }
        
        networkConfigContent := networkConfigBuilder.String()
        if err := os.WriteFile(networkConfigPath, []byte(networkConfigContent), 0644); err != nil {
            return "", fmt.Errorf("failed to write network-config: %w", err)
        }
        logger.Printf("Created network-config with %d interface(s)", len(macAddresses))
    }
    
    // Create ISO using genisoimage or mkisofs
    isoPath := filepath.Join(instanceDir, "cloud-init.iso")
    
    // Build file list for ISO
    isoFiles := []string{userDataPath, metaDataPath}
    if len(macAddresses) > 0 {
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
