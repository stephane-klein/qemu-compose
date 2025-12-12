package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "sync"
    "time"

    "github.com/spf13/cobra"
    "github.com/vishvananda/netlink"
    "gopkg.in/yaml.v3"
)

var composeFile string
var debug bool
var logger *log.Logger

// loadComposeFile reads and parses the qemu-compose.yaml file
func loadComposeFile(path string) (*ComposeConfig, error) {
    logger.Printf("Loading compose file: %s", path)
    
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read compose file: %w", err)
    }
    
    var config ComposeConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("failed to parse compose file: %w", err)
    }
    
    logger.Printf("Successfully parsed compose file (version: %s, VMs: %d)", config.Version, len(config.VMs))
    
    return &config, nil
}

// filterVMs returns a map of VMs filtered by the provided VM names
// If vmNames is empty, returns all VMs
func filterVMs(config *ComposeConfig, vmNames []string) (map[string]VM, error) {
    if len(vmNames) == 0 {
        return config.VMs, nil
    }
    
    filtered := make(map[string]VM)
    for _, vmName := range vmNames {
        vm, exists := config.VMs[vmName]
        if !exists {
            return nil, fmt.Errorf("VM not found in compose file: %s", vmName)
        }
        filtered[vmName] = vm
    }
    
    return filtered, nil
}

// getVMNames returns a list of VM names from the compose file for auto-completion
func getVMNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    // Try to find compose file
    composeFilePath := composeFile
    if composeFilePath == "" {
        if envFile := os.Getenv("QEMU_COMPOSE_FILE"); envFile != "" {
            composeFilePath = envFile
        } else if _, err := os.Stat("qemu-compose.yaml"); err == nil {
            composeFilePath = "qemu-compose.yaml"
        } else if _, err := os.Stat("qemu-compose.yml"); err == nil {
            composeFilePath = "qemu-compose.yml"
        } else {
            return nil, cobra.ShellCompDirectiveNoFileComp
        }
    }
    
    // Load compose file
    config, err := loadComposeFile(composeFilePath)
    if err != nil {
        return nil, cobra.ShellCompDirectiveNoFileComp
    }
    
    // Extract VM names
    vmNames := make([]string, 0, len(config.VMs))
    for vmName := range config.VMs {
        vmNames = append(vmNames, vmName)
    }
    
    return vmNames, cobra.ShellCompDirectiveNoFileComp
}

// getNetworkNames returns a list of network names from the compose file for auto-completion
func getNetworkNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    // Try to find compose file
    composeFilePath := composeFile
    if composeFilePath == "" {
        if envFile := os.Getenv("QEMU_COMPOSE_FILE"); envFile != "" {
            composeFilePath = envFile
        } else if _, err := os.Stat("qemu-compose.yaml"); err == nil {
            composeFilePath = "qemu-compose.yaml"
        } else if _, err := os.Stat("qemu-compose.yml"); err == nil {
            composeFilePath = "qemu-compose.yml"
        } else {
            return nil, cobra.ShellCompDirectiveNoFileComp
        }
    }
    
    // Load compose file
    config, err := loadComposeFile(composeFilePath)
    if err != nil {
        return nil, cobra.ShellCompDirectiveNoFileComp
    }
    
    // Extract network names
    networkNames := make([]string, 0, len(config.Networks))
    for networkName := range config.Networks {
        networkNames = append(networkNames, networkName)
    }
    
    return networkNames, cobra.ShellCompDirectiveNoFileComp
}

var rootCmd = &cobra.Command{
    Use:   "qemu-compose",
    Short: "A docker-compose equivalent for QEMU VMs",
    Long:  `qemu-compose is a CLI tool to orchestrate QEMU virtual machines using a declarative YAML configuration.`,
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // Initialize logger
        if debug || strings.ToLower(os.Getenv("QEMU_COMPOSE_DEBUG")) == "true" || os.Getenv("QEMU_COMPOSE_DEBUG") == "1" {
            logger = log.New(os.Stderr, "[DEBUG] ", log.Ldate|log.Ltime|log.Lshortfile)
            logger.Println("Debug mode enabled")
        } else {
            logger = log.New(io.Discard, "", 0)
        }
        
        // Skip file detection for commands that don't need it
        if cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "doctor" || cmd.Name() == "ls" || cmd.Name() == "version" {
            logger.Printf("Skipping compose file detection for command: %s", cmd.Name())
            return nil
        }
        
        // If -f flag was not provided, try environment variable or default files
        if composeFile == "" {
            logger.Println("No compose file specified via -f flag")
            
            // Check QEMU_COMPOSE_FILE environment variable
            if envFile := os.Getenv("QEMU_COMPOSE_FILE"); envFile != "" {
                composeFile = envFile
                logger.Printf("Using compose file from QEMU_COMPOSE_FILE: %s", composeFile)
            } else {
                // Try to find default files
                logger.Println("QEMU_COMPOSE_FILE not set, searching for default files")
                if _, err := os.Stat("qemu-compose.yaml"); err == nil {
                    composeFile = "qemu-compose.yaml"
                    logger.Printf("Found default compose file: %s", composeFile)
                } else if _, err := os.Stat("qemu-compose.yml"); err == nil {
                    composeFile = "qemu-compose.yml"
                    logger.Printf("Found default compose file: %s", composeFile)
                } else {
                    return fmt.Errorf("no qemu-compose.yaml or qemu-compose.yml found in current directory")
                }
            }
        } else {
            logger.Printf("Using specified compose file: %s", composeFile)
        }
        
        // Verify the specified file exists
        if _, err := os.Stat(composeFile); os.IsNotExist(err) {
            return fmt.Errorf("compose file not found: %s", composeFile)
        }
        
        return nil
    },
}

var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "Display version information",
    Long:  `Display version information about qemu-compose including version number, git commit, and build date.`,
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Printf("qemu-compose version %s\n", Version)
        fmt.Printf("Git commit: %s\n", GitCommit)
        fmt.Printf("Build date: %s\n", BuildDate)
        fmt.Printf("Go version: %s\n", runtime.Version())
        fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
    },
}

