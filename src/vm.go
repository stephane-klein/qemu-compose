package main

import (
    "crypto/md5"
    "fmt"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// getProjectName returns the project name based on the current directory
func getProjectName() string {
    cwd, err := os.Getwd()
    if err != nil {
        return "default"
    }
    return filepath.Base(cwd)
}

// getVMUnitName returns the systemd unit name for a VM
func getVMUnitName(vmName string) string {
    projectName := getProjectName()
    // Sanitize names for systemd (replace invalid characters)
    sanitizedProject := strings.ReplaceAll(projectName, " ", "-")
    sanitizedVM := strings.ReplaceAll(vmName, " ", "-")
    return fmt.Sprintf("qemu-compose-%s-%s", sanitizedProject, sanitizedVM)
}

// getConsoleSocketPath returns the path to the console Unix socket
func getConsoleSocketPath(vmName string) string {
    instanceDir, err := getInstanceDir(vmName)
    if err != nil {
        // Fallback to /tmp if we can't get instance dir
        return fmt.Sprintf("/tmp/qemu-compose-%s-console.sock", vmName)
    }
    return filepath.Join(instanceDir, "console.sock")
}

// isPortAvailable checks if a TCP port is available
func isPortAvailable(port int) bool {
    addr := fmt.Sprintf("127.0.0.1:%d", port)
    listener, err := net.Listen("tcp", addr)
    if err != nil {
        return false
    }
    listener.Close()
    return true
}

// getAllocatedPorts returns a map of all ports already allocated to VMs in this project
func getAllocatedPorts() map[int]string {
    allocatedPorts := make(map[int]string)
    
    cwd, err := os.Getwd()
    if err != nil {
        logger.Printf("Warning: could not get current directory: %v", err)
        return allocatedPorts
    }
    
    qemuComposeDir := filepath.Join(cwd, ".qemu-compose")
    
    // Check if .qemu-compose directory exists
    if _, err := os.Stat(qemuComposeDir); os.IsNotExist(err) {
        return allocatedPorts
    }
    
    // Read all VM directories
    entries, err := os.ReadDir(qemuComposeDir)
    if err != nil {
        logger.Printf("Warning: could not read .qemu-compose directory: %v", err)
        return allocatedPorts
    }
    
    // Scan each VM directory for port metadata
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        
        vmName := entry.Name()
        
        // Skip the ssh directory
        if vmName == "ssh" {
            continue
        }
        
        // Try to load port metadata for this VM
        portMetadata, err := loadPortMetadata(vmName)
        if err != nil || portMetadata == nil {
            continue
        }
        
        if portMetadata.SSH > 0 {
            allocatedPorts[portMetadata.SSH] = vmName
            logger.Printf("Found allocated port %d for VM: %s", portMetadata.SSH, vmName)
        }
    }
    
    return allocatedPorts
}

// allocateSSHPort allocates an SSH port for a VM
func allocateSSHPort(vmName string, vm VM) (int, error) {
    // Check if user specified a manual port
    if vm.SSH != nil && vm.SSH.Port > 0 {
        logger.Printf("Using manual SSH port: %d", vm.SSH.Port)
        if !isPortAvailable(vm.SSH.Port) {
            return 0, fmt.Errorf("specified SSH port %d is already in use", vm.SSH.Port)
        }
        return vm.SSH.Port, nil
    }
    
    // Try to load existing port allocation for this VM
    portMetadata, err := loadPortMetadata(vmName)
    if err != nil {
        logger.Printf("Warning: could not load port metadata: %v", err)
    }
    
    if portMetadata != nil && portMetadata.SSH > 0 {
        // Verify the port is still available
        if isPortAvailable(portMetadata.SSH) {
            logger.Printf("Reusing existing SSH port: %d", portMetadata.SSH)
            return portMetadata.SSH, nil
        }
        logger.Printf("Previously allocated port %d is no longer available", portMetadata.SSH)
    }
    
    // Get all currently allocated ports in the project
    allocatedPorts := getAllocatedPorts()
    
    // Allocate a new port starting from 2222
    const startPort = 2222
    const maxPort = 2322 // Allow up to 100 VMs
    
    for port := startPort; port <= maxPort; port++ {
        // Skip if port is already allocated to another VM
        if existingVM, exists := allocatedPorts[port]; exists {
            logger.Printf("Port %d already allocated to VM: %s", port, existingVM)
            continue
        }
        
        // Check if port is available on the network
        if isPortAvailable(port) {
            logger.Printf("Allocated new SSH port: %d", port)
            
            // Save the allocation
            metadata := &PortMetadata{SSH: port}
            if err := savePortMetadata(vmName, metadata); err != nil {
                logger.Printf("Warning: could not save port metadata: %v", err)
            }
            
            return port, nil
        }
    }
    
    return 0, fmt.Errorf("no available ports in range %d-%d", startPort, maxPort)
}

