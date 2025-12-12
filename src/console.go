package main

import (
    "fmt"
    "io"
    "net"
    "os"
    "os/signal"
    "syscall"
    
    "golang.org/x/term"
)

// attachToConsole attaches to a VM's serial console via Unix socket
func attachToConsole(vmName string) error {
    logger.Printf("Attaching to console for VM: %s", vmName)
    
    // Check if VM is running
    running, err := isVMRunning(vmName)
    if err != nil {
        return fmt.Errorf("failed to check VM status: %w", err)
    }
    
    if !running {
        return fmt.Errorf("VM is not running: %s", vmName)
    }
    
    // Get console socket path
    socketPath := getConsoleSocketPath(vmName)
    
    // Check if socket exists
    if _, err := os.Stat(socketPath); os.IsNotExist(err) {
        return fmt.Errorf("console socket not found: %s (VM may still be starting)", socketPath)
    }
    
    // Connect to Unix socket
    conn, err := net.Dial("unix", socketPath)
    if err != nil {
        return fmt.Errorf("failed to connect to console socket: %w", err)
    }
    defer conn.Close()
    
    logger.Printf("Connected to console socket: %s", socketPath)
    
    fmt.Printf("Connected to VM console: %s\n", vmName)
    fmt.Printf("Press Ctrl+] to detach\n\n")
    
    // Put terminal in raw mode
    oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
    if err != nil {
        return fmt.Errorf("failed to set terminal to raw mode: %w", err)
    }
    defer term.Restore(int(os.Stdin.Fd()), oldState)
    
    // Handle Ctrl+C gracefully
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    
    // Channel to signal completion
    done := make(chan error, 1)
    
    // Copy from socket to stdout
    go func() {
        _, err := io.Copy(os.Stdout, conn)
        done <- err
    }()
    
    // Copy from stdin to socket with escape sequence detection
    go func() {
        buf := make([]byte, 1)
        escapeCount := 0
        const escapeChar = 29 // Ctrl+]
        
        for {
            n, err := os.Stdin.Read(buf)
            if err != nil {
                done <- err
                return
            }
            
            if n > 0 {
                // Check for escape sequence (Ctrl+])
                if buf[0] == escapeChar {
                    escapeCount++
                    if escapeCount >= 1 {
                        fmt.Println("\nDetaching from console...")
                        done <- nil
                        return
                    }
                } else {
                    escapeCount = 0
                }
                
                // Write to socket
                if _, err := conn.Write(buf[:n]); err != nil {
                    done <- err
                    return
                }
            }
        }
    }()
    
    // Wait for completion or signal
    select {
    case err := <-done:
        if err != nil && err != io.EOF {
            return fmt.Errorf("console error: %w", err)
        }
        return nil
    case <-sigCh:
        fmt.Println("\nDetaching from console...")
        return nil
    }
}