var upCmd = &cobra.Command{
    Use:   "up [VM...]",
    Short: "Create and start VMs",
    Long:  `Create and start virtual machines defined in qemu-compose.yaml. If VM names are provided, only those VMs will be started.`,
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Printf("Executing 'up' command with compose file: %s", composeFile)
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        vms, err := filterVMs(config, args)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        fmt.Printf("Using compose file: %s\n", composeFile)
        fmt.Printf("Project: %s\n", getProjectName())
        if len(args) > 0 {
            fmt.Printf("Starting %d VM(s): %s\n\n", len(vms), strings.Join(args, ", "))
        } else {
            fmt.Printf("Starting %d VM(s)...\n\n", len(vms))
        }
        
        hasError := false
        for vmName, vm := range vms {
            fmt.Printf("VM: %s\n", vmName)
            
            // Only process VMs with URL-based images
            if !isValidImageURL(vm.Image) {
                logger.Printf("Skipping VM '%s': image is not a URL: %s", vmName, vm.Image)
                fmt.Printf("  ⚠ Skipping: image is not a URL\n\n")
                continue
            }
            
            // Check if VM is already running
            running, err := isVMRunning(vmName)
            if err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error checking VM status: %v\n\n", err)
                hasError = true
                continue
            }
            
            if running {
                fmt.Printf("  ⚠ VM is already running\n\n")
                continue
            }
            
            // Get base image path
            baseImagePath, err := getBaseImagePath(vm.Image)
            if err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error: %v\n\n", err)
                hasError = true
                continue
            }
            logger.Printf("Base image: %s", baseImagePath)
            
            // Create instance disk
            instanceDiskPath, err := createInstanceDisk(vmName, baseImagePath, vm.Disk)
            if err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error creating instance disk: %v\n\n", err)
                hasError = true
                continue
            }
            logger.Printf("Instance disk: %s", instanceDiskPath)
            
            // Get absolute path to compose file
            absComposeFile, err := filepath.Abs(composeFile)
            if err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error resolving compose file path: %v\n\n", err)
                hasError = true
                continue
            }
            
            // Start VM
            if err := startVM(vmName, vm, instanceDiskPath, config, absComposeFile); err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error starting VM: %v\n\n", err)
                hasError = true
                continue
            }
            
            fmt.Printf("  ✓ Started (unit: %s)\n", getVMUnitName(vmName))
            
            // Display connection info based on networking mode
            if len(vm.Networks) > 0 {
                fmt.Printf("  Networking: bridge mode (networks: %s)\n", strings.Join(vm.Networks, ", "))
                fmt.Printf("  Note: VM will obtain IP via DHCP on the bridge network\n")
            } else {
                // Get SSH port for display (user-mode networking)
                sshPort, err := getSSHPort(vmName)
                if err != nil {
                    logger.Printf("Warning: could not get SSH port: %v", err)
                } else {
                    defaultUser := getDefaultUserForOS(detectOSFromImage(vm.Image))
                    fmt.Printf("  SSH: ssh -i .qemu-compose/ssh/id_ed25519 -p %d %s@localhost\n", sshPort, defaultUser)
                }
            }
            
            fmt.Printf("  View logs: journalctl --user -u %s -f\n", getVMUnitName(vmName))
            fmt.Printf("  Attach to console: qemu-compose console %s\n\n", vmName)
        }
        
        if hasError {
            os.Exit(1)
        }
        
        fmt.Println("✓ All VMs started successfully")
    },
}

var stopCmd = &cobra.Command{
    Use:   "stop [VM...]",
    Short: "Stop VMs",
    Long:  `Stop virtual machines defined in qemu-compose.yaml without removing instance disks. If VM names are provided, only those VMs will be stopped.`,
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Printf("Executing 'stop' command with compose file: %s", composeFile)
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        vms, err := filterVMs(config, args)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        fmt.Printf("Using compose file: %s\n", composeFile)
        fmt.Printf("Project: %s\n", getProjectName())
        if len(args) > 0 {
            fmt.Printf("Stopping %d VM(s): %s\n\n", len(vms), strings.Join(args, ", "))
        } else {
            fmt.Printf("Stopping %d VM(s)...\n\n", len(vms))
        }
        
        hasError := false
        for vmName, vm := range vms {
            fmt.Printf("VM: %s\n", vmName)
            
            // Check if VM is running
            running, err := isVMRunning(vmName)
            if err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error checking VM status: %v\n\n", err)
                hasError = true
                continue
            }
            
            if !running {
                fmt.Printf("  ⚠ VM is not running\n\n")
                continue
            }
            
            // Stop VM
            if err := stopVM(vmName, vm); err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error stopping VM: %v\n\n", err)
                hasError = true
                continue
            }
            
            fmt.Printf("  ✓ Stopped\n\n")
        }
        
        if hasError {
            os.Exit(1)
        }
        
        fmt.Println("✓ All VMs stopped successfully")
    },
}

var destroyCmd = &cobra.Command{
    Use:   "destroy [VM...]",
    Short: "Stop and remove VMs",
    Long:  `Stop virtual machines, remove their instance disks, and clean up network infrastructure (TAP devices and bridges). If VM names are provided, only those VMs will be stopped and removed.`,
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Printf("Executing 'destroy' command with compose file: %s", composeFile)
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        vms, err := filterVMs(config, args)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        fmt.Printf("Using compose file: %s\n", composeFile)
        fmt.Printf("Project: %s\n", getProjectName())
        if len(args) > 0 {
            fmt.Printf("Stopping and removing %d VM(s): %s\n\n", len(vms), strings.Join(args, ", "))
        } else {
            fmt.Printf("Stopping and removing %d VM(s)...\n\n", len(vms))
        }
        
        hasError := false
        for vmName, vm := range vms {
            fmt.Printf("VM: %s\n", vmName)
            
            // Check if VM is running
            running, err := isVMRunning(vmName)
            if err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error checking VM status: %v\n\n", err)
                hasError = true
                continue
            }
            
            // Stop VM if running
            if running {
                if err := stopVM(vmName, vm); err != nil {
                    fmt.Fprintf(os.Stderr, "  ✗ Error stopping VM: %v\n\n", err)
                    hasError = true
                    continue
                }
                fmt.Printf("  ✓ Stopped\n")
            } else {
                fmt.Printf("  ⚠ VM was not running\n")
            }
            
            // Clean up network infrastructure (TAP devices)
            if len(vm.Networks) > 0 {
                if err := cleanupVMNetworks(vmName, vm); err != nil {
                    fmt.Fprintf(os.Stderr, "  ✗ Error cleaning up networks: %v\n", err)
                    hasError = true
                } else {
                    fmt.Printf("  ✓ Network infrastructure cleaned up\n")
                }
            }
            
            // Remove instance disk
            if err := removeInstanceDisk(vmName); err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Error removing instance disk: %v\n\n", err)
                hasError = true
                continue
            }
            fmt.Printf("  ✓ Instance disk removed\n\n")
        }
        
        // If destroying all VMs, also clean up bridges and dnsmasq
        if len(args) == 0 {
            fmt.Println("Cleaning up project network infrastructure...")
            
            // Collect networks that are no longer used
            networksToCleanup := make(map[string]bool)
            for networkName := range config.Networks {
                networksToCleanup[networkName] = true
            }
            
            // Clean up bridges and dnsmasq for unused networks
            for networkName := range networksToCleanup {
                if err := deleteBridge(networkName); err != nil {
                    fmt.Fprintf(os.Stderr, "  ✗ Failed to delete bridge for network %s: %v\n", networkName, err)
                    hasError = true
                } else {
                    bridgeName := getBridgeName(networkName)
                    fmt.Printf("  ✓ Deleted bridge: %s (network: %s)\n", bridgeName, networkName)
                }
            }
            
            // Remove network metadata
            metadata, err := loadNetworkMetadata()
            if err != nil {
                logger.Printf("Warning: could not load network metadata: %v", err)
            } else {
                // Remove all network entries
                for networkName := range networksToCleanup {
                    delete(metadata, networkName)
                }
                
                if err := saveNetworkMetadata(metadata); err != nil {
                    fmt.Fprintf(os.Stderr, "  ✗ Failed to update network metadata: %v\n", err)
                    hasError = true
                } else {
                    fmt.Printf("  ✓ Removed network metadata\n")
                }
            }
            
            fmt.Println()
        }
        
        if hasError {
            os.Exit(1)
        }
        
        fmt.Println("✓ All VMs stopped and removed successfully")
    },
}

// VMStatusResult holds the result of a VM status check
type VMStatusResult struct {
    VMName   string
    VM       VM
    Status   string
    DiskSize string
    IPAddr   string
    Error    error
}