// getSSHPort retrieves the allocated SSH port for a VM
func getSSHPort(vmName string) (int, error) {
    portMetadata, err := loadPortMetadata(vmName)
    if err != nil {
        return 0, err
    }
    
    if portMetadata == nil || portMetadata.SSH == 0 {
        return 0, fmt.Errorf("no SSH port allocated")
    }
    
    return portMetadata.SSH, nil
}

// generateMACAddress generates a unique MAC address for a VM network interface
func generateMACAddress(vmName string, networkIndex int) string {
    // Use a hash of the project name, VM name, and network index
    projectName := getProjectName()
    identifier := fmt.Sprintf("%s-%s-%d", projectName, vmName, networkIndex)
    
    // Generate MD5 hash
    hash := md5.Sum([]byte(identifier))
    
    // Use QEMU's OUI prefix (52:54:00) and 3 bytes from the hash
    // This ensures the MAC is in QEMU's range and unique per VM
    return fmt.Sprintf("52:54:00:%02x:%02x:%02x", hash[0], hash[1], hash[2])
}

// VMVolumeMount represents a volume mount for a VM
type VMVolumeMount struct {
    VolumeName   string
    MountPath    string
    ReadOnly     bool
    Automount    bool
    MountOptions string
    IsBindMount  bool
    HostPath     string  // For bind mounts
    DiskPath     string  // For named volumes
}

// parseVMVolumes parses volume specifications for a VM
func parseVMVolumes(vmName string, vm VM, config *ComposeConfig, composeFilePath string) ([]VMVolumeMount, error) {
    var mounts []VMVolumeMount
    
    for _, volumeMount := range vm.Volumes {
        // Validate target path
        if !strings.HasPrefix(volumeMount.Target, "/") {
            return nil, fmt.Errorf("invalid mount path for VM %s: %s (must be absolute path)", vmName, volumeMount.Target)
        }
        
        // Determine automount setting (default: true)
        automount := true
        if volumeMount.Automount != nil {
            automount = *volumeMount.Automount
        }
        
        // Check if this is a bind mount or named volume
        if isBindMount(volumeMount.Source) {
            // Bind mount
            hostPath, err := resolveBindMountPath(volumeMount.Source, composeFilePath)
            if err != nil {
                return nil, fmt.Errorf("failed to resolve bind mount path: %w", err)
            }
            
            mounts = append(mounts, VMVolumeMount{
                VolumeName:   volumeMount.Source,
                MountPath:    volumeMount.Target,
                ReadOnly:     volumeMount.ReadOnly,
                Automount:    automount,
                MountOptions: volumeMount.MountOptions,
                IsBindMount:  true,
                HostPath:     hostPath,
            })
            
            logger.Printf("Parsed bind mount for VM %s: %s -> %s (ro=%v, automount=%v)", vmName, hostPath, volumeMount.Target, volumeMount.ReadOnly, automount)
        } else {
            // Named volume
            // Ensure volume exists
            if err := ensureVolumeExists(volumeMount.Source, config); err != nil {
                return nil, fmt.Errorf("failed to ensure volume exists: %w", err)
            }
            
            // Get volume disk path
            diskPath, err := getVolumeDiskPath(volumeMount.Source)
            if err != nil {
                return nil, fmt.Errorf("failed to get volume disk path: %w", err)
            }
            
            // Named volumes are always auto-mounted (ignore automount setting)
            mounts = append(mounts, VMVolumeMount{
                VolumeName:  volumeMount.Source,
                MountPath:   volumeMount.Target,
                ReadOnly:    volumeMount.ReadOnly,
                Automount:   true, // Always true for named volumes
                IsBindMount: false,
                DiskPath:    diskPath,
            })
            
            logger.Printf("Parsed named volume for VM %s: %s -> %s (ro=%v)", vmName, volumeMount.Source, volumeMount.Target, volumeMount.ReadOnly)
        }
    }
    
    return mounts, nil
}

