//go:build linux

package sampler

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var syscallNames = map[int]string{
	0:   "read",
	1:   "write",
	2:   "open",
	3:   "close",
	4:   "stat",
	5:   "fstat",
	6:   "lstat",
	9:   "mmap",
	10:  "mprotect",
	11:  "munmap",
	12:  "brk",
	20:  "writev",
	21:  "access",
	59:  "execve",
	63:  "uname",
	79:  "getcwd",
	80:  "chdir",
	78:  "gettid",
	96:  "getpriority",
	97:  "setpriority",
	99:  "statfs",
	102: "getuid",
	104: "getgid",
	122: "uname",
	158: "arch_specific_syscall",
	186: "gettid",
	218: "set_tid_address",
	231: "exit_group",
	257: "openat",
	261: "newfstatat",
	262: "unlinkat",
	263: "renameat",
	264: "linkat",
	265: "symlinkat",
	266: "readlinkat",
	267: "fchmodat",
	268: "faccessat",
	269: "pselect6",
	270: "ppoll",
	271: "unshare",
	272: "set_robust_list",
	273: "get_robust_list",
	274: "splice",
	275: "tee",
	276: "sync_file_range",
	277: "utimensat",
	278: "epoll_pwait",
	279: "signalfd",
	280: "timerfd_create",
	281: "eventfd",
	282: "eventfd2",
	283: "signalfd4",
	284: "eventfd2",
	285: "epoll_create1",
	286: "dup3",
	287: "pipe2",
	288: "inotify_init1",
	289: "preadv",
	290: "pwritev",
	291: "rt_tgsigqueueinfo",
	292: "perf_event_open",
	293: "recvmmsg",
	294: "fanotify_init",
	295: "fanotify_mark",
	296: "prlimit64",
	297: "name_to_handle_at",
	298: "open_by_handle_at",
	299: "clock_adjtime",
	300: "syncfs",
	302: "setns",
	303: "getcpu",
	304: "process_vm_readv",
	305: "process_vm_writev",
	306: "kcmp",
	307: "finit_module",
	308: "sched_setattr",
	309: "sched_getattr",
	310: "renameat2",
	311: "seccomp",
	312: "getrandom",
	313: "memfd_create",
	314: "kexec_file_load",
	315: "bpf",
	316: "execveat",
	317: "userfaultfd",
	318: "membarrier",
	319: "mlock2",
	320: "copy_file_range",
	321: "preadv2",
	322: "pwritev2",
}

func (s *Sampler) collect() {
	procDir := filepath.Join("/proc", strconv.Itoa(s.rootPID), "task")
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		tid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		syscallPath := filepath.Join(procDir, e.Name(), "syscall")
		data, err := os.ReadFile(syscallPath)
		if err != nil {
			continue
		}
		line := strings.TrimSpace(string(data))
		name := "running"
		if line != "running" {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					if n >= 0 {
						if nm, ok := syscallNames[n]; ok {
							name = nm
						} else {
							name = "syscall_" + fields[0]
						}
					} else {
						name = "blocked"
					}
				}
			}
		}
		s.mu.Lock()
		s.samples = append(s.samples, Sample{Timestamp: now, PID: s.rootPID, TID: tid, Syscall: name})
		s.mu.Unlock()
	}
}