var psCmd = &cobra.Command{
    Use:   "ps",
    Short: "List VMs",
    Long:  `List virtual machines and their status`,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Printf("Executing 'ps' command with compose file: %s", composeFile)
        
        wait, _ := cmd.Flags().GetBool("wait")
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        logger.Printf("Configuration loaded: %+v", config)
        logger.Printf("Number of VMs defined: %d", len(config.VMs))
        
        if wait {
            fmt.Printf("Using compose file: %s\n", composeFile)
            fmt.Printf("Project: %s\n", getProjectName())
            fmt.Println("Waiting for all VMs to be ready...")
            fmt.Println()
            
            // Wait for all VMs to be ready
            ticker := time.NewTicker(2 * time.Second)
            defer ticker.Stop()
            
            timeout := time.After(5 * time.Minute)
            
            for {
                select {
                case <-timeout:
                    fmt.Fprintf(os.Stderr, "\nError: Timeout waiting for VMs to be ready\n")
                    os.Exit(1)
                    
                case <-ticker.C:
                    allReady := true
                    statusMap := make(map[string]string)
                    
                    for vmName, vm := range config.VMs {
                        // Skip VMs without URL-based images
                        if !isValidImageURL(vm.Image) {
                            continue
                        }
                        
                        status, err := getVMStatus(vmName, vm.Image)
                        if err != nil {
                            logger.Printf("Error checking VM %s status: %v", vmName, err)
                            status = "unknown"
                        }
                        
                        statusMap[vmName] = status
                        
                        if status != "ready" && status != "active" {
                            allReady = false
                        }
                    }
                    
                    // Display current status
                    fmt.Printf("\r")
                    notReadyVMs := []string{}
                    for vmName, status := range statusMap {
                        if status != "ready" && status != "active" {
                            notReadyVMs = append(notReadyVMs, fmt.Sprintf("%s (%s)", vmName, status))
                        }
                    }
                    
                    if len(notReadyVMs) > 0 {
                        fmt.Printf("Waiting for: %s", strings.Join(notReadyVMs, ", "))
                    }
                    
                    if allReady {
                        fmt.Println("\n\n✓ All VMs are ready")
                        fmt.Println()
                        break
                    }
                }
                
                // Check if we broke out of the select
                allReady := true
                for vmName, vm := range config.VMs {
                    if !isValidImageURL(vm.Image) {
                        continue
                    }
                    
                    status, err := getVMStatus(vmName, vm.Image)
                    if err != nil || (status != "ready" && status != "active") {
                        allReady = false
                        break
                    }
                }
                
                if allReady {
                    break
                }
            }
        }
        
        fmt.Printf("Using compose file: %s\n", composeFile)
        fmt.Printf("Project: %s\n\n", getProjectName())
        fmt.Printf("%-20s %-15s %-15s %-10s %-10s %-10s %s\n", "NAME", "STATUS", "IP ADDRESS", "CPU", "MEMORY", "DISK", "SYSTEMD UNIT")
        fmt.Println(strings.Repeat("-", 120))
        
        // Use goroutines to check VM statuses in parallel
        var wg sync.WaitGroup
        results := make(chan VMStatusResult, len(config.VMs))
        
        for vmName, vm := range config.VMs {
            wg.Add(1)
            go func(name string, vmConfig VM) {
                defer wg.Done()
                
                result := VMStatusResult{
                    VMName: name,
                    VM:     vmConfig,
                    IPAddr: "-",
                }
                
                // Get VM status
                status, err := getVMStatus(name, vmConfig.Image)
                if err != nil {
                    result.Status = "unknown"
                    result.Error = err
                } else {
                    result.Status = status
                }
                
                // Get disk size
                if result.Status == "not-created" {
                    result.DiskSize = "-"
                } else {
                    metadata, err := loadDiskMetadata(name)
                    if err != nil || metadata == nil {
                        result.DiskSize = "unknown"
                    } else {
                        result.DiskSize = metadata.Size
                    }
                }
                
                // Get IP address for bridge networking VMs
                if len(vmConfig.Networks) > 0 && (result.Status == "ready" || result.Status == "starting" || result.Status == "active") {
                    if ip := getVMIPAddress(name, vmConfig); ip != "" {
                        result.IPAddr = ip
                    }
                }
                
                results <- result
            }(vmName, vm)
        }
        
        // Close results channel when all goroutines complete
        go func() {
            wg.Wait()
            close(results)
        }()
        
        // Collect results into a map to preserve order
        statusMap := make(map[string]VMStatusResult)
        for result := range results {
            statusMap[result.VMName] = result
        }
        
        // Display results in the original order from config
        for vmName, vm := range config.VMs {
            result := statusMap[vmName]
            
            var unitName string
            if result.Status == "not-created" {
                unitName = "-"
            } else {
                unitName = getVMUnitName(vmName)
            }
            
            fmt.Printf("%-20s %-15s %-15s %-10d %-10d %-10s %s\n", 
                vmName, 
                result.Status,
                result.IPAddr,
                vm.CPU, 
                vm.Memory,
                result.DiskSize,
                unitName,
            )
        }
    },
}

