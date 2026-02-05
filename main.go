package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"mirage/internal/analysis"
	"mirage/internal/monitor"
	"mirage/internal/pipeline"
	"mirage/internal/pprofread"
	"mirage/internal/profiler"
	"mirage/internal/report"
	"mirage/internal/ui/tui"
)

var (
	outputFile  = flag.String("o", "", "Output file for the report")
	verbose     = flag.Bool("v", false, "Enable verbose output")
	profileDir  = flag.String("profile-dir", "", "Directory to store profile files (default: temp dir)")
	enableCPU   = flag.Bool("cpu", true, "Enable CPU profiling")
	enableMem   = flag.Bool("mem", true, "Enable memory profiling")
	enableTrace = flag.Bool("trace", false, "Enable execution trace (go tool trace)")
	enableMutex = flag.Bool("mutex", false, "Enable mutex profile (go tool pprof)")
	monitorFreq = flag.Duration("freq", 100*time.Millisecond, "System monitoring frequency")
	timeout     = flag.Duration("timeout", 0, "Maximum execution time (0 = no timeout)")
	noColor     = flag.Bool("no-color", false, "Disable colored output")
	format      = flag.String("format", "text", "Report format (text or markdown)")
	ui          = flag.Bool("ui", false, "Show live TUI dashboard while profiling")
	enableCgroup  = flag.Bool("cgroup", false, "Run target in cgroup v2 scope (Linux); report cgroup I/O and memory")
	enableStrace  = flag.Bool("strace", false, "Trace syscalls (strace -f -c); report syscall summary (Linux)")
	enableSample   = flag.Bool("sample", false, "Sample /proc syscall at high frequency; report top syscalls (Linux)")
	enablePreload   = flag.Bool("preload", false, "Run target with LD_PRELOAD shim; report interposed call counts (dynamic link only)")
	preloadShimPath = flag.String("preload-so", "", "Path to preload shim .so (default: preload_shim.so next to mirage binary)")
	instrumentGo     = flag.Bool("instrument-go", false, "Set MIRAGE_INSTRUMENT=1 for Go binaries built with -toolexec; see docs/INSTRUMENTATION.md")
	uprobeSymbols   = flag.String("uprobe-symbols", "", "Comma-separated symbols for eBPF uprobes (Linux, requires CAP_SYS_ADMIN)")
	uprobeBinary    = flag.String("uprobe-binary", "", "Binary path for uprobes (default: target command)")
	pprofPath       = flag.String("pprof", "", "Parse target-generated pprof file and report top functions (e.g. path to cpu.prof)")
	mode            = flag.String("mode", "", "Profiling mode: basic, standard, or deep (enables preset options)")
	deepMode        = flag.Bool("deep", false, "Same as --mode=deep")
	standardMode    = flag.Bool("standard", false, "Same as --mode=standard")
	help            = flag.Bool("h", false, "Show help message")
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

	resolvedMode := *mode
	if *deepMode {
		resolvedMode = "deep"
	} else if *standardMode {
		resolvedMode = "standard"
	}
	applyModePreset(resolvedMode)

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

	if *ui {
		if err := runTUI(ctx, args[0], args[1:]...); err != nil {
			if err == context.DeadlineExceeded {
				log.Fatalf("Benchmark timed out after %v", *timeout)
			}
			log.Fatalf("TUI run failed: %v", err)
		}
	} else {
		if err := runBenchmark(ctx, args[0], args[1:]...); err != nil {
			if err == context.DeadlineExceeded {
				log.Fatalf("Benchmark timed out after %v", *timeout)
			}
			log.Fatalf("Benchmark failed: %v", err)
		}
	}
}

func runTUI(ctx context.Context, command string, args ...string) error {
	metricsChan := make(chan monitor.ProcessMetrics, 64)
	doneChan := make(chan tui.ProfileDoneMsg, 1)
	resultChan := make(chan tui.ProfileDoneMsg, 1)

	uprobeList := parseUprobeSymbols(*uprobeSymbols)
	prof := profiler.NewWithTraceMutex(*enableCPU, *enableMem, *enableTrace, *enableMutex, *enableCgroup, *enableStrace, *enableSample, *enablePreload, *preloadShimPath, *instrumentGo, uprobeList, *uprobeBinary, *profileDir, *monitorFreq)
	prof.SetMetricsCallback(func(m monitor.ProcessMetrics) {
		select {
		case metricsChan <- m:
		default:
		}
	})

	mon := monitor.NewSystemMonitor(*monitorFreq)
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- mon.Start(monitorCtx)
	}()

	time.Sleep(10 * time.Millisecond)

	go func() {
		profileData, err := prof.Profile(ctx, command, args...)
		msg := tui.ProfileDoneMsg{Data: profileData, Err: err}
		select {
		case doneChan <- msg:
		default:
		}
		select {
		case resultChan <- msg:
		default:
		}
	}()

	getSystem := func() *monitor.SystemMetrics {
		return mon.GetLatestMetrics()
	}

	prog := tui.NewProgram(command, args, metricsChan, getSystem, doneChan)
	if _, err := prog.Run(); err != nil {
		monitorCancel()
		mon.Stop()
		return err
	}

	var result tui.ProfileDoneMsg
	select {
	case result = <-resultChan:
	case <-time.After(2 * time.Second):
		monitorCancel()
		mon.Stop()
		return fmt.Errorf("profile result not received")
	}

	monitorCancel()
	mon.Stop()
	select {
	case <-monitorDone:
	case <-time.After(500 * time.Millisecond):
	}

	if result.Err != nil {
		return result.Err
	}

	systemMetrics := mon.GetMetrics()
	session := pipeline.NewSession(command, args)
	session.ProfileData = result.Data
	session.SystemMetrics = systemMetrics
	session.EndTime = result.Data.EndTime
	session.TracePath = result.Data.TracePath
	session.MutexPath = result.Data.MutexPath

	attachPprofSummary(session.ProfileData, *pprofPath)
	pipeline.Normalize(session)
	pipeline.Analyze(session, analysis.DefaultConfig)

	fmt.Print("\n\n")
	if err := generateReport(session); err != nil {
		return err
	}

	if *verbose && (*enableCPU || *enableMem) {
		fmt.Printf("\nTo analyze profiles: go tool pprof %s/cpu.prof\n", *profileDir)
		fmt.Printf("  go tool pprof -http=:8080 %s/cpu.prof\n", *profileDir)
	}

	return nil
}