// buildQEMUCommand builds the QEMU command line arguments
func buildQEMUCommand(vmName string, vm VM, instanceDiskPath string, cloudInitISOPath string, sshPort int, volumeMounts []VMVolumeMount) []string {
    // Get console socket path
    socketPath := getConsoleSocketPath(vmName)
    
    args := []string{
        "qemu-system-x86_64",
        "-name", vmName,
        "-m", fmt.Sprintf("%d", vm.Memory),
        "-smp", fmt.Sprintf("%d", vm.CPU),
        "-drive", fmt.Sprintf("file=%s,format=qcow2,if=virtio", instanceDiskPath),
        "-nographic",
        "-serial", fmt.Sprintf("unix:%s,server,nowait", socketPath),
    }
    
    // Add volume disks and bind mounts
    virtfsIndex := 0
    for _, mount := range volumeMounts {
        if mount.IsBindMount {
            // Use 9p virtfs for bind mounts
            mountTag := fmt.Sprintf("mount%d", virtfsIndex)
            virtfsIndex++
            
            args = append(args, "-virtfs", fmt.Sprintf("local,path=%s,mount_tag=%s,security_model=passthrough,id=%s", mount.HostPath, mountTag, mountTag))
            logger.Printf("Added 9p bind mount to QEMU command: %s (tag: %s)", mount.HostPath, mountTag)
        } else {
            // Use virtio-blk for named volumes
            args = append(args, "-drive", fmt.Sprintf("file=%s,format=qcow2,if=virtio", mount.DiskPath))
            logger.Printf("Added volume disk to QEMU command: %s", mount.DiskPath)
        }
    }
    
    // Add network configuration
    if len(vm.Networks) > 0 {
        // Use TAP/bridge networking for VM-to-VM communication
        logger.Printf("Configuring TAP/bridge networking for VM: %s", vmName)
        for i, networkName := range vm.Networks {
            tapName := getTAPName(vmName, i)
            macAddr := generateMACAddress(vmName, i)
            args = append(args,
                "-netdev", fmt.Sprintf("tap,id=net%d,ifname=%s,script=no,downscript=no", i, tapName),
                "-device", fmt.Sprintf("virtio-net-pci,netdev=net%d,mac=%s", i, macAddr),
            )
            logger.Printf("Added TAP network interface: %s (network: %s, MAC: %s)", tapName, networkName, macAddr)
        }
        
        // ALSO add user-mode networking for SSH access from host
        if sshPort > 0 {
            netIndex := len(vm.Networks) // Use next available network index
            macAddr := generateMACAddress(vmName, netIndex)
            args = append(args,
                "-netdev", fmt.Sprintf("user,id=net%d,hostfwd=tcp:127.0.0.1:%d-:22", netIndex, sshPort),
                "-device", fmt.Sprintf("virtio-net-pci,netdev=net%d,mac=%s", netIndex, macAddr),
            )
            logger.Printf("Added user-mode network for SSH access: port %d (MAC: %s)", sshPort, macAddr)
        }
    } else {
        // Use user-mode networking only (default)
        logger.Printf("Configuring user-mode networking for VM: %s", vmName)
        if sshPort > 0 {
            macAddr := generateMACAddress(vmName, 0)
            args = append(args,
                "-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", sshPort),
                "-device", fmt.Sprintf("virtio-net-pci,netdev=net0,mac=%s", macAddr),
            )
            logger.Printf("Added user-mode network with SSH port forwarding: %d (MAC: %s)", sshPort, macAddr)
        }
    }
    
    // Add cloud-init ISO if it exists
    if cloudInitISOPath != "" {
        args = append(args, "-drive", fmt.Sprintf("file=%s,format=raw,if=virtio,media=cdrom", cloudInitISOPath))
    }
    
    return args
}