var inspectCmd = &cobra.Command{
    Use:   "inspect <vm-name>",
    Short: "Display detailed information about a VM",
    Long:  `Display detailed information about a VM including its configuration, status, networks, volumes, image, and runtime information.`,
    Args:  cobra.ExactArgs(1),
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        vmName := args[0]
        
        logger.Printf("Executing 'inspect' command for VM: %s", vmName)
        
        outputFormat, _ := cmd.Flags().GetString("format")
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        // Check if VM exists in config
        vm, exists := config.VMs[vmName]
        if !exists {
            fmt.Fprintf(os.Stderr, "Error: VM not found in compose file: %s\n", vmName)
            os.Exit(1)
        }
        
        // Gather all information about the VM
        inspectData := make(map[string]interface{})
        
        // Basic information
        inspectData["name"] = vmName
        inspectData["project"] = getProjectName()
        
        // Configuration
        inspectData["cpu"] = vm.CPU
        inspectData["memory"] = vm.Memory
        inspectData["image"] = vm.Image
        
        // Detect OS type
        osType := detectOSFromImage(vm.Image)
        inspectData["os_type"] = osType
        inspectData["default_user"] = getDefaultUserForOS(osType)
        
        // Status
        status, err := getVMStatus(vmName, vm.Image)
        if err != nil {
            inspectData["status"] = "unknown"
            inspectData["status_error"] = err.Error()
        } else {
            inspectData["status"] = status
        }
        
        // Systemd unit
        if status != "not-created" {
            inspectData["systemd_unit"] = getVMUnitName(vmName)
        }
        
        // Disk information
        if status != "not-created" {
            diskMetadata, err := loadDiskMetadata(vmName)
            if err == nil && diskMetadata != nil {
                inspectData["disk_size"] = diskMetadata.Size
            }
            
            instanceDir, err := getInstanceDir(vmName)
            if err == nil {
                instanceDiskPath := filepath.Join(instanceDir, "disk.qcow2")
                if _, err := os.Stat(instanceDiskPath); err == nil {
                    inspectData["disk_path"] = instanceDiskPath
                }
                
                cloudInitPath := filepath.Join(instanceDir, "cloud-init.iso")
                if _, err := os.Stat(cloudInitPath); err == nil {
                    inspectData["cloud_init_iso"] = cloudInitPath
                }
            }
        }
        
        // Base image information
        if isValidImageURL(vm.Image) {
            baseImagePath, err := getBaseImagePath(vm.Image)
            if err == nil {
                inspectData["base_image_path"] = baseImagePath
            }
        }
        
        // Network information
        if len(vm.Networks) > 0 {
            networkInfo := make([]map[string]interface{}, 0)
            networkMetadata, _ := loadNetworkMetadata()
            
            for i, networkName := range vm.Networks {
                netInfo := make(map[string]interface{})
                netInfo["name"] = networkName
                netInfo["index"] = i
                
                // Get network configuration
                if netConfig, exists := config.Networks[networkName]; exists {
                    driver := netConfig.Driver
                    if driver == "" {
                        driver = "bridge"
                    }
                    netInfo["driver"] = driver
                }
                
                // Get bridge name
                bridgeName := getBridgeName(networkName)
                netInfo["bridge"] = bridgeName
                
                // Get TAP device name
                tapName := getTAPName(vmName, i)
                netInfo["tap_device"] = tapName
                
                // Check if TAP exists
                if _, err := netlink.LinkByName(tapName); err == nil {
                    netInfo["tap_exists"] = true
                } else {
                    netInfo["tap_exists"] = false
                }
                
                // Get subnet information
                if meta, exists := networkMetadata[networkName]; exists {
                    if meta.Subnet != "" {
                        netInfo["subnet"] = meta.Subnet
                    }
                    if meta.DnsmasqActive {
                        netInfo["dhcp_enabled"] = true
                        netInfo["dhcp_running"] = isDnsmasqRunning(networkName)
                    }
                }
                
                networkInfo = append(networkInfo, netInfo)
            }
            
            inspectData["networks"] = networkInfo
            
            // Get IP address
            if status == "ready" || status == "starting" || status == "active" {
                if ip := getVMIPAddress(vmName, vm); ip != "" {
                    inspectData["ip_address"] = ip
                }
            }
        } else {
            inspectData["networking_mode"] = "user-mode"
            
            // Get SSH port for user-mode networking
            if status != "not-created" {
                sshPort, err := getSSHPort(vmName)
                if err == nil {
                    inspectData["ssh_port"] = sshPort
                }
            }
        }
        
        // Port mappings
        if len(vm.Ports) > 0 {
            inspectData["ports"] = vm.Ports
        }
        
        // Volume information
        if len(vm.Volumes) > 0 {
            volumeInfo := make([]map[string]interface{}, 0)
            volumeMetadata, _ := loadVolumeMetadata()
            
            for _, volMount := range vm.Volumes {
                volInfo := make(map[string]interface{})
                volInfo["source"] = volMount.Source
                volInfo["target"] = volMount.Target
                volInfo["read_only"] = volMount.ReadOnly
                
                if volMount.Automount != nil {
                    volInfo["automount"] = *volMount.Automount
                } else {
                    volInfo["automount"] = true
                }
                
                if volMount.MountOptions != "" {
                    volInfo["mount_options"] = volMount.MountOptions
                }
                
                // Check if it's a named volume or bind mount
                if strings.HasPrefix(volMount.Source, "/") || strings.HasPrefix(volMount.Source, "./") || strings.HasPrefix(volMount.Source, "../") {
                    volInfo["type"] = "bind"
                    absPath, err := filepath.Abs(volMount.Source)
                    if err == nil {
                        volInfo["host_path"] = absPath
                    }
                } else {
                    volInfo["type"] = "volume"
                    volInfo["volume_name"] = volMount.Source
                    
                    // Get volume metadata
                    if meta, exists := volumeMetadata[volMount.Source]; exists {
                        volInfo["volume_size"] = meta.Size
                        volInfo["volume_disk_path"] = meta.DiskPath
                        volInfo["volume_created"] = meta.Created
                    }
                }
                
                volumeInfo = append(volumeInfo, volInfo)
            }
            
            inspectData["volumes"] = volumeInfo
        }
        
        // Environment variables
        if len(vm.Environment) > 0 {
            inspectData["environment"] = vm.Environment
        }
        
        // Provisioning scripts
        if len(vm.Provision) > 0 {
            provisionInfo := make([]map[string]interface{}, 0)
            for _, prov := range vm.Provision {
                provInfo := make(map[string]interface{})
                provInfo["type"] = prov.Type
                if prov.Inline != "" {
                    provInfo["inline"] = prov.Inline
                }
                provisionInfo = append(provisionInfo, provInfo)
            }
            inspectData["provision"] = provisionInfo
        }
        
        // Dependencies
        if len(vm.DependsOn) > 0 {
            inspectData["depends_on"] = vm.DependsOn
        }
        
        // Console socket path
        if status != "not-created" {
            inspectData["console_socket"] = getConsoleSocketPath(vmName)
        }
        
        // Output the information
        if outputFormat == "json" {
            jsonData, err := json.MarshalIndent(inspectData, "", "  ")
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error: failed to marshal JSON: %v\n", err)
                os.Exit(1)
            }
            fmt.Println(string(jsonData))
        } else {
            // Human-readable format
            fmt.Printf("VM: %s\n", vmName)
            fmt.Printf("Project: %s\n", getProjectName())
            fmt.Println(strings.Repeat("=", 80))
            fmt.Println()
            
            // Status
            fmt.Println("Status:")
            fmt.Printf("  State: %s\n", inspectData["status"])
            if unitName, ok := inspectData["systemd_unit"].(string); ok {
                fmt.Printf("  Systemd Unit: %s\n", unitName)
            }
            fmt.Println()
            
            // Configuration
            fmt.Println("Configuration:")
            fmt.Printf("  CPU: %d\n", vm.CPU)
            fmt.Printf("  Memory: %d MB\n", vm.Memory)
            fmt.Printf("  Image: %s\n", vm.Image)
            fmt.Printf("  OS Type: %s\n", osType)
            fmt.Printf("  Default User: %s\n", inspectData["default_user"])
            fmt.Println()
            
            // Disk
            fmt.Println("Disk:")
            if diskSize, ok := inspectData["disk_size"].(string); ok {
                fmt.Printf("  Size: %s\n", diskSize)
            }
            if diskPath, ok := inspectData["disk_path"].(string); ok {
                fmt.Printf("  Instance Disk: %s\n", diskPath)
            }
            if baseImagePath, ok := inspectData["base_image_path"].(string); ok {
                fmt.Printf("  Base Image: %s\n", baseImagePath)
            }
            if cloudInitPath, ok := inspectData["cloud_init_iso"].(string); ok {
                fmt.Printf("  Cloud-Init ISO: %s\n", cloudInitPath)
            }
            fmt.Println()
            
            // Networking
            if len(vm.Networks) > 0 {
                fmt.Println("Networks:")
                if networks, ok := inspectData["networks"].([]map[string]interface{}); ok {
                    for _, netInfo := range networks {
                        fmt.Printf("  - %s:\n", netInfo["name"])
                        if driver, ok := netInfo["driver"].(string); ok {
                            fmt.Printf("      Driver: %s\n", driver)
                        }
                        if bridge, ok := netInfo["bridge"].(string); ok {
                            fmt.Printf("      Bridge: %s\n", bridge)
                        }
                        if tap, ok := netInfo["tap_device"].(string); ok {
                            fmt.Printf("      TAP Device: %s\n", tap)
                        }
                        if subnet, ok := netInfo["subnet"].(string); ok {
                            fmt.Printf("      Subnet: %s\n", subnet)
                        }
                        if dhcpEnabled, ok := netInfo["dhcp_enabled"].(bool); ok && dhcpEnabled {
                            dhcpStatus := "stopped"
                            if running, ok := netInfo["dhcp_running"].(bool); ok && running {
                                dhcpStatus = "running"
                            }
                            fmt.Printf("      DHCP: %s\n", dhcpStatus)
                        }
                    }
                }
                if ipAddr, ok := inspectData["ip_address"].(string); ok {
                    fmt.Printf("  IP Address: %s\n", ipAddr)
                }
            } else {
                fmt.Println("Networking:")
                fmt.Printf("  Mode: user-mode (NAT)\n")
                if sshPort, ok := inspectData["ssh_port"].(int); ok {
                    fmt.Printf("  SSH Port: %d\n", sshPort)
                    fmt.Printf("  SSH Command: ssh -i .qemu-compose/ssh/id_ed25519 -p %d %s@localhost\n", 
                        sshPort, inspectData["default_user"])
                }
            }
            fmt.Println()
            
            // Ports
            if len(vm.Ports) > 0 {
                fmt.Println("Port Mappings:")
                for _, port := range vm.Ports {
                    fmt.Printf("  - %s\n", port)
                }
                fmt.Println()
            }
            
            // Volumes
            if len(vm.Volumes) > 0 {
                fmt.Println("Volumes:")
                if volumes, ok := inspectData["volumes"].([]map[string]interface{}); ok {
                    for _, volInfo := range volumes {
                        volType := volInfo["type"].(string)
                        fmt.Printf("  - %s -> %s (%s)\n", volInfo["source"], volInfo["target"], volType)
                        if volType == "bind" {
                            if hostPath, ok := volInfo["host_path"].(string); ok {
                                fmt.Printf("      Host Path: %s\n", hostPath)
                            }
                        } else {
                            if volSize, ok := volInfo["volume_size"].(string); ok {
                                fmt.Printf("      Size: %s\n", volSize)
                            }
                            if volDiskPath, ok := volInfo["volume_disk_path"].(string); ok {
                                fmt.Printf("      Disk Path: %s\n", volDiskPath)
                            }
                        }
                        if readOnly, ok := volInfo["read_only"].(bool); ok && readOnly {
                            fmt.Printf("      Read-Only: true\n")
                        }
                        if automount, ok := volInfo["automount"].(bool); ok && !automount {
                            fmt.Printf("      Automount: false\n")
                        }
                        if mountOpts, ok := volInfo["mount_options"].(string); ok && mountOpts != "" {
                            fmt.Printf("      Mount Options: %s\n", mountOpts)
                        }
                    }
                }
                fmt.Println()
            }
            
            // Environment
            if len(vm.Environment) > 0 {
                fmt.Println("Environment Variables:")
                for _, env := range vm.Environment {
                    fmt.Printf("  - %s\n", env)
                }
                fmt.Println()
            }
            
            // Provisioning
            if len(vm.Provision) > 0 {
                fmt.Println("Provisioning:")
                for i, prov := range vm.Provision {
                    fmt.Printf("  [%d] Type: %s\n", i+1, prov.Type)
                    if prov.Inline != "" {
                        lines := strings.Split(prov.Inline, "\n")
                        if len(lines) > 3 {
                            fmt.Printf("      Script: %d lines\n", len(lines))
                        } else {
                            fmt.Printf("      Script: %s\n", strings.TrimSpace(prov.Inline))
                        }
                    }
                }
                fmt.Println()
            }
            
            // Dependencies
            if len(vm.DependsOn) > 0 {
                fmt.Println("Dependencies:")
                for _, dep := range vm.DependsOn {
                    fmt.Printf("  - %s\n", dep)
                }
                fmt.Println()
            }
            
            // Console
            if consolePath, ok := inspectData["console_socket"].(string); ok {
                fmt.Println("Console:")
                fmt.Printf("  Socket: %s\n", consolePath)
                fmt.Printf("  Attach: qemu-compose console %s\n", vmName)
                fmt.Println()
            }
            
            // Logs
            if unitName, ok := inspectData["systemd_unit"].(string); ok {
                fmt.Println("Logs:")
                fmt.Printf("  View: journalctl --user -u %s -f\n", unitName)
            }
        }
    },
}