func runBenchmark(ctx context.Context, command string, args ...string) error {
	if *verbose {
		fmt.Printf("Starting benchmark of: %s %v\n", command, args)
		fmt.Printf("Profile directory: %s\n", *profileDir)
		fmt.Printf("Monitoring frequency: %s\n", *monitorFreq)
	}

	uprobeList := parseUprobeSymbols(*uprobeSymbols)
	prof := profiler.NewWithTraceMutex(*enableCPU, *enableMem, *enableTrace, *enableMutex, *enableCgroup, *enableStrace, *enableSample, *enablePreload, *preloadShimPath, *instrumentGo, uprobeList, *uprobeBinary, *profileDir, *monitorFreq)
	mon := monitor.NewSystemMonitor(*monitorFreq)

	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()

	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- mon.Start(monitorCtx)
	}()

	time.Sleep(10 * time.Millisecond)

	if *verbose {
		fmt.Printf("Executing command: %s\n", command)
	} else {
		fmt.Printf("Profiling %s...\n", command)
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

	session := pipeline.NewSession(command, args)
	session.ProfileData = profileData
	session.SystemMetrics = systemMetrics
	session.EndTime = profileData.EndTime
	session.TracePath = profileData.TracePath
	session.MutexPath = profileData.MutexPath

	attachPprofSummary(session.ProfileData, *pprofPath)
	pipeline.Normalize(session)
	pipeline.Analyze(session, analysis.DefaultConfig)

	if *verbose {
		fmt.Printf("Profiling completed. Collected %d system metrics samples.\n", len(systemMetrics))
		fmt.Printf("Command executed in: %s\n", profileData.Duration)
		fmt.Printf("Exit code: %d\n", profileData.ExitCode)
	}

	fmt.Print("\n\n")

	if err := generateReport(session); err != nil {
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

func applyModePreset(mode string) {
	switch mode {
	case "standard":
		*enableCgroup = true
	case "deep":
		*enableCgroup = true
		*enableSample = true
		*enablePreload = true
		*instrumentGo = true
	}
}

func parseUprobeSymbols(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func attachPprofSummary(data *profiler.ProfileData, path string) {
	if data == nil || path == "" {
		return
	}
	tops, err := pprofread.TopFunctions(path, 15)
	if err != nil {
		return
	}
	data.TargetPprofPath = path
	for _, t := range tops {
		data.TargetPprofTop = append(data.TargetPprofTop, profiler.PprofTopEntry{Name: t.Name, Value: t.Value})
	}
}

func generateReport(session *pipeline.Session) error {
	var reportFormat report.ReportFormat
	switch *format {
	case "text":
		reportFormat = report.FormatText
	case "markdown":
		reportFormat = report.FormatMarkdown
	default:
		return fmt.Errorf("unsupported report format: %s (use text or markdown)", *format)
	}

	config := report.ReportConfig{
		OutputFile:  *outputFile,
		Format:      reportFormat,
		Verbose:     *verbose,
		ColorOutput: !*noColor,
	}

	reporter := report.New(config)
	defer reporter.Close()

	if err := reporter.GenerateReport(session.ProfileData, session.SystemMetrics, session.Findings, session.TracePath, session.MutexPath); err != nil {
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
	fmt.Printf("  %s [OPTIONS] <command> [args...]\n", appName)
	fmt.Printf("  Use --mode=basic|standard|deep or --deep / --standard to enable presets.\n\n")

	fmt.Printf("DESCRIPTION:\n")
	fmt.Printf("  Mirage profiles and benchmarks applications, providing detailed performance\n")
	fmt.Printf("  analysis including CPU usage, memory consumption, system resource utilization,\n")
	fmt.Printf("  and optimization recommendations.\n\n")

	fmt.Printf("EXAMPLES:\n")
	fmt.Printf("  %s ls -la                          # Profile 'ls -la' command\n", appName)
	fmt.Printf("  %s -o report.txt python script.py  # Profile Python script, save to file\n", appName)
	fmt.Printf("  %s -v --cpu --mem ./myapp          # Verbose profiling with CPU and memory\n", appName)
	fmt.Printf("  %s -timeout 30s long-running-app   # Profile with 30 second timeout\n", appName)
	fmt.Printf("  %s -format markdown -o report.md app  # Markdown report\n", appName)
	fmt.Printf("  %s -ui ./myapp                        # Live TUI dashboard while profiling\n", appName)
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
	fmt.Printf("  text     Terminal report with colored output (default)\n")
	fmt.Printf("  markdown Markdown report for files or documentation\n")

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
