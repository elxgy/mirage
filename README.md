# Mirage

A fast, lightweight Go application profiler that measures performance bottlenecks, resource usage, and provides optimization recommendations. Run any command under Mirage to get process and system metrics, structured findings, and optional live TUI or pprof/trace profiles.

## What is Mirage?

Mirage profiles any application in real-time: it executes your command, collects process metrics (CPU, memory, I/O, threads, FDs) and system-wide metrics (CPU, memory, load), then produces a report with structured findings and recommendations. You can use a **live TUI** (`--ui`) to watch metrics while the command runs, or run headless and get the report at the end.

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

### Modes

| Mode | Effect |
|------|--------|
| **basic** (default) | CPU + memory profiling, process/system metrics, findings. No cgroup, strace, sample, preload, or instrument-go. |
| **standard** | basic + cgroup (Linux). |
| **deep** | standard + sample, preload, instrument-go. Full instrumentation where no extra paths are required. Strace is not enabled by default; add `--strace` if you need syscall-level timing (note: strace can cause large slowdowns). |

Use `--mode=basic|standard|deep` or the shorthands `--standard` and `--deep`. Explicit flags override the preset (e.g. `--mode=deep --strace` enables strace). **Note:** `--strace` uses ptrace and can slow the target by 10-100x; use only when you need syscall counts and timing.

### Basic Examples

```bash
# Profile any command
mirage ls -la

# Live TUI dashboard while profiling (press q after run to see report)
mirage --ui ./my-app

# Save report to file (text or markdown)
mirage -o report.txt python script.py
mirage -format markdown -o report.md ./my-app

# Verbose output with profiling details
mirage -v ./my-application

# Timeout and monitoring frequency
mirage -timeout 30s -freq 50ms ./batch-job

# Mode presets
mirage --deep ./myapp
mirage --mode standard -o report.md python script.py
```

### Key Options

| Option | Description |
|--------|-------------|
| `--mode` | Profiling mode: `basic`, `standard`, or `deep` (enables preset options) |
| `--deep` | Same as `--mode=deep` |
| `--standard` | Same as `--mode=standard` |
| `--ui` | Live TUI dashboard while the command runs; report is printed after you press q |
| `-o <file>` | Write report to file (default: stdout) |
| `--format` | Report format: `text` or `markdown` (default: text) |
| `-v` | Verbose output |
| `--profile-dir` | Directory for pprof/trace files (default: temp, removed after run unless set) |
| `--cpu` / `--mem` | Enable CPU and/or memory pprof (default: both true) |
| `--trace` | Record execution trace for mirage (view with `go tool trace`) |
| `--mutex` | Record mutex profile for mirage (view with `go tool pprof`) |
| `-freq <duration>` | System monitoring interval (default: 100ms) |
| `-timeout <duration>` | Max run time (0 = no limit) |
| `--no-color` | Disable colored text output |
| `-h` | Show help |

**Deep / instrumentation** (enabled by `--deep` or individually): `--cgroup`, `--sample`, `--preload`, `--instrument-go`. Optional: `--strace` (heavy slowdown), `--uprobe-symbols`, `--pprof`

## What You Get

**Performance report** (terminal text by default, or markdown with `--format markdown`):

- **Command information** – Command, start/end time, duration, exit code
- **Process metrics** – Peak CPU, RSS, VMS, total read/write I/O, threads, open FDs (target process and children)
- **System resource usage** – Avg/peak CPU and memory, load average
- **Findings** – Structured bottlenecks (high CPU, memory growth, I/O bound, system saturation, optional CPU spikes) with evidence and recommendations
- **Profile files** – Paths to trace/mutex outputs when `--trace` or `--mutex` is used

### Sample Report (text)