var pullCmd = &cobra.Command{
    Use:   "pull [VM...]",
    Short: "Pull VM images",
    Long:  `Download VM images from remote repositories to local cache. If VM names are provided, only those VM images will be pulled.`,
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Println("Executing 'pull' command")
        
        force, _ := cmd.Flags().GetBool("force")
        logger.Printf("Force flag: %v", force)
        
        cacheDir, err := getImageCacheDir()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        vms, err := filterVMs(config, args)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        if len(vms) == 0 {
            fmt.Println("No VMs defined in compose file")
            return
        }
        
        // Collect images to download
        imagesToPull := make(map[string]string) // vmName -> imageURL
        for vmName, vm := range vms {
            if isValidImageURL(vm.Image) {
                imagesToPull[vmName] = vm.Image
            } else {
                logger.Printf("Skipping VM '%s': image is not a URL: %s", vmName, vm.Image)
            }
        }
        
        if len(imagesToPull) == 0 {
            fmt.Println("No images to pull (all images must be HTTP/HTTPS URLs)")
            return
        }
        
        if len(args) > 0 {
            fmt.Printf("Pulling %d image(s) for VMs: %s\n", len(imagesToPull), strings.Join(args, ", "))
        } else {
            fmt.Printf("Pulling %d image(s) from %s\n", len(imagesToPull), composeFile)
        }
        fmt.Printf("Target directory: %s\n\n", cacheDir)
        
        // Download images
        hasError := false
        for vmName, imageURL := range imagesToPull {
            if err := downloadImage(imageURL, vmName, force); err != nil {
                fmt.Fprintf(os.Stderr, "✗ %s: %v\n", vmName, err)
                hasError = true
            }
        }
        
        if hasError {
            os.Exit(1)
        }
        
        fmt.Println("\n✓ All images pulled successfully")
    },
}

