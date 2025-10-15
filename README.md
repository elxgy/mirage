# Mirage

A fast, lightweight Go application profiler that measures performance bottlenecks, resource usage, and provides optimization recommendations.

## What is Mirage?

Mirage profiles any application in real-time, monitoring CPU usage, memory consumption, and system resources to identify performance issues and suggest optimizations.

## Installation

```bash
git clone https://github.com/elxgy/mirage.git
cd mirage
make build
```

## Usage

```bash
mirage [options] <command> [args...]
```

### Basic Examples

```bash
# Profile any command
mirage ls -la

# Save report to file
mirage -o report.txt python script.py

# Verbose output with profiling details
mirage -v ./my-application

# Set timeout for long-running processes
mirage -timeout 30s ./batch-job
```

### Key Options

- `-o <file>` - Save report to file
- `-v` - Verbose output with detailed analysis
- `-timeout <duration>` - Maximum execution time
- `-freq <duration>` - Monitoring frequency (default: 100ms)
- `--no-color` - Disable colored output

## What You Get

**Performance Report** includes:

- Execution time and exit status
- CPU usage (user/system time breakdown)
- Memory usage (RSS, VMS, page faults)
- System resource monitoring
- Automated bottleneck detection
- Optimization recommendations

### Sample Output

```
========== MIRAGE PERFORMANCE REPORT ==========

Command: python data_processor.py
Duration: 23.4s
Exit Code: 0 (Success)

---------- CPU Performance ----------
User Time:         18.2s
System Time:       2.1s
CPU Utilization:   86.7%

---------- Memory Usage ----------
Peak RSS:          2.3 GB
Page Faults:       145,230

---------- Performance Analysis ----------
High CPU utilization detected (86.7%)
Consider optimizing CPU-intensive code paths

---------- Recommendations ----------
1. Use parallel processing for CPU-bound tasks
2. Profile memory usage to identify potential leaks
```

## Advanced Features

- **Go pprof Integration** - Generates CPU, memory, goroutine, and block profiles
- **Real-time Monitoring** - Tracks system resources during execution with configurable frequency
- **Multiple Formats** - Text reports (JSON/HTML planned)
- **Process Statistics** - Thread count, file descriptors, context switches
- **Debugging Profiles** - Goroutine leak detection and blocking analysis

### Profile Analysis

```bash
# Generate profiles
mirage --profile-dir ./profiles ./my-app

# Analyze with Go tools
go tool pprof ./profiles/cpu.prof
go tool pprof ./profiles/mem.prof
go tool pprof ./profiles/goroutine.prof
go tool pprof ./profiles/block.prof

# Interactive web interface
go tool pprof -http=:8080 ./profiles/cpu.prof
```

## Use Cases

- **Development** - Find performance bottlenecks during development
- **CI/CD** - Automated performance regression testing
- **Production** - Monitor resource usage of deployed applications
- **Optimization** - Validate performance improvements
- **Debugging** - Analyze goroutine leaks and blocking issues
- **Stack Traces** - Collect and analyze execution profiles

## Build from Source

```bash
make build    # Build binary
make test     # Run tests
make install  # Install to $GOPATH/bin
make run      # Build and run example
```

## Requirements

- Go 1.21+
- Linux/macOS

## License

MIT License - see [LICENSE](LICENSE) file.
