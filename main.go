package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

// Injected via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	useGUI := flag.Bool("gui", true, "launch GUI mode (default)")
	cfgPath := flag.String("config", "config.ini", "path to config file")
	flag.Parse()

	if *showVersion {
		fmt.Printf("goftpd %s\ncommit: %s\nbuilt:  %s\n%s/%s\n",
			version, commit, date, runtime.GOOS, runtime.GOARCH)
		return
	}

	if *useGUI {
		runGUI(*cfgPath)
		return
	}

	runCLI(*cfgPath)
}

func runCLI(cfgPath string) {
	config, err := LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	driver := NewFTPDriver(config)
	server := ftpserver.NewFtpServer(driver)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("\nShutting down...")
		os.Exit(0)
	}()

	fmt.Printf("GoFTP Server starting on port %d\n", config.Network.Port)
	fmt.Printf("Shared directory: %s\n", config.Storage.SharedDir)
	fmt.Printf("Auth enabled: %v\n", config.Auth.Enabled)
	fmt.Printf("Permissions: download=%v upload=%v delete=%v rename=%v mkdir=%v\n",
		config.Permissions.Download, config.Permissions.Upload,
		config.Permissions.Delete, config.Permissions.Rename, config.Permissions.Mkdir)
	fmt.Printf("Max connections: %d\n", config.Network.MaxConnections)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