var doctorCmd = &cobra.Command{
    Use:   "doctor",
    Short: "Check system dependencies",
    Long:  `Verify that all required system dependencies (QEMU, bridge utilities, Linux kernel features) are properly installed`,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Println("Starting system dependency checks")
        fmt.Println("Checking system dependencies...\n")
        
        allOk := true
        
        // Check if running on Linux
        logger.Printf("Checking operating system: %s", runtime.GOOS)
        if runtime.GOOS != "linux" {
            fmt.Printf("❌ Operating System: %s (qemu-compose requires Linux)\n", runtime.GOOS)
            allOk = false
        } else {
            fmt.Printf("✅ Operating System: Linux\n")
        }
        
        // Check if systemd is available
        logger.Println("Checking for systemd")
        systemctlPath, err := exec.LookPath("systemctl")
        if err != nil {
            logger.Printf("systemctl not found: %v", err)
            fmt.Println("❌ systemd: not found (qemu-compose requires systemd)")
            allOk = false
        } else {
            logger.Printf("systemctl found at: %s", systemctlPath)
            fmt.Printf("✅ systemd: found at %s\n", systemctlPath)
        }
        
        // Check if systemd-run is available
        logger.Println("Checking for systemd-run")
        systemdRunPath, err := exec.LookPath("systemd-run")
        if err != nil {
            logger.Printf("systemd-run not found: %v", err)
            fmt.Println("❌ systemd-run: not found (please install systemd)")
            allOk = false
        } else {
            logger.Printf("systemd-run found at: %s", systemdRunPath)
            fmt.Printf("✅ systemd-run: found at %s\n", systemdRunPath)
        }
        
        // Check if QEMU is installed
        logger.Println("Checking for qemu-system-x86_64")
        qemuPath, err := exec.LookPath("qemu-system-x86_64")
        if err != nil {
            logger.Printf("QEMU not found: %v", err)
            fmt.Println("❌ QEMU: not found (please install qemu-system-x86_64)")
            allOk = false
        } else {
            logger.Printf("QEMU found at: %s", qemuPath)
            fmt.Printf("✅ QEMU: found at %s\n", qemuPath)
        }
        
        // Check if qemu-img is installed
        logger.Println("Checking for qemu-img")
        qemuImgPath, err := exec.LookPath("qemu-img")
        if err != nil {
            logger.Printf("qemu-img not found: %v", err)
            fmt.Println("❌ qemu-img: not found (please install qemu-img)")
            allOk = false
        } else {
            logger.Printf("qemu-img found at: %s", qemuImgPath)
            fmt.Printf("✅ qemu-img: found at %s\n", qemuImgPath)
        }
        
        // Check if genisoimage or mkisofs is installed
        logger.Println("Checking for genisoimage or mkisofs")
        genisoimagePath, err1 := exec.LookPath("genisoimage")
        mkisofsPath, err2 := exec.LookPath("mkisofs")
        if err1 != nil && err2 != nil {
            logger.Printf("Neither genisoimage nor mkisofs found")
            fmt.Println("❌ genisoimage/mkisofs: not found (please install genisoimage for cloud-init support)")
            allOk = false
        } else if err1 == nil {
            logger.Printf("genisoimage found at: %s", genisoimagePath)
            fmt.Printf("✅ genisoimage: found at %s\n", genisoimagePath)
        } else {
            logger.Printf("mkisofs found at: %s", mkisofsPath)
            fmt.Printf("✅ mkisofs: found at %s\n", mkisofsPath)
        }
        
        // Check if ssh-keygen is installed
        logger.Println("Checking for ssh-keygen")
        sshKeygenPath, err := exec.LookPath("ssh-keygen")
        if err != nil {
            logger.Printf("ssh-keygen not found: %v", err)
            fmt.Println("❌ ssh-keygen: not found (please install openssh-client for SSH key generation)")
            allOk = false
        } else {
            logger.Printf("ssh-keygen found at: %s", sshKeygenPath)
            fmt.Printf("✅ ssh-keygen: found at %s\n", sshKeygenPath)
        }
        
        // Check if dnsmasq is installed
        logger.Println("Checking for dnsmasq")
        dnsmasqPath, err := exec.LookPath("dnsmasq")
        if err != nil {
            logger.Printf("dnsmasq not found: %v", err)
            fmt.Println("❌ dnsmasq: not found (please install dnsmasq for DHCP support)")
            allOk = false
        } else {
            logger.Printf("dnsmasq found at: %s", dnsmasqPath)
            fmt.Printf("✅ dnsmasq: found at %s\n", dnsmasqPath)
        }
        
        // Check for CAP_NET_ADMIN capability or ability to create bridges
        logger.Println("Checking for CAP_NET_ADMIN capability")
        
        execPath, err := os.Executable()
        if err != nil {
            logger.Printf("Could not determine executable path: %v", err)
            fmt.Println("⚠️  CAP_NET_ADMIN: could not determine executable path")
        } else {
            // Check if the binary has CAP_NET_ADMIN capability
            cmd := exec.Command("getcap", execPath)
            output, err := cmd.Output()
            
            if err == nil && strings.Contains(string(output), "cap_net_admin") {
                logger.Printf("Binary has CAP_NET_ADMIN capability: %s", execPath)
                fmt.Printf("✅ CAP_NET_ADMIN: granted via capability on %s\n", execPath)
            } else {
                // Try to create a test bridge to check if we can do it anyway
                logger.Println("Binary doesn't have CAP_NET_ADMIN capability, testing bridge creation")
                testBridgeName := "qc-test-bridge"
                testBridge := &netlink.Bridge{
                    LinkAttrs: netlink.LinkAttrs{
                        Name: testBridgeName,
                    },
                }
                testErr := netlink.LinkAdd(testBridge)
                if testErr == nil {
                    // Clean up test bridge
                    if link, err := netlink.LinkByName(testBridgeName); err == nil {
                        netlink.LinkDel(link)
                    }
                    logger.Println("Can create bridges (possibly running as root or with other privileges)")
                    fmt.Println("✅ CAP_NET_ADMIN: available (running with sufficient privileges)")
                } else {
                    logger.Printf("Cannot create bridges: %v", testErr)
                    fmt.Println("⚠️  CAP_NET_ADMIN: not available (bridge networking will not work)")
                    fmt.Printf("    To grant capability: sudo setcap cap_net_admin+ep %s\n", execPath)
                    fmt.Println("    Or run qemu-compose with sudo for bridge networking")
                }
            }
        }
        
        fmt.Println()
        if allOk {
            logger.Println("All dependency checks passed")
            fmt.Println("✅ All system dependencies are satisfied!")
        } else {
            logger.Println("Some dependency checks failed")
            fmt.Println("❌ Some system dependencies are missing. Please install them before using qemu-compose.")
            os.Exit(1)
        }
    },
}

var consoleCmd = &cobra.Command{
    Use:   "console <vm-name>",
    Short: "Attach to a VM's serial console",
    Long:  `Attach to a running VM's serial console with read/write access. Press Ctrl+] to detach.`,
    Args:  cobra.ExactArgs(1),
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        vmName := args[0]
        
        logger.Printf("Executing 'console' command for VM: %s", vmName)
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        // Check if VM exists in config
        if _, exists := config.VMs[vmName]; !exists {
            fmt.Fprintf(os.Stderr, "Error: VM not found in compose file: %s\n", vmName)
            os.Exit(1)
        }
        
        // Attach to console
        if err := attachToConsole(vmName); err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
    },
}

var sshCmd = &cobra.Command{
    Use:   "ssh <vm-name>",
    Short: "Connect to a VM via SSH",
    Long:  `Connect to a running VM via SSH using the project SSH key and allocated port.`,
    Args:  cobra.ExactArgs(1),
    ValidArgsFunction: getVMNames,
    Run: func(cmd *cobra.Command, args []string) {
        vmName := args[0]
        
        logger.Printf("Executing 'ssh' command for VM: %s", vmName)
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        // Check if VM exists in config
        vm, exists := config.VMs[vmName]
        if !exists {
            fmt.Fprintf(os.Stderr, "Error: VM not found in compose file: %s\n", vmName)
            os.Exit(1)
        }
        
        // Check if VM is running
        running, err := isVMRunning(vmName)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error checking VM status: %v\n", err)
            os.Exit(1)
        }
        
        if !running {
            fmt.Fprintf(os.Stderr, "Error: VM is not running: %s\n", vmName)
            fmt.Fprintf(os.Stderr, "Start the VM with: qemu-compose up %s\n", vmName)
            os.Exit(1)
        }
        
        // Get SSH port
        sshPort, err := getSSHPort(vmName)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: failed to get SSH port: %v\n", err)
            os.Exit(1)
        }
        
        // Get SSH key path
        cwd, err := os.Getwd()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
            os.Exit(1)
        }
        
        sshKeyPath := filepath.Join(cwd, ".qemu-compose", "ssh", "id_ed25519")
        
        // Check if SSH key exists
        if _, err := os.Stat(sshKeyPath); os.IsNotExist(err) {
            fmt.Fprintf(os.Stderr, "Error: SSH key not found: %s\n", sshKeyPath)
            fmt.Fprintf(os.Stderr, "The SSH key should have been created when the VM was started.\n")
            os.Exit(1)
        }
        
        // Detect default user for the OS
        defaultUser := getDefaultUserForOS(detectOSFromImage(vm.Image))
        
        logger.Printf("Connecting to VM %s via SSH (port: %d, user: %s, key: %s)", vmName, sshPort, defaultUser, sshKeyPath)
        
        // Build SSH command
        sshArgs := []string{
            "-i", sshKeyPath,
            "-p", fmt.Sprintf("%d", sshPort),
            "-o", "StrictHostKeyChecking=no",
            "-o", "UserKnownHostsFile=/dev/null",
            fmt.Sprintf("%s@localhost", defaultUser),
        }
        
        // Execute SSH command
        sshCmd := exec.Command("ssh", sshArgs...)
        sshCmd.Stdin = os.Stdin
        sshCmd.Stdout = os.Stdout
        sshCmd.Stderr = os.Stderr
        
        if err := sshCmd.Run(); err != nil {
            if exitErr, ok := err.(*exec.ExitError); ok {
                os.Exit(exitErr.ExitCode())
            }
            fmt.Fprintf(os.Stderr, "Error: failed to execute SSH: %v\n", err)
            os.Exit(1)
        }
    },
}

var imageCmd = &cobra.Command{
    Use:   "image",
    Short: "Manage images",
    Long:  `Manage VM base images in the local cache`,
}

