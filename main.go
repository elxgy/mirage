package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mirage/internal/monitor"
	"mirage/internal/profiler"
	"mirage/internal/report"
)

var (
	outputFile  = flag.String("o", "", "Output file for the report")
	verbose     = flag.Bool("v", false, "Enable verbose output")
	profileDir  = flag.String("profile-dir", "", "Directory to store profile files (default: temp dir)")
	enableCPU   = flag.Bool("cpu", true, "Enable CPU profiling")
	enableMem   = flag.Bool("mem", true, "Enable memory profiling")
	monitorFreq = flag.Duration("freq", 100*time.Millisecond, "System monitoring frequency")
	timeout     = flag.Duration("timeout", 0, "Maximum execution time (0 = no timeout)")
	noColor     = flag.Bool("no-color", false, "Disable colored output")
	format      = flag.String("format", "text", "Report format (text, json, html, csv)")
	showGraphs  = flag.Bool("graphs", false, "Include performance graphs in report")
	help        = flag.Bool("h", false, "Show help message")
)

const (
	appName    = "mirage"
	appVersion = "1.0.0"
)

func main() {
	flag.Usage = showUsage
	flag.Parse()

	if *help {
		showUsage()
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No command specified\n\n")
		showUsage()
		os.Exit(1)
	}

	if *profileDir == "" {
		tempDir, err := os.MkdirTemp("", "mirage-profile-*")
		if err != nil {
			log.Fatalf("Failed to create temp directory: %v", err)
		}
		*profileDir = tempDir
		defer os.RemoveAll(tempDir)
	} else {
		if err := os.MkdirAll(*profileDir, 0755); err != nil {
			log.Fatalf("Failed to create profile directory: %v", err)
		}
	}

	ctx := context.Background()
	var cancel context.CancelFunc

	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, stopping profiling...\n")
		cancel()
	}()

	if err := runBenchmark(ctx, args[0], args[1:]...); err != nil {
		if err == context.DeadlineExceeded {
			log.Fatalf("Benchmark timed out after %v", *timeout)
		}
		log.Fatalf("Benchmark failed: %v", err)
	}
}

func runBenchmark(ctx context.Context, command string, args ...string) error {
	if *verbose {
		fmt.Printf("Starting benchmark of: %s %v\n", command, args)
		fmt.Printf("Profile directory: %s\n", *profileDir)
		fmt.Printf("Monitoring frequency: %s\n", *monitorFreq)
	}

	prof := profiler.New(*enableCPU, *enableMem, *profileDir)
	mon := monitor.New(*monitorFreq)

	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()

	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- mon.Start(monitorCtx)
	}()

	time.Sleep(10 * time.Millisecond)

	if *verbose {
		fmt.Printf("Executing command: %s\n", command)
	}

	profileData, err := prof.Profile(ctx, command, args...)

	monitorCancel()
	mon.Stop()

	select {
	case monitorErr := <-monitorDone:
		if monitorErr != nil && monitorErr != context.Canceled {
			fmt.Fprintf(os.Stderr, "Warning: Monitor error: %v\n", monitorErr)
		}
	case <-time.After(500 * time.Millisecond):
	}

	if err != nil {
		return fmt.Errorf("profiling failed: %v", err)
	}

	systemMetrics := mon.GetMetrics()

	if *verbose {
		fmt.Printf("Profiling completed. Collected %d system metrics samples.\n", len(systemMetrics))
		fmt.Printf("Command executed in: %s\n", profileData.Duration)
		fmt.Printf("Exit code: %d\n", profileData.ExitCode)
	}

	if err := generateReport(profileData, systemMetrics); err != nil {
		return fmt.Errorf("failed to generate report: %v", err)
	}

	if *verbose {
		if *enableCPU {
			fmt.Printf("CPU profile: %s/cpu.prof\n", *profileDir)
		}
		if *enableMem {
			fmt.Printf("Memory profile: %s/mem.prof\n", *profileDir)
			fmt.Printf("Goroutine profile: %s/goroutine.prof\n", *profileDir)
			fmt.Printf("Block profile: %s/block.prof\n", *profileDir)
		}
		fmt.Printf("\nTo analyze profiles further, use:\n")
		fmt.Printf("  go tool pprof %s/cpu.prof\n", *profileDir)
		fmt.Printf("  go tool pprof %s/mem.prof\n", *profileDir)
		fmt.Printf("  go tool pprof -http=:8080 %s/cpu.prof\n", *profileDir)
	}

	return nil
}