// startVM starts a VM using systemd-run
func startVM(vmName string, vm VM, instanceDiskPath string, config *ComposeConfig, composeFilePath string) error {
    logger.Printf("Starting VM: %s", vmName)
    
    // Setup networks if configured
    if len(vm.Networks) > 0 {
        logger.Printf("VM %s uses bridge networking, setting up network infrastructure", vmName)
        if err := setupVMNetworks(vmName, vm, config); err != nil {
            return fmt.Errorf("failed to setup networks: %w", err)
        }
    }
    
    // Parse and setup volumes
    volumeMounts, err := parseVMVolumes(vmName, vm, config, composeFilePath)
    if err != nil {
        return fmt.Errorf("failed to parse volumes: %w", err)
    }
    
    // Allocate SSH port for all VMs (needed for SSH access)
    sshPort, err := allocateSSHPort(vmName, vm)
    if err != nil {
        return fmt.Errorf("failed to allocate SSH port: %w", err)
    }
    
    // Generate MAC addresses for all network interfaces
    var macAddresses []string
    
    // Add MAC addresses for bridge networks
    for i := range vm.Networks {
        macAddr := generateMACAddress(vmName, i)
        macAddresses = append(macAddresses, macAddr)
    }
    
    // Add MAC address for user-mode networking (SSH access)
    if sshPort > 0 {
        netIndex := len(vm.Networks)
        macAddr := generateMACAddress(vmName, netIndex)
        macAddresses = append(macAddresses, macAddr)
    }
    
    // Generate cloud-init ISO with MAC-based network configuration and volume mounts
    cloudInitISOPath, err := generateCloudInitISOWithVolumes(vmName, vm.Image, macAddresses, volumeMounts)
    if err != nil {
        logger.Printf("Warning: failed to generate cloud-init ISO: %v", err)
        cloudInitISOPath = "" // Continue without cloud-init
    }
    
    unitName := getVMUnitName(vmName)
    qemuArgs := buildQEMUCommand(vmName, vm, instanceDiskPath, cloudInitISOPath, sshPort, volumeMounts)
    
    // Build systemd-run command
    systemdArgs := []string{
        "systemd-run",
        "--user",
        "--unit=" + unitName,
        "--description=" + fmt.Sprintf("qemu-compose VM: %s", vmName),
        "--collect",
        "--property=KillMode=mixed",
        "--property=Type=simple",
    }
    
    // Append QEMU command
    systemdArgs = append(systemdArgs, qemuArgs...)
    
    logger.Printf("Executing: %s", strings.Join(systemdArgs, " "))
    
    cmd := exec.Command(systemdArgs[0], systemdArgs[1:]...)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("failed to start VM: %w\nOutput: %s", err, string(output))
    }
    
    logger.Printf("VM started successfully: %s (unit: %s)", vmName, unitName)
    return nil
}