var imageLsCmd = &cobra.Command{
    Use:   "ls",
    Short: "List cached images",
    Long:  `List all VM base images stored in the local cache with their full paths`,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Println("Executing 'image ls' command")
        
        // Get cache directory
        cacheDir, err := getImageCacheDir()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        // List images
        images, err := listImages()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        if len(images) == 0 {
            fmt.Printf("No images found in cache directory: %s\n", cacheDir)
            fmt.Println("\nTo download images, use: qemu-compose pull")
            return
        }
        
        fmt.Printf("Image cache directory: %s\n\n", cacheDir)
        fmt.Printf("%-50s %-15s %s\n", "FILENAME", "SIZE", "PATH")
        fmt.Println(strings.Repeat("-", 120))
        
        for _, image := range images {
            // Format size in human-readable format
            sizeStr := formatBytes(image.Size)
            fmt.Printf("%-50s %-15s %s\n", image.Filename, sizeStr, image.Path)
        }
        
        fmt.Printf("\nTotal: %d image(s)\n", len(images))
    },
}

var networkCmd = &cobra.Command{
    Use:   "network",
    Short: "Manage networks",
    Long:  `Manage network infrastructure (bridges, TAP devices, subnets)`,
}

var networkLsCmd = &cobra.Command{
    Use:   "ls",
    Short: "List network information",
    Long:  `Display information about bridges, TAP devices, DHCP servers, and network permissions`,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Println("Executing 'network ls' command")

        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }

        fmt.Printf("Using compose file: %s\n", composeFile)
        fmt.Printf("Project: %s\n\n", getProjectName())

        // Display network metadata (allocated subnets)
        metadata, err := loadNetworkMetadata()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error loading network metadata: %v\n", err)
        }

        // Display networks table
        if len(config.Networks) > 0 {
            fmt.Println("=== Networks ===")
            fmt.Printf("%-20s %-10s %-20s %-15s %-10s %-30s\n", "NAME", "DRIVER", "SUBNET", "BRIDGE", "DHCP", "DNSMASQ UNIT")
            fmt.Println(strings.Repeat("-", 110))

            for networkName, network := range config.Networks {
                driver := network.Driver
                if driver == "" {
                    driver = "bridge"
                }

                subnet := "not allocated"
                bridgeName := getBridgeName(networkName)
                dhcpStatus := "no"
                dnsmasqUnit := "-"

                if meta, exists := metadata[networkName]; exists {
                    if meta.Subnet != "" {
                        subnet = meta.Subnet
                    }
                    if meta.DnsmasqUnit != "" {
                        dnsmasqUnit = meta.DnsmasqUnit
                    }
                    if meta.DnsmasqActive && isDnsmasqRunning(networkName) {
                        dhcpStatus = "yes"
                    } else if meta.DnsmasqActive {
                        dhcpStatus = "stopped"
                    }
                }

                fmt.Printf("%-20s %-10s %-20s %-15s %-10s %-30s\n",
                    networkName, driver, subnet, bridgeName, dhcpStatus, dnsmasqUnit)
            }
            fmt.Println()
        } else {
            fmt.Println("No networks defined in compose file\n")
        }

        // Display bridge information
        if len(config.Networks) > 0 {
            fmt.Println("=== Bridges ===")
            for networkName := range config.Networks {
                bridgeName := getBridgeName(networkName)

                // Check if bridge exists
                bridge, err := netlink.LinkByName(bridgeName)
                if err != nil {
                    fmt.Printf("Bridge: %s (network: %s)\n", bridgeName, networkName)
                    fmt.Printf("  Status: not created\n\n")
                    continue
                }

                fmt.Printf("Bridge: %s (network: %s)\n", bridgeName, networkName)
                fmt.Printf("  Status: active\n")
                fmt.Printf("  Index: %d\n", bridge.Attrs().Index)
                fmt.Printf("  MTU: %d\n", bridge.Attrs().MTU)

                // Get IP addresses
                addrs, err := netlink.AddrList(bridge, netlink.FAMILY_V4)
                if err == nil && len(addrs) > 0 {
                    fmt.Printf("  IP Addresses:\n")
                    for _, addr := range addrs {
                        fmt.Printf("    - %s\n", addr.IPNet.String())
                    }
                }

                // Get bridge state
                if bridge.Attrs().Flags&net.FlagUp != 0 {
                    fmt.Printf("  State: UP\n")
                } else {
                    fmt.Printf("  State: DOWN\n")
                }

                fmt.Println()
            }
        }

        // Display DHCP server information
        if len(metadata) > 0 {
            fmt.Println("=== DHCP Servers (dnsmasq) ===")
            hasActiveDHCP := false
            for networkName, meta := range metadata {
                if meta.DnsmasqUnit == "" {
                    continue
                }

                hasActiveDHCP = true
                isRunning := isDnsmasqRunning(networkName)
                status := "stopped"
                if isRunning {
                    status = "running"
                }

                fmt.Printf("Network: %s\n", networkName)
                fmt.Printf("  Unit: %s\n", meta.DnsmasqUnit)
                fmt.Printf("  Status: %s\n", status)
                fmt.Printf("  Subnet: %s\n", meta.Subnet)

                if isRunning {
                    // Parse subnet to show DHCP range
                    ip, _, err := net.ParseCIDR(meta.Subnet)
                    if err == nil {
                        startIP := make(net.IP, len(ip))
                        endIP := make(net.IP, len(ip))
                        copy(startIP, ip.To4())
                        copy(endIP, ip.To4())
                        startIP[3] = 10
                        endIP[3] = 250
                        fmt.Printf("  DHCP Range: %s - %s\n", startIP.String(), endIP.String())
                    }
                    fmt.Printf("  View logs: journalctl --user -u %s -f\n", meta.DnsmasqUnit)
                }

                fmt.Println()
            }

            if !hasActiveDHCP {
                fmt.Println("No DHCP servers configured\n")
            }
        }

        // Display TAP devices for running VMs
        fmt.Println("=== TAP Devices ===")
        hasAnyTAP := false
        for vmName, vm := range config.VMs {
            if len(vm.Networks) == 0 {
                continue
            }

            for i, networkName := range vm.Networks {
                tapName := getTAPName(vmName, i)

                // Check if TAP exists
                tap, err := netlink.LinkByName(tapName)
                if err != nil {
                    continue
                }

                hasAnyTAP = true
                fmt.Printf("TAP: %s (VM: %s, network: %s)\n", tapName, vmName, networkName)
                fmt.Printf("  Index: %d\n", tap.Attrs().Index)
                fmt.Printf("  MTU: %d\n", tap.Attrs().MTU)

                // Get TAP state
                if tap.Attrs().Flags&net.FlagUp != 0 {
                    fmt.Printf("  State: UP\n")
                } else {
                    fmt.Printf("  State: DOWN\n")
                }

                // Get master (bridge)
                if tap.Attrs().MasterIndex > 0 {
                    masterLink, err := netlink.LinkByIndex(tap.Attrs().MasterIndex)
                    if err == nil {
                        fmt.Printf("  Attached to: %s\n", masterLink.Attrs().Name)
                    }
                }

                fmt.Println()
            }
        }

        if !hasAnyTAP {
            fmt.Println("No TAP devices found\n")
        }

        // Display capability information
        fmt.Println("=== Network Capabilities ===")
        execPath, err := os.Executable()
        if err != nil {
            fmt.Printf("Could not determine executable path: %v\n", err)
        } else {
            // Check if the binary has CAP_NET_ADMIN capability
            cmd := exec.Command("getcap", execPath)
            output, err := cmd.Output()

            if err == nil && strings.Contains(string(output), "cap_net_admin") {
                fmt.Printf("✅ qemu-compose has CAP_NET_ADMIN capability\n")
                fmt.Printf("   Binary: %s\n", execPath)
                fmt.Printf("   Capabilities: %s\n", strings.TrimSpace(string(output)))
            } else {
                fmt.Printf("⚠️  qemu-compose does NOT have CAP_NET_ADMIN capability\n")
                fmt.Printf("   Binary: %s\n", execPath)
                fmt.Printf("   To grant: sudo setcap cap_net_admin+ep %s\n", execPath)
            }
        }

        // Test if we can actually create bridges
        fmt.Println("\nTesting bridge creation capability...")
        testBridgeName := "qc-test-bridge"
        testBridge := &netlink.Bridge{
            LinkAttrs: netlink.LinkAttrs{
                Name: testBridgeName,
            },
        }
        testErr := netlink.LinkAdd(testBridge)
        if testErr == nil {
            // Clean up test bridge
            if link, err := netlink.LinkByName(testBridgeName); err == nil {
                netlink.LinkDel(link)
            }
            fmt.Println("✅ Can create bridges (sufficient privileges)")
        } else {
            fmt.Printf("❌ Cannot create bridges: %v\n", testErr)
            fmt.Println("   Bridge networking will not work without proper capabilities")
        }
    },
}