```
MIRAGE PERFORMANCE REPORT
  Target Command Analysis

Command Information
  Command:                 python data_processor.py
  Duration:                23.4s
  Exit Code:               0 (Success)

Process Metrics (Target + Children)
  Peak CPU Usage:          86.70%
  Peak RSS Memory:          2.3 GB
  ...

System Resource Usage (Total)
  Avg System CPU:           45.2% (Peak: 89.1%)
  ...

Findings
  [warning] High single-core CPU utilization
    Evidence: 86.70%
    Recommendation: Optimize hot paths or consider parallelization.
```

## Live TUI

With `--ui`, Mirage shows a **live terminal UI** while the command runs instead of only printing at the end.

**What the TUI shows:**

- **Process (target + children)** – CPU %, RSS, VMS, read/write bytes, thread count, open file descriptors (updated every 100ms)
- **System** – System-wide CPU %, memory %, load average (1m)
- **Run finished** – Duration and exit code when the command completes

**Flow:** Start your command with `mirage --ui <command>`. The TUI appears and updates in real time. When the command exits, the TUI shows "Run finished" and "Press q to quit". After you press `q`, the same text report as without `--ui` is printed (Findings, process/system metrics, etc.).

```bash
mirage --ui ./my-app
# ... TUI runs ... command exits ... press q ...
# Report is printed below
```

Ctrl+C during the run cancels the profiled command and exits.

## Report Formats

- **text** (default) – Terminal report with boxed sections and colored output (unless `--no-color`). Use for live viewing or piping.
- **markdown** – Markdown report (headings, lists, no color). Use with `-o report.md` for documentation or version control.

## Analysis and Findings

The **Findings** section is produced by an analysis engine (`internal/analysis`) with configurable thresholds:

- **CPU** – High or very high single-core usage; multi-core usage (info); optional CPU spike detection vs a rolling baseline
- **Memory** – Significant RSS growth (e.g. >20% and >10MB) as a potential leak signal
- **I/O** – Process I/O-bound when CPU is low and I/O is high
- **System** – Load average above CPU core count (saturation)

Defaults match common heuristics; you can tune thresholds (e.g. CPU warning/critical %, memory growth ratio) in code for your environment.

## Trace and Mutex Profiles

These options profile **Mirage itself** (the wrapper process), not the command you run. Useful for debugging monitoring overhead or lock contention inside Mirage.

- **`--trace`** – Writes `profile-dir/trace.out`. Use `go tool trace trace.out` to inspect scheduling and events.
- **`--mutex`** – Writes `profile-dir/mutex.prof`. Use `go tool pprof mutex.prof` (or `-http=:8080`) to inspect mutex contention.

Use a non-temporary `--profile-dir` so the files are kept:

```bash
mirage --trace --profile-dir ./profiles ./my-app
go tool trace ./profiles/trace.out
```

## Advanced Features

- **Live TUI** – Real-time process and system metrics in the terminal (`--ui`)
- **Structured findings** – Configurable analysis for CPU, memory, I/O, and system saturation
- **Go pprof** – CPU, memory, goroutine, and block profiles for the profiled run (when `--cpu`/`--mem` are on)
- **Trace and mutex** – Execution trace and mutex profile for the Mirage process (`--trace`, `--mutex`)
- **Report formats** – Terminal (text) and markdown
- **Process and system metrics** – Per-process (target + children) and system-wide, at configurable `-freq`

### Profile Analysis (pprof)

When CPU and/or memory profiling is enabled, Mirage writes pprof files into `--profile-dir` (or a temp dir, unless you set it). Use the Go tools to inspect them:

```bash
# Keep profiles in a directory
mirage --profile-dir ./profiles ./my-app

# Inspect with pprof
go tool pprof ./profiles/cpu.prof
go tool pprof ./profiles/mem.prof
go tool pprof ./profiles/goroutine.prof
go tool pprof ./profiles/block.prof

# Web UI
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

For `--deep` with preload, build the LD_PRELOAD shim so the binary and shim sit together: `make preload` (or `make -C internal/instrument/preload` then copy `preload_shim.so` to the same directory as the mirage binary, or use `--preload-so` to point to the .so).

## Requirements

- Go 1.21+
- Linux/macOS