// isVMRunning checks if a VM is currently running
func isVMRunning(vmName string) (bool, error) {
    unitName := getVMUnitName(vmName)
    
    cmd := exec.Command("systemctl", "--user", "is-active", unitName)
    output, err := cmd.Output()
    
    if err != nil {
        // Unit doesn't exist or is not active
        return false, nil
    }
    
    status := strings.TrimSpace(string(output))
    return status == "active", nil
}

// stopVM stops a running VM
func stopVM(vmName string, vm VM) error {
    logger.Printf("Stopping VM: %s", vmName)
    
    unitName := getVMUnitName(vmName)
    
    cmd := exec.Command("systemctl", "--user", "stop", unitName)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("failed to stop VM: %w\nOutput: %s", err, string(output))
    }
    
    // Cleanup network infrastructure if VM uses bridge networking
    if len(vm.Networks) > 0 {
        logger.Printf("Cleaning up network infrastructure for VM: %s", vmName)
        if err := cleanupVMNetworks(vmName, vm); err != nil {
            logger.Printf("Warning: failed to cleanup networks: %v", err)
        }
    }
    
    logger.Printf("VM stopped successfully: %s", vmName)
    return nil
}

// vmInstanceExists checks if a VM instance has been created (disk exists)
func vmInstanceExists(vmName string) bool {
    instanceDir, err := getInstanceDir(vmName)
    if err != nil {
        return false
    }
    
    diskPath := filepath.Join(instanceDir, "disk.qcow2")
    _, err = os.Stat(diskPath)
    return err == nil
}

// isVMReady checks if a VM is ready by testing SSH connectivity
func isVMReady(vmName string, imageURL string) bool {
    logger.Printf("Checking SSH readiness for VM: %s", vmName)
    
    // Get SSH port
    sshPort, err := getSSHPort(vmName)
    if err != nil {
        logger.Printf("Could not get SSH port for VM %s: %v", vmName, err)
        return false
    }
    
    // Get SSH key path
    cwd, err := os.Getwd()
    if err != nil {
        logger.Printf("Could not get current directory: %v", err)
        return false
    }
    
    sshKeyPath := filepath.Join(cwd, ".qemu-compose", "ssh", "id_ed25519")
    
    // Check if SSH key exists
    if _, err := os.Stat(sshKeyPath); os.IsNotExist(err) {
        logger.Printf("SSH key not found: %s", sshKeyPath)
        return false
    }
    
    // Detect default user for the OS
    defaultUser := getDefaultUserForOS(detectOSFromImage(imageURL))
    
    // Quick SSH connectivity test
    cmd := exec.Command("ssh",
        "-i", sshKeyPath,
        "-p", fmt.Sprintf("%d", sshPort),
        "-o", "ConnectTimeout=2",
        "-o", "BatchMode=yes",
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
        fmt.Sprintf("%s@localhost", defaultUser),
        "exit",
    )
    
    err = cmd.Run()
    if err != nil {
        logger.Printf("SSH not ready for VM %s: %v", vmName, err)
        return false
    }
    
    logger.Printf("SSH is ready for VM: %s", vmName)
    return true
}

// getVMStatus returns the status of a VM
func getVMStatus(vmName string, imageURL string) (string, error) {
    // First check if the VM instance has been created
    if !vmInstanceExists(vmName) {
        return "not-created", nil
    }
    
    unitName := getVMUnitName(vmName)
    
    cmd := exec.Command("systemctl", "--user", "show", unitName, "--property=ActiveState", "--value")
    output, err := cmd.Output()
    
    if err != nil {
        return "unknown", nil
    }
    
    status := strings.TrimSpace(string(output))
    
    // If systemd says "inactive" but the disk exists, it means the VM was created but is stopped
    if status == "inactive" {
        return "stopped", nil
    }
    
    // If VM is active, check if SSH is ready
    if status == "active" {
        if isVMReady(vmName, imageURL) {
            return "ready", nil
        }
        return "starting", nil
    }
    
    return status, nil
}