var networkDownCmd = &cobra.Command{
    Use:   "down [NETWORK...]",
    Short: "Destroy network infrastructure",
    Long:  `Stop VMs and destroy network infrastructure (bridges, TAP devices, metadata). If network names are provided, only those networks will be destroyed.`,
    ValidArgsFunction: getNetworkNames,
    Run: func(cmd *cobra.Command, args []string) {
        logger.Println("Executing 'network down' command")
        
        force, _ := cmd.Flags().GetBool("force")
        
        config, err := loadComposeFile(composeFile)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        
        fmt.Printf("Using compose file: %s\n", composeFile)
        fmt.Printf("Project: %s\n\n", getProjectName())
        
        // Determine which networks to destroy
        var networksToDestroy map[string]Network
        if len(args) > 0 {
            // Destroy specific networks
            networksToDestroy = make(map[string]Network)
            for _, networkName := range args {
                network, exists := config.Networks[networkName]
                if !exists {
                    fmt.Fprintf(os.Stderr, "Error: network not found in compose file: %s\n", networkName)
                    os.Exit(1)
                }
                networksToDestroy[networkName] = network
            }
        } else {
            // Destroy all networks
            networksToDestroy = config.Networks
        }
        
        if len(networksToDestroy) == 0 {
            fmt.Println("No networks to destroy")
            return
        }
        
        // Find VMs using these networks
        affectedVMs := make(map[string]VM)
        for vmName, vm := range config.VMs {
            for _, vmNetwork := range vm.Networks {
                if _, exists := networksToDestroy[vmNetwork]; exists {
                    affectedVMs[vmName] = vm
                    break
                }
            }
        }
        
        // Warn about affected VMs
        if len(affectedVMs) > 0 && !force {
            fmt.Println("Warning: The following VMs are using these networks:")
            for vmName, vm := range affectedVMs {
                fmt.Printf("  - %s (networks: %s)\n", vmName, strings.Join(vm.Networks, ", "))
            }
            fmt.Println()
            fmt.Print("These VMs will be stopped. Continue? [y/N]: ")
            
            reader := bufio.NewReader(os.Stdin)
            response, err := reader.ReadString('\n')
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
                os.Exit(1)
            }
            
            response = strings.TrimSpace(strings.ToLower(response))
            if response != "y" && response != "yes" {
                fmt.Println("Aborted")
                return
            }
            fmt.Println()
        }
        
        // Stop affected VMs
        if len(affectedVMs) > 0 {
            fmt.Println("Stopping VMs...")
            hasError := false
            for vmName, vm := range affectedVMs {
                running, err := isVMRunning(vmName)
                if err != nil {
                    logger.Printf("Warning: could not check VM status: %v", err)
                    continue
                }
                
                if running {
                    if err := stopVM(vmName, vm); err != nil {
                        fmt.Fprintf(os.Stderr, "  ✗ Failed to stop %s: %v\n", vmName, err)
                        hasError = true
                    } else {
                        fmt.Printf("  ✓ Stopped %s\n", vmName)
                    }
                }
            }
            
            if hasError {
                fmt.Fprintf(os.Stderr, "\nWarning: Some VMs could not be stopped. Continuing with network cleanup...\n")
            }
            fmt.Println()
        }
        
        // Clean up network infrastructure
        fmt.Println("Cleaning up network infrastructure...")
        
        // Delete TAP devices for affected VMs
        for vmName, vm := range affectedVMs {
            for i := range vm.Networks {
                tapName := getTAPName(vmName, i)
                if err := deleteTAPDevice(tapName); err != nil {
                    logger.Printf("Warning: failed to delete TAP device %s: %v", tapName, err)
                } else {
                    fmt.Printf("  ✓ Deleted TAP device: %s (%s)\n", tapName, vmName)
                }
            }
        }
        
        // Delete bridges
        for networkName := range networksToDestroy {
            if err := deleteBridge(networkName); err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Failed to delete bridge for network %s: %v\n", networkName, err)
            } else {
                bridgeName := getBridgeName(networkName)
                fmt.Printf("  ✓ Deleted bridge: %s (network: %s)\n", bridgeName, networkName)
            }
        }
        
        // Remove network metadata
        metadata, err := loadNetworkMetadata()
        if err != nil {
            logger.Printf("Warning: could not load network metadata: %v", err)
        } else {
            // Remove entries for destroyed networks
            for networkName := range networksToDestroy {
                delete(metadata, networkName)
            }
            
            if err := saveNetworkMetadata(metadata); err != nil {
                fmt.Fprintf(os.Stderr, "  ✗ Failed to update network metadata: %v\n", err)
            } else {
                fmt.Printf("  ✓ Removed network metadata\n")
            }
        }
        
        fmt.Println("\n✓ Network cleanup completed")
    },
}

// formatBytes formats a byte count into a human-readable string
func formatBytes(bytes int64) string {
    const unit = 1024
    if bytes < unit {
        return fmt.Sprintf("%d B", bytes)
    }
    div, exp := int64(unit), 0
    for n := bytes / unit; n >= unit; n /= unit {
        div *= unit
        exp++
    }
    return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func init() {
    rootCmd.PersistentFlags().StringVarP(&composeFile, "file", "f", "", "Specify an alternate compose file (default: qemu-compose.yaml or qemu-compose.yml)")
    rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging (can also use QEMU_COMPOSE_DEBUG=true)")
    
    pullCmd.Flags().BoolP("force", "", false, "Force re-download even if image already exists")
    psCmd.Flags().BoolP("wait", "", false, "Wait for all VMs to be ready before displaying status")
    networkDownCmd.Flags().BoolP("force", "", false, "Skip confirmation prompt")
    inspectCmd.Flags().StringP("format", "", "text", "Output format: text or json")
    
    imageCmd.AddCommand(imageLsCmd)
    
    networkCmd.AddCommand(networkLsCmd)
    networkCmd.AddCommand(networkDownCmd)
    
    rootCmd.AddCommand(versionCmd)
    rootCmd.AddCommand(upCmd)
    rootCmd.AddCommand(stopCmd)
    rootCmd.AddCommand(destroyCmd)
    rootCmd.AddCommand(psCmd)
    rootCmd.AddCommand(inspectCmd)
    rootCmd.AddCommand(pullCmd)
    rootCmd.AddCommand(doctorCmd)
    rootCmd.AddCommand(consoleCmd)
    rootCmd.AddCommand(sshCmd)
    rootCmd.AddCommand(imageCmd)
    rootCmd.AddCommand(networkCmd)
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