func generateReport(profileData *profiler.ProfileData, systemMetrics []monitor.SystemMetrics) error {
	var reportFormat report.ReportFormat
	switch *format {
	case "text":
		reportFormat = report.FormatText
	case "json":
		reportFormat = report.FormatJSON
	case "html":
		reportFormat = report.FormatHTML
	case "csv":
		reportFormat = report.FormatCSV
	default:
		return fmt.Errorf("unsupported report format: %s", *format)
	}

	config := report.ReportConfig{
		OutputFile:   *outputFile,
		Format:       reportFormat,
		Verbose:      *verbose,
		ShowGraphs:   *showGraphs,
		ColorOutput:  !*noColor,
		IncludePprof: *enableCPU || *enableMem,
	}

	reporter := report.New(config)
	defer reporter.Close()

	if err := reporter.GenerateReport(profileData, systemMetrics); err != nil {
		return fmt.Errorf("failed to generate report: %v", err)
	}

	if *outputFile != "" {
		absPath, _ := filepath.Abs(*outputFile)
		if *verbose {
			fmt.Printf("Report saved to: %s\n", absPath)
		} else {
			fmt.Printf("Report written to %s\n", *outputFile)
		}
	}

	return nil
}

func showUsage() {
	fmt.Printf("%s v%s - Advanced Application Profiling and Benchmarking Tool\n\n", appName, appVersion)

	fmt.Printf("USAGE:\n")
	fmt.Printf("  %s [OPTIONS] <command> [args...]\n\n", appName)

	fmt.Printf("DESCRIPTION:\n")
	fmt.Printf("  Mirage profiles and benchmarks applications, providing detailed performance\n")
	fmt.Printf("  analysis including CPU usage, memory consumption, system resource utilization,\n")
	fmt.Printf("  and optimization recommendations.\n\n")

	fmt.Printf("EXAMPLES:\n")
	fmt.Printf("  %s ls -la                          # Profile 'ls -la' command\n", appName)
	fmt.Printf("  %s -o report.txt python script.py  # Profile Python script, save to file\n", appName)
	fmt.Printf("  %s -v --cpu --mem ./myapp          # Verbose profiling with CPU and memory\n", appName)
	fmt.Printf("  %s -timeout 30s long-running-app   # Profile with 30 second timeout\n", appName)
	fmt.Printf("  %s -format json -o data.json app   # Generate JSON report\n", appName)
	fmt.Printf("\n")

	fmt.Printf("OPTIONS:\n")
	flag.VisitAll(func(f *flag.Flag) {
		name := f.Name
		if len(name) == 1 {
			name = "-" + name
		} else {
			name = "--" + name
		}

		usage := f.Usage
		defValue := f.DefValue

		fmt.Printf("  %-20s %s", name, usage)
		if defValue != "" && defValue != "false" && defValue != "0" && defValue != "0s" {
			fmt.Printf(" (default: %s)", defValue)
		}
		fmt.Printf("\n")
	})

	fmt.Printf("\nREPORT FORMATS:\n")
	fmt.Printf("  text     Human-readable text report with tables and analysis\n")
	fmt.Printf("  json     Machine-readable JSON format\n")
	fmt.Printf("  html     Interactive HTML report with charts (future)\n")
	fmt.Printf("  csv      Comma-separated values for spreadsheet import (future)\n")

	fmt.Printf("\nPROFILING FEATURES:\n")
	fmt.Printf("  CPU time measurement (user/system time split)\n")
	fmt.Printf("  Memory usage tracking (RSS, VMS, page faults)\n")
	fmt.Printf("  Process statistics (threads, file descriptors, context switches)\n")
	fmt.Printf("  System resource monitoring (CPU, memory, disk, network)\n")
	fmt.Printf("  Performance analysis and optimization recommendations\n")
	fmt.Printf("  Go pprof profile generation for detailed analysis\n")

	fmt.Printf("\nFOR MORE INFORMATION:\n")
	fmt.Printf("  Visit: https://github.com/elxgy/mirage\n")
	fmt.Printf("  Report issues: https://github.com/elxgy/mirage/issues\n")
}
